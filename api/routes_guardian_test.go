package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

func newGuardianTestServer(t *testing.T) *Server {
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

// TestHandleGetGuardianConfig_MasksAPIKey verifies that the API key is masked in GET response.
func TestHandleGetGuardianConfig_MasksAPIKey(t *testing.T) {
	s := newGuardianTestServer(t)
	s.cfg.Guardian.LLM.APIKey = "real-secret-key"

	req := httptest.NewRequest(http.MethodGet, "/api/guardian/config", nil)
	w := httptest.NewRecorder()
	s.handleGetGuardianConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	llmRaw, ok := resp["llm"]
	if !ok {
		t.Fatal("response missing 'llm' field")
	}
	llm, ok := llmRaw.(map[string]interface{})
	if !ok {
		t.Fatal("'llm' field is not an object")
	}
	if llm["api_key"] != "***" {
		t.Errorf("expected api_key to be masked as '***', got %v", llm["api_key"])
	}
}

// TestHandleUpdateGuardianConfig_InvalidTemperature verifies 400 for out-of-range temperature.
func TestHandleUpdateGuardianConfig_InvalidTemperature(t *testing.T) {
	s := newGuardianTestServer(t)

	body := config.GuardianConfig{
		LLM: config.LLMConfig{
			Provider:    "openai",
			Model:       "gpt-4",
			Temperature: 3.0,
		},
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/guardian/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateGuardianConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestHandleUpdateGuardianConfig_InvalidProvider verifies 400 for unrecognised provider.
func TestHandleUpdateGuardianConfig_InvalidProvider(t *testing.T) {
	s := newGuardianTestServer(t)

	body := config.GuardianConfig{
		LLM: config.LLMConfig{
			Provider: "bogus",
			Model:    "some-model",
		},
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/guardian/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateGuardianConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestHandleUpdateGuardianConfig_ValidPayload verifies 200 and response has masked key.
func TestHandleUpdateGuardianConfig_ValidPayload(t *testing.T) {
	s := newGuardianTestServer(t)

	body := config.GuardianConfig{
		LLM: config.LLMConfig{
			Provider:    "openai",
			Model:       "gpt-4",
			APIKey:      "new-real-key",
			Temperature: 0.7,
		},
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/guardian/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateGuardianConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	llmRaw, ok := resp["llm"]
	if !ok {
		t.Fatal("response missing 'llm' field")
	}
	llm := llmRaw.(map[string]interface{})
	if llm["api_key"] != "***" {
		t.Errorf("expected api_key to be masked as '***', got %v", llm["api_key"])
	}
}

// TestHandleUpdateGuardianConfig_SentinelPreservesStoredKey verifies stored key is kept when sentinel is sent.
func TestHandleUpdateGuardianConfig_SentinelPreservesStoredKey(t *testing.T) {
	s := newGuardianTestServer(t)
	s.cfg.Guardian.LLM.APIKey = "original-stored-key"

	body := config.GuardianConfig{
		LLM: config.LLMConfig{
			Provider: "openai",
			Model:    "gpt-4",
			APIKey:   "***", // sentinel — should not overwrite stored
		},
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/guardian/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateGuardianConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	if s.cfg.Guardian.LLM.APIKey != "original-stored-key" {
		t.Errorf("expected stored key to be preserved, got %q", s.cfg.Guardian.LLM.APIKey)
	}
}

// TestHandleTestGuardianLLM_MissingProvider verifies 400 when resolved config lacks provider.
func TestHandleTestGuardianLLM_MissingProvider(t *testing.T) {
	s := newGuardianTestServer(t)
	// Neither global LLM nor override body has provider/model set.
	s.cfg.LLM = config.LLMConfig{}

	body := struct {
		LLM config.LLMConfig `json:"llm"`
	}{
		LLM: config.LLMConfig{}, // empty override
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/guardian/test-llm", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleTestGuardianLLM(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when provider is missing, got %d (body: %s)", w.Code, w.Body.String())
	}
}
