package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

// TestHandleGetLLMStatus_NoAPIKeyInResponse verifies the api_key field is never included in the response.
func TestHandleGetLLMStatus_NoAPIKeyInResponse(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	cfg.LLM.Provider = "openai"
	cfg.LLM.Model = "gpt-4"
	cfg.LLM.APIKey = "sk-supersecret"

	server := &Server{db: database, cfg: &cfg}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/status", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if _, hasKey := resp["api_key"]; hasKey {
		t.Error("response must not contain api_key field")
	}
}

// TestHandleGetLLMStatus_Configured verifies that configured is true when provider and model are set.
func TestHandleGetLLMStatus_Configured(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	cfg.LLM.Provider = "openai"
	cfg.LLM.Model = "gpt-4"

	server := &Server{db: database, cfg: &cfg}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/status", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	configured, ok := resp["configured"].(bool)
	if !ok {
		t.Fatal("expected configured to be a bool")
	}
	if !configured {
		t.Error("expected configured to be true when provider and model are set")
	}
}

// TestHandleGetLLMStatus_NotConfigured verifies that configured is false when provider/model are empty.
func TestHandleGetLLMStatus_NotConfigured(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	cfg.LLM.Provider = ""
	cfg.LLM.Model = ""

	server := &Server{db: database, cfg: &cfg}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/status", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	configured, ok := resp["configured"].(bool)
	if !ok {
		t.Fatal("expected configured to be a bool")
	}
	if configured {
		t.Error("expected configured to be false when provider and model are empty")
	}
}

// TestHandleGetLLMStatus_BudgetFields verifies daily_budget, daily_used, daily_remaining are present.
func TestHandleGetLLMStatus_BudgetFields(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	cfg.LLM.Provider = "openai"
	cfg.LLM.Model = "gpt-4"
	cfg.Evolution.DailyTokenBudget = 50000

	server := &Server{db: database, cfg: &cfg}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/status", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	requiredFields := []string{"daily_budget", "daily_used", "daily_remaining", "reset_at"}
	for _, field := range requiredFields {
		if _, ok := resp[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}
}

// TestHandleGetLLMUsage_DefaultDays verifies that the endpoint returns 200 when called without params.
func TestHandleGetLLMUsage_DefaultDays(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/usage", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMUsage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if _, ok := resp["usage"]; !ok {
		t.Error("expected usage key in response")
	}
	if _, ok := resp["total_tokens"]; !ok {
		t.Error("expected total_tokens key in response")
	}
}

// TestHandleGetLLMUsage_ValidDays verifies that days=30 returns 200.
func TestHandleGetLLMUsage_ValidDays(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/usage?days=30", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMUsage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

// TestHandleGetLLMUsage_InvalidDays verifies that days=0 returns 400.
func TestHandleGetLLMUsage_InvalidDays(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/usage?days=0", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestHandleGetLLMUsage_DaysOutOfRange verifies that days=100 returns 400.
func TestHandleGetLLMUsage_DaysOutOfRange(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/usage?days=100", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestHandleGetLLMUsage_DaysNotANumber verifies that days=abc returns 400.
func TestHandleGetLLMUsage_DaysNotANumber(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/usage?days=abc", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMUsage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestHandleGetLLMUsage_TotalTokensCalculation verifies total_tokens is summed correctly.
func TestHandleGetLLMUsage_TotalTokensCalculation(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Pre-seed usage data.
	_ = database.RecordTokenUsage("2026-04-09", "wiki_engine", 100, 50)
	_ = database.RecordTokenUsage("2026-04-09", "guardian", 200, 100)

	server := &Server{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/llm/usage?days=7", nil)
	w := httptest.NewRecorder()

	server.handleGetLLMUsage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	totalTokens, ok := resp["total_tokens"].(float64)
	if !ok {
		t.Fatalf("total_tokens not a number: %v", resp["total_tokens"])
	}
	if totalTokens != 450 { // 100+50 + 200+100
		t.Errorf("total_tokens = %v, want 450", totalTokens)
	}
}

// TestHandleTestLLM_NotConfigured verifies 400 is returned when LLM provider/model are not set.
func TestHandleTestLLM_NotConfigured(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	cfg.LLM.Provider = ""
	cfg.LLM.Model = ""

	server := &Server{db: database, cfg: &cfg}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/test", nil)
	w := httptest.NewRecorder()

	server.handleTestLLM(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

// TestHandleTestLLM_ConfiguredButUnreachable verifies 503 when the LLM endpoint is unreachable.
func TestHandleTestLLM_ConfiguredButUnreachable(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	cfg.LLM.Provider = "ollama" // ollama doesn't need API key
	cfg.LLM.Model = "llama3.1"
	cfg.LLM.BaseURL = "http://127.0.0.1:0" // unreachable
	cfg.LLM.Timeout = 1                    // 1 second timeout

	server := &Server{db: database, cfg: &cfg}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/test", nil)
	w := httptest.NewRecorder()

	server.handleTestLLM(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d (body: %s)", w.Code, w.Body.String())
	}
}
