package hooks

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestWorkflowExistenceGuardBlocksWithoutSessionWorkflow(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "wf-other", "session_id": "session-other", "phase": "plan"},
		},
	})

	decision := WorkflowExistenceGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "session-current",
	})

	if decision.Continue {
		t.Fatalf("expected guard to block when no session workflow exists")
	}
	if decision.Reason != noActiveWorkflowReason {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}

func TestWorkflowExistenceGuardAllowsExactSessionWorkflow(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "wf-current", "session_id": "session-current", "phase": "plan"},
			{"id": "wf-other", "session_id": "session-other", "phase": "implement"},
		},
	})

	decision := WorkflowExistenceGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "session-current",
	})

	if !decision.Continue {
		t.Fatalf("expected guard to allow exact session workflow, got %#v", decision)
	}
}

func TestDelegationGuardUsesExactSessionWorkflow(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "wf-other", "session_id": "session-other", "phase": "plan"},
		},
	})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "session-current",
		ToolInput: map[string]any{
			"subagent_type": "delivery-backend-engineer",
		},
	})

	if decision.Continue {
		t.Fatalf("expected delegation guard to block when only another session has a workflow")
	}
	if decision.Reason != noActiveWorkflowReason {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}

func TestDelegationGuardPhaseAgentMatching(t *testing.T) {
	tests := []struct {
		name        string
		workflow    map[string]any
		subagent    string
		shouldAllow bool
	}{
		{
			name:        "bug analyze allows debugger",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "bug", "phase": "analyze"},
			subagent:    "delivery-debugger",
			shouldAllow: true,
		},
		{
			name:        "bug analyze blocks backend-engineer",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "bug", "phase": "analyze"},
			subagent:    "delivery-backend-engineer",
			shouldAllow: false,
		},
		{
			name:        "bug fix allows backend-engineer",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "bug", "phase": "fix"},
			subagent:    "delivery-backend-engineer",
			shouldAllow: true,
		},
		{
			name:        "bug fix blocks code-reviewer",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "bug", "phase": "fix"},
			subagent:    "delivery-code-reviewer",
			shouldAllow: false,
		},
		{
			name:        "bug review allows code-reviewer",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "bug", "phase": "review"},
			subagent:    "delivery-code-reviewer",
			shouldAllow: true,
		},
		{
			name:        "spec implement allows backend-engineer",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "implement"},
			subagent:    "delivery-backend-engineer",
			shouldAllow: true,
		},
		{
			name:        "spec verify allows code-reviewer",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "verify"},
			subagent:    "delivery-code-reviewer",
			shouldAllow: true,
		},
		{
			name:        "spec verify blocks backend-engineer",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "verify"},
			subagent:    "delivery-backend-engineer",
			shouldAllow: false,
		},
		{
			name:        "non-delivery agent always allowed",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "plan"},
			subagent:    "Explore",
			shouldAllow: true,
		},
		{
			name:        "unknown workflow type allows all",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "unknown", "phase": "any"},
			subagent:    "delivery-backend-engineer",
			shouldAllow: true,
		},
		{
			name:        "spec governance allows code-reviewer",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "governance"},
			subagent:    "delivery-code-reviewer",
			shouldAllow: true,
		},
		{
			name:        "spec design allows strategic-architect",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "design"},
			subagent:    "delivery-strategic-architect",
			shouldAllow: true,
		},
		{
			name:        "spec design blocks backend-engineer",
			workflow:    map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "design"},
			subagent:    "delivery-backend-engineer",
			shouldAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDashboardState(t, dashboardState{
				Workflows: []map[string]any{tt.workflow},
			})

			decision := DelegationGuard(HookEvent{
				ToolName:  "Task",
				SessionID: "sess",
				ToolInput: map[string]any{
					"subagent_type": tt.subagent,
				},
			})

			if decision.Continue != tt.shouldAllow {
				t.Fatalf("expected shouldAllow=%v, got Continue=%v, Reason=%q", tt.shouldAllow, decision.Continue, decision.Reason)
			}
		})
	}
}

