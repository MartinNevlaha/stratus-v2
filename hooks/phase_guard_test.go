package hooks

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
		ToolInput: map[string]any{
			"subagent_type": "delivery-backend-engineer",
		},
	})

	if !decision.Continue {
		t.Fatalf("expected single global workflow fallback to allow, got blocked: %q", decision.Reason)
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
		ToolInput: map[string]any{
			"subagent_type": "delivery-backend-engineer",
		},
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

	if !decision.Continue {
		t.Fatalf("expected single global workflow fallback to allow, got blocked: %q", decision.Reason)
	}
}

func TestDelegationGuardStructuredWorkflowIDWinsOverGlobalActiveRegression(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-dev-gpu-sluzby-na-prod", "session_id": "session-b", "type": "spec", "phase": "implement"},
			{"id": "spec-research-editor-undo-redo", "session_id": "session-a", "type": "spec", "phase": "implement"},
		},
	})

	wf, err := fetchWorkflowForTaskStrict(map[string]any{
		"subagent_type": "delivery-frontend-engineer",
		"workflow_id":   "spec-research-editor-undo-redo",
		"prompt":        "Implement task 4.",
	}, "session-a")
	if err != nil {
		t.Fatalf("expected structured workflow_id to resolve, got error: %v", err)
	}
	if id, _ := wf["id"].(string); id != "spec-research-editor-undo-redo" {
		t.Fatalf("expected spec-research-editor-undo-redo, got %q", id)
	}
}

func TestDelegationGuardWorkflowIDMismatchBlocks(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-a", "session_id": "sess", "type": "spec", "phase": "implement"},
			{"id": "spec-b", "session_id": "sess", "type": "spec", "phase": "implement"},
		},
	})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "sess",
		ToolInput: map[string]any{
			"subagent_type": "delivery-frontend-engineer",
			"workflow_id":   "spec-a",
			"prompt":        "Workflow ID: spec-b\nImplement task 4.",
		},
	})

	if decision.Continue {
		t.Fatalf("expected mismatch to block")
	}
	if !strings.Contains(decision.Reason, workflowIDMismatchErr) || !strings.Contains(decision.Reason, "requested: spec-a") || !strings.Contains(decision.Reason, "prompt: spec-b") {
		t.Fatalf("expected mismatch diagnostics, got %q", decision.Reason)
	}
}

func TestDelegationGuardMissingExplicitWorkflowDoesNotFallback(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-dev-gpu-sluzby-na-prod", "session_id": "session-b", "type": "spec", "phase": "implement"},
		},
	})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "session-a",
		ToolInput: map[string]any{
			"subagent_type": "delivery-frontend-engineer",
			"workflow_id":   "spec-does-not-exist",
		},
	})

	if decision.Continue {
		t.Fatalf("expected missing explicit workflow_id to block")
	}
	if !strings.Contains(decision.Reason, workflowNotFoundErr) || strings.Contains(decision.Reason, resolutionGlobal) {
		t.Fatalf("expected not-found without global fallback, got %q", decision.Reason)
	}
}

func TestDelegationGuardAmbiguousSessionCandidatesBlocks(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-a", "session_id": "sess", "type": "spec", "phase": "implement"},
			{"id": "spec-b", "session_id": "sess", "type": "spec", "phase": "implement"},
		},
	})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "sess",
		ToolInput: map[string]any{
			"subagent_type": "delivery-frontend-engineer",
			"prompt":        "Implement task 4.",
		},
	})

	if decision.Continue {
		t.Fatalf("expected ambiguous session candidates to block")
	}
	if !strings.Contains(decision.Reason, ambiguousWorkflowErr) || !strings.Contains(decision.Reason, resolutionSession) {
		t.Fatalf("expected session ambiguity diagnostics, got %q", decision.Reason)
	}
}

func TestDelegationGuardResolvesByExplicitWorkflowIDAmongParallel(t *testing.T) {
	// Two parallel workflows sharing one session. The task prompt names spec-a, so the
	// guard must resolve spec-a — not pick spec-b by list order.
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-b", "session_id": "sess", "type": "spec", "phase": "verify"},
			{"id": "spec-a", "session_id": "sess", "type": "spec", "phase": "implement"},
		},
	})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "sess",
		ToolInput: map[string]any{
			"subagent_type": "delivery-backend-engineer",
			"prompt":        "Implement task 3 under workflow spec-a per the plan.",
		},
	})

	if !decision.Continue {
		t.Fatalf("expected guard to allow backend-engineer under spec-a implement, got blocked: %q", decision.Reason)
	}
}

