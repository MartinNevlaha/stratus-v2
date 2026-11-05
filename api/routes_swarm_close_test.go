package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/swarm"
)

func TestBuildWorkerInstructions_HasCurlFallbackAndWorkerMarker(t *testing.T) {
	w := &db.SwarmWorker{ID: "worker-1", MissionID: "m-1", WorktreePath: "/wt", BranchName: "b"}
	got := buildWorkerInstructions(w, "http://localhost:4242")
	for _, want := range []string{
		"SWARM_WORKER_ID: worker-1",
		"curl",
		"http://localhost:4242/api/swarm/tickets/<TICKET_ID>/status",
		"http://localhost:4242/api/swarm/signals",
		"http://localhost:4242/api/swarm/workers/worker-1/heartbeat",
		"swarm_ticket_update", // the MCP path stays documented as the first choice
	} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions missing %q", want)
		}
	}
}

// swarmCloseFixture builds a mission with one worker and the given tickets (all assigned
// to the worker) under the active workflow of agentTestServer.
func swarmCloseFixture(t *testing.T, s *Server, workflowID string) {
	t.Helper()
	s.swarm = swarm.NewStore(s.db, t.TempDir())
	if err := s.db.CreateMission("m1", workflowID, "mission", "main", "merge", ""); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}
	if err := s.db.CreateWorker("w1", "m1", "delivery-backend-engineer", "/wt", "branch"); err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	for _, id := range []string{"t-open", "t-active", "t-failed", "t-done"} {
		if err := s.db.CreateTicket(id, "m1", id, "", "backend", 1, "[]", "[]"); err != nil {
			t.Fatalf("CreateTicket: %v", err)
		}
		if err := s.db.AssignTicket(id, "w1"); err != nil {
			t.Fatalf("AssignTicket: %v", err)
		}
	}
	_ = s.swarm.UpdateTicketStatus("t-active", "in_progress", "")
	_ = s.swarm.UpdateTicketStatus("t-failed", "in_progress", "")
	_ = s.swarm.UpdateTicketStatus("t-failed", "failed", "build broke")
	_ = s.swarm.UpdateTicketStatus("t-done", "in_progress", "")
	_ = s.swarm.UpdateTicketStatus("t-done", "done", "done by agent")
}

func workerTranscript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agent.jsonl")
	line := `{"type":"user","message":{"role":"user","content":"` + body + `"}}` + "\n"
	if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func checkTicket(t *testing.T, s *Server, ticket, status, resultContains string) {
	t.Helper()
	got, err := s.db.GetTicket(ticket)
	if err != nil {
		t.Fatalf("GetTicket %s: %v", ticket, err)
	}
	if got.Status != status || !strings.Contains(got.Result, resultContains) {
		t.Errorf("%s = %s %q, want %s containing %q", ticket, got.Status, got.Result, status, resultContains)
	}
}

