package evolution_loop

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/generators"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/google/uuid"
)

// RunResult summarises the outcome of a completed evolution loop run.
type RunResult struct {
	RunID            string
	HypothesesTested int
	AutoApplied      int
	WikiPagesUpdated int
	DurationMs       int64
}

// CycleResult summarises the outcome of a RunCycle call.
type CycleResult struct {
	HypothesesGenerated int
	TokensUsed          int
	PartialScoring      bool
	CategoryBreakdown   map[string]int
}

// EvolutionLoop orchestrates hypothesis generation, experiment execution,
// evaluation, and persistence for a single time-bounded evolution cycle.
type EvolutionLoop struct {
	store     EvolutionStore
	configFn  func() config.EvolutionConfig
	langFn    func() string
	llmClient llm.Client
	// applyFn is retained for backward-compat but usage is removed (T8).
	// Deprecated: auto-apply paths have been removed. This field is no-op.
	applyFn func(h *db.EvolutionHypothesis) error
	wikiFn  func(ctx context.Context, h *db.EvolutionHypothesis) (string, error)
	mu      sync.Mutex
	running bool

	// New fields wired by T8 for target-project analysis via RunCycle.
	baselineBuilder baseline.Builder
	staticScorer    scoring.StaticScorer
	llmJudge        scoring.LLMJudge  // nil → skip LLM scoring
	proposalWriter  ProposalWriter     // nil → RunCycle returns error
	projectRoot     string
}

// LoopOption configures optional behaviour of the EvolutionLoop.
type LoopOption func(*EvolutionLoop)

// WithApplyFn sets the callback invoked when an experiment is auto-applied.
// Deprecated: auto-apply paths have been removed. Setting this option is a no-op.
func WithApplyFn(fn func(*db.EvolutionHypothesis) error) LoopOption {
	return func(l *EvolutionLoop) { l.applyFn = fn }
}

// WithLangFn sets a callback that returns the active UI language code ("en",
// "sk", …). The language is read once at run start and passed to the hypothesis
// generator and experiment runner so that LLM prompts and seed descriptions are
// returned in the user's chosen language. When not set the loop defaults to "en".
func WithLangFn(fn func() string) LoopOption {
	return func(l *EvolutionLoop) { l.langFn = fn }
}

// WithWikiFn sets the callback invoked when an experiment produces a proposal.
// It should create a wiki page and return its ID.
func WithWikiFn(fn func(context.Context, *db.EvolutionHypothesis) (string, error)) LoopOption {
	return func(l *EvolutionLoop) { l.wikiFn = fn }
}

// WithBaselineBuilder sets the baseline.Builder used by RunCycle.
func WithBaselineBuilder(b baseline.Builder) LoopOption {
	return func(l *EvolutionLoop) { l.baselineBuilder = b }
}

// WithStaticScorer sets the scoring.StaticScorer used by RunCycle.
func WithStaticScorer(s scoring.StaticScorer) LoopOption {
	return func(l *EvolutionLoop) { l.staticScorer = s }
}

// WithLLMJudge sets the scoring.LLMJudge used by RunCycle.
// Pass nil (or omit) to skip LLM scoring and use static-only blending.
func WithLLMJudge(j scoring.LLMJudge) LoopOption {
	return func(l *EvolutionLoop) { l.llmJudge = j }
}

// WithProposalWriter sets the ProposalWriter used by RunCycle.
func WithProposalWriter(w ProposalWriter) LoopOption {
	return func(l *EvolutionLoop) { l.proposalWriter = w }
}

// WithProjectRoot sets the project root directory used by RunCycle.
func WithProjectRoot(root string) LoopOption {
	return func(l *EvolutionLoop) { l.projectRoot = root }
}

// NewEvolutionLoop constructs an EvolutionLoop.
// llmClient may be nil; a nil value means "no LLM available, use static/simulated behavior".
func NewEvolutionLoop(store EvolutionStore, configFn func() config.EvolutionConfig, llmClient llm.Client, opts ...LoopOption) *EvolutionLoop {
	l := &EvolutionLoop{store: store, configFn: configFn, llmClient: llmClient}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// IsRunning reports whether a run is currently in progress. Thread-safe.
func (l *EvolutionLoop) IsRunning() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}

// logf emits a slog.Info line with the loop prefix.
func (l *EvolutionLoop) logf(format string, args ...any) {
	slog.Info(fmt.Sprintf("evolution loop: "+format, args...))
}