func TestDelegationGuardSupportsClaudeAgentToolAndAgentType(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-agent", "session_id": "sess", "type": "spec", "phase": "implement"},
		},
	})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Agent",
		SessionID: "sess",
		ToolInput: map[string]any{
			"agent_type": "delivery-backend-engineer",
			"prompt":     "Implement task 0 for workflow spec-agent.",
		},
	})

	if !decision.Continue {
		t.Fatalf("expected Agent/agent_type delegation to be allowed, got blocked: %q", decision.Reason)
	}
}

func TestWorkflowExistenceGuardIgnoresNonDeliveryAgent(t *testing.T) {
	setDashboardState(t, dashboardState{Workflows: []map[string]any{}})

	decision := WorkflowExistenceGuard(HookEvent{
		ToolName:  "Agent",
		SessionID: "sess",
		ToolInput: map[string]any{
			"agent_type": "Explore",
		},
	})

	if !decision.Continue {
		t.Fatalf("expected non-delivery Agent delegation to bypass workflow guard, got blocked: %q", decision.Reason)
	}
}

func TestDelegationGuardAllowsStratusSelfRepoWhenAPIUnavailable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/MartinNevlaha/stratus-v2\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	t.Setenv("STRATUS_PORT", "1")

	decision := DelegationGuard(HookEvent{
		ToolName:  "Agent",
		SessionID: "sess",
		Cwd:       dir,
		ToolInput: map[string]any{
			"agent_type": "delivery-frontend-engineer",
			"prompt":     "Fix Stratus self repo workflow spec-runtime-delegation-terminal-fix.",
		},
	})

	if !decision.Continue {
		t.Fatalf("expected Stratus self repo delegation to fail open when API is unavailable, got blocked: %q", decision.Reason)
	}
}

func TestDelegationGuardAmbiguousParallelBlocks(t *testing.T) {
	// Two parallel workflows, neither owned by this session and no ID in the prompt →
	// the guard must not guess; it blocks with the actionable unresolved reason.
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-a", "session_id": "sess-a", "type": "spec", "phase": "implement"},
			{"id": "bug-b", "session_id": "sess-b", "type": "bug", "phase": "review"},
		},
	})

	decision := DelegationGuard(HookEvent{
		ToolName:  "Task",
		SessionID: "sess-current",
		ToolInput: map[string]any{
			"subagent_type": "delivery-backend-engineer",
			"prompt":        "Implement the fix.",
		},
	})

	if decision.Continue {
		t.Fatalf("expected guard to block on ambiguous parallel state")
	}
	if !strings.Contains(decision.Reason, ambiguousWorkflowErr) || !strings.Contains(decision.Reason, resolutionGlobal) {
		t.Fatalf("expected global ambiguity diagnostics, got %q", decision.Reason)
	}
}

func TestDelegationGuardAllowsAnyDeliveryAgentInAnyPhase(t *testing.T) {
	// Phase-agent matching was removed: a registered workflow is the only requirement.
	// Coordinators pick the agent; the guard no longer second-guesses that choice.
	tests := []struct {
		name     string
		workflow map[string]any
		subagent string
	}{
		{"bug analyze with backend-engineer", map[string]any{"id": "wf", "session_id": "sess", "type": "bug", "phase": "analyze"}, "delivery-backend-engineer"},
		{"bug fix with code-reviewer", map[string]any{"id": "wf", "session_id": "sess", "type": "bug", "phase": "fix"}, "delivery-code-reviewer"},
		{"spec verify with backend-engineer", map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "verify"}, "delivery-backend-engineer"},
		{"spec design with backend-engineer", map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "design"}, "delivery-backend-engineer"},
		{"spec learn with qa-engineer", map[string]any{"id": "wf", "session_id": "sess", "type": "spec", "phase": "learn"}, "delivery-qa-engineer"},
		{"e2e setup with frontend-engineer", map[string]any{"id": "wf", "session_id": "sess", "type": "e2e", "phase": "setup"}, "delivery-frontend-engineer"},
		{"unknown workflow type", map[string]any{"id": "wf", "session_id": "sess", "type": "unknown", "phase": "any"}, "delivery-backend-engineer"},
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

			if !decision.Continue {
				t.Fatalf("expected delegation to be allowed, got blocked: %q", decision.Reason)
			}
		})
	}
}

