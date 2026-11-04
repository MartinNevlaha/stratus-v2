package api

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/projectidentity"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

func evidenceFixture(t *testing.T) (*Server, teams.Team) {
	t.Helper()
	d, e := db.Open(filepath.Join(t.TempDir(), "evidence.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	coordinator := orchestration.NewCoordinator(d)
	if _, e := coordinator.StartOwned("spec-codex", orchestration.WorkflowSpec, orchestration.ComplexitySimple, "evidence", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: "s1"}); e != nil {
		t.Fatal(e)
	}
	s := &Server{db: d, coordinator: coordinator, hub: NewHub(), projectRoot: t.TempDir()}
	cmd := exec.Command("git", "init", "-q", s.projectRoot)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git init: %s %v", b, e)
	}
	if _, e := s.coordinator.Transition("spec-codex", orchestration.PhaseImplement); e != nil {
		t.Fatal(e)
	}
	wf, e := s.coordinator.Transition("spec-codex", orchestration.PhaseVerify)
	if e != nil {
		t.Fatal(e)
	}
	id, root, e := projectidentity.Resolve(s.projectRoot)
	if e != nil {
		t.Fatal(e)
	}
	revision, e := teamCodeRevision(root)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	tm := teams.Team{ID: "team-fixture", ProjectID: id, ProjectRoot: root, Members: []teams.Member{{ID: "lead", Role: "lead", NativeID: "s1", Profile: teams.Profile{Runtime: "codex"}}, {ID: "reviewer", Role: "review", NativeID: "claude-worker", Profile: teams.Profile{Runtime: "claude-code", Model: "opus", Effort: "high"}}}, Tasks: []teams.Task{{ID: "review-task", Kind: "task", From: "lead", To: "reviewer", Role: "review", WorkflowID: wf.ID, WorkflowPhase: string(wf.Phase), WorkflowGeneration: wf.ReviewGeneration, WorkflowTaskIndex: wf.CurrentTask, WorkflowSessionID: wf.SessionID, Revision: revision, State: "running", CreatedAt: now, StartedAt: &now}}}
	return s, tm
}

func TestTeamWorkflowEvidenceIdempotentCrossRuntimeWithoutTransitions(t *testing.T) {
	s, tm := evidenceFixture(t)
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	tm.Tasks[0].State = "complete"
	tm.Tasks[0].Result = "Verdict: PASS\nNo findings."
	end := time.Now().UTC()
	tm.Tasks[0].FinishedAt = &end
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	agents, e := s.db.ListWorkflowAgents("spec-codex")
	if e != nil || len(agents) != 1 {
		t.Fatalf("agents %v %v", agents, e)
	}
	a := agents[0]
	if a.SessionID != "codex:s1" || a.NativeAgentID != "claude-worker" || a.TurnID != "review-task" || a.AgentType != "delivery-code-reviewer" || a.Status != "done" || a.Verdict != "PASS" {
		t.Fatalf("evidence %+v", a)
	}
	wf, _ := s.coordinator.Get("spec-codex")
	if wf.Runtime != "codex" || wf.SessionID != "codex:s1" || wf.Phase != orchestration.PhaseVerify {
		t.Fatalf("workflow modified %+v", wf)
	}
	if review := db.CodexReview(agents, string(wf.Phase), wf.CurrentTask, wf.ReviewGeneration); review.Status != "PASS" {
		t.Fatalf("review %+v", review)
	}
	logs, e := s.db.GetWorkflowLogs(wf.ID, 20)
	if e != nil || len(logs) != 1 {
		t.Fatalf("duplicate result logs %v %v", logs, e)
	}
}

func TestTeamWorkflowEvidenceRejectsUnstampedAndForeignOwner(t *testing.T) {
	for _, mutate := range []func(*teams.Team){func(tm *teams.Team) { tm.Tasks[0].WorkflowSessionID = "" }, func(tm *teams.Team) { tm.Tasks[0].WorkflowSessionID = "codex:foreign" }, func(tm *teams.Team) { tm.ProjectID = "foreign" }} {
		s, tm := evidenceFixture(t)
		mutate(&tm)
		if e := s.syncTeamWorkflowEvidence(tm); !errors.Is(e, teams.ErrForbidden) {
			t.Fatalf("invalid scope %v", e)
		}
		if agents, _ := s.db.ListWorkflowAgents("spec-codex"); len(agents) != 0 {
			t.Fatal("foreign evidence stored")
		}
	}
}

