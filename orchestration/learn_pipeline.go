package orchestration

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

type LearnArtifact struct {
	ID              string
	WorkflowID      string
	TaskType        string
	WorkflowType    string
	RepoType        string
	ProblemClass    string
	AgentsUsed      []string
	RootCause       string
	SolutionPattern string
	FilesChanged    []string
	ReviewResult    string
	CycleTimeMin    int
	Success         bool
	Metadata        map[string]any
	CreatedAt       time.Time
}

type LearnArtifactBuilder interface {
	Build(ctx context.Context, workflowID string) (*LearnArtifact, error)
}

type LearnKnowledgeEngine interface {
	RunAnalysis(ctx context.Context) error
}

// LearnEventStore persists summary memory events emitted by the pipeline so
// that pipeline outcomes are observable in the workflow timeline. Optional —
// pipeline runs unchanged if nil.
type LearnEventStore interface {
	SaveEvent(in db.SaveEventInput) (int64, error)
}

type LearnPipelineDeps struct {
	State           *WorkflowState
	WikiStore       WikiAutodocStore
	Enricher        WikiEnricher
	ArtifactBuilder LearnArtifactBuilder
	KnowledgeEngine LearnKnowledgeEngine
	EventStore      LearnEventStore
	// Timeout caps the total pipeline duration. Zero falls back to 180s.
	Timeout time.Duration
}

type stepStatus struct {
	name   string
	state  string // ok | skipped | failed | disabled
	detail string
}

func (s stepStatus) String() string {
	if s.detail == "" {
		return fmt.Sprintf("%s: %s", s.name, s.state)
	}
	return fmt.Sprintf("%s: %s (%s)", s.name, s.state, s.detail)
}

func RunLearnPipeline(ctx context.Context, deps LearnPipelineDeps) {
	timeout := deps.Timeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id := deps.State.ID
	start := time.Now()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("warn: learn pipeline %s panicked: %v", id, r)
		}
	}()

	steps := make([]stepStatus, 0, 3)

	// Step 1: artifact build (insight-gated)
	var artifact *LearnArtifact
	if deps.ArtifactBuilder == nil {
		steps = append(steps, stepStatus{name: "artifact", state: "disabled", detail: "insight off"})
	} else {
		log.Printf("learn pipeline %s: building artifact", id)
		built, err := deps.ArtifactBuilder.Build(ctx, id)
		switch {
		case err != nil:
			log.Printf("warn: learn pipeline %s: artifact build failed: %v", id, err)
			steps = append(steps, stepStatus{name: "artifact", state: "failed", detail: err.Error()})
		case built == nil:
			log.Printf("learn pipeline %s: artifact skipped (insufficient events or already exists)", id)
			steps = append(steps, stepStatus{name: "artifact", state: "skipped", detail: "insufficient events or duplicate"})
		default:
			log.Printf("learn pipeline %s: artifact built (problem=%s, repo=%s)", id, built.ProblemClass, built.RepoType)
			steps = append(steps, stepStatus{
				name:   "artifact",
				state:  "ok",
				detail: fmt.Sprintf("problem=%s, repo=%s", built.ProblemClass, built.RepoType),
			})
			artifact = built
		}
	}

	// Step 2: knowledge analysis (only if artifact built)
	switch {
	case deps.KnowledgeEngine == nil:
		steps = append(steps, stepStatus{name: "knowledge", state: "disabled", detail: "insight off"})
	case artifact == nil:
		steps = append(steps, stepStatus{name: "knowledge", state: "skipped", detail: "no artifact"})
	default:
		log.Printf("learn pipeline %s: running knowledge analysis", id)
		if err := deps.KnowledgeEngine.RunAnalysis(ctx); err != nil {
			log.Printf("warn: learn pipeline %s: knowledge analysis failed: %v", id, err)
			steps = append(steps, stepStatus{name: "knowledge", state: "failed", detail: err.Error()})
		} else {
			log.Printf("learn pipeline %s: knowledge analysis complete", id)
			steps = append(steps, stepStatus{name: "knowledge", state: "ok"})
		}
	}

	// Step 3: wiki autodoc
	if deps.WikiStore == nil {
		steps = append(steps, stepStatus{name: "autodoc", state: "disabled", detail: "wiki off"})
	} else {
		log.Printf("learn pipeline %s: generating wiki page", id)
		if err := AutodocWorkflow(ctx, deps.WikiStore, deps.Enricher, deps.State); err != nil {
			log.Printf("warn: learn pipeline %s: autodoc failed: %v", id, err)
			steps = append(steps, stepStatus{name: "autodoc", state: "failed", detail: err.Error()})
		} else {
			log.Printf("learn pipeline %s: wiki page generated", id)
			steps = append(steps, stepStatus{name: "autodoc", state: "ok"})
		}
	}

	durationMs := time.Since(start).Milliseconds()
	log.Printf("learn pipeline %s: complete (%dms)", id, durationMs)

	if deps.EventStore != nil {
		emitSummaryEvent(deps.EventStore, deps.State, artifact, steps, durationMs)
	}
}

func emitSummaryEvent(store LearnEventStore, state *WorkflowState, artifact *LearnArtifact, steps []stepStatus, durationMs int64) {
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		parts = append(parts, s.String())
	}

	refs := map[string]any{
		"workflow_id": state.ID,
		"duration_ms": durationMs,
	}
	if artifact != nil {
		refs["artifact_id"] = artifact.ID
		refs["problem_class"] = artifact.ProblemClass
		refs["repo_type"] = artifact.RepoType
	}

	tags := []string{"learn", "pipeline", string(state.Type)}

	_, err := store.SaveEvent(db.SaveEventInput{
		Actor:      "coordinator",
		Scope:      "workflow",
		Type:       "learn_pipeline",
		Title:      fmt.Sprintf("Learn pipeline: %s", state.ID),
		Text:       strings.Join(parts, " | "),
		Tags:       tags,
		Refs:       refs,
		Importance: 0.4,
	})
	if err != nil {
		log.Printf("warn: learn pipeline %s: summary event save failed: %v", state.ID, err)
	}
}
