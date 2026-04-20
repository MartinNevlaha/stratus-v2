package insight

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/artifacts"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/code_analyst"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/ingest"
	evolution_baseline "github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
	evolution_scoring "github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/knowledge_engine"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/patterns"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/product_intelligence"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/proposals"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/routing"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/scorecards"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/trajectory_engine"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/workflow_intelligence"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/workflow_synthesis"
	"github.com/MartinNevlaha/stratus-v2/events"
	"github.com/google/uuid"
)

type Engine struct {
	database            *db.DB
	config              config.InsightConfig
	wikiCfg             config.WikiConfig
	evoCfg              config.EvolutionConfig
	lang                string
	scheduler           *Scheduler
	eventBus            events.EventBus
	eventStore          events.Store
	subscriptionID      events.SubscriptionID
	llmClient           llm.Client
	patternEngine       *patterns.Engine
	proposalEngine      *proposals.Engine
	scorecardEngine     *scorecards.Engine
	routingEngine       *routing.Engine
	workflowAnalyzer    *workflow_intelligence.WorkflowAnalyzer
	artifactBuilder     *artifacts.ArtifactBuilder
	knowledgeEngine     *knowledge_engine.Engine
	trajectoryRecorder  *trajectory_engine.TrajectoryRecorder
	trajectoryAnalyzer  *trajectory_engine.TrajectoryAnalyzer
	workflowSynthesizer *workflow_synthesis.Synthesizer
	productIntelligence *product_intelligence.Engine
	wikiEngine          *wiki_engine.WikiEngine
	evolutionLoop       *evolution_loop.EvolutionLoop
	wikiSynth           *wiki_engine.Synthesizer
	wikiLinker          *wiki_engine.Linker
	wikiLinkSuggester   *wiki_engine.LinkSuggester
	wikiCluster         *wiki_engine.ClusterSynthesizer
	ingester            *ingest.Ingester
	inboxWatcherCancel  context.CancelFunc
	codeAnalyst         *code_analyst.Engine
	codeAnalystCfg      config.CodeAnalysisConfig

	ctx         context.Context
	cancel      context.CancelFunc
	running     bool
	mu          sync.Mutex
	projectRoot string

	analysisMu sync.Mutex
}

func NewEngine(database *db.DB, cfg config.InsightConfig) *Engine {
	e := &Engine{
		database: database,
		config:   cfg,
	}
	e.initLLMClient()
	e.scheduler = newScheduler(e)
	e.initPatternEngine()
	e.initProposalEngine()
	e.initScorecardEngine()
	e.initRoutingEngine()
	e.initWorkflowAnalyzer()
	e.initArtifactBuilder()
	e.initKnowledgeEngine()
	e.initTrajectoryEngine()
	e.initWorkflowSynthesizer()
	e.initProductIntelligence()
	return e
}

// NewEngineWithConfig creates an Engine with explicit WikiConfig,
// EvolutionConfig, and CodeAnalysisConfig, enabling the WikiEngine,
// EvolutionLoop, and CodeAnalyst subsystems.
// Language defaults to "en". Use NewEngineWithFullConfig to specify a language.
func NewEngineWithConfig(database *db.DB, cfg config.InsightConfig, wikiCfg config.WikiConfig, evoCfg config.EvolutionConfig) *Engine {
	return NewEngineWithFullConfig(database, cfg, wikiCfg, evoCfg, config.CodeAnalysisConfig{}, "en")
}

// NewEngineWithFullConfig creates an Engine with all subsystem configs.
// lang is the UI language code ("en", "sk"); pass an empty string to default to "en".
func NewEngineWithFullConfig(database *db.DB, cfg config.InsightConfig, wikiCfg config.WikiConfig, evoCfg config.EvolutionConfig, codeAnalystCfg config.CodeAnalysisConfig, lang string) *Engine {
	if lang == "" {
		lang = "en"
	}
	e := &Engine{
		database:       database,
		config:         cfg,
		wikiCfg:        wikiCfg,
		evoCfg:         evoCfg,
		codeAnalystCfg: codeAnalystCfg,
		lang:           lang,
	}
	e.initLLMClient()
	e.scheduler = newScheduler(e)
	e.initPatternEngine()
	e.initProposalEngine()
	e.initScorecardEngine()
	e.initRoutingEngine()
	e.initWorkflowAnalyzer()
	e.initArtifactBuilder()
	e.initKnowledgeEngine()
	e.initTrajectoryEngine()
	e.initWorkflowSynthesizer()
	e.initProductIntelligence()
	e.initWikiEngine()
	e.initEvolutionLoop()
	e.initCodeAnalyst()
	return e
}

func NewEngineWithEvents(database *db.DB, cfg config.InsightConfig, eventBus events.EventBus) *Engine {
	e := &Engine{
		database:   database,
		config:     cfg,
		eventBus:   eventBus,
		eventStore: events.NewDBStore(database.SQL()),
	}
	e.initLLMClient()
	e.scheduler = newScheduler(e)
	e.initPatternEngine()
	e.initProposalEngine()
	e.initScorecardEngine()
	e.initRoutingEngine()
	e.initWorkflowAnalyzer()
	e.initArtifactBuilder()
	e.initKnowledgeEngine()
	e.initTrajectoryEngine()
	e.initWorkflowSynthesizer()
	e.initProductIntelligence()
	return e
}

func (e *Engine) initLLMClient() {
	llmCfg := llm.Config{
		Provider:             e.config.LLM.Provider,
		Model:                e.config.LLM.Model,
		APIKey:               e.config.LLM.APIKey,
		BaseURL:              e.config.LLM.BaseURL,
		Timeout:              e.config.LLM.Timeout,
		MaxTokens:            e.config.LLM.MaxTokens,
		Temperature:          e.config.LLM.Temperature,
		MaxRetries:           e.config.LLM.MaxRetries,
		Concurrency:          e.config.LLM.Concurrency,
		MinRequestIntervalMs: e.config.LLM.MinRequestIntervalMs,
	}
	if llmCfg.Provider == "" {
		llmCfg = llm.DefaultConfig()
	}
	llmCfg = llmCfg.WithEnv()
	client, err := llm.NewClient(llmCfg)
	if err != nil {
		slog.Warn("insight: failed to initialize LLM client", "error", err)
		return
	}

	// Wrap with budget tracking if DB is available.
	budgeted := llm.NewBudgetedClient(client, e.database, e.evoCfg.DailyTokenBudget)
	e.llmClient = budgeted
	slog.Info("insight: LLM client initialized with budget tracking", "provider", llmCfg.Provider, "model", llmCfg.Model, "daily_budget", e.evoCfg.DailyTokenBudget)
}

