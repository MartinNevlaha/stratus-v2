package db

import (
	"context"
	"github.com/MartinNevlaha/stratus-v2/integrations"
	"path/filepath"
	"sync"
	"testing"
)

func integrationDB(t *testing.T) *DB {
	t.Helper()
	d, e := Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	_, e = d.SQL().Exec(`INSERT INTO workflows(id,state_json) VALUES('spec-codex','{"runtime":"codex","session_id":"codex:s1","runtime_session_id":"s1"}')`)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func activity(kind, key string) IntegrationActivity {
	return IntegrationActivity{Event: integrations.Event{Kind: kind, NativeSessionID: "s1", NativeAgentID: "a1", AgentType: "worker", ToolName: "Bash", Summary: "pwd"}, EventKey: key, SessionKey: "codex:s1", AgentKey: "codex-agent-a1", WorkflowID: "spec-codex", Phase: "implement"}
}
func TestIntegrationActivity_Lifecycle(t *testing.T) {
	d := integrationDB(t)
	for _, in := range []IntegrationActivity{activity("agent_start", "start"), activity("tool", "tool"), activity("tool", "tool"), activity("agent_stop", "stop"), activity("agent_start", "late")} {
		if _, e := d.ApplyIntegrationActivity(context.Background(), in); e != nil {
			t.Fatal(e)
		}
	}
	a, e := d.GetWorkflowAgent("spec-codex", "codex-agent-a1")
	if e != nil || a.ToolCalls != 1 || a.Status != "done" {
		t.Fatal(a, e)
	}
	steps, e := d.ListIntegrationSteps(context.Background(), "spec-codex", a.ID, 40)
	if e != nil || len(steps) != 4 {
		t.Fatal(len(steps), e)
	}
}
func TestIntegrationActivity_ConcurrentDuplicate(t *testing.T) {
	d := integrationDB(t)
	d.ApplyIntegrationActivity(context.Background(), activity("agent_start", "start"))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := d.ApplyIntegrationActivity(context.Background(), activity("tool", "same")); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	a, _ := d.GetWorkflowAgent("spec-codex", "codex-agent-a1")
	if a.ToolCalls != 1 {
		t.Fatal(a)
	}
}
func TestIntegrationActivity_RollbackReceipt(t *testing.T) {
	d := integrationDB(t)
	d.ApplyIntegrationActivity(context.Background(), activity("agent_start", "start"))
	d.SQL().Exec(`CREATE TRIGGER fail_log BEFORE INSERT ON workflow_logs BEGIN SELECT RAISE(ABORT,'boom'); END`)
	if _, e := d.ApplyIntegrationActivity(context.Background(), activity("tool", "retry")); e == nil {
		t.Fatal("expected failure")
	}
	d.SQL().Exec(`DROP TRIGGER fail_log`)
	r, e := d.ApplyIntegrationActivity(context.Background(), activity("tool", "retry"))
	if e != nil || !r.Recorded || r.Duplicate {
		t.Fatal(r, e)
	}
	a, _ := d.GetWorkflowAgent("spec-codex", "codex-agent-a1")
	if a.ToolCalls != 1 {
		t.Fatal(a)
	}
}
func TestIntegrationActivity_StopBeforeStart(t *testing.T) {
	d := integrationDB(t)
	for _, in := range []IntegrationActivity{activity("agent_stop", "stop"), activity("agent_start", "start"), activity("tool", "late")} {
		if _, e := d.ApplyIntegrationActivity(context.Background(), in); e != nil {
			t.Fatal(e)
		}
	}
	a, _ := d.GetWorkflowAgent("spec-codex", "codex-agent-a1")
	if a.Status != "done" {
		t.Fatal(a)
	}
	d.SQL().Exec(`UPDATE workflows SET state_json='{"runtime":"codex","session_id":"codex:s2"}'`)
	if _, e := d.ApplyIntegrationActivity(context.Background(), activity("tool", "old-owner")); e == nil {
		t.Fatal("stale owner accepted")
	}
}

func TestReview_StopBeforeStartRetainsVerdict(t *testing.T) {
	d := integrationDB(t)
	in := activity("agent_stop", "stop")
	in.Verdict = ReviewVerdict{Verdict: "fail", MustFix: 2}
	in.Event.Report = "Verdict: FAIL\nMust Fix: 2"
	if _, err := d.ApplyIntegrationActivity(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	a, err := d.GetWorkflowAgent("spec-codex", "codex-agent-a1")
	if err != nil || a.Verdict != "fail" || a.MustFix != 2 {
		t.Fatalf("terminal report verdict lost: %+v err=%v", a, err)
	}
}

func TestIntegrationIdentityMigration_PreservesLegacyAgents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.StartWorkflowAgent(WorkflowAgent{ID: "legacy", WorkflowID: "spec-legacy", SessionID: "s1", AgentType: "delivery-backend-engineer"}); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"native_agent_id", "turn_id"} {
		if _, err = d.SQL().Exec("ALTER TABLE workflow_agents DROP COLUMN " + column); err != nil {
			t.Fatal(err)
		}
	}
	d.Close()
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	a, err := d.GetWorkflowAgent("spec-legacy", "legacy")
	if err != nil || a.Status != "running" || a.NativeAgentID != "" || a.TurnID != "" || a.AgentType != "delivery-backend-engineer" {
		t.Fatal(a, err)
	}
}
