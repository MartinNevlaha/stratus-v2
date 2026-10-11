package db

import (
	"reflect"
	"testing"
	"time"
)

func TestWorkflowAgent_StartAndList(t *testing.T) {
	d := openTestDB(t)
	task := 2
	if err := d.StartWorkflowAgent(WorkflowAgent{
		ID: "ag-1", WorkflowID: "wf-1", SessionID: "s1", AgentType: "delivery-backend-engineer",
		Phase: "implement", TaskIndex: &task, TranscriptPath: "/tmp/t.jsonl",
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	list, err := d.ListWorkflowAgents("wf-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	a := list[0]
	if a.Status != "running" || a.AgentType != "delivery-backend-engineer" || a.Phase != "implement" {
		t.Errorf("unexpected agent: %+v", a)
	}
	if a.TaskIndex == nil || *a.TaskIndex != 2 {
		t.Errorf("task index = %v, want 2", a.TaskIndex)
	}
	if a.StartedAt == "" || a.LastActivity == "" {
		t.Errorf("timestamps not set: %+v", a)
	}
}

func TestWorkflowAgent_StartTwiceKeepsOneRow(t *testing.T) {
	d := openTestDB(t)
	for i := 0; i < 2; i++ {
		if err := d.StartWorkflowAgent(WorkflowAgent{ID: "ag-1", WorkflowID: "wf-1", AgentType: "x"}); err != nil {
			t.Fatalf("start: %v", err)
		}
	}
	list, _ := d.ListWorkflowAgents("wf-1")
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
}

func TestWorkflowAgent_TouchCountsToolsAndMergesFiles(t *testing.T) {
	d := openTestDB(t)
	_ = d.StartWorkflowAgent(WorkflowAgent{ID: "ag-1", WorkflowID: "wf-1", AgentType: "x"})
	if err := d.TouchWorkflowAgent("ag-1", "Edit", []string{"a.go", "b.go"}); err != nil {
		t.Fatalf("touch: %v", err)
	}
	if err := d.TouchWorkflowAgent("ag-1", "Write", []string{"b.go", "c.go"}); err != nil {
		t.Fatalf("touch: %v", err)
	}
	list, _ := d.ListWorkflowAgents("wf-1")
	a := list[0]
	if a.ToolCalls != 2 || a.LastTool != "Write" {
		t.Errorf("tool_calls=%d last_tool=%q", a.ToolCalls, a.LastTool)
	}
	if want := []string{"a.go", "b.go", "c.go"}; !reflect.DeepEqual(a.Files, want) {
		t.Errorf("files = %v, want %v", a.Files, want)
	}
}

func TestWorkflowAgent_TouchUnknownAgentIsNoop(t *testing.T) {
	d := openTestDB(t)
	if err := d.TouchWorkflowAgent("missing", "Edit", nil); err != nil {
		t.Fatalf("touch unknown: %v", err)
	}
}

func TestWorkflowAgent_StopSetsVerdictAndEnd(t *testing.T) {
	d := openTestDB(t)
	_ = d.StartWorkflowAgent(WorkflowAgent{ID: "ag-1", WorkflowID: "wf-1", AgentType: "delivery-code-reviewer"})
	if err := d.StopWorkflowAgent("ag-1", "done", ReviewVerdict{Verdict: "FAIL", MustFix: 2, ShouldFix: 1}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	a, _ := d.ListWorkflowAgents("wf-1")
	if a[0].Status != "done" || a[0].EndedAt == "" {
		t.Errorf("not stopped: %+v", a[0])
	}
	if a[0].Verdict != "FAIL" || a[0].MustFix != 2 || a[0].ShouldFix != 1 {
		t.Errorf("verdict not stored: %+v", a[0])
	}
}

func TestWorkflowAgent_ListStaleRunning(t *testing.T) {
	d := openTestDB(t)
	_ = d.StartWorkflowAgent(WorkflowAgent{ID: "old", WorkflowID: "wf-1", AgentType: "x"})
	_ = d.StartWorkflowAgent(WorkflowAgent{ID: "fresh", WorkflowID: "wf-1", AgentType: "x"})
	old := time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	if _, err := d.SQL().Exec(`UPDATE workflow_agents SET last_activity = ? WHERE id = 'old'`, old); err != nil {
		t.Fatal(err)
	}
	stale, err := d.ListStaleWorkflowAgents(10 * time.Minute)
	if err != nil {
		t.Fatalf("stale: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != "old" {
		t.Fatalf("stale = %+v, want only old", stale)
	}
}

func TestWorkflowLog_SaveAndGet(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.SaveWorkflowLog("wf-1", "s1", "Edit", "a.go"); err != nil {
		t.Fatalf("save: %v", err)
	}
	logs, err := d.GetWorkflowLogs("wf-1", 10)
	if err != nil || len(logs) != 1 || logs[0].ToolName != "Edit" {
		t.Fatalf("logs = %+v err=%v", logs, err)
	}
}

// A DB created by an earlier build has workflow_agents without the token columns;
// reopening it must add them instead of failing every agent query.
func TestWorkflowAgent_MigratesTableWithoutTokenColumns(t *testing.T) {
	path := t.TempDir() + "/old.db"
	d, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, stmt := range []string{
		`DROP TABLE workflow_agents`,
		`CREATE TABLE workflow_agents (id TEXT PRIMARY KEY, workflow_id TEXT NOT NULL, session_id TEXT NOT NULL DEFAULT '',
		 agent_type TEXT NOT NULL DEFAULT '', phase TEXT NOT NULL DEFAULT '', task_index INTEGER, status TEXT NOT NULL DEFAULT 'running',
		 started_at TEXT NOT NULL, ended_at TEXT NOT NULL DEFAULT '', last_activity TEXT NOT NULL, tool_calls INTEGER NOT NULL DEFAULT 0,
		 last_tool TEXT NOT NULL DEFAULT '', files TEXT NOT NULL DEFAULT '[]', transcript_path TEXT NOT NULL DEFAULT '',
		 verdict TEXT NOT NULL DEFAULT '', must_fix INTEGER NOT NULL DEFAULT 0, should_fix INTEGER NOT NULL DEFAULT 0, suggestion INTEGER NOT NULL DEFAULT 0)`,
	} {
		if _, err := d.SQL().Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	d.Close()

	d2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer d2.Close()
	if err := d2.StartWorkflowAgent(WorkflowAgent{ID: "a", WorkflowID: "w", AgentType: "x"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := d2.SetWorkflowAgentUsage("a", 5, 2); err != nil {
		t.Fatalf("usage: %v", err)
	}
	list, err := d2.ListWorkflowAgents("w")
	if err != nil || len(list) != 1 || list[0].TokensIn != 5 || list[0].TokensOut != 2 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
}