func (e *Engine) initPatternEngine() {
	eventQuery := patterns.NewDBEventQuery(e.database.SQL())
	patternStore := patterns.NewDBPatternStore(e.database)
	config := patterns.DefaultDetectionConfig()
	e.patternEngine = patterns.NewEngine(eventQuery, patternStore, config)
}

func (e *Engine) initProposalEngine() {
	patternStore := patterns.NewDBPatternStore(e.database)
	proposalStore := proposals.NewDBProposalStore(e.database)
	config := proposals.DefaultEngineConfig()
	e.proposalEngine = proposals.NewEngine(patternStore, proposalStore, config)
}

func (e *Engine) initScorecardEngine() {
	eventQuery := newScorecardEventQuery(e.database.SQL())
	scorecardStore := scorecards.NewDBScorecardStore(e.database)
	config := scorecards.DefaultScorecardConfig()
	e.scorecardEngine = scorecards.NewEngine(eventQuery, scorecardStore, config)
}

func (e *Engine) initRoutingEngine() {
	scorecardStore := scorecards.NewDBScorecardStore(e.database)
	routingStore := routing.NewDBRoutingStore(e.database)
	config := routing.DefaultRoutingConfig()
	e.routingEngine = routing.NewEngine(scorecardStore, routingStore, config)
}

func (e *Engine) initWorkflowAnalyzer() {
	eventQuery := newWorkflowEventQuery(e.database.SQL())
	config := workflow_intelligence.DefaultAnalyzerConfig()
	e.workflowAnalyzer = workflow_intelligence.NewWorkflowAnalyzer(eventQuery, config)
}

func (e *Engine) initArtifactBuilder() {
	eventQuery := newArtifactEventQuery(e.database.SQL())
	artifactStore := artifacts.NewDBArtifactStore(e.database)
	config := artifacts.DefaultArtifactConfig()
	e.artifactBuilder = artifacts.NewArtifactBuilder(eventQuery, artifactStore, config)
}

func (e *Engine) initKnowledgeEngine() {
	artifactQuery := newKnowledgeArtifactQuery(e.database)
	knowledgeStore := knowledge_engine.NewDBKnowledgeStore(e.database)
	config := knowledge_engine.DefaultEngineConfig()
	e.knowledgeEngine = knowledge_engine.NewEngine(artifactQuery, knowledgeStore, config)
}

func (e *Engine) initTrajectoryEngine() {
	config := trajectory_engine.DefaultConfig()
	e.trajectoryRecorder = trajectory_engine.NewTrajectoryRecorder(e.database, e.eventBus, config)
	e.trajectoryAnalyzer = trajectory_engine.NewTrajectoryAnalyzer(e.database, config)
}

func (e *Engine) initWorkflowSynthesizer() {
	store := workflow_synthesis.NewDBStore(e.database)
	config := workflow_synthesis.DefaultConfig()
	e.workflowSynthesizer = workflow_synthesis.NewSynthesizer(store, config)
}

func (e *Engine) initProductIntelligence() {
	store := product_intelligence.NewDBStore(e.database)
	cfg := product_intelligence.DefaultEngineConfig()
	e.productIntelligence = product_intelligence.NewEngine(store, cfg, e.llmClient)
}

func (e *Engine) initWikiEngine() {
	wikiStore := wiki_engine.NewDBWikiStore(e.database)
	wikiCfgFn := func() config.WikiConfig { return e.wikiCfg }
	e.wikiEngine = wiki_engine.NewWikiEngine(wikiStore, e.llmClient, wikiCfgFn)
	e.wikiSynth = wiki_engine.NewSynthesizer(wikiStore, e.llmClient)
	e.wikiLinker = wiki_engine.NewLinker(wikiStore)
	e.wikiLinkSuggester = wiki_engine.NewLinkSuggester(wikiStore, e.llmClient)
	e.wikiCluster = wiki_engine.NewClusterSynthesizer(wikiStore, e.llmClient, wiki_engine.ClusterConfig{
		MinSources: e.wikiCfg.ClusterMinSources,
	})
	e.ingester = ingest.New(wikiStore, e.wikiEngine, wikiCfgFn, e.wikiLinkSuggester)
}

// Ingester exposes the ingest pipeline.
func (e *Engine) Ingester() *ingest.Ingester { return e.ingester }

// ClusterSynthesizer exposes the cluster subsystem.
func (e *Engine) ClusterSynthesizer() *wiki_engine.ClusterSynthesizer { return e.wikiCluster }

// LinkSuggester exposes the link suggester subsystem.
func (e *Engine) LinkSuggester() *wiki_engine.LinkSuggester { return e.wikiLinkSuggester }

func (e *Engine) initEvolutionLoop() {
	evoStore := evolution_loop.NewDBEvolutionStore(e.database)

	wikiStore := wiki_engine.NewDBWikiStore(e.database)
	wikiFn := func(_ context.Context, h *db.EvolutionHypothesis) (string, error) {
		content := formatHypothesisForWiki(h)
		page := &db.WikiPage{
			ID:          uuid.NewString(),
			PageType:    "concept",
			Title:       fmt.Sprintf("Evolution Finding: %s", h.Description),
			Content:     content,
			Status:      "published",
			Tags:        []string{"evolution", h.Category},
			GeneratedBy: "evolution",
			Version:     1,
		}
		if err := wikiStore.SavePage(page); err != nil {
			return "", fmt.Errorf("save wiki page: %w", err)
		}
		slog.Info("evolution: created wiki proposal page",
			"page_id", page.ID, "category", h.Category)
		return page.ID, nil
	}

	// Wire new target-project analysis dependencies.
	// TODO: wire real Vexor client once VexorClient adapter is implemented.
	bldr := evolution_baseline.New(evolution_baseline.Dependencies{
		DB: e.database,
		// Vexor: nil — resilient; vexor hits will be skipped until wired.
	})
	staticScorer := evolution_scoring.NewStaticScorer()
	proposalWriter := evolution_loop.NewProposalWriter(e.database)

	// TODO: wire LLM judge via adapter from insight LLM client once interface
	// alignment is confirmed (scoring.LLMClient vs llm.Client).
	// For now, LLM judge is disabled (nil) — static-only scoring is used.
	slog.Info("evolution: LLM judge disabled — using static-only scoring for RunCycle")

	root := e.projectRoot
	if root == "" {
		root = "."
	}

	initLang := e.lang
	e.evolutionLoop = evolution_loop.NewEvolutionLoop(
		evoStore,
		func() config.EvolutionConfig { return e.evoCfg },
		e.llmClient,
		evolution_loop.WithWikiFn(wikiFn),
		evolution_loop.WithLangFn(func() string {
			if l := config.Load().Language; l != "" {
				return l
			}
			return initLang
		}),
		// RunCycle dependencies.
		evolution_loop.WithBaselineBuilder(bldr),
		evolution_loop.WithStaticScorer(staticScorer),
		evolution_loop.WithProposalWriter(proposalWriter),
		evolution_loop.WithProjectRoot(root),
	)
}

