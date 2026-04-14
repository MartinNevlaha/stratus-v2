package code_analyst

import (
	"math"
	"testing"
)

const floatTolerance = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < floatTolerance
}

func defaultConfig() RankerConfig {
	return RankerConfig{
		MaxFiles:        10,
		MinChurnScore:   0.1,
		CoverageDefault: 0.5,
		ComplexityCap:   3.0,
	}
}

func TestRanker_Rank_Basic(t *testing.T) {
	// 5 files with varying churn/lines — verify top-3 are in correct order.
	// maxCommitCount = 10 (file_a)
	//
	// file_a: churn=1.0, coverage=0.5(default), complexity=min(500/100,3)=3.0 → composite=1.0*0.5*3.0=1.5
	// file_b: churn=0.8, coverage=0.5(default), complexity=min(200/100,3)=2.0 → composite=0.8*0.5*2.0=0.8
	// file_c: churn=0.5, coverage=0.5(default), complexity=min(300/100,3)=3.0 → composite=0.5*0.5*3.0=0.75
	// file_d: churn=0.3, coverage=0.5(default), complexity=min(100/100,3)=1.0 → composite=0.3*0.5*1.0=0.15
	// file_e: churn=0.1, coverage=0.5(default), complexity=min(50/100,3)=0.5  → composite=0.1*0.5*0.5=0.025 (filtered out < 0.1)
	signals := []FileSignals{
		{FilePath: "file_a.go", CommitCount: 10, LineCount: 500},
		{FilePath: "file_b.go", CommitCount: 8, LineCount: 200},
		{FilePath: "file_c.go", CommitCount: 5, LineCount: 300},
		{FilePath: "file_d.go", CommitCount: 3, LineCount: 100},
		{FilePath: "file_e.go", CommitCount: 1, LineCount: 50},
	}

	r := NewRanker(RankerConfig{MaxFiles: 3, MinChurnScore: 0.1, CoverageDefault: 0.5, ComplexityCap: 3.0})
	got := r.Rank(signals, nil)

	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}
	expectedOrder := []string{"file_a.go", "file_b.go", "file_c.go"}
	for i, path := range expectedOrder {
		if got[i].FilePath != path {
			t.Errorf("position %d: expected %s, got %s", i, path, got[i].FilePath)
		}
	}
	// Verify composite score for top file
	if !approxEqual(got[0].CompositeScore, 1.5) {
		t.Errorf("file_a composite score: expected 1.5, got %f", got[0].CompositeScore)
	}
}

func TestRanker_Rank_FiltersTestFiles(t *testing.T) {
	signals := []FileSignals{
		{FilePath: "main.go", CommitCount: 10, LineCount: 200, TestFile: false},
		{FilePath: "main_test.go", CommitCount: 10, LineCount: 200, TestFile: true},
		{FilePath: "util_test.go", CommitCount: 8, LineCount: 150, TestFile: true},
	}

	r := NewRanker(defaultConfig())
	got := r.Rank(signals, nil)

	if len(got) != 1 {
		t.Fatalf("expected 1 result (non-test file), got %d", len(got))
	}
	if got[0].FilePath != "main.go" {
		t.Errorf("expected main.go, got %s", got[0].FilePath)
	}
}

func TestRanker_Rank_WithCoverageMap(t *testing.T) {
	// file_high_cov: churn=1.0, coverage=0.9, complexity=2.0 → composite=1.0*0.1*2.0=0.2
	// file_low_cov:  churn=1.0, coverage=0.1, complexity=2.0 → composite=1.0*0.9*2.0=1.8
	// file_no_cov:   churn=1.0, coverage=0.5(default), complexity=2.0 → composite=1.0*0.5*2.0=1.0
	signals := []FileSignals{
		{FilePath: "high_cov.go", CommitCount: 10, LineCount: 200},
		{FilePath: "low_cov.go", CommitCount: 10, LineCount: 200},
		{FilePath: "no_cov.go", CommitCount: 10, LineCount: 200},
	}
	coverageMap := map[string]float64{
		"high_cov.go": 0.9,
		"low_cov.go":  0.1,
	}

	r := NewRanker(defaultConfig())
	got := r.Rank(signals, coverageMap)

	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}
	// low coverage should rank highest
	if got[0].FilePath != "low_cov.go" {
		t.Errorf("expected low_cov.go first, got %s", got[0].FilePath)
	}
	if got[1].FilePath != "no_cov.go" {
		t.Errorf("expected no_cov.go second, got %s", got[1].FilePath)
	}
	if got[2].FilePath != "high_cov.go" {
		t.Errorf("expected high_cov.go third, got %s", got[2].FilePath)
	}
	// Verify coverage stored correctly
	if !approxEqual(got[0].Coverage, 0.1) {
		t.Errorf("low_cov.go coverage: expected 0.1, got %f", got[0].Coverage)
	}
	if !approxEqual(got[2].Coverage, 0.9) {
		t.Errorf("high_cov.go coverage: expected 0.9, got %f", got[2].Coverage)
	}
}

