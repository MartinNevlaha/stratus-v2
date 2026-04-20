package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// --- stub store ---

type stubAutodocStore struct {
	called      bool
	workflowID  string
	featureSlug string
	page        *db.WikiPage
	returnErr   error
}

func (s *stubAutodocStore) UpsertWikiPageByWorkflow(ctx context.Context, workflowID, featureSlug string, page *db.WikiPage) (*db.WikiPage, error) {
	s.called = true
	s.workflowID = workflowID
	s.featureSlug = featureSlug
	s.page = page
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	out := *page
	out.ID = "wiki-generated-id"
	return &out, nil
}

// --- stub enricher ---

type stubEnricher struct {
	called     bool
	receivedW  *WorkflowState
	receivedIn string
	returnOut  string
	returnErr  error
}

func (e *stubEnricher) Enrich(ctx context.Context, w *WorkflowState, base string) (string, error) {
	e.called = true
	e.receivedW = w
	e.receivedIn = base
	return e.returnOut, e.returnErr
}

// --- helpers ---

func workflowWithTasks(id, title string) *WorkflowState {
	return &WorkflowState{
		ID:          id,
		Type:        WorkflowSpec,
		Phase:       PhaseComplete,
		Title:       title,
		PlanContent: "## Plan\nBuild the feature.",
		Tasks: []Task{
			{Index: 0, Title: "Add handler", Status: "done"},
			{Index: 1, Title: "Write tests", Status: "done"},
		},
		Delegated: map[string][]string{
			"implement": {"delivery-backend-engineer"},
			"verify":    {"delivery-code-reviewer"},
		},
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// --- AutodocWorkflow tests ---

func TestAutodocWorkflow_ProducesExpectedPageFields(t *testing.T) {
	store := &stubAutodocStore{}
	w := workflowWithTasks("spec-my-cool-feature", "My Cool Feature")

	err := AutodocWorkflow(context.Background(), store, nil, w)
	if err != nil {
		t.Fatalf("AutodocWorkflow: unexpected error: %v", err)
	}

	if !store.called {
		t.Fatal("UpsertWikiPageByWorkflow was not called")
	}

	// workflow ID passed through
	if store.workflowID != "spec-my-cool-feature" {
		t.Errorf("workflowID: got %q want %q", store.workflowID, "spec-my-cool-feature")
	}

	// feature slug strips type prefix and is kebab-case
	if store.featureSlug != "my-cool-feature" {
		t.Errorf("featureSlug: got %q want %q", store.featureSlug, "my-cool-feature")
	}

	p := store.page
	if p == nil {
		t.Fatal("page must not be nil")
	}

	// Title
	if p.Title != "My Cool Feature" {
		t.Errorf("Title: got %q want %q", p.Title, "My Cool Feature")
	}

	// Status = auto-generated
	if p.Status != "auto-generated" {
		t.Errorf("Status: got %q want %q", p.Status, "auto-generated")
	}

	// GeneratedBy = workflow
	if p.GeneratedBy != "workflow" {
		t.Errorf("GeneratedBy: got %q want %q", p.GeneratedBy, "workflow")
	}

	// Metadata source
	src, _ := p.Metadata["source"].(string)
	if src != "workflow:spec-my-cool-feature" {
		t.Errorf("metadata.source: got %q want %q", src, "workflow:spec-my-cool-feature")
	}

	// Confidence
	conf, _ := p.Metadata["confidence"].(float64)
	if conf != 0.8 {
		t.Errorf("metadata.confidence: got %v want 0.8", conf)
	}

	// Content contains task titles
	if !strings.Contains(p.Content, "Add handler") {
		t.Error("content should contain task title 'Add handler'")
	}
	if !strings.Contains(p.Content, "Write tests") {
		t.Error("content should contain task title 'Write tests'")
	}

	// Content contains Overview section
	if !strings.Contains(p.Content, "## Overview") {
		t.Error("content should contain '## Overview'")
	}

	// Content contains Tasks section
	if !strings.Contains(p.Content, "## Tasks") {
		t.Error("content should contain '## Tasks'")
	}

	// Content contains Delegated Agents section
	if !strings.Contains(p.Content, "## Delegated Agents") {
		t.Error("content should contain '## Delegated Agents'")
	}

	// Content contains Status section
	if !strings.Contains(p.Content, "## Status") {
		t.Error("content should contain '## Status'")
	}
}

func TestAutodocWorkflow_SlugFromBugWorkflow(t *testing.T) {
	store := &stubAutodocStore{}
	w := workflowWithTasks("bug-nil-pointer-crash", "Nil Pointer Crash Fix")
	w.Type = WorkflowBug

	if err := AutodocWorkflow(context.Background(), store, nil, w); err != nil {
		t.Fatalf("AutodocWorkflow: %v", err)
	}

	if store.featureSlug != "nil-pointer-crash" {
		t.Errorf("featureSlug: got %q want %q", store.featureSlug, "nil-pointer-crash")
	}
}

func TestAutodocWorkflow_SlugNoPrefix(t *testing.T) {
	store := &stubAutodocStore{}
	w := workflowWithTasks("plain-workflow-id", "Plain Workflow")

	if err := AutodocWorkflow(context.Background(), store, nil, w); err != nil {
		t.Fatalf("AutodocWorkflow: %v", err)
	}

	// No recognised type prefix — whole ID used as slug
	if store.featureSlug != "plain-workflow-id" {
		t.Errorf("featureSlug: got %q want %q", store.featureSlug, "plain-workflow-id")
	}
}

func TestAutodocWorkflow_StoreError_ReturnsError(t *testing.T) {
	storeErr := errors.New("db unavailable")
	store := &stubAutodocStore{returnErr: storeErr}
	w := workflowWithTasks("spec-feature", "Feature")

	err := AutodocWorkflow(context.Background(), store, nil, w)
	if err == nil {
		t.Fatal("expected error when store fails")
	}
	if !errors.Is(err, storeErr) {
		t.Errorf("error should wrap store error; got %v", err)
	}
}

// --- Enricher tests ---

func TestAutodocWorkflow_EnricherEnriched(t *testing.T) {
	store := &stubAutodocStore{}
	enricher := &stubEnricher{returnOut: "## Enriched\n\nRich functional description."}
	w := workflowWithTasks("spec-feature", "Feature")

	if err := AutodocWorkflow(context.Background(), store, enricher, w); err != nil {
		t.Fatalf("AutodocWorkflow: %v", err)
	}

	if !enricher.called {
		t.Fatal("enricher must be called")
	}
	if enricher.receivedW != w {
		t.Error("enricher should receive the same WorkflowState pointer")
	}
	if !strings.Contains(enricher.receivedIn, "## Overview") {
		t.Error("enricher should receive base markdown including ## Overview")
	}
	if store.page == nil || !strings.Contains(store.page.Content, "Rich functional description") {
		t.Errorf("persisted content should be enricher output; got %q", store.page.Content)
	}
}

func TestAutodocWorkflow_EnricherError_FallsBackToBase(t *testing.T) {
	store := &stubAutodocStore{}
	enricher := &stubEnricher{returnErr: errors.New("llm timeout")}
	w := workflowWithTasks("spec-feature", "Feature")

	err := AutodocWorkflow(context.Background(), store, enricher, w)
	if err != nil {
		t.Fatalf("AutodocWorkflow: enricher error must not fail the call: %v", err)
	}
	if store.page == nil {
		t.Fatal("page must still be persisted when enricher errors")
	}
	if !strings.Contains(store.page.Content, "## Overview") ||
		!strings.Contains(store.page.Content, "Add handler") {
		t.Error("content should fall back to base template on enricher error")
	}
}

func TestAutodocWorkflow_EnricherEmptyString_FallsBackToBase(t *testing.T) {
	store := &stubAutodocStore{}
	enricher := &stubEnricher{returnOut: "   \n\t  "} // whitespace only
	w := workflowWithTasks("spec-feature", "Feature")

	if err := AutodocWorkflow(context.Background(), store, enricher, w); err != nil {
		t.Fatalf("AutodocWorkflow: %v", err)
	}
	if store.page == nil || !strings.Contains(store.page.Content, "## Tasks") {
		t.Error("empty enricher output must not overwrite base template")
	}
}

// --- buildAutodocContent / ChangeSummary tests ---

func TestBuildAutodocContent_WithChangeSummary(t *testing.T) {
	w := workflowWithTasks("spec-feature", "Feature")
	w.ChangeSummary = &ChangeSummary{
		CapabilitiesAdded:    []string{"LLM enricher interface", "Richer wiki pages"},
		CapabilitiesModified: []string{"AutodocWorkflow signature"},
		CapabilitiesRemoved:  nil,
		DownstreamRisks:      []string{"LLM may hallucinate file paths"},
		TestCoverageDelta:    "+4.1%",
		FilesChanged:         5,
		LinesAdded:           180,
		LinesRemoved:         12,
	}

	content := buildAutodocContent(w)

	want := []string{
		"## Change Summary",
		"**Files changed:** 5 (+180 / -12)",
		"**Test coverage delta:** +4.1%",
		"### Capabilities added",
		"LLM enricher interface",
		"Richer wiki pages",
		"### Capabilities modified",
		"AutodocWorkflow signature",
		"### Downstream risks",
		"LLM may hallucinate file paths",
	}
	for _, w := range want {
		if !strings.Contains(content, w) {
			t.Errorf("content missing %q\n--- content ---\n%s", w, content)
		}
	}

	// Empty CapabilitiesRemoved section must be omitted entirely.
	if strings.Contains(content, "### Capabilities removed") {
		t.Error("empty Capabilities Removed section should be skipped")
	}
}

func TestBuildAutodocContent_NilChangeSummary(t *testing.T) {
	w := workflowWithTasks("spec-feature", "Feature")
	w.ChangeSummary = nil

	content := buildAutodocContent(w)

	if strings.Contains(content, "## Change Summary") {
		t.Error("nil ChangeSummary must not render Change Summary heading")
	}
	// Other sections still present
	for _, h := range []string{"## Overview", "## Tasks", "## Delegated Agents", "## Status"} {
		if !strings.Contains(content, h) {
			t.Errorf("content missing %q", h)
		}
	}
}
