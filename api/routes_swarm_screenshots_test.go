package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// tinyPNG is a PNG signature followed by filler; the server only checks the signature.
var tinyPNG = append([]byte("\x89PNG\r\n\x1a\n"), []byte("0000000000000000")...)

const shotsRel = ".stratus/screenshots"

func shotWriteBytes(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// shotFixture is handoffFixture with `.stratus/` ignored in the integration worktree, so
// recording a screenshot saved there does not itself dirty the tree.
func shotFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	s, root, wt := handoffFixture(t)
	gateWrite(t, wt, ".gitignore", ".stratus/\n")
	gateGit(t, wt, "add", ".")
	gateGit(t, wt, "commit", "-q", "-m", "ignore .stratus")
	return s, root, wt
}

// shotCommit commits an empty-ish file at rel (parent dirs created) in dir.
func shotCommit(t *testing.T, dir, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	gateCommit(t, dir, rel)
}

func recordShotReq(t *testing.T, s *Server, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"type": "screenshot", "content": path, "agent": "qa", "verdict": "info"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://localhost:41777/api/swarm/tickets/t1/evidence", strings.NewReader(string(body)))
	req.SetPathValue("id", "t1")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.handleRecordEvidence(w, req)
	return w
}

func recordShot(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	return recordShotReq(t, s, path, nil)
}

func recordShotOK(t *testing.T, s *Server, path string) db.SwarmEvidence {
	t.Helper()
	w := recordShot(t, s, path)
	if w.Code != http.StatusOK {
		t.Fatalf("record screenshot %s: status %d: %s", path, w.Code, w.Body.String())
	}
	var e db.SwarmEvidence
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func getShot(s *Server, missionID, evidenceID string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "http://localhost:41777/api/swarm/missions/"+missionID+"/screenshots/"+evidenceID, nil)
	req.SetPathValue("id", missionID)
	req.SetPathValue("evidenceId", evidenceID)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.handleMissionScreenshot(w, req)
	return w
}

func TestRecordScreenshot_ValidPNGDefaultsRevisionAndStoresAbsolutePath(t *testing.T) {
	s, _, wt := shotFixture(t)
	p := shotWriteBytes(t, wt, shotsRel+"/home.png", tinyPNG)

	e := recordShotOK(t, s, p)
	if want := gateHead(t, wt); e.Revision != want {
		t.Errorf("revision = %q, want clean integration HEAD %q", e.Revision, want)
	}
	if e.Content != p {
		t.Errorf("content = %q, want %q", e.Content, p)
	}

	// A relative path resolves against the integration worktree.
	if e2 := recordShotOK(t, s, shotsRel+"/home.png"); e2.Content != p {
		t.Errorf("relative content = %q, want %q", e2.Content, p)
	}
}

func TestRecordScreenshot_DirtyTreeStoresDirtyRevision(t *testing.T) {
	s, _, wt := shotFixture(t)
	p := shotWriteBytes(t, wt, shotsRel+"/home.png", tinyPNG)
	gateWrite(t, wt, "scratch.txt", "uncommitted\n")

	e := recordShotOK(t, s, p)
	if want := gateHead(t, wt) + "+dirty"; e.Revision != want {
		t.Errorf("revision = %q, want %q", e.Revision, want)
	}
}

func TestRecordScreenshot_Rejections(t *testing.T) {
	s, _, wt := shotFixture(t)
	outside := t.TempDir()
	outPNG := shotWriteBytes(t, outside, "out.png", tinyPNG)
	link := filepath.Join(wt, "passwd.png")
	if err := os.Symlink("/etc/passwd", link); err != nil {
		t.Fatal(err)
	}
	fake := shotWriteBytes(t, wt, "fake.png", []byte("<html>not an image at all</html>"))
	big := shotWriteBytes(t, wt, "big.png", append(append([]byte{}, tinyPNG...), make([]byte, maxScreenshotBytes)...))
	txt := shotWriteBytes(t, wt, "shot.txt", tinyPNG)
	rel, err := filepath.Rel(wt, outPNG)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"outside project": outPNG,
		"symlink to /etc": link,
		"non-image bytes": fake,
		"oversize":        big,
		"bad extension":   txt,
		"missing":         filepath.Join(wt, "nope.png"),
		"empty":           "",
		"dotdot":          rel,
		"dotdot absolute": wt + string(filepath.Separator) + rel,
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			w := recordShot(t, s, p)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "screenshot") || strings.Contains(w.Body.String(), "invalid evidence type") {
				t.Errorf("error should name the screenshot problem: %s", w.Body.String())
			}
		})
	}
	if ev := gateEvidence(t, s); len(ev) != 0 {
		t.Errorf("nothing may be recorded for rejected screenshots, got %+v", ev)
	}
}