func stopWorkerAgent(t *testing.T, s *Server) {
	t.Helper()
	tr := workerTranscript(t, "## Swarm Worker Protocol\\nSWARM_WORKER_ID: w1\\nWork in /wt")
	w := postAgentEvent(t, s, `{"event":"stop","session_id":"sess-1","agent_id":"ag","agent_type":"delivery-backend-engineer","agent_transcript_path":"`+tr+`","last_assistant_message":"Patched the parser, CPU tests pass."}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
}

func TestAgentStop_ClosesOpenTicketsOfIdentifiedWorker(t *testing.T) {
	s, id := agentTestServer(t)
	swarmCloseFixture(t, s, id)
	for _, ticket := range []string{"t-open", "t-active"} {
		if _, err := s.swarm.RecordEvidence(ticket, "m1", swarm.EvidenceTestResult, "go test ok", "w1", "pass"); err != nil {
			t.Fatalf("RecordEvidence: %v", err)
		}
	}

	stopWorkerAgent(t, s)

	checkTicket(t, s, "t-open", "done", "auto-closed")
	checkTicket(t, s, "t-active", "done", "Patched the parser")
	checkTicket(t, s, "t-failed", "failed", "build broke") // an explicit failure is never overwritten
	checkTicket(t, s, "t-done", "done", "done by agent")   // an explicit report is never overwritten

	worker, _ := s.db.GetWorker("w1")
	if worker.Status != "done" {
		t.Errorf("worker status = %q, want done", worker.Status)
	}
}

func TestAgentStop_WithoutEvidenceLeavesTicketsInProgress(t *testing.T) {
	s, id := agentTestServer(t)
	swarmCloseFixture(t, s, id)

	stopWorkerAgent(t, s)

	checkTicket(t, s, "t-open", "in_progress", "[agent stopped without evidence] Patched the parser")
	checkTicket(t, s, "t-active", "in_progress", "[agent stopped without evidence] Patched the parser")
	checkTicket(t, s, "t-failed", "failed", "build broke")
	checkTicket(t, s, "t-done", "done", "done by agent")

	ev, err := s.swarm.ListTicketEvidence("t-open")
	if err != nil || len(ev) != 1 || ev[0].Type != swarm.EvidenceNote {
		t.Errorf("t-open evidence = %+v, %v; want one note", ev, err)
	}
	worker, _ := s.db.GetWorker("w1")
	if worker.Status != "done" {
		t.Errorf("worker status = %q, want done even without ticket evidence", worker.Status)
	}
}

func TestAgentStop_RejectedOrNonQualifyingEvidenceDoesNotCloseTicket(t *testing.T) {
	s, id := agentTestServer(t)
	swarmCloseFixture(t, s, id)
	// A failed test run, and a note (not verifying evidence type) with a pass verdict.
	_, _ = s.swarm.RecordEvidence("t-open", "m1", swarm.EvidenceTestResult, "FAIL", "w1", "fail")
	_, _ = s.swarm.RecordEvidence("t-open", "m1", swarm.EvidenceNote, "looks fine", "w1", "pass")
	_, _ = s.swarm.RecordEvidence("t-active", "m1", swarm.EvidenceReview, "nope", "rev", "rejected")

	stopWorkerAgent(t, s)

	checkTicket(t, s, "t-open", "in_progress", "[agent stopped without evidence]")
	checkTicket(t, s, "t-active", "in_progress", "[agent stopped without evidence]")
}

// recordSpaced records evidence with a pause so created_at orders the rows as recorded.
func recordSpaced(t *testing.T, s *Server, ticket, typ, verdict string) {
	t.Helper()
	time.Sleep(5 * time.Millisecond)
	if _, err := s.swarm.RecordEvidence(ticket, "m1", typ, "content", "w1", verdict); err != nil {
		t.Fatalf("RecordEvidence: %v", err)
	}
}

func TestAgentStop_LatestVerifyingEvidenceDecides(t *testing.T) {
	s, id := agentTestServer(t)
	swarmCloseFixture(t, s, id)
	// t-open: pass then a later fail. t-active: fail then a later pass.
	recordSpaced(t, s, "t-open", swarm.EvidenceTestResult, "pass")
	recordSpaced(t, s, "t-open", swarm.EvidenceBuild, "fail")
	recordSpaced(t, s, "t-active", swarm.EvidenceTestResult, "fail")
	recordSpaced(t, s, "t-active", swarm.EvidenceTestResult, "pass")
	// A later non-verifying note must not change the decision for t-active.
	recordSpaced(t, s, "t-active", swarm.EvidenceNote, "fail")

	stopWorkerAgent(t, s)

	checkTicket(t, s, "t-open", "in_progress", "[agent stopped without evidence]")
	checkTicket(t, s, "t-active", "done", "auto-closed")
}

func TestAgentStop_RepeatedStopDoesNotDuplicateNote(t *testing.T) {
	s, id := agentTestServer(t)
	swarmCloseFixture(t, s, id)

	stopWorkerAgent(t, s)
	stopWorkerAgent(t, s)

	ev, err := s.swarm.ListTicketEvidence("t-open")
	if err != nil || len(ev) != 1 {
		t.Errorf("t-open evidence = %+v, %v; want exactly one note after two stops", ev, err)
	}
	checkTicket(t, s, "t-open", "in_progress", "[agent stopped without evidence]")
}

func TestAgentStop_WithoutWorkerMarkerLeavesTicketsAlone(t *testing.T) {
	s, id := agentTestServer(t)
	swarmCloseFixture(t, s, id)
	tr := workerTranscript(t, "just a normal prompt")
	postAgentEvent(t, s, `{"event":"stop","session_id":"sess-1","agent_id":"ag","agent_transcript_path":"`+tr+`","last_assistant_message":"ok"}`)
	got, _ := s.db.GetTicket("t-open")
	if got.Status != "assigned" {
		t.Errorf("t-open = %q, want assigned", got.Status)
	}
}

func TestAgentStop_UnknownWorkerIdIsIgnored(t *testing.T) {
	s, id := agentTestServer(t)
	swarmCloseFixture(t, s, id)
	tr := workerTranscript(t, "SWARM_WORKER_ID: nobody")
	w := postAgentEvent(t, s, `{"event":"stop","session_id":"sess-1","agent_id":"ag","agent_transcript_path":"`+tr+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
}