func (e *Engine) initCodeAnalyst() {
	store := code_analyst.NewDBCodeAnalystStore(e.database)

	// Build a per-subsystem LLM client. CodeAnalysis may have its own LLM
	// override in the config; fall back to the shared insight LLM client when
	// the subsystem override is not configured.
	caLLMCfg := config.ResolveLLMConfig(e.config.LLM, e.codeAnalystCfg.LLM)
	var caLLMClient llm.Client
	if caLLMCfg.Provider != "" && caLLMCfg.Model != "" {
		llmCfg := llm.Config{
			Provider:             caLLMCfg.Provider,
			Model:                caLLMCfg.Model,
			APIKey:               caLLMCfg.APIKey,
			BaseURL:              caLLMCfg.BaseURL,
			Timeout:              caLLMCfg.Timeout,
			MaxTokens:            caLLMCfg.MaxTokens,
			Temperature:          caLLMCfg.Temperature,
			MaxRetries:           caLLMCfg.MaxRetries,
			Concurrency:          caLLMCfg.Concurrency,
			MinRequestIntervalMs: caLLMCfg.MinRequestIntervalMs,
		}
		llmCfg = llmCfg.WithEnv()
		client, err := llm.NewClient(llmCfg)
		if err != nil {
			slog.Warn("insight: code analyst: failed to initialize dedicated LLM client, falling back to shared", "error", err)
			caLLMClient = e.llmClient
		} else {
			caLLMClient = client
		}
	} else {
		caLLMClient = e.llmClient
	}

	configFn := func() code_analyst.CodeAnalysisConfig {
		cfg := config.Load()
		return code_analyst.CodeAnalysisConfig{
			Enabled:             cfg.CodeAnalysis.Enabled,
			MaxFilesPerRun:      cfg.CodeAnalysis.MaxFilesPerRun,
			TokenBudgetPerRun:   cfg.CodeAnalysis.TokenBudgetPerRun,
			MinChurnScore:       cfg.CodeAnalysis.MinChurnScore,
			ConfidenceThreshold: cfg.CodeAnalysis.ConfidenceThreshold,
			ScanInterval:        cfg.CodeAnalysis.ScanInterval,
			IncludeGitHistory:   cfg.CodeAnalysis.IncludeGitHistory,
			GitHistoryDepth:     cfg.CodeAnalysis.GitHistoryDepth,
			Categories:          cfg.CodeAnalysis.Categories,
		}
	}

	wikiStore := wiki_engine.NewDBWikiStore(e.database)
	wikiFn := func(ctx context.Context, filePath string, findings []db.CodeFinding) (string, error) {
		// Deterministic page ID based on file path so re-runs update the same page.
		sanitized := strings.NewReplacer("/", "-", "\\", "-", ".", "-", " ", "-").Replace(filePath)
		pageID := "ca-" + sanitized

		// Build markdown content from findings.
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# Code Analysis: %s\n\n", filePath))
		sb.WriteString(fmt.Sprintf("**File:** `%s`\n\n", filePath))
		sb.WriteString(fmt.Sprintf("**Findings:** %d\n\n", len(findings)))
		sb.WriteString("## Findings\n\n")
		for i, f := range findings {
			sb.WriteString(fmt.Sprintf("### %d. [%s] %s\n\n", i+1, strings.ToUpper(f.Severity), f.Title))
			sb.WriteString(fmt.Sprintf("**Category:** %s  \n", f.Category))
			if f.LineStart > 0 {
				sb.WriteString(fmt.Sprintf("**Lines:** %d–%d  \n", f.LineStart, f.LineEnd))
			}
			sb.WriteString(fmt.Sprintf("**Confidence:** %.0f%%  \n\n", f.Confidence*100))
			sb.WriteString(f.Description + "\n\n")
			if f.Suggestion != "" {
				sb.WriteString("**Suggestion:** " + f.Suggestion + "\n\n")
			}
		}

		content := sb.String()

		// Try to update an existing page; create a new one if not found.
		existing, err := wikiStore.GetPage(pageID)
		if err == nil && existing != nil {
			existing.Content = content
			existing.Status = "published"
			existing.StalenessScore = 0
			existing.Version++
			existing.Tags = []string{"code_analysis", "automated"}
			if updateErr := wikiStore.UpdatePage(existing); updateErr != nil {
				return "", fmt.Errorf("update wiki page: %w", updateErr)
			}
			slog.Info("insight: code analyst: updated wiki page", "page_id", pageID, "file", filePath)
			return pageID, nil
		}

		page := &db.WikiPage{
			ID:          pageID,
			PageType:    "code_analysis",
			Title:       fmt.Sprintf("Code Analysis: %s", filePath),
			Content:     content,
			Status:      "published",
			Tags:        []string{"code_analysis", "automated"},
			GeneratedBy: "code_analyst",
			Version:     1,
		}
		if saveErr := wikiStore.SavePage(page); saveErr != nil {
			return "", fmt.Errorf("save wiki page: %w", saveErr)
		}
		slog.Info("insight: code analyst: created wiki page", "page_id", pageID, "file", filePath)
		return pageID, nil
	}

	projRoot := e.projectRoot
	if projRoot == "" {
		projRoot = "."
	}

	e.codeAnalyst = code_analyst.NewEngine(store, caLLMClient, projRoot, configFn, wikiFn)
	// Wire language function so LLM prompts are returned in the user's language.
	initCaLang := e.lang
	e.codeAnalyst.SetLangFn(func() string {
		if l := config.Load().Language; l != "" {
			return l
		}
		return initCaLang
	})
}

// RunCodeAnalysis executes a full code analysis cycle. triggerType should be
// "manual" for user-initiated runs or "scheduled" for automatic runs.
// categories may be empty to run all configured categories.
func (e *Engine) RunCodeAnalysis(ctx context.Context, triggerType string, categories []string) (*code_analyst.RunResult, error) {
	if e.codeAnalyst == nil {
		return nil, nil
	}
	result, err := e.codeAnalyst.Run(ctx, triggerType, categories)
	if err != nil {
		return nil, fmt.Errorf("run code analysis: %w", err)
	}
	return result, nil
}

