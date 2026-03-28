package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/agent_evolution"
)

func TestStartAgentExperimentCandidateNotFound(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	baseDir := t.TempDir()
	engine := agent_evolution.NewEngine(
		database,
		agent_evolution.DefaultConfig(),
		filepath.Join(baseDir, ".claude", "agents"),
		filepath.Join(baseDir, ".opencode", "agents"),
		slog.Default(),
	)

	server := &Server{
		db:                   database,
		agentEvolutionEngine: engine,
	}

	req := httptest.NewRequest(http.MethodPost, "/api/insight/agent-evolution/candidates/missing/experiment", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()

	server.handleStartAgentExperiment(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}
