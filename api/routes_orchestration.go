package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/insight/events"
	insightllm "github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

// ── Risk scoring ─────────────────────────────────────────────────────────────

var riskKeywords = map[string]float64{
	"auth": 0.15, "oauth": 0.15, "jwt": 0.15, "login": 0.10, "password": 0.15,
	"payment": 0.20, "stripe": 0.15, "billing": 0.15,
	"migration": 0.20, "schema": 0.15, "database": 0.10,
	"security": 0.15, "vulnerability": 0.20, "permission": 0.10,
	"breaking": 0.15, "remove": 0.10, "delete": 0.10, "refactor": 0.10,
	"deploy": 0.10, "production": 0.10, "release": 0.10,
}

var domainKeywords = map[string]string{
	"frontend": "frontend", "ui": "frontend", "component": "frontend", "css": "frontend",
	"api": "backend", "server": "backend", "endpoint": "backend", "handler": "backend",
	"auth": "backend", "oauth": "backend", "jwt": "backend", "login": "backend",
	"database": "database", "db": "database", "sql": "database", "query": "database", "migration": "database",
	"schema": "database",
	"test": "qa", "spec": "qa", "coverage": "qa", "playwright": "qa",
	"deploy": "devops", "docker": "devops", "ci": "devops", "kubernetes": "devops", "infra": "devops",
	"mobile": "mobile", "ios": "mobile", "android": "mobile",
}

var bugKeywords = []string{"bug", "fix", "error", "crash", "broken", "fail", "issue", "patch", "regression"}

type analyzeRequest struct {
	Description string   `json:"description"`
	FilesHint   []string `json:"files_hint"`
}

// AnalysisResult is the response from POST /api/workflows/analyze.
type AnalysisResult struct {
	RecommendedType       string                          `json:"recommended_type"`
	RecommendedComplexity string                          `json:"recommended_complexity"`
	RecommendedStrategy   string                          `json:"recommended_strategy"`
	RiskScore             float64                         `json:"risk_score"`
	RiskLevel             string                          `json:"risk_level"`
	RiskFactors           []string                        `json:"risk_factors"`
	EstimatedDurationMin  int                             `json:"estimated_duration_min"`
	SuggestedDomains      []string                        `json:"suggested_domains"`
	SimilarPastWorkflows  []orchestration.SimilarWorkflow `json:"similar_past_workflows"`
	LLMAnalysis           string                          `json:"llm_analysis,omitempty"`
}

// normalizeWords lowercases the description and strips punctuation from each token
// so "JWT," and "auth." match their keyword entries.
func normalizeWords(s string) []string {
	lower := strings.ToLower(s)
	var words []string
	for _, w := range strings.Fields(lower) {
		w = strings.Trim(w, ".,;:!?()[]{}\"'`/\\")
		if w != "" {
			words = append(words, w)
		}
	}
	return words
}

