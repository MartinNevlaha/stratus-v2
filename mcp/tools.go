package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
)

// RegisterTools registers Stratus MCP tools on the server.
func RegisterTools(s *Server, apiBase string, httpClient *http.Client) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	client := &apiClient{base: apiBase, http: httpClient}

	s.Register(Tool{
		Name:        "search",
		Description: "Search memory events using full-text search. Returns index with IDs (~50-100 tokens/result).",
		InputSchema: obj(
			req("query", "string", "Full-text search query"),
			opt("limit", "integer", "Max results (default: 20)"),
			opt("type", "string", "Filter by type (discovery, decision, bugfix, feature, etc.)"),
			opt("scope", "string", "Filter by scope (repo, global, user)"),
			opt("project", "string", "Filter by project name"),
			opt("date_start", "string", "ISO 8601 start date"),
			opt("date_end", "string", "ISO 8601 end date"),
			opt("offset", "integer", "Pagination offset"),
		),
		Handler: func(args map[string]any) (any, error) {
			params := neturl.Values{}
			if q, ok := args["query"].(string); ok {
				params.Set("q", q)
			}
			for _, k := range []string{"type", "scope", "project", "date_start", "date_end"} {
				if v, ok := args[k].(string); ok && v != "" {
					params.Set(k, v)
				}
			}
			if n := intArg(args, "limit", 0); n > 0 {
				params.Set("limit", fmt.Sprintf("%d", n))
			}
			if n := intArg(args, "offset", 0); n > 0 {
				params.Set("offset", fmt.Sprintf("%d", n))
			}
			return client.get("/api/events/search", params)
		},
	})

	s.Register(Tool{
		Name:        "timeline",
		Description: "Get chronological context around a memory event ID.",
		InputSchema: obj(
			opt("anchor_id", "integer", "Memory event ID to center on"),
			opt("query", "string", "Find best match by query, then show timeline"),
			opt("depth_before", "integer", "Events before anchor (default: 10)"),
			opt("depth_after", "integer", "Events after anchor (default: 10)"),
		),
		Handler: func(args map[string]any) (any, error) {
			anchorID := intArg(args, "anchor_id", 0)
			// If query provided, search for anchor first
			if anchorID == 0 {
				q, _ := args["query"].(string)
				if q == "" {
					return nil, fmt.Errorf("anchor_id or query is required")
				}
				p := neturl.Values{"q": {q}, "limit": {"1"}}
				results, err := client.get("/api/events/search", p)
				if err != nil {
					return nil, err
				}
				if m, ok := results.(map[string]any); ok {
					if items, ok := m["results"].([]any); ok && len(items) > 0 {
						if item, ok := items[0].(map[string]any); ok {
							if id, ok := item["id"].(float64); ok {
								anchorID = int(id)
							}
						}
					}
				}
			}
			if anchorID == 0 {
				return nil, fmt.Errorf("no event found for query")
			}
			params := neturl.Values{
				"before": {fmt.Sprintf("%d", intArg(args, "depth_before", 10))},
				"after":  {fmt.Sprintf("%d", intArg(args, "depth_after", 10))},
			}
			return client.get(fmt.Sprintf("/api/events/%d/timeline", anchorID), params)
		},
	})

	s.Register(Tool{
		Name:        "get_observations",
		Description: "Fetch full details for memory event IDs. ALWAYS batch for 2+ items.",
		InputSchema: obj(
			req("ids", "array", "Array of memory event IDs to fetch"),
		),
		Handler: func(args map[string]any) (any, error) {
			return client.post("/api/events/batch", args)
		},
	})

	s.Register(Tool{
		Name:        "save_memory",
		Description: "Save a memory for future search. Use for important discoveries, decisions, patterns.",
		InputSchema: obj(
			req("text", "string", "Content to remember"),
			opt("title", "string", "Short title"),
			opt("type", "string", "Type: discovery|decision|bugfix|feature|refactor|etc."),
			opt("tags", "array", "Tags for categorization"),
			opt("actor", "string", "Who created this: user|agent|hook|system"),
			opt("scope", "string", "Scope: repo|global|user"),
			opt("importance", "number", "0.0-1.0 importance score"),
			opt("refs", "object", "References to other resources"),
			opt("ttl", "string", "ISO 8601 expiration date"),
			opt("dedupe_key", "string", "Unique key to prevent duplicate saves"),
			opt("project", "string", "Project name"),
		),
		Handler: func(args map[string]any) (any, error) {
			return client.post("/api/events", convertMemoryArgs(args))
		},
	})

	s.Register(Tool{
		Name:        "retrieve",
		Description: "Semantic search across code (Vexor), governance docs, and wiki knowledge pages. Auto-routes by query type.",
		InputSchema: obj(
			req("query", "string", "Search query for code, governance docs, or wiki knowledge"),
			opt("corpus", "string", "Force search corpus: 'code', 'governance', or 'wiki'. Omit for auto-routing across all sources."),
			opt("top_k", "integer", "Max results (default: 10)"),
		),
		Handler: func(args map[string]any) (any, error) {
			params := neturl.Values{}
			if q, ok := args["query"].(string); ok {
				params.Set("q", q)
			}
			if corpus, ok := args["corpus"].(string); ok && corpus != "" {
				params.Set("corpus", corpus)
			}
			if topK := intArg(args, "top_k", 0); topK > 0 {
				params.Set("top_k", fmt.Sprintf("%d", topK))
			}
			return client.get("/api/retrieve", params)
		},
	})

	s.Register(Tool{
		Name:        "index_status",
		Description: "Check index freshness and backend availability.",
		InputSchema: obj(),
		Handler: func(args map[string]any) (any, error) {
			return client.get("/api/retrieve/status", nil)
		},
	})

	s.Register(Tool{
		Name:        "delivery_dispatch",
		Description: "Get delivery phase briefing, active workflows, and delegation instructions.",
		InputSchema: obj(
			opt("workflow_id", "string", "Specific workflow ID (omit for latest active)"),
		),
		Handler: func(args map[string]any) (any, error) {
			var wfID string

			if id, ok := args["workflow_id"].(string); ok && id != "" {
				wfID = id
			} else {
				dash, err := client.get("/api/dashboard/state", nil)
				if err != nil {
					return nil, err
				}
				if m, ok := dash.(map[string]any); ok {
					if workflows, ok := m["workflows"].([]any); ok && len(workflows) > 0 {
						if w, ok := workflows[0].(map[string]any); ok {
							wfID, _ = w["id"].(string)
						}
					}
				}
			}

			if wfID == "" {
				return map[string]any{"workflows": []any{}, "message": "no active workflows"}, nil
			}

			result, err := client.get(fmt.Sprintf("/api/workflows/%s/dispatch", wfID), nil)
			if err != nil {
				return nil, err
			}

			return result, nil
		},
	})

	// --- Workflow management tools ---

	s.Register(Tool{
		Name:        "register_workflow",
		Description: "Register a new workflow. REQUIRED before any Task delegation to delivery agents. Use this to start a spec, bug, or e2e workflow.",
		InputSchema: obj(
			req("id", "string", "Unique workflow ID (use format: <type>-<slug>, e.g. 'bug-fix-login', 'spec-user-auth')"),
			req("type", "string", "Workflow type: 'spec' | 'bug' | 'e2e'"),
			req("title", "string", "Human-readable title for the workflow"),
			opt("session_id", "string", "Claude session ID (use ${CLAUDE_SESSION_ID} for automatic tracking)"),
			opt("complexity", "string", "For spec workflows: 'simple' | 'complex'"),
		),
		Handler: func(args map[string]any) (any, error) {
			return client.post("/api/workflows", args)
		},
	})

	s.Register(Tool{
		Name:        "transition_phase",
		Description: "Transition a workflow to the next phase. Optionally set tasks and plan before transitioning. Validates against state machine rules.",
		InputSchema: obj(
			req("workflow_id", "string", "Workflow ID to transition"),
			req("phase", "string", "Target phase (e.g. 'implement', 'verify', 'review', 'complete')"),
			opt("tasks", "array", "Task titles to set before transitioning (for plan→implement)"),
			opt("plan_content", "string", "Full markdown plan content to set before transitioning"),
		),
		Handler: func(args map[string]any) (any, error) {
			id, _ := args["workflow_id"].(string)
			if id == "" {
				return nil, fmt.Errorf("workflow_id is required")
			}
			phase, _ := args["phase"].(string)
			if phase == "" {
				return nil, fmt.Errorf("phase is required")
			}

			if tasks, err := convertTasksArg(args["tasks"]); err != nil {
				return nil, fmt.Errorf("invalid tasks: %w", err)
			} else if len(tasks) > 0 {
				if _, err := client.post(fmt.Sprintf("/api/workflows/%s/tasks", id), map[string]any{"tasks": tasks}); err != nil {
					return nil, fmt.Errorf("failed to set tasks: %w", err)
				}
			}

			if plan, _ := args["plan_content"].(string); plan != "" {
				if _, err := client.put(fmt.Sprintf("/api/workflows/%s/plan", id), map[string]any{"content": plan}); err != nil {
					return nil, fmt.Errorf("failed to set plan: %w", err)
				}
			}

			result, err := client.put(fmt.Sprintf("/api/workflows/%s/phase", id), map[string]any{"phase": phase})
			if err != nil {
				return nil, err
			}

			return result, nil
		},
	})

	s.Register(Tool{
		Name:        "delegate_agent",
		Description: "Record an agent delegation for the current workflow phase. Call this after delegating work via Task tool to track which agents worked on which phases.",
		InputSchema: obj(
			req("workflow_id", "string", "Workflow ID"),
			req("agent_id", "string", "Agent being delegated (e.g. 'delivery-backend-engineer', 'delivery-code-reviewer')"),
		),
		Handler: func(args map[string]any) (any, error) {
			id, _ := args["workflow_id"].(string)
			if id == "" {
				return nil, fmt.Errorf("workflow_id is required")
			}
			return client.post(fmt.Sprintf("/api/workflows/%s/delegate", id), args)
		},
	})

	s.Register(Tool{
		Name:        "start_task",
		Description: "Mark a workflow task as in_progress. Call this before delegating a task to an agent.",
		InputSchema: obj(
			req("workflow_id", "string", "Workflow ID"),
			req("task_index", "integer", "Zero-based task index"),
		),
		Handler: func(args map[string]any) (any, error) {
			id, _ := args["workflow_id"].(string)
			if id == "" {
				return nil, fmt.Errorf("workflow_id is required")
			}
			index := intArg(args, "task_index", -1)
			if index < 0 {
				return nil, fmt.Errorf("task_index is required")
			}
			return client.post(fmt.Sprintf("/api/workflows/%s/tasks/%d/start", id, index), nil)
		},
	})

	s.Register(Tool{
		Name:        "complete_task",
		Description: "Mark a workflow task as done. Call this after an agent successfully completes a task.",
		InputSchema: obj(
			req("workflow_id", "string", "Workflow ID"),
			req("task_index", "integer", "Zero-based task index"),
		),
		Handler: func(args map[string]any) (any, error) {
			id, _ := args["workflow_id"].(string)
			if id == "" {
				return nil, fmt.Errorf("workflow_id is required")
			}
			index := intArg(args, "task_index", -1)
			if index < 0 {
				return nil, fmt.Errorf("task_index is required")
			}
			return client.post(fmt.Sprintf("/api/workflows/%s/tasks/%d/complete", id, index), nil)
		},
	})

	s.Register(Tool{
		Name:        "get_workflow",
		Description: "Get current workflow state including phase, tasks, and delegation history.",
		InputSchema: obj(
			req("workflow_id", "string", "Workflow ID"),
		),
		Handler: func(args map[string]any) (any, error) {
			id, _ := args["workflow_id"].(string)
			if id == "" {
				return nil, fmt.Errorf("workflow_id is required")
			}
			return client.get(fmt.Sprintf("/api/workflows/%s", id), nil)
		},
	})

	s.Register(Tool{
		Name:        "list_workflows",
		Description: "List all active workflows.",
		InputSchema: obj(),
		Handler: func(args map[string]any) (any, error) {
			return client.get("/api/dashboard/state", nil)
		},
	})

	// --- Swarm tools (worker-facing) ---

	s.Register(Tool{
		Name:        "swarm_heartbeat",
		Description: "Send heartbeat from a swarm worker. Workers should call this periodically to stay active.",
		InputSchema: obj(
			req("worker_id", "string", "The worker's ID"),
		),
		Handler: func(args map[string]any) (any, error) {
			workerID, _ := args["worker_id"].(string)
			if workerID == "" {
				return nil, fmt.Errorf("worker_id is required")
			}
			return client.post(fmt.Sprintf("/api/swarm/workers/%s/heartbeat", workerID), nil)
		},
	})

	s.Register(Tool{
		Name:        "swarm_signals",
		Description: "Poll unread signals for a worker. Returns messages from Hub or other workers, then marks them as read.",
		InputSchema: obj(
			req("worker_id", "string", "The worker's ID"),
		),
		Handler: func(args map[string]any) (any, error) {
			workerID, _ := args["worker_id"].(string)
			if workerID == "" {
				return nil, fmt.Errorf("worker_id is required")
			}
			return client.get(fmt.Sprintf("/api/swarm/workers/%s/signals", workerID), nil)
		},
	})

	s.Register(Tool{
		Name:        "swarm_ticket_update",
		Description: "Update a ticket's status. Use to start work (in_progress), complete it (done), or report failure (failed).",
		InputSchema: obj(
			req("ticket_id", "string", "Ticket ID"),
			req("status", "string", "New status: in_progress | done | failed"),
			opt("result", "string", "Completion summary or failure reason"),
		),
		Handler: func(args map[string]any) (any, error) {
			ticketID, _ := args["ticket_id"].(string)
			if ticketID == "" {
				return nil, fmt.Errorf("ticket_id is required")
			}
			return client.put(fmt.Sprintf("/api/swarm/tickets/%s/status", ticketID), args)
		},
	})

	s.Register(Tool{
		Name:        "swarm_submit_merge",
		Description: "Submit worker's branch to the Forge (merge queue). Call after committing all changes.",
		InputSchema: obj(
			req("worker_id", "string", "Worker ID"),
		),
		Handler: func(args map[string]any) (any, error) {
			workerID, _ := args["worker_id"].(string)
			if workerID == "" {
				return nil, fmt.Errorf("worker_id is required")
			}
			return client.post("/api/swarm/forge/submit", map[string]any{"worker_id": workerID})
		},
	})

	s.Register(Tool{
		Name:        "swarm_send_signal",
		Description: "Send a signal to another worker or broadcast to all workers in the mission.",
		InputSchema: obj(
			req("from_worker", "string", "Sender worker ID"),
			req("type", "string", "Signal type: HELP, STATUS, TICKET_DONE, etc."),
			opt("to_worker", "string", "Recipient worker ID. Omit or '*' for broadcast."),
			opt("mission_id", "string", "Mission ID (auto-detected from worker if omitted)"),
			opt("payload", "string", "JSON payload with additional data"),
		),
		Handler: func(args map[string]any) (any, error) {
			return client.post("/api/swarm/signals", args)
		},
	})

	s.Register(Tool{
		Name:        "swarm_reserve_files",
		Description: "Reserve file patterns for exclusive editing by a worker. Checks for conflicts with other workers' reservations before reserving.",
		InputSchema: obj(
			req("worker_id", "string", "Worker ID requesting the reservation"),
			req("patterns", "array", "Array of glob patterns to reserve (e.g. [\"src/api/**\", \"db/schema.go\"])"),
			opt("reason", "string", "Why these files are needed"),
		),
		Handler: func(args map[string]any) (any, error) {
			workerID, _ := args["worker_id"].(string)
			if workerID == "" {
				return nil, fmt.Errorf("worker_id is required")
			}
			return client.post("/api/swarm/files/reserve", args)
		},
	})

	s.Register(Tool{
		Name:        "swarm_release_files",
		Description: "Release all file reservations held by a worker. Call after finishing edits.",
		InputSchema: obj(
			req("worker_id", "string", "Worker ID releasing reservations"),
		),
		Handler: func(args map[string]any) (any, error) {
			workerID, _ := args["worker_id"].(string)
			if workerID == "" {
				return nil, fmt.Errorf("worker_id is required")
			}
			return client.post("/api/swarm/files/release", map[string]any{"worker_id": workerID})
		},
	})

	s.Register(Tool{
		Name:        "swarm_checkpoint",
		Description: "Save a coordinator checkpoint for mission recovery. Records progress percentage and context state.",
		InputSchema: obj(
			req("mission_id", "string", "Mission ID"),
			req("progress", "integer", "Progress percentage 0-100"),
			opt("context", "string", "JSON string with coordinator state snapshot"),
		),
		Handler: func(args map[string]any) (any, error) {
			missionID, _ := args["mission_id"].(string)
			if missionID == "" {
				return nil, fmt.Errorf("mission_id is required")
			}
			body := map[string]any{
				"progress":   intArg(args, "progress", 0),
				"state_json": "{}",
			}
			if ctx, ok := args["context"].(string); ok && ctx != "" {
				body["state_json"] = ctx
			}
			return client.post(fmt.Sprintf("/api/swarm/missions/%s/checkpoint", missionID), body)
		},
	})

	// --- Evidence tracking ---

	s.Register(Tool{
		Name:        "swarm_record_evidence",
		Description: "Record structured evidence for a ticket. Use after completing meaningful actions (tests, builds, reviews) to create an audit trail that reviewers can inspect.",
		InputSchema: obj(
			req("ticket_id", "string", "Ticket ID this evidence belongs to"),
			req("type", "string", "Evidence type: diff | test_result | review | build | note | gate"),
			req("content", "string", "Evidence content (diff output, test results, review comments, etc.)"),
			opt("agent", "string", "Agent that produced this evidence"),
			opt("verdict", "string", "Verdict: pass | fail | info"),
		),
		Handler: func(args map[string]any) (any, error) {
			ticketID, _ := args["ticket_id"].(string)
			if ticketID == "" {
				return nil, fmt.Errorf("ticket_id is required")
			}
			return client.post(fmt.Sprintf("/api/swarm/tickets/%s/evidence", ticketID), args)
		},
	})

	s.Register(Tool{
		Name:        "swarm_get_evidence",
		Description: "List all evidence recorded for a ticket. Use during review to see what the worker produced.",
		InputSchema: obj(
			req("ticket_id", "string", "Ticket ID"),
		),
		Handler: func(args map[string]any) (any, error) {
			ticketID, _ := args["ticket_id"].(string)
			if ticketID == "" {
				return nil, fmt.Errorf("ticket_id is required")
			}
			return client.get(fmt.Sprintf("/api/swarm/tickets/%s/evidence", ticketID), nil)
		},
	})

	// --- Guardrails ---

	s.Register(Tool{
		Name:        "swarm_track_tool_call",
		Description: "Track a tool call for guardrail safety metrics. Returns allow/block action based on loop detection and tool call ceiling.",
		InputSchema: obj(
			req("worker_id", "string", "Worker ID"),
			req("tool_name", "string", "Name of the tool being called"),
			opt("mission_id", "string", "Mission ID (auto-detected from worker if omitted)"),
		),
		Handler: func(args map[string]any) (any, error) {
			workerID, _ := args["worker_id"].(string)
			if workerID == "" {
				return nil, fmt.Errorf("worker_id is required")
			}
			return client.post("/api/swarm/guardrails/track", args)
		},
	})

	s.Register(Tool{
		Name:        "swarm_execute_forge",
		Description: "Execute the forge merge queue for a mission. Sequentially merges all pending worker branches into the integration worktree, handling stash/unstash automatically. Returns merge results and any missing commits.",
		InputSchema: obj(
			req("mission_id", "string", "Mission ID"),
		),
		Handler: func(args map[string]any) (any, error) {
			missionID, _ := args["mission_id"].(string)
			if missionID == "" {
				return nil, fmt.Errorf("mission_id is required")
			}
			return client.post("/api/swarm/missions/"+missionID+"/forge/execute", map[string]any{})
		},
	})

	// --- Wiki tools ---

	s.Register(Tool{
		Name:        "wiki_search",
		Description: "Search knowledge wiki pages using full-text search.",
		InputSchema: obj(
			req("query", "string", "Search query"),
			opt("type", "string", "Filter by page type (summary, entity, concept, answer, index)"),
			opt("limit", "integer", "Max results (default: 20)"),
		),
		Handler: func(args map[string]any) (any, error) {
			params := neturl.Values{}
			if q, ok := args["query"].(string); ok && q != "" {
				params.Set("q", q)
			}
			if t, ok := args["type"].(string); ok && t != "" {
				params.Set("type", t)
			}
			if n := intArg(args, "limit", 0); n > 0 {
				params.Set("limit", fmt.Sprintf("%d", n))
			}
			return client.get("/api/wiki/search", params)
		},
	})

	s.Register(Tool{
		Name:        "wiki_query",
		Description: "Synthesis query: asks an LLM to answer a question from wiki knowledge with citations.",
		InputSchema: obj(
			req("query", "string", "Natural language question to answer from the wiki"),
			opt("persist", "boolean", "Whether to persist the answer as a new wiki page"),
			opt("max_sources", "integer", "Maximum number of source pages to use (default: 10)"),
		),
		Handler: func(args map[string]any) (any, error) {
			body := map[string]any{}
			if q, ok := args["query"].(string); ok {
				body["query"] = q
			}
			if p, ok := args["persist"].(bool); ok {
				body["persist"] = p
			}
			if n := intArg(args, "max_sources", 0); n > 0 {
				body["max_sources"] = n
			}
			return client.post("/api/wiki/query", body)
		},
	})

	// --- Evolution tools ---

	s.Register(Tool{
		Name:        "evolution_status",
		Description: "Get recent agent evolution runs and their outcomes.",
		InputSchema: obj(
			opt("limit", "integer", "Max runs to return (default: 20)"),
		),
		Handler: func(args map[string]any) (any, error) {
			params := neturl.Values{}
			if n := intArg(args, "limit", 0); n > 0 {
				params.Set("limit", fmt.Sprintf("%d", n))
			}
			return client.get("/api/evolution/runs", params)
		},
	})

	s.Register(Tool{
		Name:        "evolution_trigger",
		Description: "Trigger an autonomous evolution cycle that tests hypotheses and applies improvements.",
		InputSchema: obj(
			opt("timeout_ms", "integer", "Timeout in milliseconds (default: 600000)"),
			opt("categories", "array", "Hypothesis categories to test: prompt_tuning, workflow_routing, agent_selection, threshold_adjustment"),
		),
		Handler: func(args map[string]any) (any, error) {
			body := map[string]any{}
			if n := intArg(args, "timeout_ms", 0); n > 0 {
				body["timeout_ms"] = n
			}
			if cats, ok := args["categories"].([]any); ok {
				body["categories"] = cats
			}
			return client.post("/api/evolution/trigger", body)
		},
	})

	// --- Code analysis tools ---

	s.Register(Tool{
		Name:        "code_analysis_trigger",
		Description: "Trigger a code quality analysis run on the host project. Analyzes top files by churn/risk score for anti-patterns, duplication, coverage gaps, error handling issues, complexity, dead code, and security concerns.",
		InputSchema: obj(
			opt("categories", "array", "Categories to analyze. Allowed: anti_pattern, duplication, coverage_gap, error_handling, complexity, dead_code, security. Empty = all."),
		),
		Handler: func(args map[string]any) (any, error) {
			body := map[string]any{}
			if cats, ok := args["categories"].([]any); ok {
				body["categories"] = cats
			}
			return client.post("/api/code-analysis/trigger", body)
		},
	})

	s.Register(Tool{
		Name:        "code_analysis_findings",
		Description: "Query code quality findings from the most recent analysis. Filter by file path, category, or severity to find specific issues.",
		InputSchema: obj(
			opt("file", "string", "Filter by file path (prefix match)"),
			opt("category", "string", "Filter by category: anti_pattern, duplication, coverage_gap, error_handling, complexity, dead_code, security"),
			opt("severity", "string", "Filter by severity: critical, warning, info"),
			opt("q", "string", "Full-text search across finding titles and descriptions"),
			opt("limit", "integer", "Max results to return (default: 20)"),
		),
		Handler: func(args map[string]any) (any, error) {
			params := neturl.Values{}
			for _, k := range []string{"file", "category", "severity", "q"} {
				if v, ok := args[k].(string); ok && v != "" {
					params.Set(k, v)
				}
			}
			if n := intArg(args, "limit", 0); n > 0 {
				params.Set("limit", fmt.Sprintf("%d", n))
			}
			return client.get("/api/code-analysis/findings", params)
		},
	})

	s.Register(Tool{
		Name:        "code_quality_summary",
		Description: "Get aggregated code quality metrics for the project over time. Shows trends in findings count, severity distribution, and coverage.",
		InputSchema: obj(
			opt("days", "integer", "Number of days of history to return (default: 30, max: 365)"),
		),
		Handler: func(args map[string]any) (any, error) {
			params := neturl.Values{}
			if n := intArg(args, "days", 0); n > 0 {
				params.Set("days", fmt.Sprintf("%d", n))
			}
			return client.get("/api/code-analysis/metrics", params)
		},
	})

	s.Register(Tool{
		Name:        "code_quality_finding_update",
		Description: "Update the lifecycle status of a code quality finding (rejected or applied). Agents should call this with status='applied' at the end of a successful fix workflow initiated from the Code Quality tab.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"finding_id": map[string]any{
					"type":        "string",
					"description": "The ID of the code quality finding to update",
				},
				"status": map[string]any{
					"type":        "string",
					"description": "New lifecycle status for the finding",
					"enum":        []string{"rejected", "applied"},
				},
			},
			"required": []string{"finding_id", "status"},
		},
		Handler: func(args map[string]any) (any, error) {
			findingID, ok := args["finding_id"].(string)
			if !ok || findingID == "" {
				return nil, fmt.Errorf("finding_id is required")
			}
			status, ok := args["status"].(string)
			if !ok || status == "" {
				return nil, fmt.Errorf("status is required")
			}
			if status != "rejected" && status != "applied" {
				return nil, fmt.Errorf("status must be one of: rejected, applied")
			}
			return client.put("/api/code-analysis/findings/"+findingID+"/status", map[string]any{
				"status": status,
			})
		},
	})

	// --- Vault sync tool ---

	s.Register(Tool{
		Name:        "vault_sync",
		Description: "Trigger a full Obsidian vault sync: exports all published wiki pages to the configured vault directory.",
		InputSchema: obj(),
		Handler: func(args map[string]any) (any, error) {
			return client.post("/api/wiki/vault/sync", map[string]any{})
		},
	})
}

