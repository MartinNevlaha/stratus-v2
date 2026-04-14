package code_analyst

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/google/uuid"
)

// WikiFn creates or updates a wiki page for code findings on a file.
// Returns the wiki page ID.
type WikiFn func(ctx context.Context, filePath string, findings []db.CodeFinding) (string, error)

// ConfigFn returns the current CodeAnalysisConfig.
type ConfigFn func() CodeAnalysisConfig

// CodeAnalysisConfig mirrors config.CodeAnalysisConfig but is defined here to avoid import cycle.
type CodeAnalysisConfig struct {
	Enabled             bool
	MaxFilesPerRun      int
	TokenBudgetPerRun   int
	MinChurnScore       float64
	ConfidenceThreshold float64
	ScanInterval        int
	IncludeGitHistory   bool
	GitHistoryDepth     int
	Categories          []string
}

// LangFn is a callback that returns the active UI language code ("en", "sk", …).
type LangFn func() string

// Engine orchestrates the collect -> rank -> analyze -> persist pipeline.
type Engine struct {
	store     CodeAnalystStore
	collector *Collector
	ranker    *Ranker
	analyzer  *Analyzer
	dedup     *Deduplicator
	wikiFn    WikiFn
	configFn  ConfigFn
	langFn    LangFn // optional; returns active UI language at run time
	projRoot  string
	mu        sync.Mutex
	running   bool
}

// NewEngine constructs an Engine with all sub-components wired together.
// llmClient may be nil; in that case AnalyzeFile will return an error for each
// file but the pipeline will still complete.
func NewEngine(store CodeAnalystStore, llmClient llm.Client, projRoot string, configFn ConfigFn, wikiFn WikiFn) *Engine {
	return &Engine{
		store:    store,
		wikiFn:   wikiFn,
		configFn: configFn,
		projRoot: projRoot,
		// Sub-components are constructed here with zero-value / default configs;
		// they are rebuilt each Run() call with the live config.
		collector: NewCollector(projRoot),
		dedup:     NewDeduplicator(store, projRoot, 0),
		// analyzer and ranker are re-created each run with live config values.
		analyzer: NewAnalyzer(llmClient, projRoot, nil, 0),
	}
}

// SetLangFn sets the callback used to retrieve the active UI language code at
// the start of each Run(). When not set the Engine defaults to "en". Call this
// after NewEngine and before the first Run() to enable localised LLM prompts.
func (e *Engine) SetLangFn(fn LangFn) {
	e.langFn = fn
}