func TestRanker_Rank_MinChurnFilter(t *testing.T) {
	// maxCommitCount = 10
	// file_a: churn=1.0, complexity=1.0, coverage=0.5 → composite=0.5  (pass)
	// file_b: churn=0.2, complexity=1.0, coverage=0.5 → composite=0.1  (pass, equals threshold)
	// file_c: churn=0.1, complexity=1.0, coverage=0.5 → composite=0.05 (filtered)
	signals := []FileSignals{
		{FilePath: "file_a.go", CommitCount: 10, LineCount: 100},
		{FilePath: "file_b.go", CommitCount: 2, LineCount: 100},
		{FilePath: "file_c.go", CommitCount: 1, LineCount: 100},
	}

	r := NewRanker(RankerConfig{MaxFiles: 10, MinChurnScore: 0.1, CoverageDefault: 0.5, ComplexityCap: 3.0})
	got := r.Rank(signals, nil)

	// file_b composite = 0.2 * 0.5 * 1.0 = 0.1, exactly at threshold — should be included
	// file_c composite = 0.1 * 0.5 * 1.0 = 0.05, below threshold — excluded
	for _, score := range got {
		if score.CompositeScore < 0.1 {
			t.Errorf("file %s has composite score %f below MinChurnScore 0.1", score.FilePath, score.CompositeScore)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results after filtering, got %d", len(got))
	}
}

func TestRanker_Rank_ComplexityCap(t *testing.T) {
	// Very large file: lineCount=100000 → raw complexity=1000.0, capped at 3.0
	signals := []FileSignals{
		{FilePath: "huge.go", CommitCount: 5, LineCount: 100000},
		{FilePath: "normal.go", CommitCount: 5, LineCount: 100},
	}

	r := NewRanker(defaultConfig())
	got := r.Rank(signals, nil)

	var hugeScore *FileScore
	for i := range got {
		if got[i].FilePath == "huge.go" {
			hugeScore = &got[i]
			break
		}
	}
	if hugeScore == nil {
		t.Fatal("huge.go not found in results")
	}
	if !approxEqual(hugeScore.ComplexityProxy, 3.0) {
		t.Errorf("huge.go complexity proxy: expected 3.0 (capped), got %f", hugeScore.ComplexityProxy)
	}
}

func TestRanker_Rank_EmptyInput(t *testing.T) {
	r := NewRanker(defaultConfig())
	got := r.Rank(nil, nil)

	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(got))
	}
}

func TestRanker_Rank_AllTestFiles(t *testing.T) {
	signals := []FileSignals{
		{FilePath: "foo_test.go", CommitCount: 10, LineCount: 200, TestFile: true},
		{FilePath: "bar_test.go", CommitCount: 5, LineCount: 100, TestFile: true},
	}

	r := NewRanker(defaultConfig())
	got := r.Rank(signals, nil)

	if len(got) != 0 {
		t.Errorf("expected 0 results when all files are test files, got %d", len(got))
	}
}

func TestRanker_Rank_ZeroCommits(t *testing.T) {
	signals := []FileSignals{
		{FilePath: "file_a.go", CommitCount: 0, LineCount: 200},
		{FilePath: "file_b.go", CommitCount: 0, LineCount: 100},
	}

	r := NewRanker(defaultConfig())
	got := r.Rank(signals, nil)

	// All churn rates = 0, so all composite scores = 0, all filtered out by MinChurnScore
	if len(got) != 0 {
		t.Errorf("expected 0 results when all commits are 0 (scores below threshold), got %d", len(got))
	}
}

func TestRanker_Rank_MaxFilesDefault(t *testing.T) {
	// MaxFiles <= 0 should default to 10
	signals := make([]FileSignals, 20)
	for i := range signals {
		signals[i] = FileSignals{
			FilePath:    "file.go",
			CommitCount: 10,
			LineCount:   200,
		}
		// Use distinct paths
		signals[i].FilePath = "file_" + string(rune('a'+i)) + ".go"
	}

	r := NewRanker(RankerConfig{MaxFiles: 0, MinChurnScore: 0.0, CoverageDefault: 0.5, ComplexityCap: 3.0})
	got := r.Rank(signals, nil)

	if len(got) > 10 {
		t.Errorf("expected at most 10 results with MaxFiles=0 (default), got %d", len(got))
	}
}

func TestRanker_Rank_ConfigDefaults(t *testing.T) {
	// Zero-value config should use sensible defaults without panicking
	signals := []FileSignals{
		{FilePath: "file.go", CommitCount: 5, LineCount: 200},
	}

	r := NewRanker(RankerConfig{}) // all zero values
	got := r.Rank(signals, nil)
	// Should not panic; result may be empty or non-empty depending on defaults
	_ = got
}

func TestRanker_Rank_ChurnRateStoredCorrectly(t *testing.T) {
	signals := []FileSignals{
		{FilePath: "top.go", CommitCount: 10, LineCount: 200},
		{FilePath: "mid.go", CommitCount: 5, LineCount: 200},
	}

	r := NewRanker(defaultConfig())
	got := r.Rank(signals, nil)

	if len(got) < 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if !approxEqual(got[0].ChurnRate, 1.0) {
		t.Errorf("top.go churn rate: expected 1.0, got %f", got[0].ChurnRate)
	}
	if !approxEqual(got[1].ChurnRate, 0.5) {
		t.Errorf("mid.go churn rate: expected 0.5, got %f", got[1].ChurnRate)
	}
}

func TestRanker_Rank_OriginalSignalsStoredInScore(t *testing.T) {
	signals := []FileSignals{
		{FilePath: "file.go", CommitCount: 7, LineCount: 350, TechDebtMarkers: 5, Language: "go"},
	}

	r := NewRanker(defaultConfig())
	got := r.Rank(signals, nil)

	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].CommitCount != 7 {
		t.Errorf("CommitCount: expected 7, got %d", got[0].CommitCount)
	}
	if got[0].LineCount != 350 {
		t.Errorf("LineCount: expected 350, got %d", got[0].LineCount)
	}
	if got[0].TechDebtMarkers != 5 {
		t.Errorf("TechDebtMarkers: expected 5, got %d", got[0].TechDebtMarkers)
	}
}