func TestTeamWorkflowEvidenceDoesNotInventVerdictsOrAcceptStaleReview(t *testing.T) {
	for _, scenario := range []string{"no verdict", "failed", "stale completion", "stale after completion"} {
		t.Run(scenario, func(t *testing.T) {
			s, tm := evidenceFixture(t)
			if e := s.syncTeamWorkflowEvidence(tm); e != nil {
				t.Fatal(e)
			}
			tm.Tasks[0].State = "complete"
			tm.Tasks[0].Result = "Verdict: PASS"
			end := time.Now().UTC()
			tm.Tasks[0].FinishedAt = &end
			if scenario == "no verdict" {
				tm.Tasks[0].Result = "Everything looks good."
			}
			if scenario == "failed" {
				tm.Tasks[0].State = "needs_user"
				tm.Tasks[0].Error = "transport failed"
			}
			if scenario == "stale completion" {
				os.WriteFile(filepath.Join(tm.ProjectRoot, "changed.go"), []byte("changed"), 0600)
			}
			if e := s.syncTeamWorkflowEvidence(tm); e != nil {
				t.Fatal(e)
			}
			if scenario == "stale after completion" {
				os.WriteFile(filepath.Join(tm.ProjectRoot, "changed.go"), []byte("changed"), 0600)
				if e := s.syncTeamWorkflowEvidence(tm); e != nil {
					t.Fatal(e)
				}
			}
			agents, _ := s.db.ListWorkflowAgents("spec-codex")
			if len(agents) != 1 || agents[0].Verdict == "PASS" {
				t.Fatalf("invalid PASS %+v", agents)
			}
		})
	}
}

func TestTeamWorkflowEvidencePreservesGovernanceRoleAndObservedRuntime(t *testing.T) {
	s, tm := evidenceFixture(t)
	tm.Tasks[0].Role = "delivery-governance-checker"
	tm.Members[1].ObservedModel = "opus-actual"
	tm.Members[1].ObservedEffort = "high"
	in, out := int64(12), int64(8)
	tm.Tasks[0].InputTokens = &in
	tm.Tasks[0].OutputTokens = &out
	tm.Tasks[0].State = "complete"
	tm.Tasks[0].Result = "Verdict: PASS"
	now := time.Now().UTC()
	tm.Tasks[0].FinishedAt = &now
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	agents, _ := s.db.ListWorkflowAgents("spec-codex")
	agents = s.enrichTeamEvidenceAgents(runtimeAgents(agents, "codex"))
	if len(agents) != 1 || agents[0].AgentType != "delivery-governance-checker" || agents[0].Runtime != "claude-code" || agents[0].UsageAvailable == nil || !*agents[0].UsageAvailable || agents[0].TokensIn != 12 {
		t.Fatalf("metadata %+v", agents)
	}
	if agents[0].Verdict != "" {
		t.Fatal("governance PASS fabricated a code review PASS")
	}
	wf, _ := s.coordinator.Get("spec-codex")
	if len(wf.Delegated["verify"]) != 1 || wf.Delegated["verify"][0] != "delivery-governance-checker" {
		t.Fatalf("role %v", wf.Delegated)
	}
}

