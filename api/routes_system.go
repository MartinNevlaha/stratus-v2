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
	"syscall"
	"time"
)

// semverGT returns true if version a is strictly greater than b.
// Strips pre-release/build suffixes (e.g. "1.2.3-rc1", "1.2.3+build.1") before comparing.
// Any component that fails to parse is treated as 0.
func semverGT(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	// Drop everything after the first '-' or '+' (pre-release / build metadata).
	stripMeta := func(s string) string {
		if i := strings.IndexAny(s, "-+"); i != -1 {
			return s[:i]
		}
		return s
	}
	parse := func(s string) [3]int {
		parts := strings.SplitN(stripMeta(s), ".", 3)
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
	if err == nil && ghResp.StatusCode == http.StatusOK {
		defer ghResp.Body.Close()
		// Cap body at 100 KB — release notes can be arbitrarily large.
		body, _ := io.ReadAll(io.LimitReader(ghResp.Body, 100*1024))
		var release githubRelease
		if json.Unmarshal(body, &release) == nil && release.TagName != "" {
			latest := strings.TrimPrefix(release.TagName, "v")
			resp.Latest = latest
			resp.UpdateAvailable = semverGT(latest, s.version)
			resp.ReleaseURL = release.HTMLURL
			// Truncate notes sent to the client so the JSON payload stays small.
			notes := release.Body
			if len(notes) > 4096 {
				notes = notes[:4096] + "…"
			}
			resp.ReleaseNotes = notes
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleUpdate triggers an async update and immediately returns 202 Accepted.
// Returns 409 Conflict if an update is already running.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.updateMu.TryLock() {
		jsonErr(w, http.StatusConflict, "update already in progress")
		return
	}
	// updateMu is released inside runUpdate (via defer) once the goroutine finishes.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]bool{"accepted": true})

	go s.runUpdate()
}

func (s *Server) runUpdate() {
	defer s.updateMu.Unlock()

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

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}
	newBin := filepath.Join(gopath, "bin", "stratus")

	// Verify the binary actually exists before proceeding — go install can
	// succeed exit-0 while still failing to write the binary (permissions, etc.).
	if _, err := os.Stat(newBin); err != nil {
		fail(fmt.Sprintf("new binary not found at %s: %v", newBin, err))
		return
	}

	progress("Binary installed. Running stratus refresh…")

	refresh := exec.Command(newBin, "refresh")
	refresh.Dir = s.projectRoot
	if out, err := refresh.CombinedOutput(); err != nil {
		fail(fmt.Sprintf("refresh failed: %v\n%s", err, string(out)))
		return
	}

	s.hub.BroadcastJSON("update_complete", map[string]string{"msg": "Restarting server with new binary…"})
	// Give the WebSocket hub a moment to flush the message before replacing the process.
	time.Sleep(500 * time.Millisecond)

	// Replace this process with the new binary (same args, e.g. "stratus serve").
	// syscall.Exec is a clean exec-replace: no gap in PID, SQLite WAL handles the
	// brief file-descriptor handoff. The dashboard WebSocket will reconnect automatically.
	if err := syscall.Exec(newBin, os.Args, os.Environ()); err != nil {
		fail(fmt.Sprintf("restart failed: %v — restart manually with: stratus serve", err))
	}
}
