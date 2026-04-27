package orchestration

import (
	"context"
	"log"
	"time"
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

type LearnPipelineDeps struct {
	State           *WorkflowState
	WikiStore       WikiAutodocStore
	Enricher        WikiEnricher
	ArtifactBuilder LearnArtifactBuilder
	KnowledgeEngine LearnKnowledgeEngine
}

func RunLearnPipeline(ctx context.Context, deps LearnPipelineDeps) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	id := deps.State.ID
	defer func() {
		if r := recover(); r != nil {
			log.Printf("warn: learn pipeline %s panicked: %v", id, r)
		}
	}()

	if deps.ArtifactBuilder != nil {
		log.Printf("learn pipeline %s: building artifact", id)
		artifact, err := deps.ArtifactBuilder.Build(ctx, id)
		if err != nil {
			log.Printf("warn: learn pipeline %s: artifact build failed: %v", id, err)
		} else if artifact != nil {
			log.Printf("learn pipeline %s: artifact built (problem=%s, repo=%s)", id, artifact.ProblemClass, artifact.RepoType)

			if deps.KnowledgeEngine != nil {
				log.Printf("learn pipeline %s: running knowledge analysis", id)
				if err := deps.KnowledgeEngine.RunAnalysis(ctx); err != nil {
					log.Printf("warn: learn pipeline %s: knowledge analysis failed: %v", id, err)
				} else {
					log.Printf("learn pipeline %s: knowledge analysis complete", id)
				}
			}
		}
	}

	if deps.WikiStore != nil {
		log.Printf("learn pipeline %s: generating wiki page", id)
		if err := AutodocWorkflow(ctx, deps.WikiStore, deps.Enricher, deps.State); err != nil {
			log.Printf("warn: learn pipeline %s: autodoc failed: %v", id, err)
		} else {
			log.Printf("learn pipeline %s: wiki page generated", id)
		}
	}

	log.Printf("learn pipeline %s: complete", id)
}
