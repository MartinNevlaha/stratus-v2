package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/events"
	"github.com/MartinNevlaha/stratus-v2/vexor"
)

func TestBuildPrefetchQueries_SpecPlan(t *testing.T) {
	qs := buildPrefetchQueries("spec", "plan", "Add dark mode")
	if len(qs) != 2 {
		t.Fatalf("spec.plan: expected 2 queries, got %d", len(qs))
	}
	if qs[0].Corpus != "governance" || qs[1].Corpus != "wiki" {
		t.Errorf("spec.plan: expected [governance, wiki], got [%s, %s]", qs[0].Corpus, qs[1].Corpus)
	}
	for _, q := range qs {
		if q.Query != "Add dark mode" {
			t.Errorf("spec.plan: expected query to be title, got %q", q.Query)
		}
	}
}

func TestBuildPrefetchQueries_SpecImplement(t *testing.T) {
	qs := buildPrefetchQueries("spec", "implement", "Refactor auth")
	if len(qs) != 2 || qs[0].Corpus != "code" || qs[1].Corpus != "wiki" {
		t.Errorf("spec.implement: expected [code, wiki], got %+v", qs)
	}
}

func TestBuildPrefetchQueries_SpecVerify(t *testing.T) {
	qs := buildPrefetchQueries("spec", "verify", "Payment flow")
	if len(qs) != 2 || qs[0].Corpus != "governance" || qs[1].Corpus != "code" {
		t.Fatalf("spec.verify: expected [governance, code], got %+v", qs)
	}
	if qs[0].Query != "test review security Payment flow" {
		t.Errorf("spec.verify governance query: got %q", qs[0].Query)
	}
}

func TestBuildPrefetchQueries_SpecGovernance(t *testing.T) {
	qs := buildPrefetchQueries("spec", "governance", "API redesign")
	if len(qs) != 1 || qs[0].Corpus != "governance" {
		t.Fatalf("spec.governance: expected [governance], got %+v", qs)
	}
	if qs[0].Query != "rule ADR API redesign" {
		t.Errorf("spec.governance query: got %q", qs[0].Query)
	}
}

func TestBuildPrefetchQueries_BugFix(t *testing.T) {
	qs := buildPrefetchQueries("bug", "fix", "Null pointer in login")
	if len(qs) != 2 || qs[0].Corpus != "code" || qs[1].Corpus != "wiki" {
		t.Errorf("bug.fix: expected [code, wiki], got %+v", qs)
	}
}

func TestBuildPrefetchQueries_E2EGenerate(t *testing.T) {
	qs := buildPrefetchQueries("e2e", "generate", "Checkout flow")
	if len(qs) != 2 || qs[0].Corpus != "code" || qs[1].Corpus != "wiki" {
		t.Fatalf("e2e.generate: expected [code, wiki], got %+v", qs)
	}
	if qs[0].Query != "test e2e Checkout flow" {
		t.Errorf("e2e.generate code query: got %q", qs[0].Query)
	}
}

func TestBuildPrefetchQueries_EmptyTitle(t *testing.T) {
	qs := buildPrefetchQueries("spec", "plan", "   ")
	if qs != nil {
		t.Errorf("expected nil for empty title, got %+v", qs)
	}
}

func TestBuildPrefetchQueries_UnknownPhase(t *testing.T) {
	qs := buildPrefetchQueries("spec", "complete", "Whatever")
	if qs != nil {
		t.Errorf("expected nil for unmapped phase, got %+v", qs)
	}
	qs = buildPrefetchQueries("bug", "analyze", "Whatever")
	if qs != nil {
		t.Errorf("bug.analyze should not be mapped (handled elsewhere), got %+v", qs)
	}
}

func newPrefetchServer(t *testing.T, database *db.DB) *Server {
	t.Helper()
	return &Server{
		db:          database,
		vexor:       vexor.New("__nonexistent_vexor_binary__", "", 1),
		projectRoot: t.TempDir(),
	}
}

func countPrefetchEvents(t *testing.T, database *db.DB) int {
	t.Helper()
	evs, err := database.SearchEvents(db.SearchEventsInput{Type: "context_prefetch", Limit: 100})
	if err != nil {
		t.Fatalf("SearchEvents: %v", err)
	}
	return len(evs)
}

