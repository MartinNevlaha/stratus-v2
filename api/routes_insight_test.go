package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func setupTestDB(t *testing.T) *db.DB {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	return database
}

func TestListInsightProposals(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	proposal1 := &db.InsightProposal{
		ID:              "test-1",
		Type:            "routing.change",
		Status:          "detected",
		Title:           "Test Proposal 1",
		Description:     "Test description",
		Confidence:      0.85,
		RiskLevel:       "high",
		SourcePatternID: "pattern-1",
	}
	proposal2 := &db.InsightProposal{
		ID:              "test-2",
		Type:            "workflow.investigate",
		Status:          "drafted",
		Title:           "Test Proposal 2",
		Description:     "Test description 2",
		Confidence:      0.70,
		RiskLevel:       "medium",
		SourcePatternID: "pattern-2",
	}

	if err := database.SaveInsightProposal(proposal1); err != nil {
		t.Fatalf("Failed to save proposal1: %v", err)
	}
	if err := database.SaveInsightProposal(proposal2); err != nil {
		t.Fatalf("Failed to save proposal2: %v", err)
	}

	tests := []struct {
		name          string
		queryParams   string
		expectedCount int
		checkFirstID  string
	}{
		{"all proposals", "", 2, ""},
		{"filter by status", "?status=detected", 1, "test-1"},
		{"filter by type", "?type=routing.change", 1, "test-1"},
		{"filter by risk", "?risk=high", 1, "test-1"},
		{"limit results", "?limit=1", 1, ""},
		{"offset results", "?offset=1", 1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/insight/proposals"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			server.handleGetInsightProposals(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
			}

			var response map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			proposals, ok := response["proposals"].([]interface{})
			if !ok {
				t.Fatal("Expected proposals to be an array")
			}

			if len(proposals) != tt.expectedCount {
				t.Errorf("Expected %d proposals, got %d", tt.expectedCount, len(proposals))
			}

			if tt.checkFirstID != "" && len(proposals) > 0 {
				firstProposal := proposals[0].(map[string]interface{})
				if firstProposal["id"] != tt.checkFirstID {
					t.Errorf("Expected first proposal ID to be %s, got %s", tt.checkFirstID, firstProposal["id"])
				}
			}
		})
	}
}

