package guardian

import (
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

func TestCheckStaleWorkflowAgents_FlagsOnlyIdleRunningAgents(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()
	_ = database.StartWorkflowAgent(db.WorkflowAgent{ID: "idle", WorkflowID: "wf-1", AgentType: "delivery-backend-engineer"})
	_ = database.StartWorkflowAgent(db.WorkflowAgent{ID: "busy", WorkflowID: "wf-1", AgentType: "delivery-qa-engineer"})
	old := time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	if _, err := database.SQL().Exec(`UPDATE workflow_agents SET last_activity = ? WHERE id = 'idle'`, old); err != nil {
		t.Fatal(err)
	}

	alerts := checkStaleWorkflowAgents(database, config.GuardianConfig{}, "en")
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(alerts))
	}
	a := alerts[0]
	if a.Type != "stale_agent" || a.Metadata["agent_id"] != "idle" || a.Metadata["workflow_id"] != "wf-1" {
		t.Errorf("alert = %+v", a)
	}
	if a.Metadata["dedup_key"] != "stale_agent_idle" {
		t.Errorf("dedup_key = %v", a.Metadata["dedup_key"])
	}
}

func TestCheckTokenBudget_AlertsAtEightyPercentOnlyWhenEnabled(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()
	coord := orchestrationFor(t, database, "wf-budget")
	_ = database.StartWorkflowAgent(db.WorkflowAgent{ID: "a", WorkflowID: "wf-budget", AgentType: "x"})
	_ = database.SetWorkflowAgentUsage("a", 700, 150) // 850 of 1000

	if got := checkTokenBudget(coord, database, config.GuardianConfig{}, "en"); len(got) != 0 {
		t.Fatalf("budget disabled but got %d alerts", len(got))
	}
	got := checkTokenBudget(coord, database, config.GuardianConfig{WorkflowTokenBudget: 1000}, "en")
	if len(got) != 1 || got[0].Type != "token_budget" || got[0].Severity != "warning" || got[0].Metadata["workflow_id"] != "wf-budget" {
		t.Fatalf("alerts = %+v", got)
	}
	_ = database.SetWorkflowAgentUsage("a", 900, 200) // 1100 of 1000
	got = checkTokenBudget(coord, database, config.GuardianConfig{WorkflowTokenBudget: 1000}, "en")
	if len(got) != 1 || got[0].Severity != "critical" {
		t.Fatalf("over budget alerts = %+v", got)
	}
	_ = database.SetWorkflowAgentUsage("a", 10, 10)
	if got := checkTokenBudget(coord, database, config.GuardianConfig{WorkflowTokenBudget: 1000}, "en"); len(got) != 0 {
		t.Fatalf("under 80%% should not alert, got %+v", got)
	}
}
