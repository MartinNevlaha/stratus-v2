package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

func newLLMConfigTestServer(t *testing.T) *Server {
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

// TestGetLLMConfig_MasksAPIKey verifies that the API key is masked in GET response.
func TestGetLLMConfig_MasksAPIKey(t *testing.T) {
	s := newLLMConfigTestServer(t)
	s.cfg.LLM.APIKey = "sk-secret"

	req := httptest.NewRequest(http.MethodGet, "/api/llm/config", nil)
	w := httptest.NewRecorder()
	s.handleGetLLMConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["api_key"] != "***" {
		t.Errorf("expected api_key to be masked as '***', got %v", resp["api_key"])
	}
}

// TestUpdateLLMConfig_RejectsOutOfBoundsTemperature verifies 400 for temperature > 2.
func TestUpdateLLMConfig_RejectsOutOfBoundsTemperature(t *testing.T) {
	s := newLLMConfigTestServer(t)

	body := config.LLMConfig{
		Provider:    "openai",
		Model:       "gpt-4o",
		Temperature: 3.0,
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/llm/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateLLMConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestUpdateLLMConfig_RejectsInvalidProvider verifies 400 for an unrecognised provider.
func TestUpdateLLMConfig_RejectsInvalidProvider(t *testing.T) {
	s := newLLMConfigTestServer(t)

	body := config.LLMConfig{
		Provider: "bogus",
		Model:    "some-model",
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/llm/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateLLMConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestUpdateLLMConfig_AcceptsValidPayload verifies 200 and response has masked api_key.
func TestUpdateLLMConfig_AcceptsValidPayload(t *testing.T) {
	s := newLLMConfigTestServer(t)

	body := config.LLMConfig{
		Provider:    "openai",
		Model:       "gpt-4o",
		APIKey:      "sk-real-key",
		Temperature: 0.5,
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/llm/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateLLMConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["api_key"] != "***" {
		t.Errorf("expected api_key to be masked as '***', got %v", resp["api_key"])
	}
}

// TestUpdateLLMConfig_PreservesStoredKeyOnSentinel verifies stored key is kept when sentinel is sent.
func TestUpdateLLMConfig_PreservesStoredKeyOnSentinel(t *testing.T) {
	s := newLLMConfigTestServer(t)
	s.cfg.LLM.APIKey = "sk-stored"

	body := config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4o",
		APIKey:   "***", // sentinel — should not overwrite stored
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/llm/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateLLMConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	if s.cfg.LLM.APIKey != "sk-stored" {
		t.Errorf("expected stored key to be preserved, got %q", s.cfg.LLM.APIKey)
	}
}

// TestUpdateLLMConfig_EmptyBodyAccepted verifies that a zero-value LLMConfig returns 200.
func TestUpdateLLMConfig_EmptyBodyAccepted(t *testing.T) {
	s := newLLMConfigTestServer(t)

	body := config.LLMConfig{} // fully zero-value
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/llm/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateLLMConfig(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for empty (allowEmpty=true), got %d (body: %s)", w.Code, w.Body.String())
	}
}
