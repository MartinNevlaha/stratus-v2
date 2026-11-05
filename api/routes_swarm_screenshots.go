package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/swarm"
)

const (
	maxScreenshotBytes = 5 << 20
	maxHandoffShots    = 6
)

// screenshotTypes maps an allowed file extension to the content type served for it.
var screenshotTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".webp": "image/webp", ".gif": "image/gif",
}

// Why a screenshot path was refused. The first three are shown to the caller (they only
// depend on the path text or on files inside the project); every other reason is logged and
// answered with screenshotRejectedMsg, so a caller cannot probe the file system.
var (
	errShotEmpty    = errors.New("screenshot path is required")
	errShotExt      = errors.New("screenshot must be a .png, .jpg, .jpeg, .webp or .gif file")
	errShotTooLarge = errors.New("screenshot is larger than 5 MiB")

	errShotMissing    = errors.New("file does not exist")
	errShotOutside    = errors.New("file is outside the project directory")
	errShotNotRegular = errors.New("not a regular file")
	errShotMagic      = errors.New("content does not match the image extension")
	errShotChanged    = errors.New("opened file is not the validated file")
)

const screenshotRejectedMsg = "screenshot path is not an allowed image file"

// screenshotRejection is the message a caller gets for a refused screenshot.
func screenshotRejection(err error) string {
	for _, e := range []error{errShotEmpty, errShotExt, errShotTooLarge} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	return screenshotRejectedMsg
}

// hasImageMagic reports whether head starts with the signature of the image type mime.
func hasImageMagic(mime string, head []byte) bool {
	switch mime {
	case "image/png":
		return bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n"))
	case "image/jpeg":
		return bytes.HasPrefix(head, []byte("\xff\xd8\xff"))
	case "image/gif":
		return bytes.HasPrefix(head, []byte("GIF87a")) || bytes.HasPrefix(head, []byte("GIF89a"))
	case "image/webp":
		return len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP"))
	}
	return false
}

// insideDir reports whether path (already symlink-resolved) is dir itself or below it.
func insideDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// screenshotRoots are the symlink-resolved directories a screenshot may live in. The
// worktrees directory is listed on its own because it may be a symlink to another disk, in
// which case mission worktrees resolve outside the project root yet must stay allowed.
func (s *Server) screenshotRoots() []string {
	var roots []string
	for _, root := range []string{s.projectRoot, filepath.Join(s.projectRoot, ".stratus", "worktrees")} {
		if r, err := filepath.EvalSymlinks(root); err == nil {
			roots = append(roots, r)
		}
	}
	return roots
}

func (s *Server) insideScreenshotRoots(resolved string) bool {
	for _, r := range s.screenshotRoots() {
		if insideDir(r, resolved) {
			return true
		}
	}
	return false
}

// openScreenshot validates p (absolute, or relative to baseDir) and returns it opened, with
// its content type and cleaned absolute path. The file must resolve, through symlinks, to a
// regular file inside the project root or its worktrees directory, carry an image
// extension, be at most maxScreenshotBytes and start with the signature of that image type,
// so the server never serves anything but an image from inside the project. Errors wrap one
// of the errShot* reasons. The caller closes the file.
func (s *Server) openScreenshot(baseDir, p string) (*os.File, string, string, error) {
	if p == "" {
		return nil, "", "", errShotEmpty
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, p)
	}
	p = filepath.Clean(p)
	mime, ok := screenshotTypes[strings.ToLower(filepath.Ext(p))]
	if !ok {
		return nil, "", "", errShotExt
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", "", fmt.Errorf("%w: %s", errShotMissing, p)
		}
		return nil, "", "", fmt.Errorf("resolve screenshot path %s: %w", p, err)
	}
	if !s.insideScreenshotRoots(resolved) {
		return nil, "", "", fmt.Errorf("%w: %s resolves to %s", errShotOutside, p, resolved)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, "", "", fmt.Errorf("open screenshot %s: %w", p, err)
	}
	reject := func(err error) (*os.File, string, string, error) {
		f.Close()
		return nil, "", "", err
	}
	fi, err := f.Stat()
	if err != nil {
		return reject(fmt.Errorf("stat screenshot %s: %w", p, err))
	}
	if !fi.Mode().IsRegular() {
		return reject(fmt.Errorf("%w: %s", errShotNotRegular, p))
	}
	if err := s.checkOpenedScreenshot(f, fi); err != nil {
		return reject(fmt.Errorf("%s: %w", p, err))
	}
	if fi.Size() > maxScreenshotBytes {
		return reject(errShotTooLarge)
	}
	head := make([]byte, 12)
	n, _ := io.ReadFull(f, head)
	if !hasImageMagic(mime, head[:n]) {
		return reject(fmt.Errorf("%w: %s is not %s", errShotMagic, p, mime))
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return reject(fmt.Errorf("rewind screenshot %s: %w", p, err))
	}
	return f, mime, p, nil
}