// IsCodeAnalysisRunning reports whether a code analysis run is currently in
// progress.
func (e *Engine) IsCodeAnalysisRunning() bool {
	if e.codeAnalyst == nil {
		return false
	}
	return e.codeAnalyst.IsRunning()
}

func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return errors.New("insight: engine already running")
	}

	state, err := e.database.GetInsightState()
	if err != nil {
		return fmt.Errorf("get state: %w", err)
	}

	if state == nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		state = &db.InsightState{
			LastAnalysis:       now,
			NextAnalysis:       now,
			PatternsDetected:   0,
			ProposalsGenerated: 0,
			ProposalsAccepted:  0,
			AcceptanceRate:     0,
			ModelVersion:       "v1",
			ConfigJSON:         "{}",
		}
		if err := e.database.SaveInsightState(state); err != nil {
			return fmt.Errorf("init state: %w", err)
		}
	}

	e.ctx, e.cancel = context.WithCancel(ctx)
	e.running = true

	// Non-blocking startup staleness check — failures are logged and ignored.
	go e.checkStartupStaleness()

	go func() {
		defer func() {
			e.mu.Lock()
			e.running = false
			e.mu.Unlock()
		}()

		if err := e.scheduler.Run(e.ctx); err != nil && err != context.Canceled {
			slog.Error("insight: scheduler stopped with error", "error", err)
		}
	}()

	if e.eventBus != nil {
		e.subscriptionID = e.eventBus.Subscribe(e.HandleEvent)
	}

	if e.trajectoryRecorder != nil {
		if err := e.trajectoryRecorder.Start(e.ctx); err != nil {
			slog.Warn("insight: failed to start trajectory recorder", "error", err)
		}
	}

	e.startInboxWatcher()
	e.startClusterTicker()

	slog.Info("insight: engine started")
	return nil
}

// startClusterTicker runs ClusterSynthesizer periodically if configured.
func (e *Engine) startClusterTicker() {
	if !e.wikiCfg.ClusterSynthesisEnabled || e.wikiCluster == nil {
		return
	}
	interval := time.Duration(e.wikiCfg.ClusterIntervalMinutes) * time.Minute
	if interval < 5*time.Minute {
		interval = 6 * time.Hour
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-t.C:
				if _, err := e.wikiCluster.SynthesizeClusters(e.ctx); err != nil {
					slog.Warn("insight: cluster synthesis tick failed", "err", err)
				}
			}
		}
	}()
}

// startInboxWatcher spins up the Obsidian-inbox file watcher if config allows.
// It is a best-effort subsystem; errors are logged and do not fail Start.
func (e *Engine) startInboxWatcher() {
	if e.wikiCfg.VaultPath == "" || !e.wikiCfg.InboxWatcherEnabled || e.ingester == nil {
		return
	}
	ctx, cancel := context.WithCancel(e.ctx)
	e.inboxWatcherCancel = cancel
	w := wiki_engine.NewInboxWatcher(wiki_engine.InboxWatcherConfig{
		VaultPath: e.wikiCfg.VaultPath,
		InboxDir:  e.wikiCfg.InboxDir,
		Ingester:  e.ingester,
	})
	go func() {
		if err := w.Run(ctx); err != nil && err != context.Canceled {
			slog.Warn("insight: inbox watcher stopped", "err", err)
		}
	}()
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}

	if e.trajectoryRecorder != nil {
		e.trajectoryRecorder.Stop()
	}

	if e.inboxWatcherCancel != nil {
		e.inboxWatcherCancel()
		e.inboxWatcherCancel = nil
	}

	if e.eventBus != nil && e.subscriptionID != 0 {
		e.eventBus.Unsubscribe(e.subscriptionID)
		e.subscriptionID = 0
	}

	if e.cancel != nil {
		e.cancel()
	}
	e.running = false

	slog.Info("insight: engine stopped")
}