func TestFetchActiveWorkflowExactSessionMatchAmongParallel(t *testing.T) {
	// Two parallel workflows in different phases. The resolver must return the one
	// owned by the querying session, not whichever is first in the list.
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-b", "session_id": "session-b", "type": "spec", "phase": "verify"},
			{"id": "spec-a", "session_id": "session-a", "type": "spec", "phase": "implement"},
		},
	})

	wf := fetchActiveWorkflow("session-a")
	if wf == nil {
		t.Fatalf("expected to resolve session-a workflow, got nil")
	}
	if id, _ := wf["id"].(string); id != "spec-a" {
		t.Fatalf("expected spec-a, got %q", id)
	}
}

func TestFetchActiveWorkflowAmbiguousReturnsNil(t *testing.T) {
	// Multiple parallel workflows, none owned by this session → do not guess.
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-b", "session_id": "session-b", "type": "spec", "phase": "verify"},
			{"id": "bug-c", "session_id": "", "type": "bug", "phase": "review"},
		},
	})

	if wf := fetchActiveWorkflow("session-a"); wf != nil {
		t.Fatalf("expected nil for ambiguous multi-workflow state, got %#v", wf)
	}
	// Empty session must not fall back to first when several flows are active.
	if wf := fetchActiveWorkflow(""); wf != nil {
		t.Fatalf("expected nil for empty session with multiple workflows, got %#v", wf)
	}
}

func TestFetchActiveWorkflowSingleWorkflowFallback(t *testing.T) {
	// A single active workflow is safe to return even without a session match
	// (session-tracking glitch / resumed session).
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-a", "session_id": "", "type": "spec", "phase": "implement"},
		},
	})

	wf := fetchActiveWorkflow("session-unknown")
	if wf == nil {
		t.Fatalf("expected single-workflow fallback, got nil")
	}
	if id, _ := wf["id"].(string); id != "spec-a" {
		t.Fatalf("expected spec-a, got %q", id)
	}
}

func TestPhaseGuardDoesNotBlockAcrossParallelWorkflows(t *testing.T) {
	// A delivery agent implementing in session-a must not be blocked just because a
	// different parallel workflow (session-b) is in its verify phase.
	t.Setenv("CLAUDE_AGENT_ID", "delivery-backend-engineer")
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-b", "session_id": "session-b", "type": "spec", "phase": "verify"},
			{"id": "spec-a", "session_id": "session-a", "type": "spec", "phase": "implement"},
		},
	})

	decision := PhaseGuard(HookEvent{
		ToolName:  "Write",
		SessionID: "session-a",
	})
	if !decision.Continue {
		t.Fatalf("expected write to be allowed in session-a implement phase, got blocked: %q", decision.Reason)
	}
}

func TestPhaseGuardUsesHookAgentType(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-a", "session_id": "session-a", "type": "spec", "phase": "verify"},
		},
	})

	decision := PhaseGuard(HookEvent{
		ToolName:  "Write",
		SessionID: "session-a",
		AgentType: "delivery-backend-engineer",
	})

	if decision.Continue {
		t.Fatalf("expected write to be blocked for delivery agent in verify phase")
	}
}

// A reviewer that cannot run the tests cannot verify what it reviews. `Bash` sits in
// isWriteTool alongside Write/Edit, so the review/verify guard used to deny every Bash
// call a delivery agent made -- `git diff` and `pytest` included. The phase guard must
// judge the COMMAND, the way BashWriteGuard already does, not the tool name.
func TestPhaseGuardAllowsReadOnlyBashForReviewerInReviewPhase(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "bug-x", "session_id": "session-a", "type": "bug", "phase": "review"},
		},
	})

	for _, cmd := range []string{
		"git diff --stat",
		"git status --short",
		"pytest tests/unit -q",
		"grep -rn TODO .",
	} {
		decision := PhaseGuard(HookEvent{
			ToolName:  "Bash",
			SessionID: "session-a",
			AgentType: "delivery-code-reviewer",
			ToolInput: map[string]any{"command": cmd},
		})
		if !decision.Continue {
			t.Fatalf("expected read-only %q to be allowed in review phase, got blocked: %q", cmd, decision.Reason)
		}
	}
}

