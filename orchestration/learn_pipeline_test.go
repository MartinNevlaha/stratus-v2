package orchestration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type recordingEventStore struct {
	mu     sync.Mutex
	events []db.SaveEventInput
}

func (r *recordingEventStore) SaveEvent(in db.SaveEventInput) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, in)
	return int64(len(r.events)), nil
}

func (r *recordingEventStore) snapshot() []db.SaveEventInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]db.SaveEventInput, len(r.events))
	copy(out, r.events)
	return out
}

type fakeArtifactBuilder struct {
	artifact *LearnArtifact
	err      error
}

func (f *fakeArtifactBuilder) Build(ctx context.Context, workflowID string) (*LearnArtifact, error) {
	return f.artifact, f.err
}

type fakeKnowledgeEngine struct {
	called bool
	err    error
}

func (f *fakeKnowledgeEngine) RunAnalysis(ctx context.Context) error {
	f.called = true
	return f.err
}

func TestRunLearnPipeline_EmitsSummaryEvent_AllStepsOK(t *testing.T) {
	store := &recordingEventStore{}
	state := &WorkflowState{ID: "wf-1", Type: WorkflowSpec, Phase: PhaseLearn}
	artifact := &LearnArtifact{ID: "art-1", ProblemClass: "bug", RepoType: "monorepo"}

	RunLearnPipeline(context.Background(), LearnPipelineDeps{
		State:           state,
		ArtifactBuilder: &fakeArtifactBuilder{artifact: artifact},
		KnowledgeEngine: &fakeKnowledgeEngine{},
		EventStore:      store,
		Timeout:         5 * time.Second,
	})

	events := store.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 summary event, got %d", len(events))
	}

	ev := events[0]
	if ev.Type != "learn_pipeline" {
		t.Errorf("Type: got %q want learn_pipeline", ev.Type)
	}
	if !strings.Contains(ev.Text, "artifact: ok") {
		t.Errorf("Text missing artifact:ok: %q", ev.Text)
	}
	if !strings.Contains(ev.Text, "knowledge: ok") {
		t.Errorf("Text missing knowledge:ok: %q", ev.Text)
	}
	if !strings.Contains(ev.Text, "autodoc: disabled") {
		t.Errorf("Text missing autodoc:disabled (no wiki store): %q", ev.Text)
	}
	if got, _ := ev.Refs["workflow_id"].(string); got != "wf-1" {
		t.Errorf("Refs.workflow_id: got %v want wf-1", ev.Refs["workflow_id"])
	}
	if got, _ := ev.Refs["artifact_id"].(string); got != "art-1" {
		t.Errorf("Refs.artifact_id: got %v want art-1", ev.Refs["artifact_id"])
	}
}

func TestRunLearnPipeline_ArtifactSkipped_KnowledgeNotRun(t *testing.T) {
	store := &recordingEventStore{}
	knowledge := &fakeKnowledgeEngine{}
	state := &WorkflowState{ID: "wf-2", Type: WorkflowBug, Phase: PhaseLearn}

	RunLearnPipeline(context.Background(), LearnPipelineDeps{
		State:           state,
		ArtifactBuilder: &fakeArtifactBuilder{artifact: nil}, // skipped
		KnowledgeEngine: knowledge,
		EventStore:      store,
		Timeout:         5 * time.Second,
	})

	if knowledge.called {
		t.Error("knowledge engine must not run when artifact is skipped")
	}

	events := store.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 summary event, got %d", len(events))
	}
	if !strings.Contains(events[0].Text, "artifact: skipped") {
		t.Errorf("Text missing artifact:skipped: %q", events[0].Text)
	}
	if !strings.Contains(events[0].Text, "knowledge: skipped") {
		t.Errorf("Text missing knowledge:skipped: %q", events[0].Text)
	}
}

func TestRunLearnPipeline_ArtifactBuildFailed_RecordsFailure(t *testing.T) {
	store := &recordingEventStore{}
	state := &WorkflowState{ID: "wf-3", Type: WorkflowSpec, Phase: PhaseLearn}

	RunLearnPipeline(context.Background(), LearnPipelineDeps{
		State:           state,
		ArtifactBuilder: &fakeArtifactBuilder{err: errors.New("db unavailable")},
		EventStore:      store,
		Timeout:         5 * time.Second,
	})

	events := store.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 summary event, got %d", len(events))
	}
	if !strings.Contains(events[0].Text, "artifact: failed") {
		t.Errorf("Text missing artifact:failed: %q", events[0].Text)
	}
	if !strings.Contains(events[0].Text, "db unavailable") {
		t.Errorf("Text missing error detail: %q", events[0].Text)
	}
}

func TestRunLearnPipeline_NoEventStore_NoCrash(t *testing.T) {
	state := &WorkflowState{ID: "wf-4", Type: WorkflowSpec, Phase: PhaseLearn}

	// Should not panic and should not require an event store.
	RunLearnPipeline(context.Background(), LearnPipelineDeps{
		State:   state,
		Timeout: 5 * time.Second,
	})
}

func TestRunLearnPipeline_DefaultTimeoutApplied(t *testing.T) {
	// Sanity: zero timeout falls back to a positive default rather than
	// cancelling immediately.
	store := &recordingEventStore{}
	state := &WorkflowState{ID: "wf-5", Type: WorkflowSpec, Phase: PhaseLearn}

	RunLearnPipeline(context.Background(), LearnPipelineDeps{
		State:      state,
		EventStore: store,
		// Timeout: 0 → default.
	})

	events := store.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 summary event, got %d", len(events))
	}
}