func calculateRisk(req analyzeRequest, history *orchestration.WorkflowHistorySummary) AnalysisResult {
	desc := strings.ToLower(req.Description)
	words := normalizeWords(req.Description)

	// Deduplicated domain set
	domainSet := map[string]struct{}{}
	for _, w := range words {
		if d, ok := domainKeywords[w]; ok {
			domainSet[d] = struct{}{}
		}
	}
	// Also infer domains from files_hint paths
	for _, f := range req.FilesHint {
		fl := strings.ToLower(f)
		for kw, d := range domainKeywords {
			if strings.Contains(fl, kw) {
				domainSet[d] = struct{}{}
			}
		}
	}
	domains := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}
	sort.Strings(domains)

	// A) Keyword risk
	var keywordRisk float64
	var matchedKeywords []string
	for _, w := range words {
		if weight, ok := riskKeywords[w]; ok {
			keywordRisk += weight
			matchedKeywords = append(matchedKeywords, w)
		}
	}
	if keywordRisk > 0.50 {
		keywordRisk = 0.50
	}

	// B) Historical risk
	historyRisk := history.AbortRate * 0.30

	// C) Domain count risk
	domainRisk := float64(len(domains)) * 0.05
	if domainRisk > 0.20 {
		domainRisk = 0.20
	}

	// Total risk score
	riskScore := keywordRisk + historyRisk + domainRisk
	if riskScore > 1.0 {
		riskScore = 1.0
	}
	// Round to 2 decimal places
	riskScore = float64(int(riskScore*100+0.5)) / 100

	riskLevel := "low"
	if riskScore >= 0.65 {
		riskLevel = "high"
	} else if riskScore >= 0.35 {
		riskLevel = "medium"
	}

	// Risk factors (human-readable)
	var factors []string
	if len(matchedKeywords) > 0 {
		factors = append(factors, "Contains high-risk keywords: "+strings.Join(matchedKeywords, ", "))
	}
	if len(domains) >= 3 {
		factors = append(factors, "Affects "+strconv.Itoa(len(domains))+" domains: "+strings.Join(domains, ", "))
	} else if len(domains) > 0 {
		factors = append(factors, "Domains involved: "+strings.Join(domains, ", "))
	}
	if history.AbortRate > 0.10 {
		pct := int(history.AbortRate*100 + 0.5)
		factors = append(factors, strconv.Itoa(pct)+"% abort rate in past similar workflows")
	}
	if len(factors) == 0 {
		factors = append(factors, "No significant risk factors detected")
	}

	// Recommendations
	recType := "spec"
	for _, kw := range bugKeywords {
		if strings.Contains(desc, kw) {
			recType = "bug"
			break
		}
	}
	recComplexity := "simple"
	if riskScore >= 0.60 {
		recComplexity = "complex"
	}
	recStrategy := "single"
	if len(domains) >= 3 {
		recStrategy = "swarm"
	}

	// Duration estimate
	base := history.AvgDurationMin
	if base == 0 {
		base = 30
	}
	estimated := int(base*(1.0+riskScore*1.5) + 0.5)

	similar := history.SimilarWorkflows
	if similar == nil {
		similar = []orchestration.SimilarWorkflow{}
	}

	return AnalysisResult{
		RecommendedType:       recType,
		RecommendedComplexity: recComplexity,
		RecommendedStrategy:   recStrategy,
		RiskScore:             riskScore,
		RiskLevel:             riskLevel,
		RiskFactors:           factors,
		EstimatedDurationMin:  estimated,
		SuggestedDomains:      domains,
		SimilarPastWorkflows:  similar,
	}
}