// RunCycle executes one target-project analysis cycle:
//  1. Builds a baseline bundle via the injected Builder.
//  2. Redacts secrets from the bundle.
//  3. Runs all configured generators.
//  4. Scores each hypothesis (static always; LLM until token budget exhausted).
//  5. Writes each proposal via the injected ProposalWriter.
//
// It returns ErrTokenCapRequired when cfg.MaxTokensPerCycle <= 0, and an
// error when no ProposalWriter is configured.
func (l *EvolutionLoop) RunCycle(ctx context.Context) (CycleResult, error) {
	cfg := l.configFn()

	if cfg.MaxTokensPerCycle <= 0 {
		return CycleResult{}, fmt.Errorf("loop: run cycle: %w", config.ErrTokenCapRequired)
	}
	if l.proposalWriter == nil {
		return CycleResult{}, fmt.Errorf("loop: proposal writer not configured")
	}

	// Resolve effective baseline builder and static scorer — use injected
	// instances if present, fall back to reasonable defaults so existing
	// call sites that don't wire these deps don't break.
	bldr := l.baselineBuilder
	if bldr == nil {
		// TODO: wire real Vexor client in a future task.
		slog.Warn("evolution loop: RunCycle: no baseline builder configured, using nil-safe default")
		bldr = baseline.New(baseline.Dependencies{})
	}
	ss := l.staticScorer
	if ss == nil {
		ss = scoring.NewStaticScorer()
	}

	// 1. Build baseline bundle.
	root := l.projectRoot
	if root == "" {
		root = "."
	}
	bundle, err := bldr.Build(ctx, root, cfg.BaselineLimits)
	if err != nil {
		return CycleResult{}, fmt.Errorf("loop: build baseline: %w", err)
	}
	redacted := baseline.Redact(&bundle)

	// 2. Run generators.
	gens := generators.Registry(cfg.AllowedEvolutionCategories, cfg.StratusSelfEnabled)
	maxPerGen := cfg.MaxHypothesesPerRun
	if maxPerGen <= 0 {
		maxPerGen = 5
	}
	var all []scoring.Hypothesis
	for _, g := range gens {
		all = append(all, g.Generate(*redacted, maxPerGen)...)
	}

	// 3. Score and write proposals.
	tokensUsed := 0
	partial := false

	for _, h := range all {
		static := ss.Score(h, *redacted)

		var llmScores scoring.LLMScores
		if l.llmJudge != nil {
			remaining := cfg.MaxTokensPerCycle - tokensUsed
			perCall := cfg.ScoringWeights.MaxTokensPerJudgeCall
			if perCall <= 0 {
				perCall = remaining
			}
			if perCall > remaining {
				perCall = remaining
			}
			if perCall <= 0 {
				partial = true // token budget exhausted — skip remaining LLM calls
			} else {
				ls, used, jerr := l.llmJudge.Score(ctx, h, *redacted, perCall)
				if jerr != nil {
					l.logf("llm judge error for %q: %v", h.Title, jerr)
					partial = true
					// keep llmScores = zero values; proceed with static-only
				} else {
					llmScores = ls
					tokensUsed += used
				}
			}
		}

		blended := scoring.Blend(static, llmScores, cfg.ScoringWeights)

		_, werr := l.proposalWriter.Write(ctx, ProposalInput{
			Hypothesis: h,
			Final:      blended.Final,
			Static:     blended.Static,
			LLM:        blended.LLM,
			Breakdown:  blended.Breakdown,
		})
		if werr != nil {
			l.logf("proposal writer error for %q: %v", h.Title, werr)
			// don't abort the whole cycle on one write failure
		}
	}

	return CycleResult{
		HypothesesGenerated: len(all),
		TokensUsed:          tokensUsed,
		PartialScoring:      partial,
		CategoryBreakdown:   countByCategory(all),
	}, nil
}

// countByCategory returns a map of category → count for the given hypotheses.
func countByCategory(all []scoring.Hypothesis) map[string]int {
	m := make(map[string]int, len(all))
	for _, h := range all {
		m[h.Category]++
	}
	return m
}

