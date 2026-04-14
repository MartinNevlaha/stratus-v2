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

	err := AutodocWorkflow(context.Background(), store, w)
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

	if err := AutodocWorkflow(context.Background(), store, w); err != nil {
		t.Fatalf("AutodocWorkflow: %v", err)
	}

	if store.featureSlug != "nil-pointer-crash" {
		t.Errorf("featureSlug: got %q want %q", store.featureSlug, "nil-pointer-crash")
	}
}

func TestAutodocWorkflow_SlugNoPrefix(t *testing.T) {
	store := &stubAutodocStore{}
	w := workflowWithTasks("plain-workflow-id", "Plain Workflow")

	if err := AutodocWorkflow(context.Background(), store, w); err != nil {
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

	err := AutodocWorkflow(context.Background(), store, w)
	if err == nil {
		t.Fatal("expected error when store fails")
	}
	if !errors.Is(err, storeErr) {
		t.Errorf("error should wrap store error; got %v", err)
	}
}