// A caller must not learn whether a path exists, lies outside the project or is not an image.
func TestRecordScreenshot_FailureModesShareOneMessage(t *testing.T) {
	s, _, wt := shotFixture(t)
	outPNG := shotWriteBytes(t, t.TempDir(), "secret-name.png", tinyPNG)
	wrong := shotWriteBytes(t, wt, "wrong.png", []byte("\xff\xd8\xff\xe0 jpeg bytes here"))
	dir := filepath.Join(wt, "dir.png")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	var first string
	for name, p := range map[string]string{
		"missing inside":  filepath.Join(wt, "nope.png"),
		"missing outside": "/nonexistent-dir/secret-name.png",
		"outside exists":  outPNG,
		"wrong magic":     wrong,
		"not a file":      dir,
	} {
		w := recordShot(t, s, p)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", name, w.Code)
		}
		if strings.Contains(w.Body.String(), "secret-name") || strings.Contains(w.Body.String(), p) && p != "" {
			t.Errorf("%s: message echoes the path: %s", name, w.Body.String())
		}
		if first == "" {
			first = w.Body.String()
		} else if w.Body.String() != first {
			t.Errorf("%s: message %q differs from %q", name, w.Body.String(), first)
		}
	}
}

func TestRecordScreenshot_CrossOriginIs403AndRecordsNothing(t *testing.T) {
	s, _, wt := shotFixture(t)
	p := shotWriteBytes(t, wt, shotsRel+"/home.png", tinyPNG)

	for name, hdr := range map[string]map[string]string{
		"cross origin": {"Origin": "https://evil.example"},
		"cross site":   {"Sec-Fetch-Site": "cross-site"},
	} {
		if w := recordShotReq(t, s, p, hdr); w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403: %s", name, w.Code, w.Body.String())
		}
	}
	if ev := gateEvidence(t, s); len(ev) != 0 {
		t.Errorf("cross-site request must record nothing, got %+v", ev)
	}
	// Same-origin is fine, and other evidence types are not subject to the guard.
	if w := recordShotReq(t, s, p, map[string]string{"Origin": "http://localhost:41777"}); w.Code != http.StatusOK {
		t.Errorf("same-origin status = %d", w.Code)
	}
}

func TestOpenScreenshot_WhichRuleFires(t *testing.T) {
	s, root, wt := shotFixture(t)
	outDir := t.TempDir()
	outPNG := shotWriteBytes(t, outDir, "out.png", tinyPNG)
	inPNG := shotWriteBytes(t, wt, shotsRel+"/in.png", tinyPNG)

	sibling := root + "-evil"
	shotWriteBytes(t, sibling, "x.png", tinyPNG)
	t.Cleanup(func() { os.RemoveAll(sibling) })

	rel, err := filepath.Rel(wt, outPNG)
	if err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(wt, "linkdir")
	if err := os.Symlink(outDir, linkDir); err != nil {
		t.Fatal(err)
	}
	passwd := filepath.Join(wt, "passwd.png")
	if err := os.Symlink("/etc/passwd", passwd); err != nil {
		t.Fatal(err)
	}
	inLink := filepath.Join(wt, "alias.png")
	if err := os.Symlink(inPNG, inLink); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(wt, "dir.png")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jpg := shotWriteBytes(t, wt, "a.jpg", []byte("\xff\xd8\xff\xe0 jpeg"))
	jpeg := shotWriteBytes(t, wt, "a.JPEG", []byte("\xff\xd8\xff\xdb jpeg"))
	gif := shotWriteBytes(t, wt, "a.gif", []byte("GIF89a......"))
	gif87 := shotWriteBytes(t, wt, "b.gif", []byte("GIF87a......"))
	webp := shotWriteBytes(t, wt, "a.webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "))
	badWebp := shotWriteBytes(t, wt, "b.webp", []byte("RIFF\x00\x00\x00\x00WAVEfmt "))
	wrong := shotWriteBytes(t, wt, "wrong.png", []byte("\xff\xd8\xff\xe0 jpeg"))
	big := shotWriteBytes(t, wt, "big.png", append(append([]byte{}, tinyPNG...), make([]byte, maxScreenshotBytes)...))
	txt := shotWriteBytes(t, wt, "a.txt", tinyPNG)

	cases := []struct {
		name string
		path string
		want error // nil: accepted
	}{
		{"png inside", inPNG, nil},
		{"jpg", jpg, nil},
		{"jpeg uppercase ext", jpeg, nil},
		{"gif89", gif, nil},
		{"gif87", gif87, nil},
		{"webp", webp, nil},
		{"in-project symlink to in-project image", inLink, nil},
		{"relative inside", shotsRel + "/in.png", nil},
		{"outside", outPNG, errShotOutside},
		{"dotdot relative to an existing outside image", rel, errShotOutside},
		{"dotdot absolute to an existing outside image", wt + string(filepath.Separator) + rel, errShotOutside},
		{"symlinked directory component", filepath.Join(linkDir, "out.png"), errShotOutside},
		{"symlink to /etc/passwd", passwd, errShotOutside},
		{"sibling directory sharing the root prefix", filepath.Join(sibling, "x.png"), errShotOutside},
		{"missing inside", filepath.Join(wt, "nope.png"), errShotMissing},
		{"missing outside", "/nonexistent-dir/x.png", errShotMissing},
		{"directory", dir, errShotNotRegular},
		{"jpeg bytes named png", wrong, errShotMagic},
		{"riff but not webp", badWebp, errShotMagic},
		{"oversize", big, errShotTooLarge},
		{"bad extension", txt, errShotExt},
		{"empty path", "", errShotEmpty},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, mime, abs, err := s.openScreenshot(wt, c.path)
			if f != nil {
				f.Close()
			}
			if c.want == nil {
				if err != nil || mime == "" || abs == "" {
					t.Fatalf("want accepted, got mime=%q abs=%q err=%v", mime, abs, err)
				}
				return
			}
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want rule %v", err, c.want)
			}
			if f != nil {
				t.Errorf("a rejected screenshot must not return an open file")
			}
		})
	}
}

