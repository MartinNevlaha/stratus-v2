package api

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/guardian"
	"github.com/MartinNevlaha/stratus-v2/insight"
	"github.com/MartinNevlaha/stratus-v2/insight/events"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/agent_evolution"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/product_intelligence"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/swarm"
	"github.com/MartinNevlaha/stratus-v2/terminal"
	"github.com/MartinNevlaha/stratus-v2/vexor"
)

const emitEventTimeout = 5 * time.Second

type Server struct {
	db                   *db.DB
	coordinator          *orchestration.Coordinator
	vexor                *vexor.Client
	hub                  *Hub
	terminal             *terminal.Manager
	projectRoot          string
	sttEndpoint          string
	sttModel             string
	staticFiles          fs.FS
	version              string
	syncedVersion        string   // version when assets were last refreshed to disk
	skippedFiles         []string // asset files skipped in last refresh (user-customized)
	swarm                *swarm.Store
	insight              *insight.Engine
	agentEvolutionEngine *agent_evolution.Engine
	eventBus             events.EventBus
	piEngine             *product_intelligence.Engine
	guardianSvc          *guardian.Guardian
	cfg                  *config.Config // pointer so guardian config updates are reflected

	dirtyFiles map[string]struct{}
	dirtyMu    sync.Mutex
	dirtyCh    chan struct{}

	updateMu sync.Mutex
}

// NewServer creates the HTTP server with all routes wired up.
func NewServer(
	database *db.DB,
	coord *orchestration.Coordinator,
	vexorClient *vexor.Client,
	hub *Hub,
	termMgr *terminal.Manager,
	projectRoot string,
	sttEndpoint string,
	sttModel string,
	staticFiles fs.FS,
	version string,
	syncedVersion string,
	skippedFiles []string,
	swarmStore *swarm.Store,
	insightEngine *insight.Engine,
	agentEvolutionEng *agent_evolution.Engine,
	cfg *config.Config,
) *Server {
	s := &Server{
		db:                   database,
		coordinator:          coord,
		vexor:                vexorClient,
		hub:                  hub,
		terminal:             termMgr,
		projectRoot:          projectRoot,
		sttEndpoint:          sttEndpoint,
		sttModel:             sttModel,
		staticFiles:          staticFiles,
		version:              version,
		syncedVersion:        syncedVersion,
		skippedFiles:         skippedFiles,
		swarm:                swarmStore,
		insight:              insightEngine,
		agentEvolutionEngine: agentEvolutionEng,
		cfg:                  cfg,
		dirtyFiles:           make(map[string]struct{}),
		dirtyCh:              make(chan struct{}, 1),
	}
	go s.indexWorker()
	return s
}

// SetGuardian attaches the guardian service so routes can trigger manual scans.
func (s *Server) SetGuardian(g *guardian.Guardian) {
	s.guardianSvc = g
}

// markDirty adds file paths to the dirty set and signals the index worker.
func (s *Server) markDirty(paths []string) {
	s.dirtyMu.Lock()
	for _, p := range paths {
		s.dirtyFiles[p] = struct{}{}
	}
	s.dirtyMu.Unlock()
	select {
	case s.dirtyCh <- struct{}{}:
	default:
	}
}

func (s *Server) emitEvent(eventType events.EventType, source string, payload map[string]any) {
	if s.eventBus == nil {
		return
	}
	evt := events.NewEvent(eventType, source, payload)
	ctx, cancel := context.WithTimeout(context.Background(), emitEventTimeout)
	defer cancel()
	s.eventBus.Publish(ctx, evt)
}

func (s *Server) SetEventBus(bus events.EventBus) {
	s.eventBus = bus
}

func (s *Server) SetProductIntelligenceEngine(engine *product_intelligence.Engine) {
	s.piEngine = engine
}

