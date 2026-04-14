package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

func newInsightTestServer(t *testing.T) *Server {
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

// TestHandleGetInsightConfig_MasksAPIKey verifies that the API key is masked in GET response.
func TestHandleGetInsightConfig_MasksAPIKey(t *testing.T) {
	s := newInsightTestServer(t)
	s.cfg.Insight.LLM.APIKey = "real-insight-key"

	req := httptest.NewRequest(http.MethodGet, "/api/insight/config", nil)
	w := httptest.NewRecorder()
	s.handleGetInsightConfig(w, req)

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

// TestHandleUpdateInsightConfig_InvalidTemperature verifies 400 for out-of-range temperature.
func TestHandleUpdateInsightConfig_InvalidTemperature(t *testing.T) {
	s := newInsightTestServer(t)

	body := config.InsightConfig{
		LLM: config.LLMConfig{
			Provider:    "openai",
			Model:       "gpt-4",
			Temperature: 3.0,
		},
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/insight/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateInsightConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestHandleUpdateInsightConfig_InvalidProvider verifies 400 for unrecognised provider.
func TestHandleUpdateInsightConfig_InvalidProvider(t *testing.T) {
	s := newInsightTestServer(t)

	body := config.InsightConfig{
		LLM: config.LLMConfig{
			Provider: "bogus",
			Model:    "some-model",
		},
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/insight/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateInsightConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestHandleUpdateInsightConfig_ValidPayload verifies 200 and response has masked key.
func TestHandleUpdateInsightConfig_ValidPayload(t *testing.T) {
	s := newInsightTestServer(t)

	body := config.InsightConfig{
		LLM: config.LLMConfig{
			Provider:    "openai",
			Model:       "gpt-4",
			APIKey:      "insight-real-key",
			Temperature: 0.5,
		},
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/insight/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateInsightConfig(w, req)

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

// TestHandleUpdateInsightConfig_SentinelPreservesStoredKey verifies stored key is kept when sentinel is sent.
func TestHandleUpdateInsightConfig_SentinelPreservesStoredKey(t *testing.T) {
	s := newInsightTestServer(t)
	s.cfg.Insight.LLM.APIKey = "stored-insight-key"

	body := config.InsightConfig{
		LLM: config.LLMConfig{
			Provider: "openai",
			Model:    "gpt-4",
			APIKey:   "***", // sentinel
		},
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/insight/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateInsightConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	if s.cfg.Insight.LLM.APIKey != "stored-insight-key" {
		t.Errorf("expected stored key to be preserved, got %q", s.cfg.Insight.LLM.APIKey)
	}
}

// TestHandleUpdateInsightConfig_EmptyAPIKeyPreservesStoredKey verifies stored key is kept when empty string is sent.
func TestHandleUpdateInsightConfig_EmptyAPIKeyPreservesStoredKey(t *testing.T) {
	s := newInsightTestServer(t)
	s.cfg.Insight.LLM.APIKey = "stored-insight-key"

	body := config.InsightConfig{
		LLM: config.LLMConfig{
			Provider: "openai",
			Model:    "gpt-4",
			APIKey:   "", // empty
		},
	}
	payload, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPut, "/api/insight/config", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleUpdateInsightConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	if s.cfg.Insight.LLM.APIKey != "stored-insight-key" {
		t.Errorf("expected stored key preserved from empty, got %q", s.cfg.Insight.LLM.APIKey)
	}
}
