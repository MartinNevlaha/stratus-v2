package mcp

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// Managed identity is inherited by the MCP subprocess, never supplied by the model.
func managedHeaders(r *http.Request) {
	if token := os.Getenv("STRATUS_TEAM_TOKEN"); token != "" {
		r.Header.Set("X-Stratus-Team-Token", token)
	}
}

func registerTeamTools(s *Server, apiBase string, client *http.Client, mapSession sessionMapper) {
	c := &apiClient{base: apiBase, http: client}
	identity := func(args map[string]any) (string, error) {
		id, _ := args["session_id"].(string)
		if strings.EqualFold(strings.TrimSpace(id), "user") {
			return "", fmt.Errorf("user is reserved for the dashboard")
		}
		if os.Getenv("STRATUS_TEAM_TOKEN") != "" {
			return "managed", nil
		}
		return mapSession(id)
	}
	s.Register(Tool{Name: "team_status", ReadOnly: true, Description: "Get your managed project team, current work and relevant results. Use once when needed; do not poll. Stratus wakes idle members automatically.", InputSchema: obj(opt("session_id", "string", "Your native session ID; optional in a Stratus-managed session"), opt("task_id", "string", "Retrieve a full task brief/result by ID when the compact handoff was truncated")), Handler: func(args map[string]any) (any, error) {
		key, err := identity(args)
		if err != nil {
			return nil, err
		}
		q := url.Values{"session_key": {key}}
		if id, _ := args["task_id"].(string); id != "" {
			q.Set("task_id", id)
		}
		return c.get("/api/team-context", q)
	}})
	for _, name := range []string{"team_delegate", "team_message"} {
		endpoint := "/api/team-delegate"
		desc := "Delegate a scoped task by role. Stratus selects the configured model and wakes the worker; its result comes back automatically. Preserve workflow ID, revision and acceptance criteria; no permission to expand user scope."
		if name == "team_message" {
			endpoint = "/api/team-message"
			desc = "Send a relevant question or answer to a teammate by member ID. Delivery wakes an idle teammate. Do not send acknowledgements or periodic status chatter; never poll for a response."
		}
		s.Register(Tool{Name: name, Description: desc, InputSchema: obj(opt("session_id", "string", "Your native session ID; optional when managed"), req("text", "string", "Concise goal/question, scope, decisions, files, acceptance criteria and evidence references"), opt("role", "string", "lead, implementation, design, review, diagnosis, mechanical, or delivery role"), opt("to", "string", "Member ID; omit for role-based routing"), opt("workflow_id", "string", "Existing workflow owned by your team lead"), opt("revision", "string", "Current code revision for implementation/review evidence"), opt("request_id", "string", "Stable request identifier; reuse only for an identical retry")), Handler: func(args map[string]any) (any, error) {
			key, err := identity(args)
			if err != nil {
				return nil, err
			}
			body := map[string]any{"session_key": key}
			for _, k := range []string{"text", "role", "to", "workflow_id", "revision"} {
				if v, ok := args[k].(string); ok {
					body[k] = v
				}
			}
			if v, ok := args["request_id"].(string); ok {
				body["id"] = v
			}
			return c.post(endpoint, body)
		}})
	}
}

// Native session IDs are transport metadata. Managed agents should not have to
// guess or manually relay them when registering the workflow they will own.
func configureManagedTools(s *Server, c *apiClient) {
	if os.Getenv("STRATUS_TEAM_TOKEN") == "" {
		return
	}
	var mu sync.Mutex
	nativeID := ""
	resolve := func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if nativeID != "" {
			return nativeID, nil
		}
		data, err := c.get("/api/team-context", nil)
		if err != nil {
			return "", err
		}
		body, _ := data.(map[string]any)
		nativeID, _ = body["native_session_id"].(string)
		if nativeID == "" {
			return "", fmt.Errorf("managed native session is not ready")
		}
		return nativeID, nil
	}
	for name, tool := range s.tools {
		handler := tool.Handler
		tool.Handler = func(args map[string]any) (any, error) {
			id, err := resolve()
			if err != nil {
				return nil, err
			}
			bound := make(map[string]any, len(args)+1)
			for k, v := range args {
				bound[k] = v
			}
			bound["session_id"] = id
			return handler(bound)
		}
		if required, ok := tool.InputSchema["required"].([]string); ok {
			out := []string{}
			for _, field := range required {
				if field != "session_id" {
					out = append(out, field)
				}
			}
			tool.InputSchema["required"] = out
		}
		s.tools[name] = tool
	}
	s.instructions += "\nThis is a Stratus-managed project team. Use team_status for your role and assigned work, team_delegate for scoped delivery roles and team_message for actionable questions. Native session identity is supplied by the transport. Preserve existing spec/spec-complex/bug/e2e phases. Only the lead owns and advances the workflow. Finish your turn when waiting; relevant results wake you automatically. Do not poll, send acknowledgements, or copy whole conversations."
}