// apiClient is a minimal HTTP client for calling the Stratus API.
type apiClient struct {
	base string
	http *http.Client
}

func (c *apiClient) get(path string, params neturl.Values) (any, error) {
	u := c.base + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	resp, err := c.http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	return c.decodeResponse(resp, "GET", path)
}

func (c *apiClient) post(path string, body any) (any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Post(c.base+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	return c.decodeResponse(resp, "POST", path)
}

func (c *apiClient) put(path string, body any) (any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPut, c.base+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("PUT %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PUT %s: %w", path, err)
	}
	defer resp.Body.Close()
	return c.decodeResponse(resp, "PUT", path)
}

// decodeResponse checks the HTTP status code before decoding JSON.
func (c *apiClient) decodeResponse(resp *http.Response, method, path string) (any, error) {
	result, err := decodeJSON(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s %s: failed to decode response: %w", method, path, err)
	}
	if resp.StatusCode >= 400 {
		// Try to extract error message from response body
		if m, ok := result.(map[string]any); ok {
			if msg, ok := m["error"].(string); ok {
				return nil, fmt.Errorf("%s %s: %s (HTTP %d)", method, path, msg, resp.StatusCode)
			}
		}
		return nil, fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return result, nil
}

func decodeJSON(r io.Reader) (any, error) {
	var result any
	if err := json.NewDecoder(r).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// Schema helpers
func obj(fields ...map[string]any) map[string]any {
	properties := map[string]any{}
	required := []string{}
	for _, f := range fields {
		name := f["name"].(string)
		prop := map[string]any{"type": f["type"], "description": f["description"]}
		if f["type"] == "array" {
			prop["items"] = map[string]any{"type": "string"}
		}
		properties[name] = prop
		if r, ok := f["required"].(bool); ok && r {
			required = append(required, name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func req(name, typ, desc string) map[string]any {
	return map[string]any{"name": name, "type": typ, "description": desc, "required": true}
}

func opt(name, typ, desc string) map[string]any {
	return map[string]any{"name": name, "type": typ, "description": desc}
}

func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return def
}

func convertMemoryArgs(args map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range args {
		switch k {
		case "importance":
			if s, ok := v.(string); ok {
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					result[k] = f
					continue
				}
			}
		case "tags":
			if s, ok := v.(string); ok {
				var arr []string
				if json.Unmarshal([]byte(s), &arr) == nil {
					result[k] = arr
					continue
				}
			}
		}
		result[k] = v
	}
	return result
}

func convertTasksArg(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	if arr, ok := v.([]any); ok {
		result := make([]string, 0, len(arr))
		for i, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			} else {
				return nil, fmt.Errorf("task at index %d is not a string (got %T)", i, item)
			}
		}
		return result, nil
	}
	if s, ok := v.(string); ok && s != "" {
		var arr []string
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return nil, fmt.Errorf("invalid JSON array: %w", err)
		}
		return arr, nil
	}
	return nil, nil
}

