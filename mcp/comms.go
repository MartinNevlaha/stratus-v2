package mcp

import (
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

// message_wait stays under the hosts' MCP tool timeout (Codex defaults to 60 s): a
// host that gives up first would leave the wait running and its message consumed
// by a response nobody reads. Agents loop for longer waits.
const (
	defaultWaitSeconds = 50
	maxWaitSeconds     = 50
)

// waitTimeoutGrace is added to timeout_s for message_wait's HTTP client, so a long
// poll is not cut off by the shared client's short timeout.
var waitTimeoutGrace = 10 * time.Second

// commsInstructions is appended to the server instructions.
const commsInstructions = "- Comms: peers_list, message_send, message_inbox and message_wait let this session talk to other agent sessions (Claude Code, OpenCode, Codex) and the user; shared_task_* is a task board all of them share. Pass the session_id from the \"Stratus comms\" context line."

// sessionMapper turns the session_id an agent passes into the peer session key.
type sessionMapper func(sessionID string) (string, error)

func identitySession(id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("session_id is required")
	}
	return id, nil
}

// codexSession maps a native Codex session id to its peer key (codex:<native>).
func codexSession(id string) (string, error) {
	native := strings.TrimPrefix(id, "codex:")
	return orchestration.SessionKey(orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: native})
}