func TestCheckOpenedScreenshot_ReadsTheOpenFileBack(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the read-back uses /proc/self/fd")
	}
	s, _, wt := shotFixture(t)
	outDir := t.TempDir()

	open := func(p string) (*os.File, os.FileInfo) {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		fi, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		return f, fi
	}

	inside := shotWriteBytes(t, wt, shotsRel+"/in.png", tinyPNG)
	if f, fi := open(inside); s.checkOpenedScreenshot(f, fi) != nil {
		t.Errorf("a file inside the project must pass the read-back")
	}

	f, fi := open(shotWriteBytes(t, outDir, "out.png", tinyPNG))
	if err := s.checkOpenedScreenshot(f, fi); !errors.Is(err, errShotOutside) {
		t.Errorf("file outside the roots: err = %v, want errShotOutside", err)
	}

	// The file that was validated is moved out of the project after being opened.
	moving := shotWriteBytes(t, wt, shotsRel+"/moving.png", tinyPNG)
	f, fi = open(moving)
	if err := os.Rename(moving, filepath.Join(outDir, "moved.png")); err != nil {
		t.Skipf("cannot move across filesystems: %v", err)
	}
	if err := s.checkOpenedScreenshot(f, fi); !errors.Is(err, errShotOutside) {
		t.Errorf("file moved out after open: err = %v, want errShotOutside", err)
	}
}

func TestMissionScreenshot_ServesBytesWithSafeHeaders(t *testing.T) {
	s, _, wt := shotFixture(t)
	p := shotWriteBytes(t, wt, shotsRel+"/shot.PNG", tinyPNG)
	e := recordShotOK(t, s, p)

	w := getShot(s, "m1", e.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if string(w.Body.Bytes()) != string(tinyPNG) {
		t.Errorf("body differs from the stored image")
	}
	for k, want := range map[string]string{
		"Content-Type":            "image/png",
		"X-Content-Type-Options":  "nosniff",
		"Content-Disposition":     "inline",
		"Cache-Control":           "no-store",
		"Content-Security-Policy": "default-src 'none'; sandbox",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	// Same-origin Origin and Sec-Fetch-Site are fine.
	if w := getShot(s, "m1", e.ID, map[string]string{"Origin": "http://localhost:41777", "Sec-Fetch-Site": "same-origin"}); w.Code != http.StatusOK {
		t.Errorf("same-origin read = %d", w.Code)
	}

	// Range requests are honoured.
	rw := getShot(s, "m1", e.ID, map[string]string{"Range": "bytes=0-3"})
	if rw.Code != http.StatusPartialContent || rw.Body.Len() != 4 {
		t.Errorf("range read = %d with %d bytes, want 206 with 4", rw.Code, rw.Body.Len())
	}
}

func TestMissionScreenshot_RefusesCrossSiteReads(t *testing.T) {
	s, _, wt := shotFixture(t)
	e := recordShotOK(t, s, shotWriteBytes(t, wt, shotsRel+"/shot.png", tinyPNG))

	for name, hdr := range map[string]map[string]string{
		"cross origin":    {"Origin": "https://evil.example"},
		"cross site":      {"Sec-Fetch-Site": "cross-site"},
		"garbage origin":  {"Origin": "://bad"},
		"other port host": {"Origin": "http://localhost:9999"},
	} {
		if w := getShot(s, "m1", e.ID, hdr); w.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", name, w.Code)
		}
	}
}

func TestMissionScreenshot_NotFoundCases(t *testing.T) {
	s, _, wt := shotFixture(t)
	e := recordShotOK(t, s, shotWriteBytes(t, wt, shotsRel+"/shot.png", tinyPNG))
	note := postEvidence(t, s, "t1", `{"type":"note","content":"x","agent":"a","verdict":"info"}`)
	var n db.SwarmEvidence
	if err := json.Unmarshal(note.Body.Bytes(), &n); err != nil {
		t.Fatal(err)
	}
	m1, err := s.swarm.GetMission("m1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.CreateMission("m2", m1.WorkflowID, "other", "main", "merge2", ""); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}

	for name, c := range map[string][2]string{
		"other mission": {"m2", e.ID},
		"unknown id":    {"m1", "doesnotexist"},
		"not a shot":    {"m1", n.ID},
		"odd id":        {"m1", "../x"},
	} {
		if w := getShot(s, c[0], c[1], nil); w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, w.Code)
		}
	}
}