func TestTeamWorkflowEvidenceInvalidatesPhaseGenerationAndRejectsNoNativeRun(t *testing.T) {
	s, tm := evidenceFixture(t)
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	if _, e := s.coordinator.Transition("spec-codex", orchestration.PhaseImplement); e != nil {
		t.Fatal(e)
	}
	tm.Tasks[0].State = "complete"
	tm.Tasks[0].Result = "Verdict: PASS"
	now := time.Now().UTC()
	tm.Tasks[0].FinishedAt = &now
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	agents, _ := s.db.ListWorkflowAgents("spec-codex")
	if len(agents) != 1 || agents[0].Verdict != "" || agents[0].Phase != "verify" {
		t.Fatalf("stale scope %+v", agents)
	}
	wf, _ := s.coordinator.Get("spec-codex")
	if len(wf.Delegated["implement"]) != 0 {
		t.Fatal("old scope counted in new phase")
	}
	s, tm = evidenceFixture(t)
	tm.Members[1].NativeID = ""
	tm.Tasks[0].State = "complete"
	tm.Tasks[0].Result = "Verdict: PASS"
	tm.Tasks[0].FinishedAt = &now
	if e := s.syncTeamWorkflowEvidence(tm); !errors.Is(e, teams.ErrForbidden) {
		t.Fatalf("accepted missing native run %v", e)
	}
}

func TestTeamWorkflowHistoricalPassSurvivesForwardPhaseButNotNewReviewGeneration(t *testing.T) {
	s, tm := evidenceFixture(t)
	tm.Tasks[0].State = "complete"
	tm.Tasks[0].Result = "Verdict: PASS"
	now := time.Now().UTC()
	tm.Tasks[0].FinishedAt = &now
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	if _, e := s.coordinator.Transition("spec-codex", orchestration.PhaseLearn); e != nil {
		t.Fatal(e)
	}
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	agents, _ := s.db.ListWorkflowAgents("spec-codex")
	if len(agents) != 1 || agents[0].Verdict != "PASS" {
		t.Fatalf("historical PASS erased %+v", agents)
	}
	wf, _ := s.coordinator.Get("spec-codex")
	if review := db.CodexReview(agents, string(wf.Phase), wf.CurrentTask, wf.ReviewGeneration); review.Status == "PASS" {
		t.Fatal("historic PASS applied to forward phase")
	}

	s, tm = evidenceFixture(t)
	tm.Tasks[0].State = "complete"
	tm.Tasks[0].Result = "Verdict: PASS"
	tm.Tasks[0].FinishedAt = &now
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	if _, e := s.coordinator.Transition("spec-codex", orchestration.PhaseImplement); e != nil {
		t.Fatal(e)
	}
	if _, e := s.coordinator.Transition("spec-codex", orchestration.PhaseVerify); e != nil {
		t.Fatal(e)
	}
	if e := s.syncTeamWorkflowEvidence(tm); e != nil {
		t.Fatal(e)
	}
	agents, _ = s.db.ListWorkflowAgents("spec-codex")
	if agents[0].Verdict == "PASS" {
		t.Fatal("old review accepted for newer generation")
	}
}

func TestTeamWorkflowConcurrentReceiptsRecordOneResult(t *testing.T) {
	s, tm := evidenceFixture(t)
	tm.Tasks[0].State = "complete"
	tm.Tasks[0].Result = "Verdict: PASS"
	now := time.Now().UTC()
	tm.Tasks[0].FinishedAt = &now
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.syncTeamWorkflowEvidence(tm); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	agents, _ := s.db.ListWorkflowAgents("spec-codex")
	logs, _ := s.db.GetWorkflowLogs("spec-codex", 20)
	if len(agents) != 1 || len(logs) != 1 {
		t.Fatalf("duplicate receipts: agents=%d logs=%d", len(agents), len(logs))
	}
}

func TestTeamReviewVerdictRequiresAnExplicitReportLine(t *testing.T) {
	for _, report := range []string{"Example output: Verdict: PASS", "> Verdict: PASS", "```\nVerdict: PASS\n```", "The previous review said Verdict: PASS"} {
		if v := teamReviewVerdict(report); v.Verdict != "" {
			t.Fatalf("quoted/example verdict accepted: %q", report)
		}
	}
	for _, report := range []string{"Verdict: PASS", "## Review Verdict: PASS", "**Verdict: PASS**"} {
		if v := teamReviewVerdict(report); v.Verdict != "PASS" {
			t.Fatalf("explicit verdict omitted: %q", report)
		}
	}
}
