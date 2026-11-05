package knowledge_engine

import (
	"context"
	"fmt"
	"testing"
)

// fakeArtifactQuery mirrors production semantics: GetSuccessfulArtifactsWithSolution
// returns only successful artifacts, ListArtifacts returns every artifact.
type fakeArtifactQuery struct {
	artifacts []ArtifactData
}

func (f *fakeArtifactQuery) GetSuccessfulArtifactsWithSolution(ctx context.Context, limit int) ([]ArtifactData, error) {
	var out []ArtifactData
	for _, a := range f.artifacts {
		if a.Success && a.SolutionPattern != "" && a.ProblemClass != "" {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeArtifactQuery) ListArtifacts(ctx context.Context, filters ArtifactFilterOptions) ([]ArtifactData, error) {
	out := f.artifacts
	if filters.Limit > 0 && len(out) > filters.Limit {
		out = out[:filters.Limit]
	}
	return out, nil
}

func (f *fakeArtifactQuery) CountArtifacts(ctx context.Context) (int, error) {
	return len(f.artifacts), nil
}

func (f *fakeArtifactQuery) GetProblemClassStats(ctx context.Context) ([]map[string]any, error) {
	return nil, nil
}

func (f *fakeArtifactQuery) GetAgentSuccessByProblem(ctx context.Context) ([]map[string]any, error) {
	return nil, nil
}

// fakeKnowledgeStore records saved patterns; other methods are unused by MineSolutionPatterns.
type fakeKnowledgeStore struct {
	KnowledgeStore
	saved   []SolutionPattern
	deleted []string
}

func (f *fakeKnowledgeStore) DeleteSolutionPattern(ctx context.Context, problemClass, solutionPattern, repoType string) error {
	f.deleted = append(f.deleted, problemClass+"|"+solutionPattern+"|"+repoType)
	return nil
}

func (f *fakeKnowledgeStore) SaveSolutionPattern(ctx context.Context, p SolutionPattern) error {
	f.saved = append(f.saved, p)
	return nil
}

func makeArtifacts(pattern string, successes, failures int) []ArtifactData {
	var out []ArtifactData
	for i := 0; i < successes+failures; i++ {
		out = append(out, ArtifactData{
			ID:              fmt.Sprintf("%s-%d", pattern, i),
			ProblemClass:    "bug",
			SolutionPattern: pattern,
			RepoType:        "go",
			Success:         i < successes,
		})
	}
	return out
}

// permissiveConfig lets a pattern be saved whatever its rate, so the test can
// observe the computed rate itself.
func permissiveConfig() EngineConfig {
	return EngineConfig{
		MinOccurrencesForPattern:   2,
		MinSuccessRateForPattern:   0,
		MaxExampleArtifacts:        5,
		PatternConfidenceThreshold: 0,
		AnalysisBatchSize:          500,
	}
}

func runMine(t *testing.T, cfg EngineConfig, artifacts []ArtifactData) []SolutionPattern {
	t.Helper()
	store := &fakeKnowledgeStore{}
	e := NewEngine(&fakeArtifactQuery{artifacts: artifacts}, store, cfg)
	if err := e.MineSolutionPatterns(context.Background()); err != nil {
		t.Fatalf("MineSolutionPatterns: %v", err)
	}
	return store.saved
}

func TestMineSolutionPatterns_MixedOutcomes(t *testing.T) {
	saved := runMine(t, permissiveConfig(), makeArtifacts("retry", 2, 2))
	if len(saved) != 1 {
		t.Fatalf("saved %d patterns, want 1", len(saved))
	}
	if saved[0].SuccessRate != 0.5 {
		t.Errorf("SuccessRate = %v, want 0.5", saved[0].SuccessRate)
	}
	if saved[0].OccurrenceCount != 4 {
		t.Errorf("OccurrenceCount = %d, want 4", saved[0].OccurrenceCount)
	}
}

func TestMineSolutionPatterns_AllFailed(t *testing.T) {
	saved := runMine(t, permissiveConfig(), makeArtifacts("retry", 0, 3))
	if len(saved) != 1 {
		t.Fatalf("saved %d patterns, want 1", len(saved))
	}
	if saved[0].SuccessRate != 0 {
		t.Errorf("SuccessRate = %v, want 0", saved[0].SuccessRate)
	}
}

func TestMineSolutionPatterns_MostlyFailedPatternRejectedByDefaultThreshold(t *testing.T) {
	// 3 successes, 7 failures: rate 0.3 < default MinSuccessRateForPattern (0.6).
	saved := runMine(t, DefaultEngineConfig(), makeArtifacts("retry", 3, 7))
	if len(saved) != 0 {
		t.Errorf("saved %d patterns, want 0 for a 30%% success rate", len(saved))
	}
}

func TestMineSolutionPatterns_ReliablePatternStillSaved(t *testing.T) {
	// 9 successes, 1 failure: rate 0.9, 10 occurrences.
	saved := runMine(t, DefaultEngineConfig(), makeArtifacts("retry", 9, 1))
	if len(saved) != 1 {
		t.Fatalf("saved %d patterns, want 1", len(saved))
	}
	if saved[0].SuccessRate != 0.9 {
		t.Errorf("SuccessRate = %v, want 0.9", saved[0].SuccessRate)
	}
}

func TestMineSolutionPatterns_NoArtifacts(t *testing.T) {
	saved := runMine(t, DefaultEngineConfig(), nil)
	if len(saved) != 0 {
		t.Errorf("saved %d patterns, want 0", len(saved))
	}
}

func TestMineSolutionPatterns_SkipsArtifactsWithoutPatternOrClass(t *testing.T) {
	artifacts := makeArtifacts("retry", 2, 0)
	artifacts = append(artifacts,
		ArtifactData{ID: "a", ProblemClass: "bug", Success: false},
		ArtifactData{ID: "b", SolutionPattern: "retry", Success: false},
	)
	saved := runMine(t, permissiveConfig(), artifacts)
	if len(saved) != 1 {
		t.Fatalf("saved %d patterns, want 1", len(saved))
	}
	if saved[0].SuccessRate != 1 || saved[0].OccurrenceCount != 2 {
		t.Errorf("rate/count = %v/%d, want 1/2", saved[0].SuccessRate, saved[0].OccurrenceCount)
	}
}

func TestMineSolutionPatterns_DeletesStaleRowOfDisqualifiedPattern(t *testing.T) {
	// "flaky" now has 3 successes and 7 failures (0.3): below the default threshold,
	// so its old inflated row must be removed. "solid" still qualifies and is saved.
	artifacts := append(makeArtifacts("flaky", 3, 7), makeArtifacts("solid", 9, 1)...)
	store := &fakeKnowledgeStore{}
	e := NewEngine(&fakeArtifactQuery{artifacts: artifacts}, store, DefaultEngineConfig())

	if err := e.MineSolutionPatterns(context.Background()); err != nil {
		t.Fatalf("MineSolutionPatterns: %v", err)
	}

	if len(store.deleted) != 1 || store.deleted[0] != "bug|flaky|go" {
		t.Errorf("deleted = %v, want [bug|flaky|go]", store.deleted)
	}
	if len(store.saved) != 1 || store.saved[0].SolutionPattern != "solid" {
		t.Errorf("saved = %+v, want only the solid pattern", store.saved)
	}
}