func TestIsWriteBashCommand(t *testing.T) {
	tests := []struct {
		cmd      string
		expected bool
	}{
		// Write commands
		{"echo foo > file.txt", true},
		{"echo foo >> file.txt", true},
		{"cmd 1>out.txt", true},
		{"cmd 2>err.txt", true},
		{"cmd &>all.txt", true},
		{"cmd 2>&1", true},
		{"sed -i 's/foo/bar/' file.txt", true},
		{"git commit -m 'msg'", true},
		{"git push origin main", true},
		{"rm file.txt", true},
		{"mkdir newdir", true},
		{"touch newfile", true},
		{"mv old new", true},
		{"cp src dst", true},
		{"tee output.txt", true},
		{"chmod +x script.sh", true},
		{"dd if=/dev/zero of=file", true},
		{"truncate -s 0 file", true},

		// Tab handling
		{"rm\t-rf /path", true}, // tab instead of space

		// Read-only commands
		{"git status", false},
		{"git log --oneline", false},
		{"git diff HEAD", false},
		{"cat file.txt", false},
		{"ls -la", false},
		{"grep pattern file.txt", false},
		{"curl http://example.com", false},
		{"curl http://example.com/api?foo=bar>baz", false}, // > in URL, preceded by =
		{"go test ./...", false},
		{"npm test", false},
		{"pytest tests/", false},

		// Edge cases: write patterns take precedence
		{"cat file | tee output", true},

		// Edge cases: > not a redirect
		{"curl http://example.com/path>next", false}, // part of URL, preceded by /
		// Note: "echo $a>$b" IS a redirect in bash - redirects to file named by $b
		{"cmd>file", true}, // redirect without spaces
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			result := isWriteBashCommand(tt.cmd)
			if result != tt.expected {
				t.Errorf("isWriteBashCommand(%q) = %v, expected %v", tt.cmd, result, tt.expected)
			}
		})
	}
}

func TestBashWriteGuard(t *testing.T) {
	tests := []struct {
		name        string
		isDelivery  bool
		cmd         string
		hasWorkflow bool
		shouldAllow bool
	}{
		{
			name:        "non-delivery agent always allowed",
			isDelivery:  false,
			cmd:         "rm file.txt",
			hasWorkflow: false,
			shouldAllow: true,
		},
		{
			name:        "delivery agent read cmd without workflow",
			isDelivery:  true,
			cmd:         "cat file.txt",
			hasWorkflow: false,
			shouldAllow: true,
		},
		{
			name:        "delivery agent write cmd with workflow",
			isDelivery:  true,
			cmd:         "echo foo > file.txt",
			hasWorkflow: true,
			shouldAllow: true,
		},
		{
			name:        "delivery agent write cmd without workflow",
			isDelivery:  true,
			cmd:         "echo foo > file.txt",
			hasWorkflow: false,
			shouldAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.isDelivery {
				t.Setenv("CLAUDE_AGENT_ID", "delivery-backend-engineer")
			}

			if tt.hasWorkflow {
				setDashboardState(t, dashboardState{
					Workflows: []map[string]any{
						{"id": "wf", "session_id": "sess", "type": "bug", "phase": "fix"},
					},
				})
			} else {
				setDashboardState(t, dashboardState{
					Workflows: []map[string]any{},
				})
			}

			decision := BashWriteGuard(HookEvent{
				ToolName:  "Bash",
				SessionID: "sess",
				ToolInput: map[string]any{
					"command": tt.cmd,
				},
			})

			if decision.Continue != tt.shouldAllow {
				t.Fatalf("expected shouldAllow=%v, got Continue=%v, Reason=%q", tt.shouldAllow, decision.Continue, decision.Reason)
			}
		})
	}
}

func setDashboardState(t *testing.T, state dashboardState) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/dashboard/state" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(state)
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	t.Setenv("STRATUS_PORT", port)
}
