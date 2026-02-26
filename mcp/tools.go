package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
)

// RegisterTools registers the 7 standard Stratus MCP tools on the server.
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
			return client.post("/api/events", args)
		},
	})

	s.Register(Tool{
		Name:        "retrieve",
		Description: "Semantic search across code (Vexor) and governance docs. Auto-routes by query type.",
		InputSchema: obj(
			req("query", "string", "Search query for code or governance docs"),
			opt("corpus", "string", "Force search corpus: 'code' or 'governance'. Omit for auto-routing."),
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
			if id, ok := args["workflow_id"].(string); ok && id != "" {
				return client.get(fmt.Sprintf("/api/workflows/%s/dispatch", id), nil)
			}
			// Return active workflow dispatch info (first active workflow)
			dash, err := client.get("/api/dashboard/state", nil)
			if err != nil {
				return nil, err
			}
			if m, ok := dash.(map[string]any); ok {
				if workflows, ok := m["workflows"].([]any); ok && len(workflows) > 0 {
					if wf, ok := workflows[0].(map[string]any); ok {
						if id, ok := wf["id"].(string); ok {
							return client.get(fmt.Sprintf("/api/workflows/%s/dispatch", id), nil)
						}
					}
				}
			}
			return map[string]any{"workflows": []any{}, "message": "no active workflows"}, nil
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
		properties[name] = map[string]any{"type": f["type"], "description": f["description"]}
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