func (s *Server) handleAnalyzeWorkflow(w http.ResponseWriter, r *http.Request) {
	var req analyzeRequest
	if err := decodeBody(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		jsonErr(w, http.StatusBadRequest, "description is required")
		return
	}

	// Detect workflow type from description for similar-workflow lookup
	recType := "spec"
	desc := strings.ToLower(req.Description)
	for _, kw := range bugKeywords {
		if strings.Contains(desc, kw) {
			recType = "bug"
			break
		}
	}

	history, err := s.coordinator.WorkflowHistory(recType)
	if err != nil {
		history = &orchestration.WorkflowHistorySummary{}
	}

	result := calculateRisk(req, history)

	// Enrich with LLM analysis if a shared client is configured.
	if s.guardianLLM != nil {
		// Gather context from Vexor (code embeddings) and governance docs
		var contextParts []string

		if s.vexor != nil {
			if results, err := s.vexor.Search(req.Description, 5, "auto"); err == nil && len(results) > 0 {
				var sb strings.Builder
				sb.WriteString("Relevant code files (from semantic search):\n")
				for _, r := range results {
					sb.WriteString(fmt.Sprintf("- %s (lines %d-%d, score %.2f): %s\n", r.FilePath, r.LineStart, r.LineEnd, r.Score, r.Heading))
					if r.Excerpt != "" {
						excerpt := r.Excerpt
						if len(excerpt) > 300 {
							excerpt = excerpt[:300] + "..."
						}
						sb.WriteString("  ```\n  " + excerpt + "\n  ```\n")
					}
				}
				contextParts = append(contextParts, sb.String())
			}
		}

		if govDocs, err := s.db.SearchDocs(req.Description, "", "", 5); err == nil && len(govDocs) > 0 {
			var sb strings.Builder
			sb.WriteString("Relevant governance rules and docs:\n")
			for _, d := range govDocs {
				sb.WriteString(fmt.Sprintf("- [%s] %s", d.DocType, d.Title))
				if d.Content != "" {
					content := d.Content
					if len(content) > 300 {
						content = content[:300] + "..."
					}
					sb.WriteString(": " + content)
				}
				sb.WriteString("\n")
			}
			contextParts = append(contextParts, sb.String())
		}

		contextBlock := ""
		if len(contextParts) > 0 {
			contextBlock = "\n\nCodebase context:\n" + strings.Join(contextParts, "\n")
		}

		systemPrompt := "You are a senior software architect performing risk analysis for code changes. " +
			"You have access to the actual codebase context and governance rules. " +
			"Be concise and practical. Respond in 3-5 bullet points."
		userPrompt := fmt.Sprintf(
			"Analyze the risk of this task:\n\nDescription: %s\n\nHeuristic risk score: %.2f (%s)\nDetected domains: %s\nRisk factors: %s%s\n\n"+
				"Based on the codebase context and governance rules, provide insights: what files will likely be affected, "+
				"what could go wrong, which governance rules apply, and any recommendations.",
			req.Description,
			result.RiskScore, result.RiskLevel,
			strings.Join(result.SuggestedDomains, ", "),
			strings.Join(result.RiskFactors, "; "),
			contextBlock,
		)
		resp, err := s.guardianLLM.Complete(r.Context(), insightllm.CompletionRequest{
			SystemPrompt: systemPrompt,
			Messages:     []insightllm.Message{{Role: "user", Content: userPrompt}},
		})
		if err == nil {
			result.LLMAnalysis = resp.Content
		}
	}

	json200(w, result)
}

func (s *Server) handleStartWorkflow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID         string `json:"id"`
		Type       string `json:"type"`       // "spec" | "bug" | "e2e"
		Complexity string `json:"complexity"` // "simple" | "complex"
		Title      string `json:"title"`
		SessionID  string `json:"session_id"` // Claude Code session — optional
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.ID == "" {
		jsonErr(w, http.StatusBadRequest, "id is required")
		return
	}
	wtype := orchestration.WorkflowSpec
	if body.Type == "bug" {
		wtype = orchestration.WorkflowBug
	} else if body.Type == "e2e" {
		wtype = orchestration.WorkflowE2E
	}
	complexity := orchestration.ComplexitySimple
	if body.Complexity == "complex" {
		complexity = orchestration.ComplexityComplex
	}
	state, err := s.coordinator.Start(body.ID, wtype, complexity, body.Title)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if body.SessionID != "" {
		if err2 := s.coordinator.SetSessionID(body.ID, body.SessionID); err2 == nil {
			state.SessionID = body.SessionID
		}
	}
	if commit, err2 := runGit(s.projectRoot, "rev-parse", "HEAD"); err2 == nil {
		commit = strings.TrimSpace(commit)
		if err2 := s.coordinator.SetBaseCommit(body.ID, commit); err2 == nil {
			state.BaseCommit = commit
		}
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	s.hub.BroadcastJSON("workflow_started", map[string]any{
		"workflow_id": body.ID,
		"type":        body.Type,
		"complexity":  body.Complexity,
		"title":       body.Title,
	})
	json200(w, state)
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, err := s.coordinator.Get(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	json200(w, state)
}

func (s *Server) handleTransitionPhase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Phase string `json:"phase"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	state, err := s.coordinator.Transition(id, orchestration.Phase(body.Phase))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	s.hub.BroadcastJSON("phase_changed", map[string]any{
		"workflow_id": id,
		"phase":       body.Phase,
	})
	s.emitEvent(events.EventPhaseTransition, "orchestration", map[string]any{
		"workflow_id":   state.ID,
		"workflow_type": string(state.Type),
		"to_phase":      string(state.Phase),
		"title":         state.Title,
	})
	if body.Phase == "complete" {
		s.hub.BroadcastJSON("workflow_completed", map[string]any{
			"workflow_id": id,
			"success":     true,
		})
		go s.generateChangeSummary(id, state.BaseCommit)
	}
	json200(w, state)
}

func (s *Server) handleRecordDelegation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		AgentID string `json:"agent_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	state, err := s.coordinator.RecordDelegation(id, body.AgentID)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleSetTasks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Tasks []string `json:"tasks"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	state, err := s.coordinator.SetTasks(id, body.Tasks)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleStartTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid task index")
		return
	}
	state, err := s.coordinator.StartTask(id, index)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid task index")
		return
	}
	state, err := s.coordinator.CompleteTask(id, index)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	s.hub.BroadcastJSON("task_completed", map[string]any{
		"workflow_id": id,
		"task_index":  index,
	})
	json200(w, state)
}