func registerCommsTools(s *Server, apiBase string, httpClient *http.Client, mapSession sessionMapper) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	c := &apiClient{base: apiBase, http: httpClient}
	sessionField := req("session_id", "string", "Your own session id, from the \"Stratus comms\" context line")

	// withSession validates and maps session_id, then hands the peer key to fn.
	withSession := func(fn func(key string, args map[string]any) (any, error)) func(map[string]any) (any, error) {
		return func(args map[string]any) (any, error) {
			raw, _ := args["session_id"].(string)
			// "user" is the dashboard human; an agent must not speak with its authority.
			if strings.EqualFold(strings.TrimSpace(raw), "user") {
				return nil, fmt.Errorf("session_id: \"user\" is reserved for the dashboard; pass your own session id")
			}
			key, err := mapSession(raw)
			if err != nil {
				return nil, fmt.Errorf("session_id: %w", err)
			}
			return fn(key, args)
		}
	}
	peerPath := func(key, suffix string) string {
		return "/api/peers/" + neturl.PathEscape(key) + suffix
	}

	s.Register(Tool{
		Name:        "peers_list",
		ReadOnly:    true,
		Description: "List the active agent sessions (peers) in this project with their handles and runtimes; \"you\" is your own entry. Use it to find who you can message.",
		InputSchema: obj(sessionField),
		Handler: withSession(func(key string, _ map[string]any) (any, error) {
			res, err := c.get("/api/peers", neturl.Values{"active": {"1"}})
			if err != nil {
				return nil, err
			}
			m, _ := res.(map[string]any)
			peers, _ := m["peers"].([]any)
			// Session keys are the only credential on the local API: show handles only,
			// except the caller's own entry.
			out := map[string]any{}
			public := make([]any, 0, len(peers))
			for _, p := range peers {
				peer, _ := p.(map[string]any)
				if peer["session_key"] == key {
					out["you"] = peer
				}
				shown := map[string]any{}
				for k, v := range peer {
					if k != "session_key" {
						shown[k] = v
					}
				}
				public = append(public, shown)
			}
			out["peers"] = public
			return out, nil
		}),
	})

	s.Register(Tool{
		Name:        "peer_rename",
		Description: "Give your session a readable handle (lowercase letters, digits and dashes, 2-32 chars, e.g. \"reviewer\"). Other agents address you by it.",
		InputSchema: obj(sessionField, req("handle", "string", "New handle, e.g. reviewer")),
		Handler: withSession(func(key string, args map[string]any) (any, error) {
			return c.patch(peerPath(key, ""), map[string]any{"handle": args["handle"]})
		}),
	})

	s.Register(Tool{
		Name: "message_send",
		Description: "Send a message to another agent session or the user. " +
			"`to` is a peer handle (see peers_list), a runtime name (claude-code, opencode or codex: every active session of that runtime except you), '*' (all active sessions except you) or 'user' (shown in the dashboard only). " +
			"`kind` is text (default), done (you finished what was asked), question (you need an answer) or task_update; task_assigned and task_done are sent by shared_task_*. " +
			"Replies arrive in the recipient's inbox; use message_inbox or message_wait to read yours.",
		InputSchema: obj(sessionField,
			req("to", "string", "Handle, runtime name, '*' or 'user'"),
			req("body", "string", "Message text"),
			opt("kind", "string", "text (default) | done | question | task_update"),
			opt("task_id", "integer", "Shared task this message relates to"),
		),
		Handler: withSession(func(key string, args map[string]any) (any, error) {
			to, _ := args["to"].(string)
			body, _ := args["body"].(string)
			if strings.TrimSpace(to) == "" || strings.TrimSpace(body) == "" {
				return nil, fmt.Errorf("to and body are required")
			}
			payload := map[string]any{"from": key, "to": to, "body": body}
			if kind, _ := args["kind"].(string); kind != "" {
				payload["kind"] = kind
			}
			if id := intArg(args, "task_id", 0); id > 0 {
				payload["task_id"] = id
			}
			return c.post("/api/messages", payload)
		}),
	})

	s.Register(Tool{
		Name:        "message_inbox",
		Description: "Read and clear your inbox: returns the messages other agents (or the user) sent you since the last check, without waiting. Optionally only from one sender (`from`: handle) or one `kind`.",
		InputSchema: obj(sessionField,
			opt("from", "string", "Only messages from this handle"),
			opt("kind", "string", "Only messages of this kind"),
		),
		Handler: withSession(func(key string, args map[string]any) (any, error) {
			return c.post(peerPath(key, "/inbox/claim")+inboxQuery(args, nil), map[string]any{})
		}),
	})

	s.Register(Tool{
		Name: "message_wait",
		Description: "Block until a message arrives in your inbox, then return it. Use it when you must wait for another agent, e.g. the user says \"wait until codex finishes\": filter with from=<handle> and kind=done. " +
			"Returns {messages, timed_out}; on {timed_out: true} nothing arrived, so call message_wait again if you should keep waiting.",
		InputSchema: obj(sessionField,
			opt("from", "string", "Only messages from this handle"),
			opt("kind", "string", "Only messages of this kind, e.g. done"),
			opt("timeout_s", "integer", fmt.Sprintf("Seconds to wait, 1-%d (default %d); call again to wait longer", maxWaitSeconds, defaultWaitSeconds)),
		),
		Handler: withSession(func(key string, args map[string]any) (any, error) {
			secs := intArg(args, "timeout_s", defaultWaitSeconds)
			if secs < 1 || secs > maxWaitSeconds {
				return nil, fmt.Errorf("timeout_s must be between 1 and %d", maxWaitSeconds)
			}
			// The shared client has a short timeout; this call needs to outlive the poll.
			long := &apiClient{base: c.base, http: &http.Client{
				Transport: c.http.Transport,
				Timeout:   time.Duration(secs)*time.Second + waitTimeoutGrace,
			}}
			extra := neturl.Values{"timeout_s": {strconv.Itoa(secs)}}
			return long.post(peerPath(key, "/inbox/wait")+inboxQuery(args, extra), map[string]any{})
		}),
	})

	s.Register(Tool{
		Name:        "shared_task_create",
		Description: "Add a task to the board shared by all agent sessions. With `assignee` (handle) that agent is notified.",
		InputSchema: obj(sessionField,
			req("title", "string", "Task title"),
			opt("description", "string", "Details"),
			opt("assignee", "string", "Handle of the agent who should do it"),
		),
		Handler: withSession(func(key string, args map[string]any) (any, error) {
			title, _ := args["title"].(string)
			if strings.TrimSpace(title) == "" {
				return nil, fmt.Errorf("title is required")
			}
			payload := map[string]any{"created_by": key, "title": title}
			copyStringArgs(payload, args, "description", "assignee")
			return c.post("/api/shared-tasks", payload)
		}),
	})

	s.Register(Tool{
		Name:        "shared_task_list",
		ReadOnly:    true,
		Description: "List the shared task board, optionally filtered by status (open, claimed, done, cancelled).",
		InputSchema: obj(opt("status", "string", "open | claimed | done | cancelled")),
		Handler: func(args map[string]any) (any, error) {
			params := neturl.Values{}
			if st, _ := args["status"].(string); st != "" {
				params.Set("status", st)
			}
			return c.get("/api/shared-tasks", params)
		},
	})

	s.Register(Tool{
		Name:        "shared_task_update",
		Description: "Update a shared task: claim it (status=claimed, assigns you), finish it (status=done with a `result`), cancel it, or reassign it. The task's creator is notified of status changes.",
		InputSchema: obj(sessionField,
			req("task_id", "integer", "Shared task id"),
			opt("status", "string", "open | claimed | done | cancelled"),
			opt("result", "string", "Outcome, when done"),
			opt("assignee", "string", "Handle to assign the task to"),
		),
		Handler: withSession(func(key string, args map[string]any) (any, error) {
			id := intArg(args, "task_id", 0)
			if id <= 0 {
				return nil, fmt.Errorf("task_id is required")
			}
			payload := map[string]any{"actor": key}
			copyStringArgs(payload, args, "status", "result", "assignee")
			return c.patch(fmt.Sprintf("/api/shared-tasks/%d", id), payload)
		}),
	})
}

// inboxQuery builds the "?from=&kind=" suffix for inbox calls, plus any extra params.
func inboxQuery(args map[string]any, extra neturl.Values) string {
	params := neturl.Values{}
	for k, v := range extra {
		params[k] = v
	}
	copyStringParams(params, args, "from", "kind")
	if len(params) == 0 {
		return ""
	}
	return "?" + params.Encode()
}

func copyStringParams(dst neturl.Values, args map[string]any, keys ...string) {
	for _, k := range keys {
		if v, _ := args[k].(string); v != "" {
			dst.Set(k, v)
		}
	}
}

func copyStringArgs(dst, args map[string]any, keys ...string) {
	for _, k := range keys {
		if v, _ := args[k].(string); v != "" {
			dst[k] = v
		}
	}
}
