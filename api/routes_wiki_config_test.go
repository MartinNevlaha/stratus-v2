package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

func newWikiConfigTestServer(t *testing.T) *Server {
	t.Helper()
	database := setupTestDB(t)
	t.Cleanup(func() { database.Close() })
	cfg := config.Default()
	return &Server{
		db:          database,
		cfg:         &cfg,
		projectRoot: t.TempDir(),
	}
}

func TestHandleGetWikiConfig_ReturnsCurrentConfig(t *testing.T) {
	s := newWikiConfigTestServer(t)
	s.cfg.Wiki.VaultPath = "/tmp/some-vault"
	s.cfg.Wiki.StalenessThreshold = 0.8

	req := httptest.NewRequest(http.MethodGet, "/api/wiki/config", nil)
	w := httptest.NewRecorder()
	s.handleGetWikiConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["vault_path"] != "/tmp/some-vault" {
		t.Errorf("expected vault_path=/tmp/some-vault, got %v", resp["vault_path"])
	}
	if resp["staleness_threshold"] != 0.8 {
		t.Errorf("expected staleness_threshold=0.8, got %v", resp["staleness_threshold"])
	}
}

func TestHandleUpdateWikiConfig_PersistsAndRebuildsVaultSync(t *testing.T) {
	s := newWikiConfigTestServer(t)
	tmpVault := filepath.Join(s.projectRoot, "vault")

	// Initially no vaultSync.
	if s.getVaultSync() != nil {
		t.Fatal("expected nil vaultSync before update")
	}

	payload := config.WikiConfig{
		Enabled:            true,
		IngestOnEvent:      true,
		MaxPagesPerIngest:  10,
		StalenessThreshold: 0.5,
		MaxPageSizeTokens:  2048,
		VaultPath:          tmpVault,
		VaultSyncOnSave:    false,
		OnboardingDepth:    "standard",
		OnboardingMaxPages: 15,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/wiki/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateWikiConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// vaultSync must be rebuilt (non-nil) and vault dir must exist.
	if s.getVaultSync() == nil {
		t.Error("expected vaultSync to be rebuilt after setting vault_path")
	}
	if info, err := os.Stat(tmpVault); err != nil || !info.IsDir() {
		t.Errorf("expected vault dir to be created at %s, err=%v", tmpVault, err)
	}

	// Config file must be written.
	cfgPath := filepath.Join(s.projectRoot, ".stratus.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("expected .stratus.json to be written, err=%v", err)
	}

	// Clearing vault_path should drop the vaultSync instance.
	payload.VaultPath = ""
	body, _ = json.Marshal(payload)
	req = httptest.NewRequest(http.MethodPut, "/api/wiki/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.handleUpdateWikiConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on clear, got %d: %s", w.Code, w.Body.String())
	}
	if s.getVaultSync() != nil {
		t.Error("expected vaultSync to be nil after clearing vault_path")
	}
}

func TestHandleUpdateWikiConfig_RejectsRelativeVaultPath(t *testing.T) {
	s := newWikiConfigTestServer(t)
	payload := config.WikiConfig{VaultPath: "relative/path"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/wiki/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleUpdateWikiConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for relative vault_path, got %d", w.Code)
	}
}

func TestHandleUpdateWikiConfig_RejectsVaultPathPointingToFile(t *testing.T) {
	s := newWikiConfigTestServer(t)
	filePath := filepath.Join(s.projectRoot, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	payload := config.WikiConfig{VaultPath: filePath}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/wiki/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleUpdateWikiConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when vault_path is a file, got %d", w.Code)
	}
}

func TestHandleUpdateWikiConfig_RejectsOutOfBoundsStaleness(t *testing.T) {
	s := newWikiConfigTestServer(t)
	cases := []float64{-0.1, 1.5}
	for _, v := range cases {
		payload := config.WikiConfig{StalenessThreshold: v, OnboardingDepth: "standard"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPut, "/api/wiki/config", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleUpdateWikiConfig(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for staleness_threshold=%v, got %d", v, w.Code)
		}
	}
}

func TestHandleUpdateWikiConfig_RejectsInvalidOnboardingDepth(t *testing.T) {
	s := newWikiConfigTestServer(t)
	payload := config.WikiConfig{OnboardingDepth: "extreme"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/wiki/config", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleUpdateWikiConfig(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid onboarding_depth, got %d", w.Code)
	}
}