func (e *Engine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// SetProjectRoot configures the directory used for git-based staleness checks
// at startup. Must be called before Start().
func (e *Engine) SetProjectRoot(dir string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.projectRoot = dir
}

// runGitCmd executes a git command in the given directory and returns its stdout.
func runGitCmd(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// checkStartupStaleness compares the stored HEAD SHA against the current one
// and boosts the staleness score of any wiki pages whose source files changed.
func (e *Engine) checkStartupStaleness() {
	if e.database == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get current HEAD.
	currentSHA, err := runGitCmd(ctx, e.projectRoot, "rev-parse", "HEAD")
	if err != nil {
		slog.Warn("startup staleness: git rev-parse failed, skipping", "err", err)
		return
	}
	currentSHA = strings.TrimSpace(currentSHA)

	// Get stored HEAD.
	storedSHA, err := e.database.GetGuardianBaseline("wiki_last_head_sha")
	if err != nil {
		slog.Warn("startup staleness: get baseline failed", "err", err)
	}

	// Always update stored HEAD before returning.
	defer func() {
		if setErr := e.database.SetGuardianBaseline("wiki_last_head_sha", currentSHA); setErr != nil {
			slog.Warn("startup staleness: set baseline failed", "err", setErr)
		}
	}()

	// First run or same SHA — nothing to diff.
	if storedSHA == "" || storedSHA == currentSHA {
		return
	}

	// Get changed files.
	diffCtx, diffCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer diffCancel()

	diffOutput, err := runGitCmd(diffCtx, e.projectRoot, "diff", "--name-only", storedSHA, currentSHA)
	if err != nil {
		slog.Warn("startup staleness: git diff failed", "storedSHA", storedSHA, "err", err)
		return
	}

	changedFiles := strings.Split(strings.TrimSpace(diffOutput), "\n")
	if len(changedFiles) == 0 || (len(changedFiles) == 1 && changedFiles[0] == "") {
		return
	}

	// Cap at 500 files to avoid query bloat.
	if len(changedFiles) > 500 {
		changedFiles = changedFiles[:500]
	}

	// Find wiki pages that reference any of the changed files.
	pageIDs, err := e.database.FindPagesBySourceFiles(changedFiles)
	if err != nil {
		slog.Warn("startup staleness: find pages failed", "err", err)
		return
	}

	if len(pageIDs) == 0 {
		return
	}

	// Boost staleness for each affected page.
	for _, id := range pageIDs {
		page, getErr := e.database.GetWikiPage(id)
		if getErr != nil || page == nil {
			continue
		}
		newScore := page.StalenessScore + 0.3
		if newScore > 1.0 {
			newScore = 1.0
		}
		if updateErr := e.database.UpdateWikiPageStaleness(id, newScore); updateErr != nil {
			slog.Warn("startup staleness: update failed", "page_id", id, "err", updateErr)
		}
	}

	slog.Info("startup staleness: updated wiki pages",
		"changed_files", len(changedFiles),
		"affected_pages", len(pageIDs),
	)
}

func (e *Engine) HandleEvent(ctx context.Context, event events.Event) {
	if !e.IsRunning() {
		return
	}
	slog.Info("insight event received",
		"type", event.Type,
		"source", event.Source,
		"id", event.ID)

	if e.eventStore != nil {
		if err := e.eventStore.SaveEvent(ctx, event); err != nil {
			slog.Error("insight: failed to persist event", "error", err, "event_id", event.ID)
		}
	}

	if event.Type == events.EventWorkflowCompleted {
		go e.handleWorkflowWikiStaleness(event.Payload)
	}

	if event.Type == events.EventGovernanceViolation {
		go e.handleGovernanceViolation(ctx, event.Payload)
	}
}

// handleGovernanceViolation pairs an incoming Guardian governance.violation
// event with a remediation proposal in Insight. Runs in its own goroutine
// because proposal persistence is a DB write and must not block the bus.
//
// Errors are logged but never surfaced: a failed pairing should not stop
// other subscribers from processing the same event.
func (e *Engine) handleGovernanceViolation(ctx context.Context, payload map[string]any) {
	if e.proposalEngine == nil || e.database == nil {
		return
	}

	alertID, _ := payload["alert_id"].(int64)
	if alertID == 0 {
		// Some bus encoders round-trip int64 as float64; accept both.
		if f, ok := payload["alert_id"].(float64); ok {
			alertID = int64(f)
		}
	}
	file, _ := payload["file"].(string)
	rules, _ := payload["rules"].(string)
	severity, _ := payload["severity"].(string)

	proposal := proposals.NewGovernanceRemediationProposal(alertID, file, rules, severity)

	store := proposals.NewDBProposalStore(e.database)
	if err := store.SaveProposal(ctx, proposal); err != nil {
		slog.Warn("insight: failed to save governance remediation proposal",
			"error", err, "alert_id", alertID, "file", file)
		return
	}
	slog.Info("insight: paired governance proposal saved",
		"proposal_id", proposal.ID, "alert_id", alertID, "file", file)
}

// handleWorkflowWikiStaleness boosts the staleness score of wiki pages that
// reference files listed in the event payload's "files" key. It is intended to
// be called as a goroutine and must never propagate errors to the caller.
func (e *Engine) handleWorkflowWikiStaleness(payload map[string]any) {
	if e.database == nil {
		return
	}

	// Extract file paths from the payload.
	var files []string
	if rawFiles, ok := payload["files"]; ok {
		if fileList, ok := rawFiles.([]any); ok {
			for _, f := range fileList {
				if s, ok := f.(string); ok {
					files = append(files, s)
				}
			}
		}
	}

	if len(files) == 0 {
		slog.Debug("workflow wiki staleness: no files in event payload, skipping")
		return
	}

	pageIDs, err := e.database.FindPagesBySourceFiles(files)
	if err != nil {
		slog.Warn("workflow wiki staleness: find pages failed", "err", err)
		return
	}

	for _, id := range pageIDs {
		page, getErr := e.database.GetWikiPage(id)
		if getErr != nil || page == nil {
			continue
		}
		newScore := page.StalenessScore + 0.2
		if newScore > 1.0 {
			newScore = 1.0
		}
		if updateErr := e.database.UpdateWikiPageStaleness(id, newScore); updateErr != nil {
			slog.Warn("workflow wiki staleness: update failed", "page_id", id, "err", updateErr)
		}
	}

	if len(pageIDs) > 0 {
		slog.Info("workflow wiki staleness: updated pages",
			"files", len(files),
			"pages", len(pageIDs),
		)
	}
}

func (e *Engine) EventBus() events.EventBus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.eventBus
}

func (e *Engine) SetEventBus(bus events.EventBus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running {
		slog.Warn("insight: cannot set event bus while engine is running")
		return
	}
	e.eventBus = bus
	e.eventStore = events.NewDBStore(e.database.SQL())
}

func (e *Engine) PatternEngine() *patterns.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.patternEngine
}

func (e *Engine) RunPatternDetection(ctx context.Context) error {
	if e.patternEngine == nil {
		return errors.New("insight: pattern engine not initialized")
	}
	return e.patternEngine.RunDetection(ctx)
}

func (e *Engine) ProposalEngine() *proposals.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.proposalEngine
}

func (e *Engine) RunProposalGeneration(ctx context.Context) error {
	if e.proposalEngine == nil {
		return errors.New("insight: proposal engine not initialized")
	}
	return e.proposalEngine.RunGeneration(ctx)
}

func (e *Engine) ScorecardEngine() *scorecards.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.scorecardEngine
}

func (e *Engine) RunScorecardComputation(ctx context.Context) error {
	if e.scorecardEngine == nil {
		return errors.New("insight: scorecard engine not initialized")
	}
	return e.scorecardEngine.RunComputation(ctx)
}

func (e *Engine) RoutingEngine() *routing.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.routingEngine
}

func (e *Engine) RunRoutingAnalysis(ctx context.Context) error {
	if e.routingEngine == nil {
		return errors.New("insight: routing engine not initialized")
	}
	return e.routingEngine.RunAnalysis(ctx)
}

func newScorecardEventQuery(db *sql.DB) scorecards.EventQuery {
	return &scorecardEventQuery{db: db}
}

type scorecardEventQuery struct {
	db *sql.DB
}

