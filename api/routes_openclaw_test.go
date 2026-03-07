package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestListOpenClawProposals(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	proposal1 := &db.OpenClawProposal{
		ID:              "test-1",
		Type:            "routing.change",
		Status:          "detected",
		Title:           "Test Proposal 1",
		Description:     "Test description",
		Confidence:      0.85,
		RiskLevel:       "high",
		SourcePatternID: "pattern-1",
	}
	proposal2 := &db.OpenClawProposal{
		ID:              "test-2",
		Type:            "workflow.investigate",
		Status:          "drafted",
		Title:           "Test Proposal 2",
		Description:     "Test description 2",
		Confidence:      0.70,
		RiskLevel:       "medium",
		SourcePatternID: "pattern-2",
	}

	if err := database.SaveOpenClawProposal(proposal1); err != nil {
		t.Fatalf("Failed to save proposal1: %v", err)
	}
	if err := database.SaveOpenClawProposal(proposal2); err != nil {
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
			req := httptest.NewRequest("GET", "/api/openclaw/proposals"+tt.queryParams, nil)
			w := httptest.NewRecorder()

			server.handleGetOpenClawProposals(w, req)

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

func TestGetOpenClawProposalDetail(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	proposal := &db.OpenClawProposal{
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

	if err := database.SaveOpenClawProposal(proposal); err != nil {
		t.Fatalf("Failed to save proposal: %v", err)
	}

	t.Run("existing proposal", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/openclaw/proposals/test-detail-1", nil)
		req.SetPathValue("id", "test-detail-1")
		w := httptest.NewRecorder()

		server.handleGetOpenClawProposal(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var response db.OpenClawProposal
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
		req := httptest.NewRequest("GET", "/api/openclaw/proposals/nonexistent", nil)
		req.SetPathValue("id", "nonexistent")
		w := httptest.NewRecorder()

		server.handleGetOpenClawProposal(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestOpenClawDashboardSummary(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	proposal := &db.OpenClawProposal{
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

	if err := database.SaveOpenClawProposal(proposal); err != nil {
		t.Fatalf("Failed to save proposal: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/openclaw/dashboard", nil)
	w := httptest.NewRecorder()

	server.handleGetOpenClawDashboard(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response db.OpenClawDashboardSummary
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

	proposal := &db.OpenClawProposal{
		ID:              "update-1",
		Type:            "routing.change",
		Status:          "detected",
		Title:           "Status Update Test",
		Description:     "Test",
		Confidence:      0.85,
		RiskLevel:       "high",
		SourcePatternID: "pattern-1",
	}

	if err := database.SaveOpenClawProposal(proposal); err != nil {
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
				database.UpdateOpenClawProposalStatus(tt.proposalID, tt.currentStatus)
			}

			body := map[string]string{
				"status": tt.newStatus,
				"reason": tt.reason,
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest("PATCH", "/api/openclaw/proposals/"+tt.proposalID+"/status", bytes.NewReader(bodyBytes))
			req.SetPathValue("id", tt.proposalID)
			w := httptest.NewRecorder()

			server.handleUpdateOpenClawProposalStatus(w, req)

			if w.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d (body: %s)", tt.expectedCode, w.Code, w.Body.String())
			}

			if tt.shouldSucceed {
				var response db.OpenClawProposal
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

	req := httptest.NewRequest("GET", "/api/openclaw/scorecards/agents", nil)
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
		req := httptest.NewRequest("GET", "/api/openclaw/scorecards/agents/test-agent", nil)
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
		req := httptest.NewRequest("GET", "/api/openclaw/scorecards/agents/nonexistent", nil)
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

	req := httptest.NewRequest("GET", "/api/openclaw/scorecards/workflows", nil)
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
