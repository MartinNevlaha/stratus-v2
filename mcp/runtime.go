package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"net/http"
	"net/url"
	"strings"
)

type RuntimeOptions struct{ Runtime string }

func RegisterToolsForRuntime(s *Server, base string, h *http.Client, opts RuntimeOptions) {
	RegisterTools(s, base, h)
	if opts.Runtime != "codex" {
		return
	}
	if h == nil {
		h = http.DefaultClient
	}
	c := &apiClient{base: base, http: h}
	s.instructions = "Stratus tracks this project's workflows, memory and wiki. Use the native Codex session ID from SessionStart when registering or binding a workflow. Follow the project Codex instructions and Superpowers. Change phases only with transition_phase; do not complete a workflow merely because a turn ends."
	for name, tool := range s.tools {
		if tool.ReadOnly || !workflowMutation(name) {
			continue
		}
		handler := tool.Handler
		toolName := name
		tool.Handler = func(args map[string]any) (any, error) {
			if e := c.guardMutation(toolName, args, opts); e != nil {
				return nil, e
			}
			return handler(args)
		}
		s.tools[name] = tool
	}
	register := s.tools["register_workflow"]
	register.Description = "Register a Codex-owned spec, bug, or e2e workflow before tracked delivery. Pass the exact native session ID from SessionStart."
	register.InputSchema = obj(
		req("id", "string", "Unique workflow ID: <type>-<slug>"),
		req("type", "string", "Workflow type: spec | bug | e2e"),
		req("title", "string", "Human-readable workflow title"),
		req("session_id", "string", "Exact native Codex session ID from SessionStart"),
		opt("complexity", "string", "For spec workflows: simple | complex"),
	)
	register.Handler = func(args map[string]any) (any, error) {
		body, e := codexSessionArgs(args)
		if e != nil {
			return nil, e
		}
		return c.post("/api/workflows", body)
	}
	s.tools[register.Name] = register
	dispatch := s.tools["delivery_dispatch"]
	dispatch.Handler = func(args map[string]any) (any, error) {
		body, e := codexSessionArgs(args)
		if e != nil {
			return nil, e
		}
		dash, e := c.get("/api/integrations/codex/workflows", nil)
		if e != nil {
			return nil, e
		}
		id, _ := args["workflow_id"].(string)
		var found []string
		for _, wf := range dashboardWorkflows(dash) {
			if id != "" && wf["id"] != id {
				continue
			}
			if wf["runtime"] == "codex" && wf["session_id"] == body["session_id"] {
				found = append(found, workflowID(wf))
			} else if id != "" {
				return nil, orchestration.ErrOwnershipConflict
			}
		}
		if len(found) > 1 {
			return nil, orchestration.ErrAmbiguousWorkflow
		}
		if len(found) == 0 {
			if id != "" {
				return nil, orchestration.ErrWorkflowNotFound
			}
			return map[string]any{"workflows": []any{}, "code": "WORKFLOW_NOT_RESOLVED"}, nil
		}
		return c.get("/api/workflows/"+url.PathEscape(found[0])+"/dispatch", nil)
	}
	s.tools[dispatch.Name] = dispatch
	s.Register(Tool{Name: "bind_workflow_session", Description: "Bind a Codex-owned workflow to this native Codex session when resuming.", InputSchema: obj(req("workflow_id", "string", "Explicit workflow ID"), req("session_id", "string", "Native Codex session ID from SessionStart")), Handler: func(args map[string]any) (any, error) {
		body, e := codexSessionArgs(args)
		if e != nil {
			return nil, e
		}
		if e = c.guardMutation("bind_workflow_session", args, opts); e != nil {
			return nil, e
		}
		id, _ := args["workflow_id"].(string)
		return c.patch("/api/workflows/"+url.PathEscape(id)+"/session", body)
	}})
}
func codexSessionArgs(args map[string]any) (map[string]any, error) {
	native, _ := args["session_id"].(string)
	key, e := orchestration.SessionKey(orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: native})
	if e != nil {
		return nil, e
	}
	body := map[string]any{}
	for k, v := range args {
		body[k] = v
	}
	body["runtime"] = "codex"
	body["runtime_session_id"] = native
	body["session_id"] = key
	return body, nil
}
func (c *apiClient) patch(path string, body any) (any, error) {
	b, e := json.Marshal(body)
	if e != nil {
		return nil, e
	}
	r, e := http.NewRequest("PATCH", c.base+path, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Content-Type", "application/json")
	res, e := c.http.Do(r)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	return c.decodeResponse(res, "PATCH", path)
}
func (c *apiClient) guardMutation(name string, args map[string]any, opts RuntimeOptions) error {
	id, _ := args["workflow_id"].(string)
	if strings.HasPrefix(name, "swarm_") {
		workflow, e := c.swarmMutationWorkflow(args)
		if e != nil {
			return e
		}
		if id != "" && id != workflow {
			return orchestration.ErrOwnershipConflict
		}
		id = workflow
	}
	if id == "" {
		return fmt.Errorf("workflow_id is required for %s", name)
	}
	v, e := c.get("/api/workflows/"+url.PathEscape(id), nil)
	if e != nil {
		return e
	}
	wf, _ := v.(map[string]any)
	runtime, _ := wf["runtime"].(string)
	if runtime != opts.Runtime {
		return orchestration.ErrOwnershipConflict
	}
	return nil
}

// Shared wiki and memory operations are not workflow mutations.
func workflowMutation(name string) bool {
	switch name {
	case "transition_phase", "delegate_agent", "start_task", "complete_task":
		return true
	}
	return strings.HasPrefix(name, "swarm_")
}

func (c *apiClient) swarmMutationWorkflow(args map[string]any) (string, error) {
	mission, _ := args["mission_id"].(string)
	bind := func(derived string) error {
		if derived == "" || mission != "" && mission != derived {
			return orchestration.ErrOwnershipConflict
		}
		mission = derived
		return nil
	}
	for _, field := range []string{"worker_id", "from_worker", "to_worker"} {
		worker, _ := args[field].(string)
		if worker == "" || field == "to_worker" && worker == "*" {
			continue
		}
		v, e := c.get("/api/swarm/workers/"+url.PathEscape(worker), nil)
		if e != nil {
			return "", e
		}
		w, _ := v.(map[string]any)
		derived, _ := w["mission_id"].(string)
		if e := bind(derived); e != nil {
			return "", e
		}
	}
	ticket, _ := args["ticket_id"].(string)
	if mission == "" && ticket != "" {
		v, e := c.get("/api/swarm/missions", nil)
		if e != nil {
			return "", e
		}
		all, _ := v.([]any)
		for _, entry := range all {
			m, _ := entry.(map[string]any)
			mid, _ := m["id"].(string)
			d, e := c.get("/api/swarm/missions/"+url.PathEscape(mid), nil)
			if e != nil {
				return "", e
			}
			if missionHasTicket(d, ticket) {
				if e := bind(mid); e != nil {
					return "", e
				}
			}
		}
	}
	if mission == "" {
		return "", fmt.Errorf("swarm workflow ownership unresolved")
	}
	v, e := c.get("/api/swarm/missions/"+url.PathEscape(mission), nil)
	if e != nil {
		return "", e
	}
	if ticket != "" && !missionHasTicket(v, ticket) {
		return "", orchestration.ErrOwnershipConflict
	}
	m, _ := v.(map[string]any)
	if nested, ok := m["mission"].(map[string]any); ok {
		m = nested
	}
	id, _ := m["workflow_id"].(string)
	return id, nil
}

func missionHasTicket(v any, id string) bool {
	m, _ := v.(map[string]any)
	tickets, _ := m["tickets"].([]any)
	for _, raw := range tickets {
		t, _ := raw.(map[string]any)
		if t["id"] == id {
			return true
		}
	}
	return false
}