// checkOpenedScreenshot closes the gap between validating a path and opening it: on Linux
// it reads the open descriptor back through /proc/self/fd, requires that location to be
// inside the allowed roots and the very file that was stat-ed, so a symlink swapped in
// between the checks is caught. Other systems keep the path-based check only.
func (s *Server) checkOpenedScreenshot(f *os.File, fi os.FileInfo) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	target, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))
	if err != nil {
		return fmt.Errorf("%w: read back descriptor: %v", errShotChanged, err)
	}
	if !s.insideScreenshotRoots(target) {
		return fmt.Errorf("%w: opened file is at %s", errShotOutside, target)
	}
	st, err := os.Stat(target)
	if err != nil || !os.SameFile(st, fi) {
		return fmt.Errorf("%w: %s", errShotChanged, target)
	}
	return nil
}

// checkScreenshot validates the path of screenshot evidence for a mission. It returns the
// cleaned absolute path to store and the revision to record by default (the integration
// tree's HEAD, with "+dirty" when it has uncommitted changes, so such evidence is never
// current). Relative paths resolve against the integration worktree, else the project root.
func (s *Server) checkScreenshot(missionID, content string) (abs, revision string, err error) {
	mission, err := s.swarm.GetMission(missionID)
	if err != nil {
		return "", "", fmt.Errorf("load mission %s: %w", missionID, err)
	}
	rev := s.resolveMissionRevision(mission)
	if rev.Err != nil {
		log.Printf("warning: screenshot revision for mission %s: %v", missionID, rev.Err)
	}
	f, _, abs, err := s.openScreenshot(rev.Dir, content)
	if err != nil {
		return "", "", err
	}
	f.Close()
	return abs, rev.recorded(), nil
}

// handleMissionScreenshot serves the image of a screenshot evidence row of the mission,
// validating the stored path again so a file replaced after recording is never served.
func (s *Server) handleMissionScreenshot(w http.ResponseWriter, r *http.Request) {
	if !ciLocalRequest(r) {
		jsonErr(w, http.StatusForbidden, "screenshots can only be read from the local dashboard")
		return
	}
	evidenceID := r.PathValue("evidenceId")
	evidence, err := s.swarm.ListMissionEvidence(r.PathValue("id"))
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list evidence: "+err.Error())
		return
	}
	var shot *db.SwarmEvidence
	for i := range evidence {
		if evidence[i].ID == evidenceID && evidence[i].Type == swarm.EvidenceScreenshot {
			shot = &evidence[i]
			break
		}
	}
	if shot == nil {
		jsonErr(w, http.StatusNotFound, "screenshot not found")
		return
	}
	f, mime, _, err := s.openScreenshot(s.projectRoot, shot.Content)
	if err != nil {
		log.Printf("screenshot %s of mission %s is not servable: %v", shot.ID, shot.MissionID, err)
		if errors.Is(err, errShotMissing) {
			jsonErr(w, http.StatusNotFound, "screenshot file is gone")
		} else {
			jsonErr(w, http.StatusForbidden, screenshotRejection(err))
		}
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "", time.Time{}, f)
}

type handoffScreenshot struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	Current   bool   `json:"current"`
	CreatedAt string `json:"created_at"`
	Agent     string `json:"agent"`
}

// handoffScreenshots lists the newest screenshot evidence first (by created_at, then id),
// capped at maxHandoffShots. A screenshot is current when taken at the current HEAD of a
// clean tree. anyCurrent reports whether any screenshot, shown or not, is current.
func handoffScreenshots(evidence []db.SwarmEvidence, headSHA string, dirty bool) (shots []handoffScreenshot, anyCurrent bool) {
	var all []db.SwarmEvidence
	for _, e := range evidence {
		if e.Type == swarm.EvidenceScreenshot {
			all = append(all, e)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt != all[j].CreatedAt {
			return all[i].CreatedAt > all[j].CreatedAt
		}
		return all[i].ID > all[j].ID
	})
	shots = []handoffScreenshot{}
	for _, e := range all {
		current := !dirty && sameRevision(e.Revision, headSHA)
		anyCurrent = anyCurrent || current
		if len(shots) < maxHandoffShots {
			shots = append(shots, handoffScreenshot{ID: e.ID, Revision: e.Revision, Current: current, CreatedAt: e.CreatedAt, Agent: e.Agent})
		}
	}
	return shots, anyCurrent
}

// isUIFile reports whether a changed path (as git prints it, with slashes) is UI work:
// markup, component and style files, or anything under frontend/ except the lockfile and
// test files.
func isUIFile(f string) bool {
	base := path.Base(f)
	if base == "package-lock.json" || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return false
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".svelte", ".vue", ".jsx", ".tsx", ".html", ".css", ".scss":
		return true
	}
	return strings.HasPrefix(f, "frontend/")
}

// diffTouchesUI reports whether the mission's diff versus its base branch touches UI files.
// A diff that cannot be read counts as not touching the UI.
func diffTouchesUI(rev missionRevision, baseBranch string) bool {
	if baseBranch == "" {
		return false
	}
	out, err := runGit(rev.Dir, "diff", "--name-only", "-z", "--end-of-options", baseBranch+"...HEAD")
	if err != nil {
		return false
	}
	for _, f := range strings.Split(out, "\x00") {
		if isUIFile(f) {
			return true
		}
	}
	return false
}