// Run executes one full evolution cycle. It returns an error if a run is
// already in progress, if persisting the run record fails, or if the context
// is cancelled before the cycle begins.
//
// timeoutOverrideMs, when > 0, overrides the configured TimeoutMs for this
// run only. categories, when non-empty, overrides the configured Categories
// for this run only. Pass 0 and nil to use the values from config.
//
// Internally it uses context.WithTimeout derived from the effective TimeoutMs
// so that individual experiments are cancelled automatically when the budget
// expires.
func (l *EvolutionLoop) Run(ctx context.Context, triggerType string, timeoutOverrideMs int64, categories []string) (*RunResult, error) {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return nil, fmt.Errorf("evolution loop: run: already running")
	}
	l.running = true
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	cfg := l.configFn()

	// Apply per-call overrides.
	if timeoutOverrideMs > 0 {
		cfg.TimeoutMs = timeoutOverrideMs
	}
	if len(categories) > 0 {
		cfg.Categories = categories
	}

	start := time.Now()

	run := &db.EvolutionRun{
		ID:          uuid.NewString(),
		TriggerType: triggerType,
		Status:      "running",
		TimeoutMs:   cfg.TimeoutMs,
		Metadata:    map[string]any{},
		StartedAt:   start.UTC().Format(time.RFC3339Nano),
	}

	if err := l.store.SaveRun(run); err != nil {
		return nil, fmt.Errorf("evolution loop: save run: %w", err)
	}

	slog.Info("evolution loop started", "run_id", run.ID, "trigger", triggerType)

	tctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
	defer cancel()

	lang := "en"
	if l.langFn != nil {
		lang = l.langFn()
	}

	result, finalStatus, runErr := l.execute(tctx, run, cfg, lang)

	// Always persist final state.
	durationMs := time.Since(start).Milliseconds()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	run.Status = finalStatus
	run.DurationMs = durationMs
	run.CompletedAt = &now
	if runErr != nil {
		run.ErrorMessage = runErr.Error()
	}
	if result != nil {
		run.HypothesesCount = result.HypothesesTested
		run.AutoApplied = result.AutoApplied
		run.WikiPagesUpdated = result.WikiPagesUpdated
	}

	if err := l.store.UpdateRun(run); err != nil {
		slog.Error("evolution loop: update run failed", "run_id", run.ID, "err", err)
	}

	slog.Info("evolution loop finished",
		"run_id", run.ID,
		"status", finalStatus,
		"duration_ms", durationMs,
	)

	if runErr != nil {
		return nil, fmt.Errorf("evolution loop: run: %w", runErr)
	}
	result.RunID = run.ID
	result.DurationMs = durationMs
	return result, nil
}

// execute performs the core hypothesis → experiment → evaluate pipeline.
// It returns a partial RunResult, the final status string, and any error.
func (l *EvolutionLoop) execute(
	ctx context.Context,
	run *db.EvolutionRun,
	cfg config.EvolutionConfig,
	lang string,
) (*RunResult, string, error) {
	generator := NewHypothesisGenerator(l.store, l.llmClient)
	runner := NewExperimentRunner(l.llmClient)
	evaluator := NewEvaluator(l.configFn)

	hypotheses, err := generator.GenerateWithLang(ctx, run.ID, cfg.Categories, cfg.MaxHypothesesPerRun, lang)
	if err != nil {
		return nil, "failed", fmt.Errorf("generate hypotheses: %w", err)
	}

	result := &RunResult{}

	deadline, hasDeadline := ctx.Deadline()

	for i := range hypotheses {
		// Bail out early if the timeout budget is exhausted.
		select {
		case <-ctx.Done():
			slog.Info("evolution loop: experiment budget reached, stopping early",
				"run_id", run.ID,
				"hypotheses_tested", result.HypothesesTested,
			)
			return result, "timeout", nil
		default:
		}

		h := &hypotheses[i]

		// Persist the hypothesis before running the experiment so we have a
		// record even if the context is cancelled mid-run.
		if err := l.store.SaveHypothesis(h); err != nil {
			slog.Error("evolution loop: save hypothesis failed", "err", err)
			continue
		}

		var expResult *ExperimentResult
		if hasDeadline {
			remaining := time.Until(deadline)
			budget := remaining / time.Duration(len(hypotheses)-i)
			if budget < 5*time.Second {
				budget = 5 * time.Second
			}
			expCtx, expCancel := context.WithTimeout(ctx, budget)
			expResult = runner.ExecuteWithLang(expCtx, h, lang)
			expCancel()
		} else {
			expResult = runner.ExecuteWithLang(ctx, h, lang)
		}
		if expResult.Error != nil {
			// Context cancelled during experiment — stop the loop.
			if ctx.Err() != nil {
				return result, "timeout", nil
			}
			slog.Error("evolution loop: experiment error", "hypothesis_id", h.ID, "err", expResult.Error)
			continue
		}

		decision, reason, confidence := evaluator.Evaluate(h, expResult, cfg)

		h.ExperimentMetric = expResult.Metric
		h.Confidence = confidence
		h.Decision = decision
		h.DecisionReason = reason

		if err := l.store.UpdateHypothesis(h); err != nil {
			slog.Error("evolution loop: update hypothesis failed", "err", err)
		}

		result.HypothesesTested++

		switch decision {
		case "proposal_created":
			if l.wikiFn != nil {
				pageID, wikiErr := l.wikiFn(ctx, h)
				if wikiErr != nil {
					slog.Error("evolution loop: wiki page creation failed",
						"hypothesis_id", h.ID, "err", wikiErr)
				} else {
					h.WikiPageID = &pageID
					if updateErr := l.store.UpdateHypothesis(h); updateErr != nil {
						slog.Error("evolution loop: update hypothesis with wiki page ID",
							"hypothesis_id", h.ID, "err", updateErr)
					}
					result.WikiPagesUpdated++
				}
			} else {
				result.WikiPagesUpdated++
			}
		}
	}

	// Update run experiment count while we still have the numbers.
	run.ExperimentsRun = result.HypothesesTested

	return result, "completed", nil
}
