package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

func newConfigTestServer(t *testing.T) *Server {
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

// TestConfigLanguage_GetDefault verifies GET /api/config/language returns default "en".
func TestConfigLanguage_GetDefault(t *testing.T) {
	s := newConfigTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config/language", nil)
	w := httptest.NewRecorder()
	s.handleGetLanguage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["language"] != "en" {
		t.Errorf("expected language %q, got %q", "en", resp["language"])
	}
}

// TestConfigLanguage_PutValid verifies PUT /api/config/language with "sk" returns the new value.
func TestConfigLanguage_PutValid(t *testing.T) {
	s := newConfigTestServer(t)

	body := map[string]string{"language": "sk"}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/config/language", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handlePutLanguage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["language"] != "sk" {
		t.Errorf("expected language %q, got %q", "sk", resp["language"])
	}

	// Verify in-memory config was updated.
	if s.cfg.Language != "sk" {
		t.Errorf("cfg.Language not updated, got %q", s.cfg.Language)
	}
}

// TestConfigLanguage_PutInvalidReturns400 verifies PUT with unsupported language returns 400.
func TestConfigLanguage_PutInvalidReturns400(t *testing.T) {
	s := newConfigTestServer(t)

	cases := []struct {
		name string
		body string
	}{
		{"unsupported language", `{"language":"de"}`},
		{"empty language", `{"language":""}`},
		{"uppercase", `{"language":"EN"}`},
		{"missing key", `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/config/language", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.handlePutLanguage(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
			}

			var resp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if resp["error"] != "invalid language" {
				t.Errorf("expected error %q, got %q", "invalid language", resp["error"])
			}
		})
	}
}