func TestPhaseGuardBlocksWriteBashInReviewPhase(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "bug-x", "session_id": "session-a", "type": "bug", "phase": "review"},
		},
	})

	for _, cmd := range []string{
		"git commit -m wip",
		"sed -i s/a/b/ file.go",
		"rm -rf build",
	} {
		decision := PhaseGuard(HookEvent{
			ToolName:  "Bash",
			SessionID: "session-a",
			AgentType: "delivery-code-reviewer",
			ToolInput: map[string]any{"command": cmd},
		})
		if decision.Continue {
			t.Fatalf("expected write command %q to stay blocked in review phase", cmd)
		}
	}
}

// The same split has to hold for spec/verify, and Write/Edit must not become reachable.
func TestPhaseGuardKeepsBlockingFileToolsInVerifyPhase(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "spec-a", "session_id": "session-a", "type": "spec", "phase": "verify"},
		},
	})

	readOnly := PhaseGuard(HookEvent{
		ToolName:  "Bash",
		SessionID: "session-a",
		AgentType: "delivery-code-reviewer",
		ToolInput: map[string]any{"command": "go test ./..."},
	})
	if !readOnly.Continue {
		t.Fatalf("expected `go test` to be allowed in verify phase, got blocked: %q", readOnly.Reason)
	}

	for _, tool := range []string{"Write", "Edit", "MultiEdit", "NotebookEdit"} {
		decision := PhaseGuard(HookEvent{
			ToolName:  tool,
			SessionID: "session-a",
			AgentType: "delivery-code-reviewer",
		})
		if decision.Continue {
			t.Fatalf("expected %s to stay blocked in verify phase", tool)
		}
	}
}