func (q *scorecardEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]scorecards.EventForScorecard, error) {
	if limit <= 0 {
		limit = 10000
	}
	if len(eventTypes) == 0 {
		return []scorecards.EventForScorecard{}, nil
	}

	placeholders := ""
	for i := range eventTypes {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	query := `SELECT id, type, timestamp, source, payload
		FROM insight_events
		WHERE type IN (` + placeholders + `) AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?`

	args := make([]any, 0, len(eventTypes)+3)
	for _, et := range eventTypes {
		args = append(args, et)
	}
	args = append(args, start.Format(time.RFC3339Nano))
	args = append(args, end.Format(time.RFC3339Nano))
	args = append(args, limit)

	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []scorecards.EventForScorecard
	for rows.Next() {
		var e scorecards.EventForScorecard
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			slog.Warn("failed to parse timestamp", "event_id", e.ID, "timestamp", timestamp, "error", err)
		} else {
			e.Timestamp = parsed
		}
		if err := unmarshalPayload(payloadStr, &e); err != nil {
			slog.Warn("failed to parse event payload", "event_id", e.ID, "error", err)
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

func unmarshalPayload(payloadStr string, e *scorecards.EventForScorecard) error {
	if payloadStr == "" {
		e.Payload = make(map[string]any)
		return nil
	}
	if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
		return err
	}
	if wfID, ok := e.Payload["workflow_id"].(string); ok {
		e.WorkflowID = wfID
	}
	if agentName, ok := e.Payload["agent_name"].(string); ok {
		e.AgentName = agentName
	}
	if phase, ok := e.Payload["phase"].(string); ok {
		e.Phase = phase
	}
	if durMs, ok := e.Payload["duration_ms"].(float64); ok {
		e.DurationMs = int64(durMs)
	}
	return nil
}

func (e *Engine) WorkflowAnalyzer() *workflow_intelligence.WorkflowAnalyzer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.workflowAnalyzer
}

func (e *Engine) RunWorkflowAnalysis(ctx context.Context) error {
	if e.workflowAnalyzer == nil {
		return errors.New("insight: workflow analyzer not initialized")
	}
	_, err := e.workflowAnalyzer.AnalyzeWorkflowPerformance(ctx)
	return err
}

func (e *Engine) ArtifactBuilder() *artifacts.ArtifactBuilder {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.artifactBuilder
}

func (e *Engine) BuildArtifact(ctx context.Context, workflowID string) (*artifacts.Artifact, error) {
	if e.artifactBuilder == nil {
		return nil, errors.New("insight: artifact builder not initialized")
	}
	return e.artifactBuilder.Build(ctx, workflowID)
}

func (e *Engine) BuildRecentArtifacts(ctx context.Context, since time.Time) (int, error) {
	if e.artifactBuilder == nil {
		return 0, errors.New("insight: artifact builder not initialized")
	}
	return e.artifactBuilder.BuildRecent(ctx, since)
}

func (e *Engine) KnowledgeEngine() *knowledge_engine.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.knowledgeEngine
}

func (e *Engine) RunKnowledgeAnalysis(ctx context.Context) error {
	if e.knowledgeEngine == nil {
		return errors.New("insight: knowledge engine not initialized")
	}
	return e.knowledgeEngine.RunAnalysis(ctx)
}

func (e *Engine) GetKnowledgeRecommendation(ctx context.Context, problemClass, repoType string) (*knowledge_engine.KnowledgeRecommendation, error) {
	if e.knowledgeEngine == nil {
		return nil, errors.New("insight: knowledge engine not initialized")
	}
	return e.knowledgeEngine.GetRecommendation(ctx, problemClass, repoType)
}

func (e *Engine) TrajectoryRecorder() *trajectory_engine.TrajectoryRecorder {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.trajectoryRecorder
}

func (e *Engine) TrajectoryAnalyzer() *trajectory_engine.TrajectoryAnalyzer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.trajectoryAnalyzer
}

func (e *Engine) RunTrajectoryAnalysis(ctx context.Context) (*trajectory_engine.AnalysisResult, error) {
	if e.trajectoryAnalyzer == nil {
		return nil, errors.New("insight: trajectory analyzer not initialized")
	}
	return e.trajectoryAnalyzer.Analyze(ctx)
}

func (e *Engine) GetOptimalAgentSequence(ctx context.Context, problemType, repoType string) ([]string, float64, error) {
	if e.trajectoryAnalyzer == nil {
		return nil, 0, errors.New("insight: trajectory analyzer not initialized")
	}
	return e.trajectoryAnalyzer.GetOptimalSequence(problemType, repoType)
}

func (e *Engine) GetEnrichedRecommendation(ctx context.Context, problemClass, repoType string) (*knowledge_engine.KnowledgeRecommendation, error) {
	rec, err := e.GetKnowledgeRecommendation(ctx, problemClass, repoType)
	if err != nil {
		return nil, err
	}

	if rec == nil {
		rec = &knowledge_engine.KnowledgeRecommendation{
			ProblemClass: problemClass,
			RepoType:     repoType,
		}
	}

	if e.trajectoryAnalyzer != nil {
		sequence, confidence, err := e.trajectoryAnalyzer.GetOptimalSequence(problemClass, repoType)
		if err == nil && len(sequence) > 0 {
			rec.OptimalAgentSequence = sequence
			rec.SequenceConfidence = confidence
			rec.TypicalSteps = len(sequence)
		}
	}

	return rec, nil
}

func (e *Engine) WorkflowSynthesizer() *workflow_synthesis.Synthesizer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.workflowSynthesizer
}

func (e *Engine) RunWorkflowSynthesis(ctx context.Context) (*workflow_synthesis.SynthesisResult, error) {
	if e.workflowSynthesizer == nil {
		return nil, errors.New("insight: workflow synthesizer not initialized")
	}
	return e.workflowSynthesizer.Run(ctx)
}

func (e *Engine) GetSynthesizedWorkflow(ctx context.Context, taskType, repoType string) (*db.WorkflowCandidate, bool, error) {
	if e.workflowSynthesizer == nil {
		return nil, false, errors.New("insight: workflow synthesizer not initialized")
	}
	return e.workflowSynthesizer.SelectWorkflow(ctx, taskType, repoType)
}

func (e *Engine) RecordSynthesisResult(ctx context.Context, taskType, repoType, workflowID string, useCandidate bool, success bool, cycleTimeMin, retryCount, reviewPasses int) error {
	if e.workflowSynthesizer == nil {
		return errors.New("insight: workflow synthesizer not initialized")
	}
	return e.workflowSynthesizer.RecordResult(ctx, taskType, repoType, workflowID, useCandidate, success, cycleTimeMin, retryCount, reviewPasses)
}

func (e *Engine) GetSynthesisStats(ctx context.Context) (map[string]interface{}, error) {
	if e.workflowSynthesizer == nil {
		return nil, errors.New("insight: workflow synthesizer not initialized")
	}
	return e.workflowSynthesizer.GetExperimentStats(ctx)
}

func (e *Engine) ProductIntelligence() *product_intelligence.Engine {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.productIntelligence
}