func (s *Server) handleAbortWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, err := s.coordinator.Abort(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_aborted", map[string]string{"id": id})
	json200(w, state)
}

func (s *Server) handleSetWorkflowSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.SessionID == "" {
		jsonErr(w, http.StatusBadRequest, "session_id is required")
		return
	}
	state, err := s.coordinator.UpdateSessionID(id, body.SessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, orchestration.ErrWorkflowNotFound) {
			status = http.StatusNotFound
		}
		jsonErr(w, status, err.Error())
		return
	}
	json200(w, state)
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	workflows, err := s.coordinator.ListAll()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if workflows == nil {
		workflows = []*orchestration.WorkflowState{}
	}
	json200(w, workflows)
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.coordinator.Delete(id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, orchestration.ErrWorkflowNotFound) {
			status = http.StatusNotFound
		}
		jsonErr(w, status, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_deleted", map[string]string{"id": id})
	json200(w, map[string]bool{"deleted": true})
}

func (s *Server) handleSetPlanContent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	state, err := s.coordinator.SetPlanContent(id, body.Content)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, orchestration.ErrWorkflowNotFound) {
			status = http.StatusNotFound
		}
		jsonErr(w, status, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleSetDesignContent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	state, err := s.coordinator.SetDesignContent(id, body.Content)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, orchestration.ErrWorkflowNotFound) {
			status = http.StatusNotFound
		}
		jsonErr(w, status, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	json200(w, state)
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, err := s.coordinator.Get(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	phase := string(state.Phase)
	delegated := state.Delegated[phase]
	if delegated == nil {
		delegated = []string{}
	}
	json200(w, map[string]any{
		"workflow_id":      id,
		"type":             state.Type,
		"phase":            phase,
		"delegated_agents": delegated,
		"total_tasks":      state.TotalTasks,
		"current_task":     state.CurrentTask,
		"tasks":            state.Tasks,
	})
}

type pastItem struct {
	Kind      string      `json:"kind"`
	Data      interface{} `json:"data"`
	UpdatedAt string      `json:"-"`
}

func (s *Server) handleListPast(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 20)
	offset := queryInt(r, "offset", 0)
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	wfCount, err := s.coordinator.CountPastWorkflows()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	mCount, err := s.swarm.CountPastMissions()
	if err != nil {
		mCount = 0
	}
	total := wfCount + mCount

	fetchLimit := offset + limit

	workflows, err := s.coordinator.ListPastWorkflows(0, fetchLimit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	missions, err := s.swarm.ListPastMissions(0, fetchLimit)
	if err != nil {
		missions = nil
	}

	items := make([]pastItem, 0, len(workflows)+len(missions))
	for _, wf := range workflows {
		items = append(items, pastItem{Kind: "workflow", Data: wf, UpdatedAt: wf.UpdatedAt})
	}
	for _, m := range missions {
		items = append(items, pastItem{Kind: "mission", Data: m, UpdatedAt: m.UpdatedAt})
	}

	sort.Slice(items, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339Nano, items[i].UpdatedAt)
		tj, _ := time.Parse(time.RFC3339Nano, items[j].UpdatedAt)
		return ti.After(tj)
	})

	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	page := items[offset:end]
	if page == nil {
		page = []pastItem{}
	}

	json200(w, map[string]any{
		"items":  page,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	})
}