// IsRunning reports whether a run is currently in progress. Thread-safe.
func (e *Engine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// Run executes a full code analysis cycle. Returns RunResult or error.
func (e *Engine) Run(ctx context.Context, triggerType string, categories []string) (*RunResult, error) {
	// Step 1: acquire mutex — fail fast if already running.
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil, fmt.Errorf("code analyst: engine: already running")
	}
	e.running = true
	e.mu.Unlock()

	start := time.Now()

	// Step 2: load config.
	config := e.configFn()

	// Resolve effective categories: call-site override > config.Categories.
	effectiveCategories := config.Categories
	if len(categories) > 0 {
		effectiveCategories = categories
	}

	// Resolve active language — read once per run so UI changes take effect.
	lang := "en"
	if e.langFn != nil {
		lang = e.langFn()
	}

	// Build run-specific sub-components with live config.
	ranker := NewRanker(RankerConfig{
		MaxFiles:      config.MaxFilesPerRun,
		MinChurnScore: config.MinChurnScore,
	})
	analyzer := NewAnalyzer(e.analyzer.llmClient, e.projRoot, effectiveCategories, config.ConfidenceThreshold).WithLang(lang)

	// Step 3: get HEAD commit hash.
	commitHash, err := getHeadCommit(ctx, e.projRoot)
	if err != nil {
		// Non-fatal for non-git projects; log and continue with empty hash.
		slog.Warn("code analyst: engine: could not get HEAD commit", "err", err)
	}

	// Step 4: create CodeAnalysisRun with status "running".
	runID := generateRunID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	run := &db.CodeAnalysisRun{
		ID:            runID,
		Status:        "running",
		GitCommitHash: commitHash,
		Metadata:      map[string]any{"trigger_type": triggerType},
		StartedAt:     now,
	}

	if saveErr := e.store.SaveRun(run); saveErr != nil {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
		return nil, fmt.Errorf("code analyst: engine: save run: %w", saveErr)
	}

	slog.Info("code analyst: engine: run started", "run_id", runID, "trigger", triggerType)

	// Use a deferred finalizer to always update run status and release mutex.
	var runErr error
	var result *RunResult

	defer func() {
		durationMs := time.Since(start).Milliseconds()
		completedAt := time.Now().UTC().Format(time.RFC3339Nano)

		finalStatus := "completed"
		if runErr != nil {
			finalStatus = "failed"
		}

		run.Status = finalStatus
		run.DurationMs = durationMs
		run.CompletedAt = &completedAt
		if runErr != nil {
			run.ErrorMessage = runErr.Error()
		}
		if result != nil {
			run.FilesScanned = result.FilesScanned
			run.FilesAnalyzed = result.FilesAnalyzed
			run.FindingsCount = result.FindingsCount
			run.WikiPagesCreated = result.WikiPagesCreated
			run.WikiPagesUpdated = result.WikiPagesUpdated
			run.TokensUsed = int64(result.TokensUsed)
		}

		if updateErr := e.store.UpdateRun(run); updateErr != nil {
			slog.Error("code analyst: engine: update run failed", "run_id", runID, "err", updateErr)
		}

		slog.Info("code analyst: engine: run finished",
			"run_id", runID,
			"status", finalStatus,
			"duration_ms", durationMs,
		)

		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
	}()

	result, runErr = e.execute(ctx, run, config, ranker, analyzer)

	if runErr != nil {
		return nil, fmt.Errorf("code analyst: engine: run: %w", runErr)
	}

	result.RunID = runID
	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// execute performs the core collect → rank → dedup → analyze → persist pipeline.
func (e *Engine) execute(
	ctx context.Context,
	run *db.CodeAnalysisRun,
	config CodeAnalysisConfig,
	ranker *Ranker,
	analyzer *Analyzer,
) (*RunResult, error) {
	result := &RunResult{}

	// Step 5: collect signals.
	gitDepth := config.GitHistoryDepth
	if gitDepth <= 0 {
		gitDepth = 50
	}
	signals, err := e.collector.CollectAll(ctx, gitDepth)
	if err != nil {
		// If the project is not a git repository (or has no commits), treat
		// as an empty signal set rather than a fatal failure.
		if isGitAbsenceError(err) {
			slog.Warn("code analyst: engine: project has no git history, skipping analysis",
				"run_id", run.ID, "err", err)
			e.saveDailyMetric(run.ID, result, nil, nil)
			return result, nil
		}
		return nil, fmt.Errorf("collect signals: %w", err)
	}

	result.FilesScanned = len(signals)

	if len(signals) == 0 {
		// Nothing to do: save an empty metric and return.
		e.saveDailyMetric(run.ID, result, nil, signals)
		return result, nil
	}

	// Step 6: rank files.
	ranked := ranker.Rank(signals, nil)

	// Step 7: dedup — filter files whose git hash hasn't changed.
	toAnalyze, err := e.dedup.FilterUnchanged(ctx, ranked)
	if err != nil {
		return nil, fmt.Errorf("dedup: %w", err)
	}

	// Track findings by severity/category for the daily metric.
	findingsBySeverity := make(map[string]int)
	findingsByCategory := make(map[string]int)

	var totalChurn, totalCoverage float64
	for _, fs := range ranked {
		totalChurn += fs.ChurnRate
		totalCoverage += fs.Coverage
	}

	// Step 8: analyze each file.
	totalTokens := 0
	tokenBudget := config.TokenBudgetPerRun

	for _, file := range toAnalyze {
		// Check context deadline.
		select {
		case <-ctx.Done():
			slog.Info("code analyst: engine: context cancelled, stopping analysis early",
				"run_id", run.ID,
				"files_analyzed", result.FilesAnalyzed,
			)
			// Return partial result as a failure since context was cancelled.
			return result, fmt.Errorf("context cancelled: %w", ctx.Err())
		default:
		}

		// Token budget check: stop if we've used >= 80% of the budget.
		if tokenBudget > 0 && totalTokens >= int(float64(tokenBudget)*0.8) {
			slog.Info("code analyst: engine: token budget exhausted, stopping early",
				"run_id", run.ID,
				"tokens_used", totalTokens,
				"budget", tokenBudget,
			)
			break
		}

		// Step 8a: call analyzer.
		analysisResult, analyzeErr := analyzer.AnalyzeFile(ctx, file, "")
		if analyzeErr != nil {
			// Context cancellation is fatal; other errors are per-file and non-fatal.
			if ctx.Err() != nil {
				return result, fmt.Errorf("context cancelled during analysis: %w", ctx.Err())
			}
			slog.Warn("code analyst: engine: analyze file failed",
				"run_id", run.ID, "file", file.FilePath, "err", analyzeErr)
			continue
		}

		totalTokens += analysisResult.TokensUsed
		result.FilesAnalyzed++
		result.TokensUsed += analysisResult.TokensUsed

		// Step 8b: convert findings to db.CodeFinding and save each.
		dbFindings := make([]db.CodeFinding, 0, len(analysisResult.Findings))
		for _, f := range analysisResult.Findings {
			dbF := db.CodeFinding{
				RunID:       run.ID,
				FilePath:    file.FilePath,
				Category:    f.Category,
				Severity:    f.Severity,
				Title:       f.Title,
				Description: f.Description,
				LineStart:   f.LineStart,
				LineEnd:     f.LineEnd,
				Confidence:  f.Confidence,
				Suggestion:  f.Suggestion,
				Evidence:    f.Evidence,
			}
			if saveErr := e.store.SaveFinding(&dbF); saveErr != nil {
				slog.Warn("code analyst: engine: save finding failed",
					"run_id", run.ID, "file", file.FilePath, "err", saveErr)
				continue
			}
			dbFindings = append(dbFindings, dbF)
			result.FindingsCount++
			findingsBySeverity[f.Severity]++
			findingsByCategory[f.Category]++
		}

		// Step 8c: call wikiFn if set and there are findings.
		if e.wikiFn != nil && len(dbFindings) > 0 {
			pageID, wikiErr := e.wikiFn(ctx, file.FilePath, dbFindings)
			if wikiErr != nil {
				slog.Warn("code analyst: engine: wiki fn failed",
					"run_id", run.ID, "file", file.FilePath, "err", wikiErr)
			} else {
				_ = pageID
				result.WikiPagesUpdated++
			}
		}

		// Step 8d: update dedup cache with the file's git hash.
		gitHash := ""
		if file.LastGitHash != nil {
			gitHash = *file.LastGitHash
		}
		if cacheErr := e.dedup.UpdateCache(file.FilePath, gitHash, run.ID, file.CompositeScore, len(dbFindings)); cacheErr != nil {
			slog.Warn("code analyst: engine: update cache failed",
				"run_id", run.ID, "file", file.FilePath, "err", cacheErr)
		}
	}

	// Step 9: save daily metric.
	avgChurn := 0.0
	avgCoverage := 0.0
	if len(ranked) > 0 {
		avgChurn = totalChurn / float64(len(ranked))
		avgCoverage = totalCoverage / float64(len(ranked))
	}
	e.saveDailyMetric(run.ID, result, &dailyMetricExtra{
		findingsBySeverity: findingsBySeverity,
		findingsByCategory: findingsByCategory,
		avgChurn:           avgChurn,
		avgCoverage:        avgCoverage,
	}, signals)

	return result, nil
}

type dailyMetricExtra struct {
	findingsBySeverity map[string]int
	findingsByCategory map[string]int
	avgChurn           float64
	avgCoverage        float64
}

func (e *Engine) saveDailyMetric(runID string, result *RunResult, extra *dailyMetricExtra, signals []FileSignals) {
	bySeverity := map[string]int{}
	byCategory := map[string]int{}
	avgChurn := 0.0
	avgCoverage := 0.0

	if extra != nil {
		bySeverity = extra.findingsBySeverity
		byCategory = extra.findingsByCategory
		avgChurn = extra.avgChurn
		avgCoverage = extra.avgCoverage
	}

	metric := &db.CodeQualityMetric{
		MetricDate:         time.Now().UTC().Format("2006-01-02"),
		TotalFiles:         len(signals),
		FilesAnalyzed:      result.FilesAnalyzed,
		FindingsTotal:      result.FindingsCount,
		FindingsBySeverity: bySeverity,
		FindingsByCategory: byCategory,
		AvgChurnScore:      avgChurn,
		AvgCoverage:        avgCoverage,
	}
	if saveErr := e.store.SaveMetric(metric); saveErr != nil {
		slog.Warn("code analyst: engine: save metric failed", "run_id", runID, "err", saveErr)
	}
}

// isGitAbsenceError reports whether err indicates git is unavailable or the
// directory is not a git repository.
func isGitAbsenceError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not a git repository") ||
		strings.Contains(msg, "exit status 128") ||
		strings.Contains(msg, "does not have any commits")
}

// getHeadCommit returns the current HEAD commit hash for the project.
func getHeadCommit(ctx context.Context, projRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = projRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("code analyst: engine: get head commit: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// generateRunID returns a new unique run ID.
func generateRunID() string {
	return uuid.NewString()
}