func (e *Engine) LLMClient() llm.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.llmClient
}

func (e *Engine) ProductIntelligenceEngine() *product_intelligence.Engine {
	return e.ProductIntelligence()
}

func (e *Engine) RunProductAnalysis(ctx context.Context, projectPath string, projectName string) (*product_intelligence.AnalysisResult, error) {
	if e.productIntelligence == nil {
		return nil, errors.New("insight: product intelligence not initialized")
	}

	project, err := e.productIntelligence.RegisterProject(ctx, projectPath, projectName)
	if err != nil {
		return nil, fmt.Errorf("register project: %w", err)
	}

	cfg := product_intelligence.ProjectAnalysisConfig{}
	return e.productIntelligence.AnalyzeProject(ctx, project.ID, cfg)
}

func newWorkflowEventQuery(db *sql.DB) workflow_intelligence.EventQuery {
	return &workflowEventQuery{db: db}
}

type workflowEventQuery struct {
	db *sql.DB
}

func (q *workflowEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]workflow_intelligence.EventForMetrics, error) {
	if limit <= 0 {
		limit = 10000
	}
	if len(eventTypes) == 0 {
		return []workflow_intelligence.EventForMetrics{}, nil
	}

	placeholders := ""
	for i := range eventTypes {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	query := `SELECT id, type, timestamp, source, payload
		FROM insight_events
		WHERE type IN (` + placeholders + `) AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?`

	args := make([]any, 0, len(eventTypes)+3)
	for _, et := range eventTypes {
		args = append(args, et)
	}
	args = append(args, start.Format(time.RFC3339Nano))
	args = append(args, end.Format(time.RFC3339Nano))
	args = append(args, limit)

	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []workflow_intelligence.EventForMetrics
	for rows.Next() {
		var e workflow_intelligence.EventForMetrics
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			slog.Warn("failed to parse timestamp", "event_id", e.ID, "timestamp", timestamp, "error", err)
		} else {
			e.Timestamp = parsed
		}
		if payloadStr != "" {
			if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
				slog.Warn("failed to parse event payload", "event_id", e.ID, "error", err)
				e.Payload = make(map[string]any)
			}
		} else {
			e.Payload = make(map[string]any)
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

func newArtifactEventQuery(db *sql.DB) artifacts.EventQuery {
	return &artifactEventQuery{db: db}
}

type artifactEventQuery struct {
	db *sql.DB
}

func (q *artifactEventQuery) GetEventsByTypesInTimeRange(ctx context.Context, eventTypes []string, start, end time.Time, limit int) ([]artifacts.EventForArtifact, error) {
	if limit <= 0 {
		limit = 10000
	}
	if len(eventTypes) == 0 {
		return []artifacts.EventForArtifact{}, nil
	}

	placeholders := ""
	for i := range eventTypes {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	query := `SELECT id, type, timestamp, source, payload
		FROM insight_events
		WHERE type IN (` + placeholders + `) AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT ?`

	args := make([]any, 0, len(eventTypes)+3)
	for _, et := range eventTypes {
		args = append(args, et)
	}
	args = append(args, start.Format(time.RFC3339Nano))
	args = append(args, end.Format(time.RFC3339Nano))
	args = append(args, limit)

	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []artifacts.EventForArtifact
	for rows.Next() {
		var e artifacts.EventForArtifact
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestamp)
		if payloadStr != "" {
			if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
				e.Payload = make(map[string]any)
			}
		} else {
			e.Payload = make(map[string]any)
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

func (q *artifactEventQuery) GetEventsByWorkflowID(ctx context.Context, workflowID string) ([]artifacts.EventForArtifact, error) {
	query := `SELECT id, type, timestamp, source, payload
		FROM insight_events
		WHERE json_extract(payload, '$.workflow_id') = ?
		ORDER BY timestamp ASC`

	rows, err := q.db.QueryContext(ctx, query, workflowID)
	if err != nil {
		return nil, fmt.Errorf("query events by workflow: %w", err)
	}
	defer rows.Close()

	var events []artifacts.EventForArtifact
	for rows.Next() {
		var e artifacts.EventForArtifact
		var timestamp, payloadStr string
		if err := rows.Scan(&e.ID, &e.Type, &timestamp, &e.Source, &payloadStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestamp)
		if payloadStr != "" {
			if err := json.Unmarshal([]byte(payloadStr), &e.Payload); err != nil {
				e.Payload = make(map[string]any)
			}
		} else {
			e.Payload = make(map[string]any)
		}
		events = append(events, e)
	}

	return events, rows.Err()
}

func (q *artifactEventQuery) GetCompletedWorkflowIDs(ctx context.Context, since time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 1000
	}

	query := `SELECT DISTINCT json_extract(payload, '$.workflow_id')
		FROM insight_events
		WHERE type = 'workflow.completed'
		  AND timestamp >= ?
		  AND json_extract(payload, '$.workflow_id') IS NOT NULL
		ORDER BY timestamp DESC
		LIMIT ?`

	rows, err := q.db.QueryContext(ctx, query, since.Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("query completed workflows: %w", err)
	}
	defer rows.Close()

	var workflowIDs []string
	for rows.Next() {
		var id *string
		if err := rows.Scan(&id); err != nil {
			slog.Warn("failed to scan workflow id", "error", err)
			continue
		}
		if id != nil && *id != "" {
			workflowIDs = append(workflowIDs, *id)
		}
	}

	return workflowIDs, rows.Err()
}

func newKnowledgeArtifactQuery(database *db.DB) knowledge_engine.ArtifactQuery {
	return &knowledgeArtifactQuery{database: database}
}

type knowledgeArtifactQuery struct {
	database *db.DB
}

func (q *knowledgeArtifactQuery) GetSuccessfulArtifactsWithSolution(ctx context.Context, limit int) ([]knowledge_engine.ArtifactData, error) {
	if limit <= 0 {
		limit = 100
	}

	artifacts, err := q.database.GetSuccessfulArtifactsWithSolution(limit)
	if err != nil {
		return nil, fmt.Errorf("get successful artifacts: %w", err)
	}

	result := make([]knowledge_engine.ArtifactData, len(artifacts))
	for i, a := range artifacts {
		result[i] = knowledgeArtifactDataFromDB(a)
	}

	return result, nil
}

func (q *knowledgeArtifactQuery) ListArtifacts(ctx context.Context, filters knowledge_engine.ArtifactFilterOptions) ([]knowledge_engine.ArtifactData, error) {
	dbFilters := db.ArtifactFilters{
		WorkflowType: filters.WorkflowType,
		ProblemClass: filters.ProblemClass,
		RepoType:     filters.RepoType,
		Success:      filters.Success,
		Limit:        filters.Limit,
		Offset:       filters.Offset,
	}

	artifacts, err := q.database.ListArtifacts(dbFilters)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}

	result := make([]knowledge_engine.ArtifactData, len(artifacts))
	for i, a := range artifacts {
		result[i] = knowledgeArtifactDataFromDB(a)
	}

	return result, nil
}

func (q *knowledgeArtifactQuery) CountArtifacts(ctx context.Context) (int, error) {
	return q.database.CountArtifacts()
}

func (q *knowledgeArtifactQuery) GetProblemClassStats(ctx context.Context) ([]map[string]any, error) {
	return q.database.GetProblemClassStats()
}

func (q *knowledgeArtifactQuery) GetAgentSuccessByProblem(ctx context.Context) ([]map[string]any, error) {
	return q.database.GetAgentSuccessByProblem()
}

func knowledgeArtifactDataFromDB(a db.Artifact) knowledge_engine.ArtifactData {
	return knowledge_engine.ArtifactData{
		ID:              a.ID,
		WorkflowID:      a.WorkflowID,
		TaskType:        a.TaskType,
		WorkflowType:    a.WorkflowType,
		RepoType:        a.RepoType,
		ProblemClass:    a.ProblemClass,
		AgentsUsed:      a.AgentsUsed,
		RootCause:       a.RootCause,
		SolutionPattern: a.SolutionPattern,
		FilesChanged:    a.FilesChanged,
		ReviewResult:    a.ReviewResult,
		CycleTimeMin:    a.CycleTimeMin,
		Success:         a.Success,
		Metadata:        a.Metadata,
		CreatedAt:       parseTime(a.CreatedAt),
	}
}

// WikiEngine returns the WikiEngine subsystem. It is nil when the engine was
// created without wiki configuration (via NewEngine or NewEngineWithEvents).
func (e *Engine) WikiEngine() *wiki_engine.WikiEngine { return e.wikiEngine }

// EvolutionLoop returns the EvolutionLoop subsystem. It is nil when the engine
// was created without evolution configuration.
func (e *Engine) EvolutionLoop() *evolution_loop.EvolutionLoop { return e.evolutionLoop }

// WikiSynthesizer returns the wiki Synthesizer subsystem.
func (e *Engine) WikiSynthesizer() *wiki_engine.Synthesizer { return e.wikiSynth }

// RunWikiIngest runs a wiki ingest cycle. It returns nil, nil when the wiki
// engine has not been initialised.
func (e *Engine) RunWikiIngest(ctx context.Context) (*wiki_engine.IngestResult, error) {
	if e.wikiEngine == nil {
		return nil, nil
	}
	result, err := e.wikiEngine.RunIngest(ctx)
	if err != nil {
		return nil, fmt.Errorf("wiki ingest: %w", err)
	}
	return result, nil
}

// Ingest runs a single ingest pass via the ingester. Returns ErrNotConfigured
// when the wiki engine was not initialised.
func (e *Engine) Ingest(ctx context.Context, source string, opts ingest.Options) (*ingest.Result, error) {
	if e.ingester == nil {
		return nil, fmt.Errorf("insight: ingester not initialized")
	}
	return e.ingester.Ingest(ctx, source, opts)
}

// RunClusterSynthesis triggers the cluster job on demand.
func (e *Engine) RunClusterSynthesis(ctx context.Context) (*wiki_engine.ClusterResult, error) {
	if e.wikiCluster == nil {
		return nil, fmt.Errorf("insight: cluster synthesizer not initialized")
	}
	return e.wikiCluster.SynthesizeClusters(ctx)
}

// RunVaultPull pulls externally-edited vault files back into the DB. Returns
// nil, nil if vault sync is not configured on the server layer.
func (e *Engine) RunVaultPull(ctx context.Context, vs *wiki_engine.VaultSync) (*wiki_engine.PullStatus, error) {
	if vs == nil {
		return nil, fmt.Errorf("insight: vault sync not configured")
	}
	return vs.PullAll(ctx)
}

// RunWikiMaintenance runs a wiki maintenance cycle. It returns nil, nil when
// the wiki engine has not been initialised.
func (e *Engine) RunWikiMaintenance(ctx context.Context) (*wiki_engine.MaintenanceResult, error) {
	if e.wikiEngine == nil {
		return nil, nil
	}
	result, err := e.wikiEngine.RunMaintenance(ctx)
	if err != nil {
		return nil, fmt.Errorf("wiki maintenance: %w", err)
	}
	return result, nil
}

// RunEvolutionCycle executes one full evolution loop run with the given
// triggerType. It returns nil, nil when the evolution loop has not been
// initialised.
//
// timeoutMs, when > 0, overrides the configured TimeoutMs for this run only.
// categories, when non-empty, overrides the configured Categories for this
// run only. Pass 0 and nil to use the values from config.
func (e *Engine) RunEvolutionCycle(ctx context.Context, triggerType string, timeoutMs int64, categories []string) (*evolution_loop.RunResult, error) {
	if e.evolutionLoop == nil {
		return nil, nil
	}

	// StratusSelfEnabled=true → legacy Run() path (HypothesisGenerator + ExperimentRunner).
	// Otherwise → RunTargetCycle for target-project analysis.
	if e.evoCfg.StratusSelfEnabled {
		result, err := e.evolutionLoop.Run(ctx, triggerType, timeoutMs, categories)
		if err != nil {
			return nil, fmt.Errorf("evolution cycle: %w", err)
		}
		return result, nil
	}

	result, err := e.evolutionLoop.RunTargetCycle(ctx, triggerType, timeoutMs, categories)
	if err != nil {
		return nil, fmt.Errorf("evolution cycle: %w", err)
	}
	return result, nil
}

// SynthesizeWikiAnswer uses the wiki synthesizer to answer a natural-language
// query from existing wiki pages. An error is returned when the synthesizer has
// not been initialised.
func (e *Engine) SynthesizeWikiAnswer(ctx context.Context, query string, maxSources int, persist bool) (*wiki_engine.SynthesisResult, error) {
	if e.wikiSynth == nil {
		return nil, fmt.Errorf("wiki synthesizer not initialized")
	}
	result, err := e.wikiSynth.SynthesizeAnswer(ctx, query, maxSources, persist)
	if err != nil {
		return nil, fmt.Errorf("synthesize wiki answer: %w", err)
	}
	return result, nil
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}