func TestMissionScreenshot_RevalidatesAtServeTime(t *testing.T) {
	s, _, wt := shotFixture(t)
	p := shotWriteBytes(t, wt, shotsRel+"/shot.png", tinyPNG)
	e := recordShotOK(t, s, p)

	if err := os.WriteFile(p, []byte("secret text, not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := getShot(s, "m1", e.ID, nil); w.Code == http.StatusOK || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("replaced file must not be served, got %d %q", w.Code, w.Body.String())
	}

	// Replaced by a symlink to a file outside the project.
	outside := shotWriteBytes(t, t.TempDir(), "o.png", tinyPNG)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, p); err != nil {
		t.Fatal(err)
	}
	if w := getShot(s, "m1", e.ID, nil); w.Code == http.StatusOK {
		t.Fatalf("symlink escape must not be served, got %d", w.Code)
	}

	// Removed file.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if w := getShot(s, "m1", e.ID, nil); w.Code != http.StatusNotFound {
		t.Errorf("missing file status = %d, want 404", w.Code)
	}
}

type shotItem struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	Current   bool   `json:"current"`
	CreatedAt string `json:"created_at"`
	Agent     string `json:"agent"`
}

func handoffShots(t *testing.T, s *Server) ([]shotItem, []string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/swarm/missions/m1/handoff", nil)
	req.SetPathValue("id", "m1")
	w := httptest.NewRecorder()
	s.handleMissionHandoff(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handoff status = %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Screenshots []shotItem `json:"screenshots"`
		Warnings    []string   `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Screenshots == nil {
		t.Errorf("screenshots must be an array even when empty:\n%s", w.Body.String())
	}
	return out.Screenshots, out.Warnings
}

func TestHandoff_ScreenshotsCurrentOnlyOnCleanHead(t *testing.T) {
	s, _, wt := shotFixture(t)
	shotCommit(t, wt, "a.txt")
	p := shotWriteBytes(t, wt, shotsRel+"/a.png", tinyPNG)
	old := recordShotOK(t, s, p)

	shots, _ := handoffShots(t, s)
	if len(shots) != 1 || !shots[0].Current || shots[0].ID != old.ID || shots[0].Agent != "qa" || shots[0].Revision != old.Revision {
		t.Fatalf("clean tree at the recorded HEAD must be current: %+v", shots)
	}

	// Uncommitted changes since: the screenshot no longer shows the tree as it is.
	gateWrite(t, wt, "scratch.txt", "uncommitted\n")
	if shots, _ = handoffShots(t, s); len(shots) != 1 || shots[0].Current {
		t.Fatalf("dirty tree: screenshot must not be current: %+v", shots)
	}
	if err := os.Remove(filepath.Join(wt, "scratch.txt")); err != nil {
		t.Fatal(err)
	}

	shotCommit(t, wt, "b.txt")
	if shots, _ = handoffShots(t, s); len(shots) != 1 || shots[0].Current {
		t.Fatalf("screenshot of an older revision must be outdated: %+v", shots)
	}

	for i := 0; i < 7; i++ {
		recordShotOK(t, s, p)
	}
	shots, _ = handoffShots(t, s)
	if len(shots) != 6 {
		t.Fatalf("cap = %d, want 6", len(shots))
	}
	for _, sh := range shots {
		if !sh.Current {
			t.Errorf("screenshots recorded at the clean HEAD must be current: %+v", sh)
		}
	}
}

func TestHandoff_ScreenshotRecordedOnDirtyTreeIsNeverCurrent(t *testing.T) {
	s, _, wt := shotFixture(t)
	shotCommit(t, wt, "a.txt")
	p := shotWriteBytes(t, wt, shotsRel+"/a.png", tinyPNG)
	gateWrite(t, wt, "scratch.txt", "uncommitted\n")
	recordShotOK(t, s, p)
	if err := os.Remove(filepath.Join(wt, "scratch.txt")); err != nil {
		t.Fatal(err)
	}
	if shots, _ := handoffShots(t, s); len(shots) != 1 || shots[0].Current {
		t.Fatalf("a screenshot taken on a dirty tree must not become current: %+v", shots)
	}
}

func TestHandoffScreenshots_NewestFirstByCreatedAtThenID(t *testing.T) {
	ev := []db.SwarmEvidence{
		{ID: "a", Type: "screenshot", CreatedAt: "2026-01-01T00:00:00.001Z", Revision: "abcdef1"},
		{ID: "c", Type: "screenshot", CreatedAt: "2026-01-01T00:00:00.002Z", Revision: "abcdef1"},
		{ID: "n", Type: "note", CreatedAt: "2026-01-01T00:00:00.009Z"},
		{ID: "b", Type: "screenshot", CreatedAt: "2026-01-01T00:00:00.002Z", Revision: "abcdef1"},
	}
	shots, any := handoffScreenshots(ev, "abcdef1234", false)
	var ids []string
	for _, s := range shots {
		ids = append(ids, s.ID)
	}
	if got := strings.Join(ids, ","); got != "c,b,a" || !any {
		t.Errorf("order = %s any=%v, want c,b,a true", got, any)
	}
	if shots, any = handoffScreenshots(ev, "abcdef1234", true); any || shots[0].Current {
		t.Errorf("a dirty tree makes nothing current: %+v", shots)
	}
}

func TestHandoff_WarnsOnUIChangesWithoutCurrentScreenshot(t *testing.T) {
	const warn = "UI changes without a current screenshot"

	// Backend-only change: no warning.
	s, _, wt := shotFixture(t)
	shotCommit(t, wt, "server.go")
	if _, ws := handoffShots(t, s); hasWarning(ws, warn) {
		t.Errorf("non-UI change must not warn: %v", ws)
	}

	// UI change, no screenshot: warns.
	s, _, wt = shotFixture(t)
	shotCommit(t, wt, "frontend/src/App.svelte")
	if _, ws := handoffShots(t, s); !hasWarning(ws, warn) {
		t.Errorf("expected UI warning: %v", ws)
	}

	// A current screenshot silences it; a newer commit brings it back.
	p := shotWriteBytes(t, wt, shotsRel+"/a.png", tinyPNG)
	recordShotOK(t, s, p)
	if _, ws := handoffShots(t, s); hasWarning(ws, warn) {
		t.Errorf("current screenshot should clear the warning: %v", ws)
	}
	shotCommit(t, wt, "style.css")
	if _, ws := handoffShots(t, s); !hasWarning(ws, warn) {
		t.Errorf("outdated screenshot should warn again: %v", ws)
	}
}

func TestHandoff_UIWarningFileRules(t *testing.T) {
	const warn = "UI changes without a current screenshot"
	for name, c := range map[string]struct {
		file string
		want bool
	}{
		"vue component":          {"web/Item.vue", true},
		"jsx":                    {"web/Item.jsx", true},
		"tsx":                    {"web/Item.tsx", true},
		"html":                   {"web/index.html", true},
		"scss":                   {"web/site.scss", true},
		"non-ASCII css path":     {"styles/ľščť.css", true},
		"frontend ts source":     {"frontend/src/lib/api.ts", true},
		"frontend lockfile":      {"frontend/package-lock.json", false},
		"frontend unit test":     {"frontend/src/lib/api.test.ts", false},
		"frontend spec":          {"frontend/src/lib/api.spec.ts", false},
		"backend go":             {"api/server.go", false},
		"markdown outside ui":    {"docs/readme.md", false},
		"frontendish dir prefix": {"frontend-tools/x.go", false},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, wt := shotFixture(t)
			shotCommit(t, wt, c.file)
			if _, ws := handoffShots(t, s); hasWarning(ws, warn) != c.want {
				t.Errorf("%s: warning = %v, want %v (%v)", c.file, !c.want, c.want, ws)
			}
		})
	}
}
