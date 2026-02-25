package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// semverGT returns true if version a is strictly greater than b.
// Both must be in "X.Y.Z" form; any parse error treats a as not greater.
func semverGT(a, b string) bool {
	parse := func(s string) [3]int {
		parts := strings.SplitN(s, ".", 3)
		var v [3]int
		for i := 0; i < 3 && i < len(parts); i++ {
			v[i], _ = strconv.Atoi(parts[i])
		}
		return v
	}
	av, bv := parse(a), parse(b)
	for i := range av {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return false
}

// VersionResponse is returned by GET /api/system/version.
type VersionResponse struct {
	Current         string   `json:"current"`
	Latest          string   `json:"latest"`
	UpdateAvailable bool     `json:"update_available"`
	ReleaseURL      string   `json:"release_url"`
	ReleaseNotes    string   `json:"release_notes"`
	SyncRequired    bool     `json:"sync_required"`   // binary upgraded but refresh not yet run
	SkippedFiles    []string `json:"skipped_files"`   // assets skipped in last refresh (user-customized)
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
}

// handleVersion returns the current version and checks GitHub for a newer release.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	// Re-read sync state from disk so that changes made by `stratus refresh`
	// (a separate process) are reflected without restarting the server.
	syncedVersion := s.syncedVersion
	skipped := s.skippedFiles
	cfgPath := filepath.Join(s.projectRoot, ".stratus.json")
	if raw, err := os.ReadFile(cfgPath); err == nil {
		var diskCfg struct {
			SyncState *struct {
				SyncedVersion string   `json:"synced_version"`
				SkippedFiles  []string `json:"skipped_files"`
			} `json:"sync_state"`
		}
		if json.Unmarshal(raw, &diskCfg) == nil && diskCfg.SyncState != nil {
			syncedVersion = diskCfg.SyncState.SyncedVersion
			skipped = diskCfg.SyncState.SkippedFiles
		}
	}
	if skipped == nil {
		skipped = []string{}
	}
	resp := VersionResponse{
		Current:      s.version,
		SyncRequired: syncedVersion != "" && syncedVersion != s.version,
		SkippedFiles: skipped,
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet,
		"https://api.github.com/repos/MartinNevlaha/stratus-v2/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")

	ghResp, err := client.Do(req)
	if err == nil {
		defer ghResp.Body.Close()
		body, _ := io.ReadAll(ghResp.Body)
		var release githubRelease
		if json.Unmarshal(body, &release) == nil && release.TagName != "" {
			latest := strings.TrimPrefix(release.TagName, "v")
			resp.Latest = latest
			resp.UpdateAvailable = semverGT(latest, s.version)
			resp.ReleaseURL = release.HTMLURL
			resp.ReleaseNotes = release.Body
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleUpdate triggers an async update and immediately returns 202 Accepted.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]bool{"accepted": true})

	go s.runUpdate()
}

func (s *Server) runUpdate() {
	progress := func(msg string) {
		s.hub.BroadcastJSON("update_progress", map[string]string{"msg": msg})
	}
	fail := func(errMsg string) {
		s.hub.BroadcastJSON("update_failed", map[string]string{"error": errMsg})
	}

	progress("Running go install github.com/MartinNevlaha/stratus-v2/cmd/stratus@latest…")

	cmd := exec.Command("go", "install", "github.com/MartinNevlaha/stratus-v2/cmd/stratus@latest")
	if out, err := cmd.CombinedOutput(); err != nil {
		fail(fmt.Sprintf("go install failed: %v\n%s", err, string(out)))
		return
	}

	progress("Binary installed. Running stratus refresh…")

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}
	newBin := filepath.Join(gopath, "bin", "stratus")

	refresh := exec.Command(newBin, "refresh")
	refresh.Dir = s.projectRoot
	if out, err := refresh.CombinedOutput(); err != nil {
		fail(fmt.Sprintf("refresh failed: %v\n%s", err, string(out)))
		return
	}

	s.hub.BroadcastJSON("update_complete", map[string]string{"msg": "Restart stratus to apply."})
}