func (s *Server) indexWorker() {
	const quietPeriod = 5 * time.Second
	const maxBatch = 20

	for {
		<-s.dirtyCh // wait for first dirty signal

		timer := time.NewTimer(quietPeriod)
	debounce:
		for {
			select {
			case <-s.dirtyCh:
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(quietPeriod)
			case <-timer.C:
				break debounce
			}
		}

		s.dirtyMu.Lock()
		files := make([]string, 0, len(s.dirtyFiles))
		for f := range s.dirtyFiles {
			files = append(files, f)
		}
		s.dirtyFiles = make(map[string]struct{})
		s.dirtyMu.Unlock()

		if len(files) == 0 || !s.vexor.Available() {
			continue
		}
		if len(files) > maxBatch {
			files = nil // full reindex
		}
		if err := s.vexor.Index(files); err != nil {
			log.Printf("vexor reindex: %v", err)
		}
	}
}

// Handler builds and returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/dashboard/state", s.handleDashboardState)

	// Memory / events
	mux.HandleFunc("POST /api/events", s.handleSaveEvent)
	mux.HandleFunc("GET /api/events/search", s.handleSearchEvents)
	mux.HandleFunc("GET /api/events/{id}/timeline", s.handleTimeline)
	mux.HandleFunc("POST /api/events/batch", s.handleBatchEvents)

	// Sessions
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)

	// Retrieval
	mux.HandleFunc("GET /api/retrieve", s.handleRetrieve)
	mux.HandleFunc("GET /api/retrieve/status", s.handleRetrieveStatus)
	mux.HandleFunc("POST /api/retrieve/index", s.handleReIndex)
	mux.HandleFunc("POST /api/retrieve/dirty", s.handleMarkDirty)

	// Orchestration
	mux.HandleFunc("GET /api/past", s.handleListPast)
	mux.HandleFunc("POST /api/workflows/analyze", s.handleAnalyzeWorkflow)
	mux.HandleFunc("GET /api/workflows", s.handleListWorkflows)
	mux.HandleFunc("POST /api/workflows", s.handleStartWorkflow)
	mux.HandleFunc("GET /api/workflows/{id}", s.handleGetWorkflow)
	mux.HandleFunc("PUT /api/workflows/{id}/phase", s.handleTransitionPhase)
	mux.HandleFunc("POST /api/workflows/{id}/delegate", s.handleRecordDelegation)
	mux.HandleFunc("POST /api/workflows/{id}/tasks", s.handleSetTasks)
	mux.HandleFunc("POST /api/workflows/{id}/tasks/{index}/start", s.handleStartTask)
	mux.HandleFunc("POST /api/workflows/{id}/tasks/{index}/complete", s.handleCompleteTask)
	mux.HandleFunc("DELETE /api/workflows/{id}", s.handleDeleteWorkflow)
	mux.HandleFunc("PATCH /api/workflows/{id}/session", s.handleSetWorkflowSession)
	mux.HandleFunc("PUT /api/workflows/{id}/plan", s.handleSetPlanContent)
	mux.HandleFunc("PUT /api/workflows/{id}/design", s.handleSetDesignContent)
	mux.HandleFunc("GET /api/workflows/{id}/dispatch", s.handleDispatch)
	mux.HandleFunc("GET /api/workflows/{id}/summary", s.handleGetSummary)
	mux.HandleFunc("GET /api/workflows/{id}/summary.md", s.handleGetSummaryMD)
	mux.HandleFunc("PUT /api/workflows/{id}/summary", s.handleUpdateSummary)

	// Learning
	mux.HandleFunc("GET /api/learning/candidates", s.handleListCandidates)
	mux.HandleFunc("POST /api/learning/candidates", s.handleSaveCandidate)
	mux.HandleFunc("GET /api/learning/proposals", s.handleListProposals)
	mux.HandleFunc("POST /api/learning/proposals", s.handleSaveProposal)
	mux.HandleFunc("POST /api/learning/proposals/{id}/decide", s.handleDecideProposal)

	// System
	mux.HandleFunc("GET /api/system/version", s.handleVersion)
	mux.HandleFunc("POST /api/system/update", s.handleUpdate)

	// STT
	mux.HandleFunc("POST /api/stt/transcribe", s.handleSTTTranscribe)
	mux.HandleFunc("GET /api/stt/status", s.handleSTTStatus)

	// Swarm
	mux.HandleFunc("POST /api/swarm/missions", s.handleCreateMission)
	mux.HandleFunc("GET /api/swarm/missions", s.handleListMissions)
	mux.HandleFunc("GET /api/swarm/missions/{id}", s.handleGetMission)
	mux.HandleFunc("PUT /api/swarm/missions/{id}/status", s.handleUpdateMissionStatus)
	mux.HandleFunc("DELETE /api/swarm/missions/{id}", s.handleDeleteMission)
	mux.HandleFunc("POST /api/swarm/missions/{id}/workers", s.handleSpawnWorker)
	mux.HandleFunc("GET /api/swarm/missions/{id}/workers", s.handleListWorkers)
	mux.HandleFunc("POST /api/swarm/workers/{id}/heartbeat", s.handleWorkerHeartbeat)
	mux.HandleFunc("PUT /api/swarm/workers/{id}/status", s.handleUpdateWorkerStatus)
	mux.HandleFunc("POST /api/swarm/missions/{id}/tickets", s.handleCreateTicket)
	mux.HandleFunc("POST /api/swarm/missions/{id}/tickets/batch", s.handleBatchCreateTickets)
	mux.HandleFunc("GET /api/swarm/missions/{id}/tickets", s.handleListTickets)
	mux.HandleFunc("PUT /api/swarm/tickets/{id}/status", s.handleUpdateTicketStatus)
	mux.HandleFunc("POST /api/swarm/missions/{id}/dispatch", s.handleSwarmDispatch)
	mux.HandleFunc("POST /api/swarm/signals", s.handleSendSignal)
	mux.HandleFunc("GET /api/swarm/workers/{id}/signals", s.handlePollSignals)
	mux.HandleFunc("POST /api/swarm/missions/{id}/forge", s.handleSubmitToForge)
	mux.HandleFunc("GET /api/swarm/missions/{id}/forge", s.handleListForgeEntries)
	mux.HandleFunc("PUT /api/swarm/forge/{id}/status", s.handleUpdateForgeEntry)
	mux.HandleFunc("POST /api/swarm/forge/submit", s.handleSubmitToForgeByWorker)
	mux.HandleFunc("GET /api/swarm/workers/{id}", s.handleGetWorker)
	mux.HandleFunc("GET /api/swarm/missions/{id}/files", s.handleListMissionFiles)
	mux.HandleFunc("POST /api/swarm/files/reserve", s.handleReserveFiles)
	mux.HandleFunc("POST /api/swarm/files/release", s.handleReleaseFiles)
	mux.HandleFunc("POST /api/swarm/files/check", s.handleCheckFileConflicts)
	mux.HandleFunc("POST /api/swarm/missions/{id}/checkpoint", s.handleSaveCheckpoint)
	mux.HandleFunc("GET /api/swarm/missions/{id}/checkpoint/latest", s.handleGetLatestCheckpoint)
	mux.HandleFunc("PUT /api/swarm/missions/{id}/strategy-outcome", s.handleUpdateStrategyOutcome)
	mux.HandleFunc("POST /api/swarm/tickets/{id}/evidence", s.handleRecordEvidence)
	mux.HandleFunc("GET /api/swarm/tickets/{id}/evidence", s.handleListTicketEvidence)
	mux.HandleFunc("GET /api/swarm/missions/{id}/evidence", s.handleListMissionEvidence)
	mux.HandleFunc("POST /api/swarm/guardrails/track", s.handleTrackToolCall)
	mux.HandleFunc("GET /api/swarm/workers/{id}/guardrails", s.handleGetGuardrail)
	mux.HandleFunc("POST /api/swarm/missions/{id}/drift", s.handleCheckDrift)

	// Insight
	mux.HandleFunc("GET /api/insight/config", s.handleGetInsightConfig)
	mux.HandleFunc("PUT /api/insight/config", s.handleUpdateInsightConfig)
	mux.HandleFunc("GET /api/insight/status", s.handleGetInsightStatus)
	mux.HandleFunc("POST /api/insight/trigger", s.handleTriggerInsightAnalysis)
	mux.HandleFunc("GET /api/insight/patterns", s.handleGetInsightPatterns)
	mux.HandleFunc("GET /api/insight/analyses", s.handleGetInsightAnalyses)
	mux.HandleFunc("GET /api/insight/proposals", s.handleGetInsightProposals)
	mux.HandleFunc("GET /api/insight/proposals/{id}", s.handleGetInsightProposal)
	mux.HandleFunc("POST /api/insight/proposals/generate", s.handleTriggerInsightProposalGeneration)
	mux.HandleFunc("GET /api/insight/dashboard", s.handleGetInsightDashboard)
	mux.HandleFunc("PATCH /api/insight/proposals/{id}/status", s.handleUpdateInsightProposalStatus)

	// Insight Scorecards
	mux.HandleFunc("GET /api/insight/scorecards/agents", s.handleGetAgentScorecards)
	mux.HandleFunc("GET /api/insight/scorecards/agents/{name}", s.handleGetAgentScorecardByName)
	mux.HandleFunc("GET /api/insight/scorecards/workflows", s.handleGetWorkflowScorecards)
	mux.HandleFunc("GET /api/insight/scorecards/workflows/{type}", s.handleGetWorkflowScorecardByType)
	mux.HandleFunc("POST /api/insight/scorecards/compute", s.handleTriggerScorecardComputation)
	mux.HandleFunc("GET /api/insight/scorecards/highlights", s.handleGetScorecardHighlights)

	// Insight Routing Recommendations
	mux.HandleFunc("GET /api/insight/routing/recommendations", s.handleGetRoutingRecommendations)
	mux.HandleFunc("GET /api/insight/routing/recommendations/{id}", s.handleGetRoutingRecommendation)
	mux.HandleFunc("POST /api/insight/routing/analyze", s.handleTriggerRoutingAnalysis)

	// Insight LLM
	mux.HandleFunc("POST /api/insight/llm/test", s.handleTestLLMConnection)

	// Agent Evolution
	mux.HandleFunc("GET /api/insight/agent-evolution/summary", s.handleGetAgentEvolutionSummary)
	mux.HandleFunc("GET /api/insight/agent-evolution/opportunities", s.handleGetAgentEvolutionOpportunities)
	mux.HandleFunc("POST /api/insight/agent-evolution/trigger", s.handleTriggerAgentEvolution)
	mux.HandleFunc("GET /api/insight/agent-evolution/candidates", s.handleGetAgentCandidates)
	mux.HandleFunc("GET /api/insight/agent-evolution/candidates/{id}", s.handleGetAgentCandidateByID)
	mux.HandleFunc("POST /api/insight/agent-evolution/candidates/{id}/approve", s.handleApproveAgentCandidate)
	mux.HandleFunc("POST /api/insight/agent-evolution/candidates/{id}/reject", s.handleRejectAgentCandidate)
	mux.HandleFunc("POST /api/insight/agent-evolution/candidates/{id}/experiment", s.handleStartAgentExperiment)
	mux.HandleFunc("GET /api/insight/agent-evolution/candidates/{id}/markdown", s.handleGetAgentCandidateMarkdown)
	mux.HandleFunc("GET /api/insight/agent-evolution/experiments", s.handleGetAgentExperiments)
	mux.HandleFunc("GET /api/insight/agent-evolution/experiments/{id}", s.handleGetAgentExperimentByID)
	mux.HandleFunc("GET /api/insight/agent-evolution/experiments/{id}/results", s.handleGetAgentExperimentResults)
	mux.HandleFunc("POST /api/insight/agent-evolution/experiments/{id}/cancel", s.handleCancelAgentExperiment)

	// Product Intelligence
	mux.HandleFunc("GET /api/pi/projects", s.handlePIListProjects)
	mux.HandleFunc("POST /api/pi/projects", s.handlePIRegisterProject)
	mux.HandleFunc("GET /api/pi/projects/{id}", s.handlePIGetProject)
	mux.HandleFunc("DELETE /api/pi/projects/{id}", s.handlePIDeleteProject)
	mux.HandleFunc("POST /api/pi/projects/{id}/analyze", s.handlePIAnalyzeProject)
	mux.HandleFunc("GET /api/pi/projects/{id}/features", s.handlePIGetProjectFeatures)
	mux.HandleFunc("GET /api/pi/projects/{id}/gaps", s.handlePIGetProjectGaps)
	mux.HandleFunc("GET /api/pi/projects/{id}/proposals", s.handlePIGetProjectProposals)
	mux.HandleFunc("GET /api/pi/proposals/{id}", s.handlePIGetProposal)
	mux.HandleFunc("POST /api/pi/proposals/{id}/accept", s.handlePIAcceptProposal)
	mux.HandleFunc("POST /api/pi/proposals/{id}/reject", s.handlePIRejectProposal)
	mux.HandleFunc("GET /api/pi/market-features", s.handlePIGetMarketFeatures)
	mux.HandleFunc("POST /api/pi/market-research/refresh", s.handlePIRefreshMarketResearch)
	mux.HandleFunc("GET /api/pi/dashboard", s.handlePIGetDashboard)

	// Agents
	mux.HandleFunc("GET /api/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/agents/{name}", s.handleGetAgent)
	mux.HandleFunc("POST /api/agents", s.handleCreateAgent)
	mux.HandleFunc("PUT /api/agents/{name}", s.handleUpdateAgent)
	mux.HandleFunc("DELETE /api/agents/{name}", s.handleDeleteAgent)
	mux.HandleFunc("PUT /api/agents/{name}/skills", s.handleAssignSkills)

	// Skills
	mux.HandleFunc("GET /api/skills", s.handleListSkills)
	mux.HandleFunc("GET /api/skills/{name}", s.handleGetSkill)
	mux.HandleFunc("POST /api/skills", s.handleCreateSkill)
	mux.HandleFunc("PUT /api/skills/{name}", s.handleUpdateSkill)
	mux.HandleFunc("DELETE /api/skills/{name}", s.handleDeleteSkill)

	// Rules
	mux.HandleFunc("GET /api/rules", s.handleListRules)
	mux.HandleFunc("GET /api/rules/{name}", s.handleGetRule)
	mux.HandleFunc("POST /api/rules", s.handleCreateRule)
	mux.HandleFunc("PUT /api/rules/{name}", s.handleUpdateRule)
	mux.HandleFunc("DELETE /api/rules/{name}", s.handleDeleteRule)

	// Guardian
	mux.HandleFunc("GET /api/guardian/alerts", s.handleListGuardianAlerts)
	mux.HandleFunc("PUT /api/guardian/alerts/{id}/dismiss", s.handleDismissGuardianAlert)
	mux.HandleFunc("DELETE /api/guardian/alerts/{id}", s.handleDeleteGuardianAlert)
	mux.HandleFunc("GET /api/guardian/config", s.handleGetGuardianConfig)
	mux.HandleFunc("PUT /api/guardian/config", s.handleUpdateGuardianConfig)
	mux.HandleFunc("POST /api/guardian/run", s.handleRunGuardianScan)
	mux.HandleFunc("POST /api/guardian/test-llm", s.handleTestGuardianLLM)

	// Metrics
	mux.HandleFunc("POST /api/metrics/aggregate", s.handleMetricsAggregate)
	mux.HandleFunc("GET /api/metrics/summary", s.handleMetricsSummary)
	mux.HandleFunc("GET /api/metrics/workflows", s.handleMetricsWorkflows)
	mux.HandleFunc("GET /api/metrics/daily", s.handleMetricsDaily)
	mux.HandleFunc("GET /api/metrics/agents", s.handleMetricsAgents)
	mux.HandleFunc("GET /api/metrics/projects", s.handleMetricsProjects)

	// Terminal
	mux.HandleFunc("POST /api/terminal/upload-image", s.handleTerminalUploadImage)

	// WebSocket
	mux.HandleFunc("/api/ws", s.hub.ServeWS)
	mux.HandleFunc("/api/terminal/ws", s.terminal.ServeWS)

	// Static files (embedded Svelte SPA) with SPA fallback to index.html
	mux.Handle("/", s.spaHandler())

	return corsMiddleware(mux)
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(port int) error {
	addr := fmt.Sprintf(":%d", port)
	return http.ListenAndServe(addr, s.Handler())
}

// spaHandler serves the embedded Svelte frontend. For any path that doesn't
// correspond to a real file (e.g. /memory, /retrieval), it falls back to
// index.html so the client-side router can take over.
func (s *Server) spaHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.staticFiles))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip leading slash for fs.Stat
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(s.staticFiles, path); err != nil {
			// File not found — serve index.html for SPA routing
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