func TestPrefetcher_SkipsBugAnalyze(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	srv := newPrefetchServer(t, database)
	p := newPrefetcher(srv)

	evt := events.NewEvent(events.EventPhaseTransition, "test", map[string]any{
		"workflow_id":   "bug-123",
		"workflow_type": "bug",
		"to_phase":      "analyze",
		"title":         "Null pointer",
	})
	p.handleEvent(context.Background(), evt)

	if count := countPrefetchEvents(t, database); count != 0 {
		t.Errorf("expected 0 prefetch events for bug.analyze, got %d", count)
	}
	// Also verify the dedup map was NOT populated (no work happened).
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.lastRun["bug-123|analyze"]; ok {
		t.Error("lastRun should not be set for skipped bug.analyze")
	}
}

func TestPrefetcher_IgnoresUnrelatedEvents(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	srv := newPrefetchServer(t, database)
	p := newPrefetcher(srv)

	evt := events.NewEvent(events.EventWorkflowStarted, "test", map[string]any{
		"workflow_id":   "spec-999",
		"workflow_type": "spec",
		"to_phase":      "plan",
		"title":         "Some workflow",
	})
	p.handleEvent(context.Background(), evt)

	if count := countPrefetchEvents(t, database); count != 0 {
		t.Errorf("unrelated event type should not create prefetch, got %d events", count)
	}
}

func TestPrefetcher_MissingRequiredFields(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	srv := newPrefetchServer(t, database)
	p := newPrefetcher(srv)

	// Missing workflow_id
	evt := events.NewEvent(events.EventPhaseTransition, "test", map[string]any{
		"workflow_type": "spec",
		"to_phase":      "plan",
		"title":         "Title",
	})
	p.handleEvent(context.Background(), evt)

	if count := countPrefetchEvents(t, database); count != 0 {
		t.Errorf("expected 0 events when workflow_id missing, got %d", count)
	}
}

func TestPrefetcher_DeduplicatesRecent(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	srv := newPrefetchServer(t, database)
	p := newPrefetcher(srv)

	// Seed lastRun to simulate a prefetch that ran 5s ago.
	p.mu.Lock()
	p.lastRun["spec-dedup|implement"] = time.Now().Add(-5 * time.Second)
	p.mu.Unlock()

	evt := events.NewEvent(events.EventPhaseTransition, "test", map[string]any{
		"workflow_id":   "spec-dedup",
		"workflow_type": "spec",
		"to_phase":      "implement",
		"title":         "Dedup test",
	})
	p.handleEvent(context.Background(), evt)

	if count := countPrefetchEvents(t, database); count != 0 {
		t.Errorf("expected 0 events inside dedup window, got %d", count)
	}
}

func TestPrefetcher_SavesEventWithGovernance(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Seed a governance doc by writing a rules markdown file into a temp project
	// root and running the real IndexGovernance scanner (the only public path
	// into the docs table).
	projectRoot := t.TempDir()
	rulesDir := filepath.Join(projectRoot, ".claude", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatalf("mkdir rules: %v", err)
	}
	ruleContent := "# Testing Guidelines\n\nunit tests must cover happy path and error cases\n"
	if err := os.WriteFile(filepath.Join(rulesDir, "testing.md"), []byte(ruleContent), 0o644); err != nil {
		t.Fatalf("write rule: %v", err)
	}
	if err := database.IndexGovernance(projectRoot); err != nil {
		t.Fatalf("IndexGovernance: %v", err)
	}

	srv := newPrefetchServer(t, database)
	srv.projectRoot = projectRoot
	p := newPrefetcher(srv)

	evt := events.NewEvent(events.EventPhaseTransition, "test", map[string]any{
		"workflow_id":   "spec-save",
		"workflow_type": "spec",
		"to_phase":      "plan",
		"title":         "unit tests",
	})
	p.handleEvent(context.Background(), evt)

	if count := countPrefetchEvents(t, database); count != 1 {
		t.Fatalf("expected 1 prefetch event saved, got %d", count)
	}

	// Verify dedup map was populated.
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.lastRun["spec-save|plan"]; !ok {
		t.Error("lastRun should be set after successful prefetch")
	}
}

func TestPrefetcher_NoResultsDoesNotSaveEvent(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	srv := newPrefetchServer(t, database)
	p := newPrefetcher(srv)

	// No governance docs seeded, vexor unavailable → retrieve returns nothing.
	evt := events.NewEvent(events.EventPhaseTransition, "test", map[string]any{
		"workflow_id":   "spec-empty",
		"workflow_type": "spec",
		"to_phase":      "plan",
		"title":         "nothing here",
	})
	p.handleEvent(context.Background(), evt)

	if count := countPrefetchEvents(t, database); count != 0 {
		t.Errorf("expected 0 events when retrieve returns nothing, got %d", count)
	}
}