func TestGetInsightProposalDetail(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	proposal := &db.InsightProposal{
		ID:              "test-detail-1",
		Type:            "routing.change",
		Status:          "approved",
		Title:           "Test Proposal Detail",
		Description:     "Detailed description",
		Confidence:      0.90,
		RiskLevel:       "high",
		SourcePatternID: "pattern-1",
		Evidence:        map[string]interface{}{"key": "value"},
		Recommendation:  map[string]interface{}{"action": "reroute"},
		DecisionReason:  "Strong evidence",
	}

	if err := database.SaveInsightProposal(proposal); err != nil {
		t.Fatalf("Failed to save proposal: %v", err)
	}

	t.Run("existing proposal", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/insight/proposals/test-detail-1", nil)
		req.SetPathValue("id", "test-detail-1")
		w := httptest.NewRecorder()

		server.handleGetInsightProposal(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var response db.InsightProposal
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response.ID != "test-detail-1" {
			t.Errorf("Expected ID test-detail-1, got %s", response.ID)
		}
		if response.DecisionReason != "Strong evidence" {
			t.Errorf("Expected decision reason, got %s", response.DecisionReason)
		}
	})

	t.Run("non-existent proposal", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/insight/proposals/nonexistent", nil)
		req.SetPathValue("id", "nonexistent")
		w := httptest.NewRecorder()

		server.handleGetInsightProposal(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestInsightDashboardSummary(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	proposal := &db.InsightProposal{
		ID:              "dash-1",
		Type:            "routing.change",
		Status:          "approved",
		Title:           "Dashboard Test",
		Description:     "Test",
		Confidence:      0.85,
		RiskLevel:       "high",
		SourcePatternID: "pattern-1",
		Evidence:        map[string]interface{}{"affected_workflow": "spec-complex"},
	}

	if err := database.SaveInsightProposal(proposal); err != nil {
		t.Fatalf("Failed to save proposal: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/insight/dashboard", nil)
	w := httptest.NewRecorder()

	server.handleGetInsightDashboard(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response db.InsightDashboardSummary
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.TimeWindowHours != 24 {
		t.Errorf("Expected time window 24, got %d", response.TimeWindowHours)
	}

	if response.RecentProposals < 1 {
		t.Errorf("Expected at least 1 recent proposal, got %d", response.RecentProposals)
	}
}

func TestUpdateProposalStatus(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	proposal := &db.InsightProposal{
		ID:              "update-1",
		Type:            "routing.change",
		Status:          "detected",
		Title:           "Status Update Test",
		Description:     "Test",
		Confidence:      0.85,
		RiskLevel:       "high",
		SourcePatternID: "pattern-1",
	}

	if err := database.SaveInsightProposal(proposal); err != nil {
		t.Fatalf("Failed to save proposal: %v", err)
	}

	tests := []struct {
		name          string
		proposalID    string
		currentStatus string
		newStatus     string
		reason        string
		expectedCode  int
		shouldSucceed bool
	}{
		{"valid: detected to drafted", "update-1", "detected", "drafted", "Reviewed", http.StatusOK, true},
		{"valid: drafted to approved", "update-1", "drafted", "approved", "Good to go", http.StatusOK, true},
		{"invalid: approved to drafted", "update-1", "approved", "drafted", "", http.StatusBadRequest, false},
		{"invalid: detected to approved", "update-1", "approved", "detected", "", http.StatusBadRequest, false},
		{"non-existent proposal", "nonexistent", "", "drafted", "", http.StatusNotFound, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.currentStatus != "" {
				database.UpdateInsightProposalStatus(tt.proposalID, tt.currentStatus)
			}

			body := map[string]string{
				"status": tt.newStatus,
				"reason": tt.reason,
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest("PATCH", "/api/insight/proposals/"+tt.proposalID+"/status", bytes.NewReader(bodyBytes))
			req.SetPathValue("id", tt.proposalID)
			w := httptest.NewRecorder()

			server.handleUpdateInsightProposalStatus(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d (body: %s)", tt.expectedCode, w.Code, w.Body.String())
			}

			if tt.shouldSucceed {
				var response db.InsightProposal
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if response.Status != tt.newStatus {
					t.Errorf("Expected status %s, got %s", tt.newStatus, response.Status)
				}

				if tt.reason != "" && response.DecisionReason != tt.reason {
					t.Errorf("Expected decision reason '%s', got '%s'", tt.reason, response.DecisionReason)
				}
			}
		})
	}
}

func TestListAgentScorecards(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	card1 := &db.AgentScorecard{
		ID:              "agent-1",
		AgentName:       "claude-3-opus",
		Window:          "7d",
		WindowStart:     "2025-01-01T00:00:00Z",
		WindowEnd:       "2025-01-08T00:00:00Z",
		TotalRuns:       100,
		SuccessRate:     0.95,
		FailureRate:     0.05,
		ReviewPassRate:  0.90,
		ReworkRate:      0.10,
		AvgCycleTimeMs:  5000,
		RegressionRate:  0.02,
		ConfidenceScore: 0.85,
		Trend:           "improving",
	}
	card2 := &db.AgentScorecard{
		ID:              "agent-2",
		AgentName:       "claude-3-haiku",
		Window:          "7d",
		WindowStart:     "2025-01-01T00:00:00Z",
		WindowEnd:       "2025-01-08T00:00:00Z",
		TotalRuns:       50,
		SuccessRate:     0.80,
		FailureRate:     0.20,
		ReviewPassRate:  0.75,
		ReworkRate:      0.15,
		AvgCycleTimeMs:  3000,
		RegressionRate:  0.05,
		ConfidenceScore: 0.70,
		Trend:           "stable",
	}

	if err := database.SaveAgentScorecard(card1); err != nil {
		t.Fatalf("Failed to save card1: %v", err)
	}
	if err := database.SaveAgentScorecard(card2); err != nil {
		t.Fatalf("Failed to save card2: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/insight/scorecards/agents", nil)
	w := httptest.NewRecorder()

	server.handleGetAgentScorecards(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	scorecards, ok := response["scorecards"].([]interface{})
	if !ok {
		t.Fatal("Expected scorecards to be an array")
	}

	if len(scorecards) != 2 {
		t.Errorf("Expected 2 scorecards, got %d", len(scorecards))
	}
}

func TestGetAgentScorecardByName(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	card := &db.AgentScorecard{
		ID:              "agent-test",
		AgentName:       "test-agent",
		Window:          "7d",
		WindowStart:     "2025-01-01T00:00:00Z",
		WindowEnd:       "2025-01-08T00:00:00Z",
		TotalRuns:       10,
		SuccessRate:     0.80,
		FailureRate:     0.20,
		ReviewPassRate:  0.85,
		ConfidenceScore: 0.60,
		Trend:           "stable",
	}

	if err := database.SaveAgentScorecard(card); err != nil {
		t.Fatalf("Failed to save card: %v", err)
	}

	t.Run("existing agent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/insight/scorecards/agents/test-agent", nil)
		req.SetPathValue("name", "test-agent")
		w := httptest.NewRecorder()

		server.handleGetAgentScorecardByName(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var response db.AgentScorecard
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if response.AgentName != "test-agent" {
			t.Errorf("Expected agent name test-agent, got %s", response.AgentName)
		}
	})

	t.Run("non-existent agent", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/insight/scorecards/agents/nonexistent", nil)
		req.SetPathValue("name", "nonexistent")
		w := httptest.NewRecorder()

		server.handleGetAgentScorecardByName(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestListWorkflowScorecards(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	card := &db.WorkflowScorecard{
		ID:                  "wf-1",
		WorkflowType:        "bug",
		Window:              "7d",
		WindowStart:         "2025-01-01T00:00:00Z",
		WindowEnd:           "2025-01-08T00:00:00Z",
		TotalRuns:           30,
		CompletionRate:      0.85,
		FailureRate:         0.15,
		ReviewRejectionRate: 0.10,
		ReworkRate:          0.20,
		AvgDurationMs:       150000,
		ConfidenceScore:     0.70,
		Trend:               "stable",
	}

	if err := database.SaveWorkflowScorecard(card); err != nil {
		t.Fatalf("Failed to save card: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/insight/scorecards/workflows", nil)
	w := httptest.NewRecorder()

	server.handleGetWorkflowScorecards(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	scorecards, ok := response["scorecards"].([]interface{})
	if !ok {
		t.Fatal("Expected scorecards to be an array")
	}

	if len(scorecards) != 1 {
		t.Errorf("Expected 1 scorecard, got %d", len(scorecards))
	}
}

// ---- applyAssetProposal tests ----

func TestApplyAssetProposal_WritesFile(t *testing.T) {
	dir := t.TempDir()
	server := &Server{projectRoot: dir}

	proposal := &db.InsightProposal{
		ID:   "asset-write-1",
		Type: "asset.skill",
		Recommendation: map[string]interface{}{
			"proposed_path":    "skills/new-skill.md",
			"proposed_content": "# New Skill\nContent here.",
		},
	}

	if err := server.applyAssetProposal(proposal); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	dest := filepath.Join(dir, "skills", "new-skill.md")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected file to exist at %s: %v", dest, err)
	}
	if string(data) != "# New Skill\nContent here." {
		t.Errorf("unexpected file content: %s", string(data))
	}
}

func TestApplyAssetProposal_MissingPath_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	server := &Server{projectRoot: dir}

	proposal := &db.InsightProposal{
		ID:   "asset-missing-path",
		Type: "asset.skill",
		Recommendation: map[string]interface{}{
			"proposed_content": "some content",
		},
	}

	err := server.applyAssetProposal(proposal)
	if err == nil {
		t.Fatal("expected error for missing proposed_path, got nil")
	}
}

func TestApplyAssetProposal_MissingContent_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	server := &Server{projectRoot: dir}

	proposal := &db.InsightProposal{
		ID:   "asset-missing-content",
		Type: "asset.skill",
		Recommendation: map[string]interface{}{
			"proposed_path": "skills/empty.md",
		},
	}

	err := server.applyAssetProposal(proposal)
	if err == nil {
		t.Fatal("expected error for missing proposed_content, got nil")
	}
}

func TestApplyAssetProposal_PathTraversal_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	server := &Server{projectRoot: dir}

	proposal := &db.InsightProposal{
		ID:   "asset-traversal",
		Type: "asset.skill",
		Recommendation: map[string]interface{}{
			"proposed_path":    "../../etc/passwd",
			"proposed_content": "malicious content",
		},
	}

	err := server.applyAssetProposal(proposal)
	if err == nil {
		t.Fatal("expected path traversal error, got nil")
	}
}

func TestApplyAssetProposal_FileAlreadyExists_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	server := &Server{projectRoot: dir}

	// Pre-create the target file.
	dest := filepath.Join(dir, "skills", "existing.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(dest, []byte("existing"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	proposal := &db.InsightProposal{
		ID:   "asset-exists",
		Type: "asset.skill",
		Recommendation: map[string]interface{}{
			"proposed_path":    "skills/existing.md",
			"proposed_content": "new content",
		},
	}

	err := server.applyAssetProposal(proposal)
	if err == nil {
		t.Fatal("expected error for existing file, got nil")
	}
}

func TestApplyAssetProposal_CreatesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	server := &Server{projectRoot: dir}

	proposal := &db.InsightProposal{
		ID:   "asset-nested-dirs",
		Type: "asset.agent",
		Recommendation: map[string]interface{}{
			"proposed_path":    "agents/sub/dir/agent.md",
			"proposed_content": "agent content",
		},
	}

	if err := server.applyAssetProposal(proposal); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dest := filepath.Join(dir, "agents", "sub", "dir", "agent.md")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected file at %s: %v", dest, err)
	}
}

// ---- Integration: approval of asset proposal triggers file write ----

func TestHandleUpdateInsightProposalStatus_AssetApproval_WritesFile(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	dir := t.TempDir()
	server := &Server{db: database, projectRoot: dir}

	proposal := &db.InsightProposal{
		ID:     "asset-approval-1",
		Type:   "asset.skill",
		Status: "drafted",
		Title:  "New Skill Proposal",
		Recommendation: map[string]interface{}{
			"proposed_path":    "skills/auto-skill.md",
			"proposed_content": "# Auto Skill",
		},
		Confidence: 0.9,
		RiskLevel:  "low",
	}

	if err := database.SaveInsightProposal(proposal); err != nil {
		t.Fatalf("setup: save proposal: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"status": "approved", "reason": "looks good"})
	req := httptest.NewRequest("PATCH", "/api/insight/proposals/asset-approval-1/status", bytes.NewReader(body))
	req.SetPathValue("id", "asset-approval-1")
	w := httptest.NewRecorder()

	server.handleUpdateInsightProposalStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	dest := filepath.Join(dir, "skills", "auto-skill.md")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected file to be written at %s: %v", dest, err)
	}
	if string(data) != "# Auto Skill" {
		t.Errorf("unexpected content: %s", string(data))
	}
}

func TestHandleUpdateInsightProposalStatus_AssetApproval_RollsBackOnWriteError(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	dir := t.TempDir()
	server := &Server{db: database, projectRoot: dir}

	// Pre-create the file so the write will fail (file already exists guard).
	dest := filepath.Join(dir, "skills", "conflict.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(dest, []byte("pre-existing"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	proposal := &db.InsightProposal{
		ID:     "asset-rollback-1",
		Type:   "asset.skill",
		Status: "drafted",
		Title:  "Conflicting Skill",
		Recommendation: map[string]interface{}{
			"proposed_path":    "skills/conflict.md",
			"proposed_content": "new content",
		},
		Confidence: 0.9,
		RiskLevel:  "low",
	}

	if err := database.SaveInsightProposal(proposal); err != nil {
		t.Fatalf("setup: save proposal: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"status": "approved"})
	req := httptest.NewRequest("PATCH", "/api/insight/proposals/asset-rollback-1/status", bytes.NewReader(body))
	req.SetPathValue("id", "asset-rollback-1")
	w := httptest.NewRecorder()

	server.handleUpdateInsightProposalStatus(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d (body: %s)", w.Code, w.Body.String())
	}

	// Status should have been rolled back to "drafted".
	fetched, err := database.GetInsightProposalByID("asset-rollback-1")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if fetched.Status != "drafted" {
		t.Errorf("expected status rolled back to 'drafted', got %s", fetched.Status)
	}
}

func TestHandleUpdateInsightProposalStatus_NonAssetApproval_NoFileWrite(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	dir := t.TempDir()
	server := &Server{db: database, projectRoot: dir}

	proposal := &db.InsightProposal{
		ID:     "routing-approval-1",
		Type:   "routing.change",
		Status: "drafted",
		Title:  "Routing Change",
		Recommendation: map[string]interface{}{
			"action": "reroute",
		},
		Confidence: 0.85,
		RiskLevel:  "medium",
	}

	if err := database.SaveInsightProposal(proposal); err != nil {
		t.Fatalf("setup: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"status": "approved"})
	req := httptest.NewRequest("PATCH", "/api/insight/proposals/routing-approval-1/status", bytes.NewReader(body))
	req.SetPathValue("id", "routing-approval-1")
	w := httptest.NewRecorder()

	server.handleUpdateInsightProposalStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}

	// No files should have been created in the temp dir (except system dirs).
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files written for non-asset proposal, found: %v", entries)
	}
}

// ---- handleCreateInsightProposal tests ----

func TestCreateProposal_NewTypeIdeaAccepted(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	server := &Server{db: database}

	body, _ := json.Marshal(map[string]any{
		"type":        "idea",
		"title":       "Add retry logic to LLM client",
		"description": "The LLM client should retry transient failures",
		"signal_refs": []string{"internal/insight/llm/client.go"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/insight/proposals", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleCreateInsightProposal(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] == nil || resp["id"] == "" {
		t.Error("expected non-empty id in response")
	}
}

func TestCreateProposal_UnknownTypeReturns422(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	server := &Server{db: database}

	body, _ := json.Marshal(map[string]any{
		"type":  "unknown_custom_type",
		"title": "Some proposal",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/insight/proposals", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleCreateInsightProposal(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errMsg, _ := resp["error"].(string)
	if !strings.Contains(errMsg, "unsupported proposal type") {
		t.Errorf("expected 'unsupported proposal type' in error, got: %s", errMsg)
	}
}

func TestCreateProposal_ClientSuppliedHashReturns400(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	server := &Server{db: database}

	body, _ := json.Marshal(map[string]any{
		"type":             "idea",
		"title":            "Something",
		"idempotency_hash": "abc123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/insight/proposals", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleCreateInsightProposal(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errMsg, _ := resp["error"].(string)
	if !strings.Contains(errMsg, "idempotency_hash is server-computed") {
		t.Errorf("expected server-computed error, got: %s", errMsg)
	}
}

func TestCreateProposal_SignalRefsForwarded(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	server := &Server{db: database}

	signalRefs := []string{"pkg/foo.go", "pkg/bar.go"}
	body, _ := json.Marshal(map[string]any{
		"type":        "refactor_opportunity",
		"title":       "Refactor foo package",
		"description": "Foo package needs cleanup",
		"signal_refs": signalRefs,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/insight/proposals", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleCreateInsightProposal(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	id, _ := resp["id"].(string)
	if id == "" {
		t.Fatal("expected non-empty id in response")
	}

	// Query back to verify signal_refs persisted.
	var storedJSON string
	err := database.SQL().QueryRow(`SELECT signal_refs FROM insight_proposals WHERE id = ?`, id).Scan(&storedJSON)
	if err != nil {
		t.Fatalf("query signal_refs: %v", err)
	}

	var stored []string
	if err := json.Unmarshal([]byte(storedJSON), &stored); err != nil {
		t.Fatalf("unmarshal signal_refs: %v", err)
	}

	// Both refs should be present (sorted order).
	if len(stored) != 2 {
		t.Errorf("expected 2 signal_refs, got %d: %v", len(stored), stored)
	}
}

func TestCreateProposal_WikiPageIDForwarded(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	server := &Server{db: database}

	// First create a wiki page so the FK reference is valid.
	_, err := database.SQL().Exec(`
		INSERT INTO wiki_pages (id, page_type, title, content, status, generated_by, tags_json, metadata_json, created_at, updated_at)
		VALUES ('wiki-test-1', 'idea', 'Test Page', 'content', 'draft', 'test', '[]', '{}', datetime('now'), datetime('now'))
	`)
	if err != nil {
		t.Fatalf("setup wiki page: %v", err)
	}

	wikiID := "wiki-test-1"
	body, _ := json.Marshal(map[string]any{
		"type":         "feature_idea",
		"title":        "Feature with wiki page",
		"description":  "References a wiki page",
		"wiki_page_id": wikiID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/insight/proposals", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleCreateInsightProposal(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	id, _ := resp["id"].(string)
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	var storedWikiID sql.NullString
	err = database.SQL().QueryRow(`SELECT wiki_page_id FROM insight_proposals WHERE id = ?`, id).Scan(&storedWikiID)
	if err != nil {
		t.Fatalf("query wiki_page_id: %v", err)
	}
	if !storedWikiID.Valid || storedWikiID.String != wikiID {
		t.Errorf("expected wiki_page_id %q, got %q (valid=%v)", wikiID, storedWikiID.String, storedWikiID.Valid)
	}
}

func TestCreateProposal_Idempotent_SecondCallReturnsSameID(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	server := &Server{db: database}

	bodyPayload := map[string]any{
		"type":        "test_gap",
		"title":       "Missing tests for orchestration",
		"description": "The orchestration package lacks unit tests",
		"signal_refs": []string{"orchestration/coordinator.go"},
	}
	body1, _ := json.Marshal(bodyPayload)
	body2, _ := json.Marshal(bodyPayload)

	req1 := httptest.NewRequest(http.MethodPost, "/api/insight/proposals", bytes.NewReader(body1))
	w1 := httptest.NewRecorder()
	server.handleCreateInsightProposal(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first call: expected 201, got %d (body: %s)", w1.Code, w1.Body.String())
	}

	var resp1 map[string]any
	json.NewDecoder(w1.Body).Decode(&resp1)
	id1, _ := resp1["id"].(string)

	req2 := httptest.NewRequest(http.MethodPost, "/api/insight/proposals", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	server.handleCreateInsightProposal(w2, req2)
	// Second call may return 200 (idempotent) or 201 — both acceptable; what matters is same ID.
	if w2.Code != http.StatusOK && w2.Code != http.StatusCreated {
		t.Fatalf("second call: expected 200 or 201, got %d (body: %s)", w2.Code, w2.Body.String())
	}

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	id2, _ := resp2["id"].(string)

	if id1 == "" || id2 == "" {
		t.Fatal("expected non-empty ids from both calls")
	}
	if id1 != id2 {
		t.Errorf("expected idempotent: same id on both calls, got %q vs %q", id1, id2)
	}
}
