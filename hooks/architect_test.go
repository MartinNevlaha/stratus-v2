package hooks

import (
	"path/filepath"
	"strings"
	"testing"
)

// Architects write design docs and ADRs, never source: Markdown under a docs/ directory of
// the project, or of a swarm worktree inside it, is theirs; anything else is denied.
func TestPhaseGuard_ArchitectsWriteOnlyDocs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	setDashboardState(t, dashboardState{})

	cases := []struct {
		agent, tool, path string
		allowed           bool
	}{
		{"delivery-system-architect", "Write", "docs/plans/x-design.md", true},
		{"delivery-strategic-architect", "Edit", "docs/decisions/ADR-001.md", true},
		{"delivery-system-architect", "Write", ".stratus/worktrees/w1/docs/adr/ADR-002.md", true},
		{"delivery-system-architect", "Write", "hooks/x.go", false},
		{"delivery-system-architect", "Edit", "docs/x.go", false},
		{"delivery-strategic-architect", "Write", "README.md", false},
		{"delivery-system-architect", "Write", "../outside/docs/a.md", false},
		{"delivery-system-architect", "Write", ".claude/rules/docs/override.md", false},
		{"delivery-system-architect", "Write", "node_modules/x/docs/README.md", false},
		{"delivery-strategic-architect", "Write", "Docs/decisions/ADR-003.md", true},
		{"delivery-backend-engineer", "Write", "hooks/x.go", true},
	}
	for _, c := range cases {
		path := filepath.Join(root, c.path)
		decision := PhaseGuard(HookEvent{
			ToolName:  c.tool,
			AgentType: c.agent,
			Cwd:       root,
			SessionID: "s1",
			ToolInput: map[string]any{"file_path": path},
		})
		if decision.Continue != c.allowed {
			t.Errorf("%s %s %s: allowed=%v, want %v (%s)", c.agent, c.tool, c.path, decision.Continue, c.allowed, decision.Reason)
		}
		if !c.allowed && !strings.Contains(decision.Reason, "docs/") {
			t.Errorf("%s %s: the reason should say where it may write: %q", c.agent, c.path, decision.Reason)
		}
	}
}

func TestPhaseGuard_ArchitectNotebookOutsideDocsDenied(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	setDashboardState(t, dashboardState{})

	decision := PhaseGuard(HookEvent{
		ToolName:  "NotebookEdit",
		AgentType: "delivery-system-architect",
		Cwd:       root,
		ToolInput: map[string]any{"notebook_path": filepath.Join(root, "analysis.ipynb")},
	})
	if decision.Continue {
		t.Fatalf("an architect edited a notebook outside docs/")
	}
}

// Architects get Bash for reading; a Bash command that writes would bypass the docs/ rule.
func TestPhaseGuard_ArchitectBashWritesDenied(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	setDashboardState(t, dashboardState{})

	for cmd, allowed := range map[string]bool{
		"cat > cmd/main.go <<'EOF'\npackage main\nEOF": false,
		"git log --oneline -5":                         true,
	} {
		decision := PhaseGuard(HookEvent{
			ToolName: "Bash", AgentType: "delivery-system-architect", Cwd: root,
			ToolInput: map[string]any{"command": cmd},
		})
		if decision.Continue != allowed {
			t.Errorf("%q: allowed=%v, want %v (%s)", cmd, decision.Continue, allowed, decision.Reason)
		}
	}
}

// Architects write design docs with a heredoc as often as with Write; a Bash write whose every
// target is a Markdown file under docs/ is the same act and must not be denied.
func TestPhaseGuard_ArchitectBashDocsWritesAllowed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	setDashboardState(t, dashboardState{})

	for cmd, allowed := range map[string]bool{
		"cat > docs/plans/x-design.md <<'EOF'\n# Design\nEOF":                 true,
		"mkdir -p docs/plans && cat > docs/plans/x.md <<'EOF'\nbody\nEOF":     true,
		"ls docs/adr | grep ADR; cat >> docs/adr/ADR-1.md <<'EOF'\nmore\nEOF": true,
		"cat > " + root + "/docs/decisions/ADR-2.md <<'EOF'\nx\nEOF":          true,
		"cat > docs/plans/x.go <<'EOF'\nx\nEOF":                               false,
		"cat > hooks/x.go <<'EOF'\nx\nEOF":                                    false,
		"cat > docs/a.md <<'EOF'\nx\nEOF\ncat > hooks/x.go <<'EOF'\ny\nEOF":   false,
		"cat > ../docs/a.md <<'EOF'\nx\nEOF":                                  false,
		"mkdir -p hooks/new":                                                  false,
		"rm docs/plans/x.md":                                                  false,
		"echo x > docs/a.md && git commit -am x":                              false,
	} {
		decision := PhaseGuard(HookEvent{
			ToolName: "Bash", AgentType: "delivery-system-architect", Cwd: root,
			ToolInput: map[string]any{"command": cmd},
		})
		if decision.Continue != allowed {
			t.Errorf("%q: allowed=%v, want %v (%s)", cmd, decision.Continue, allowed, decision.Reason)
		}
	}
}
