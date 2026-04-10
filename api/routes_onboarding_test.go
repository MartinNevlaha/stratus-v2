package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/onboarding"
)

// newOnboardingServer creates a minimal Server for onboarding handler tests.
// It wires up a real in-memory DB and a default config with the given projectRoot.
func newOnboardingServer(t *testing.T, projectRoot string) *Server {
	t.Helper()
	database := setupTestDB(t)
	t.Cleanup(func() { database.Close() })
	cfg := config.Default()
	return &Server{
		db:          database,
		cfg:         &cfg,
		projectRoot: projectRoot,
	}
}

// TestHandleOnboard_InvalidDepth verifies that an unknown depth value returns 400.
func TestHandleOnboard_InvalidDepth(t *testing.T) {
	s := newOnboardingServer(t, t.TempDir())

	body := bytes.NewBufferString(`{"depth":"invalid"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboard", body)
	w := httptest.NewRecorder()

	s.handleOnboard(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Error("expected 'error' field in response")
	}
}

// TestHandleOnboard_MaxPagesBounds verifies that max_pages outside [0, 50] returns 400.
func TestHandleOnboard_MaxPagesBounds(t *testing.T) {
	tests := []struct {
		name     string
		maxPages int
	}{
		{"negative max_pages", -1},
		{"max_pages above limit", 51},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newOnboardingServer(t, t.TempDir())

			body, _ := json.Marshal(map[string]any{"max_pages": tt.maxPages})
			req := httptest.NewRequest(http.MethodPost, "/api/onboard", bytes.NewReader(body))
			w := httptest.NewRecorder()

			s.handleOnboard(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for max_pages=%d, got %d (body: %s)", tt.maxPages, w.Code, w.Body.String())
			}
			var resp map[string]any
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if _, ok := resp["error"]; !ok {
				t.Error("expected 'error' field in response")
			}
		})
	}
}

// TestHandleOnboard_PathTraversal verifies that an output_dir escaping the project root returns 400.
func TestHandleOnboard_PathTraversal(t *testing.T) {
	projectRoot := t.TempDir()
	s := newOnboardingServer(t, projectRoot)
	// Set provider so we don't short-circuit before path validation.
	s.cfg.LLM.Provider = ""

	body, _ := json.Marshal(map[string]any{
		"depth":      "standard",
		"output_dir": "../../../etc",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/onboard", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleOnboard(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal output_dir, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Error("expected 'error' field in response")
	}
}

// TestHandleOnboard_InvalidJSON verifies that malformed request body returns 400.
func TestHandleOnboard_InvalidJSON(t *testing.T) {
	s := newOnboardingServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/onboard", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()

	s.handleOnboard(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// TestHandleOnboard_NoLLMConfig verifies that missing LLM provider returns 503.
func TestHandleOnboard_NoLLMConfig(t *testing.T) {
	s := newOnboardingServer(t, t.TempDir())
	s.cfg.LLM.Provider = "" // no LLM configured

	body := bytes.NewBufferString(`{"depth":"standard"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboard", body)
	w := httptest.NewRecorder()

	s.handleOnboard(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when LLM not configured, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestHandleOnboard_AcceptsValidRequest verifies that a valid request with LLM configured
// is accepted (202) and a job_id is returned.
func TestHandleOnboard_AcceptsValidRequest(t *testing.T) {
	s := newOnboardingServer(t, t.TempDir())
	s.cfg.LLM.Provider = "openai" // configure provider so we get past that check

	body := bytes.NewBufferString(`{"depth":"shallow","max_pages":5}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboard", body)
	w := httptest.NewRecorder()

	s.handleOnboard(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["job_id"] == "" || resp["job_id"] == nil {
		t.Error("expected non-empty job_id in response")
	}
	if resp["status"] != "scanning" {
		t.Errorf("expected status 'scanning', got %v", resp["status"])
	}
}

// TestHandleOnboard_AlreadyRunning verifies that starting onboarding while one is in progress returns 409.
func TestHandleOnboard_AlreadyRunning(t *testing.T) {
	s := newOnboardingServer(t, t.TempDir())
	s.cfg.LLM.Provider = "openai"

	// Inject an in-progress state.
	s.onboardingProgress = &onboarding.OnboardingProgress{
		JobID:  "existing-job",
		Status: "generating",
	}

	body := bytes.NewBufferString(`{"depth":"standard"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/onboard", body)
	w := httptest.NewRecorder()

	s.handleOnboard(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 when already in progress, got %d (body: %s)", w.Code, w.Body.String())
	}
}

// TestHandleOnboardStatus_Idle verifies that GET /api/onboard/status returns idle when
// no onboarding has been started.
func TestHandleOnboardStatus_Idle(t *testing.T) {
	s := newOnboardingServer(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/onboard/status", nil)
	w := httptest.NewRecorder()

	s.handleOnboardStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "idle" {
		t.Errorf("expected status 'idle', got %v", resp["status"])
	}
	if resp["job_id"] != "" && resp["job_id"] != nil {
		// job_id should be empty string for idle
		if resp["job_id"] != "" {
			t.Errorf("expected empty job_id for idle status, got %v", resp["job_id"])
		}
	}
}

// TestHandleOnboardStatus_WithProgress verifies that an in-progress job is reflected correctly.
func TestHandleOnboardStatus_WithProgress(t *testing.T) {
	s := newOnboardingServer(t, t.TempDir())
	s.onboardingProgress = &onboarding.OnboardingProgress{
		JobID:       "job-123",
		Status:      "generating",
		CurrentPage: "Architecture Overview",
		Generated:   3,
		Total:       10,
		Errors:      []string{},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/onboard/status", nil)
	w := httptest.NewRecorder()

	s.handleOnboardStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "generating" {
		t.Errorf("expected status 'generating', got %v", resp["status"])
	}
	if resp["job_id"] != "job-123" {
		t.Errorf("expected job_id 'job-123', got %v", resp["job_id"])
	}
	if resp["current_page"] != "Architecture Overview" {
		t.Errorf("expected current_page 'Architecture Overview', got %v", resp["current_page"])
	}
	generated, ok := resp["generated"].(float64)
	if !ok {
		t.Fatal("expected 'generated' to be a number")
	}
	if int(generated) != 3 {
		t.Errorf("expected generated=3, got %v", generated)
	}
}

// TestHandleOnboardStatus_Complete verifies that completed jobs include the result.
func TestHandleOnboardStatus_Complete(t *testing.T) {
	s := newOnboardingServer(t, t.TempDir())
	s.onboardingProgress = &onboarding.OnboardingProgress{
		JobID:     "job-done",
		Status:    "complete",
		Generated: 5,
		Total:     5,
	}
	s.onboardingResult = &onboarding.OnboardingResult{
		PagesGenerated: 5,
		PagesFailed:    0,
		LinksCreated:   3,
		VaultSynced:    false,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/onboard/status", nil)
	w := httptest.NewRecorder()

	s.handleOnboardStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "complete" {
		t.Errorf("expected status 'complete', got %v", resp["status"])
	}
	if resp["result"] == nil {
		t.Error("expected non-nil result for completed job")
	}
}