// A Bash call with no command at all must not slip through as "read-only".
func TestPhaseGuardBlocksBashWithoutCommandInReviewPhase(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "bug-x", "session_id": "session-a", "type": "bug", "phase": "review"},
		},
	})

	decision := PhaseGuard(HookEvent{
		ToolName:  "Bash",
		SessionID: "session-a",
		AgentType: "delivery-code-reviewer",
		ToolInput: map[string]any{},
	})
	if decision.Continue {
		t.Fatalf("expected a Bash call with no command to stay blocked")
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
		// Inverted 2026-08-20: `2>&1` duplicates a descriptor, it does not write a file.
		// The old expectation denied every `pytest ... 2>&1 | tail` a reviewer ran.
		{"cmd 2>&1", false},
		{"cmd > out.txt 2>&1", true}, // ...but a real redirect next to it still writes
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

// `2>&1` duplicates a file descriptor -- nothing reaches disk. Reading it as a write
// denied the reviewer every command it actually needed: `pytest ... 2>&1 | tail` is the
// documented way to run this repo's suites. Measured live 2026-08-20.
func TestIsWriteBashCommandTreatsDescriptorDuplicationAsReadOnly(t *testing.T) {
	readOnly := []string{
		".venv/bin/python -m pytest tests/unit/research/ -q 2>&1 | tail -15",
		"go test ./... 2>&1 | tail -5",
		"cd uvo-admin && pnpm test -- --maxWorkers=4 2>&1 | tail -20",
		"git diff --stat 2>&1",
		"grep -rn TODO . 1>&2",
	}
	for _, cmd := range readOnly {
		if isWriteBashCommand(cmd) {
			t.Errorf("isWriteBashCommand(%q) = true, want false (fd duplication is not a write)", cmd)
		}
	}

	// Redirecting into a FILE is still a write, descriptor duplication next to it or not.
	stillWrites := []string{
		"pytest -q > results.txt",
		"pytest -q > results.txt 2>&1",
		"go build ./... &> build.log",
		"echo hi >> notes.md",
	}
	for _, cmd := range stillWrites {
		if !isWriteBashCommand(cmd) {
			t.Errorf("isWriteBashCommand(%q) = false, want true (writes a file)", cmd)
		}
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
		if r.URL.Path == "/api/dashboard/state" {
			_ = json.NewEncoder(w).Encode(state)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/workflows/") {
			id, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/workflows/"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			for _, wf := range state.Workflows {
				if got, _ := wf["id"].(string); got == id {
					_ = json.NewEncoder(w).Encode(wf)
					return
				}
			}
		}
		http.NotFound(w, r)
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

// Redirecting a stream to /dev/null discards it -- nothing reaches disk, so it is not a
// write. Measured live 2026-09-01: `grep -rn "..." --include=*.py uvo_rag_api/ 2>/dev/null
// | head -80` was denied twice to a reviewer in verify phase, because the write pattern
// " 2>" is tested BEFORE the read-only pattern "grep ". Same class as the `2>&1` inversion
// above, one step further: the fix there normalized descriptor duplication only.
func TestIsWriteBashCommandTreatsDevNullRedirectAsReadOnly(t *testing.T) {
	readOnly := []string{
		`grep -rn "MISSING_VALUE" --include=*.py uvo_rag_api/ tests/ 2>/dev/null | head -80`,
		"find . -name '*.go' 2>/dev/null",
		"cat missing.txt 2> /dev/null",
		"ls /nope &>/dev/null",
		"pytest -q >/dev/null 2>&1",
	}
	for _, cmd := range readOnly {
		if isWriteBashCommand(cmd) {
			t.Errorf("isWriteBashCommand(%q) = true, want false (/dev/null discards, it does not write)", cmd)
		}
	}

	// A redirect into a real FILE is still a write, /dev/null elsewhere or not.
	stillWrites := []string{
		"pytest -q > results.txt 2>/dev/null",
		"go build ./... 2>/dev/null > build.log",
		"echo hi >> /dev/null/notes.md",
	}
	for _, cmd := range stillWrites {
		if !isWriteBashCommand(cmd) {
			t.Errorf("isWriteBashCommand(%q) = false, want true (writes a file)", cmd)
		}
	}
}

// N1: one session can own several workflows at once. Judging an agent by whichever
// of them happens to be first denied a `fix`-phase engineer its first Edit because a
// sibling bug workflow sat in `review`. Measured live 2026-09-02.
func TestPhaseGuardAllowsWhenSessionHasMixedPhases(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "bug-x", "session_id": "session-a", "type": "bug", "phase": "review"},
			{"id": "spec-y", "session_id": "session-a", "type": "spec", "phase": "implement"},
		},
	})

	decision := PhaseGuard(HookEvent{
		ToolName:  "Edit",
		SessionID: "session-a",
		AgentType: "delivery-backend-engineer",
		ToolInput: map[string]any{"file_path": "/tmp/x.go"},
	})
	if !decision.Continue {
		t.Fatalf("expected allow when the session also owns a non-blocking workflow, got %q", decision.Reason)
	}
}

func TestPhaseGuardBlocksWhenAllSessionWorkflowsAreBlocking(t *testing.T) {
	setDashboardState(t, dashboardState{
		Workflows: []map[string]any{
			{"id": "bug-x", "session_id": "session-a", "type": "bug", "phase": "review"},
			{"id": "bug-y", "session_id": "session-a", "type": "bug", "phase": "review"},
		},
	})

	decision := PhaseGuard(HookEvent{
		ToolName:  "Write",
		SessionID: "session-a",
		AgentType: "delivery-code-reviewer",
		ToolInput: map[string]any{"file_path": "/tmp/x.go"},
	})
	if decision.Continue {
		t.Fatalf("expected block when every workflow of the session is in a blocking phase")
	}
	if !strings.Contains(decision.Reason, "bug-x") {
		t.Errorf("expected reason to name the blocking workflow, got %q", decision.Reason)
	}
}

// N3: "dd " matched the substring inside "add --detach", so a reviewer could not
// create the baseline worktree it needs to tell a regression from a pre-existing failure.
func TestIsWriteBashCommandWordBoundaries(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git worktree add --detach /tmp/baseline abc123", false},
		{"git worktree list", false},
		{"echo confirm", false},
		{"dd if=/dev/zero of=file", true},
		{"git add -A", true},
		{"git -C /repo commit -m x", true},
		{"rmdir olddir", true},
	}
	for _, tt := range tests {
		if got := isWriteBashCommand(tt.cmd); got != tt.want {
			t.Errorf("isWriteBashCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}
