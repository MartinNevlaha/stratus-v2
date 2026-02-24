package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/MartinNevlaha/stratus-v2/api"
	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/hooks"
	"github.com/MartinNevlaha/stratus-v2/mcp"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/terminal"
	"github.com/MartinNevlaha/stratus-v2/vexor"
)

//go:embed skills
var skillsFS embed.FS

//go:embed agents
var agentsFS embed.FS

//go:embed rules
var rulesFS embed.FS

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe()
	case "mcp-serve":
		cmdMCPServe()
	case "hook":
		cmdHook()
	case "init":
		cmdInit()
	case "update":
		cmdUpdate()
	case "refresh":
		cmdRefresh()
	case "version":
		fmt.Println("stratus v2.0.0")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `stratus - Claude Code extension framework

Commands:
  serve       Start HTTP API server + dashboard
  mcp-serve   Start MCP stdio server
  hook <name> Run a Claude Code hook handler
  init        Initialize stratus in the current project
  update      Update stratus binary and refresh project files
  refresh     Refresh agents, skills, and rules from the current binary
  version     Print version`)
}

func cmdServe() {
	cfg := config.Load()
	database := mustOpenDB(cfg)
	defer database.Close()

	// Index governance docs on startup (best-effort)
	go func() {
		if err := database.IndexGovernance(cfg.ProjectRoot); err != nil {
			log.Printf("governance index warning: %v", err)
		}
	}()

	coord := orchestration.NewCoordinator(database)
	vexorClient := vexor.New(cfg.Vexor.BinaryPath, cfg.Vexor.Model, cfg.Vexor.TimeoutSec)
	hub := api.NewHub()
	termMgr := terminal.NewManager()

	srv := api.NewServer(database, coord, vexorClient, hub, termMgr, cfg.ProjectRoot, cfg.STT.Endpoint)

	log.Printf("stratus serving on http://localhost:%d", cfg.Port)
	if err := srv.ListenAndServe(cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func cmdMCPServe() {
	cfg := config.Load()
	apiBase := fmt.Sprintf("http://localhost:%d", cfg.Port)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	srv := mcp.New()
	mcp.RegisterTools(srv, apiBase, httpClient)

	if err := srv.Serve(); err != nil {
		log.Fatalf("mcp serve error: %v", err)
	}
}

func cmdHook() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: stratus hook <name>")
		os.Exit(1)
	}
	hookName := os.Args[2]
	handlers := map[string]hooks.Handler{
		"phase_guard":       hooks.PhaseGuard,
		"delegation_guard":  hooks.DelegationGuard,
		"workflow_enforcer": hooks.WorkflowEnforcer,
	}
	hooks.Run(hookName, handlers)
}

func cmdInit() {
	wd, _ := os.Getwd()
	cfgPath := filepath.Join(wd, ".stratus.json")

	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Println("stratus already initialized (.stratus.json exists)")
		return
	}

	cfgContent := `{
  "port": 41777,
  "vexor": {
    "binary_path": "vexor",
    "model": "nomic-embed-text-v1.5",
    "timeout_sec": 15
  },
  "stt": {
    "endpoint": "http://localhost:8765"
  }
}
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		log.Fatalf("write .stratus.json: %v", err)
	}

	mcpJSON := `{
  "mcpServers": {
    "stratus": {
      "type": "stdio",
      "command": "stratus",
      "args": ["mcp-serve"]
    }
  }
}
`
	_ = os.WriteFile(filepath.Join(wd, ".mcp.json"), []byte(mcpJSON), 0o644)

	// Write coordinator skills to .claude/skills/
	if err := writeSkills(wd); err != nil {
		log.Printf("warning: could not write skills: %v", err)
	}

	// Write delivery agents to .claude/agents/
	if err := writeAgents(wd); err != nil {
		log.Printf("warning: could not write agents: %v", err)
	}

	// Write governance rules to .claude/rules/
	if err := writeRules(wd); err != nil {
		log.Printf("warning: could not write rules: %v", err)
	}

	// Run initial Vexor index (best-effort — skip if vexor not installed)
	vexorIndex(wd)

	fmt.Println(`stratus initialized!

Skills written to .claude/skills/:
  /spec          — spec-driven development
  /bug           — bug-fixing workflow
  /learn         — pattern learning
  /sync-stratus  — installation health check

Agents written to .claude/agents/:
  delivery-implementation-expert  — general-purpose implementation
  delivery-backend-engineer       — API, services, handlers
  delivery-frontend-engineer      — UI, components, pages
  delivery-database-engineer      — schema, migrations, queries
  delivery-devops-engineer        — CI/CD, Docker, infrastructure
  delivery-qa-engineer            — tests, coverage, lint
  delivery-code-reviewer          — code quality + security review
  delivery-debugger               — root cause diagnosis

Rules written to .claude/rules/:
  review-verdict-format  — structured PASS/FAIL verdicts
  tdd-requirements       — test-driven development
  error-handling         — consistent error patterns

Add hooks to .claude/settings.json:
{
  "hooks": {
    "PreToolUse": [
      {"command": "stratus hook phase_guard"},
      {"command": "stratus hook delegation_guard"}
    ]
  }
}`)
}

// writeSkills extracts embedded SKILL.md files into <project>/.claude/skills/.
func writeSkills(projectRoot string) error {
	return fs.WalkDir(skillsFS, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := skillsFS.ReadFile(path)
		if err != nil {
			return err
		}
		// path is like "skills/spec/SKILL.md"
		// target is .claude/skills/spec/SKILL.md
		rel, _ := filepath.Rel("skills", path)
		dest := filepath.Join(projectRoot, ".claude", "skills", rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}

// writeAgents extracts embedded agent .md files into <project>/.claude/agents/.
func writeAgents(projectRoot string) error {
	return fs.WalkDir(agentsFS, "agents", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := agentsFS.ReadFile(path)
		if err != nil {
			return err
		}
		// path is like "agents/delivery-backend-engineer.md"
		// target is .claude/agents/delivery-backend-engineer.md
		rel, _ := filepath.Rel("agents", path)
		dest := filepath.Join(projectRoot, ".claude", "agents", rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}

// writeRules extracts embedded rule .md files into <project>/.claude/rules/.
func writeRules(projectRoot string) error {
	return fs.WalkDir(rulesFS, "rules", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := rulesFS.ReadFile(path)
		if err != nil {
			return err
		}
		// path is like "rules/tdd-requirements.md"
		// target is .claude/rules/tdd-requirements.md
		rel, _ := filepath.Rel("rules", path)
		dest := filepath.Join(projectRoot, ".claude", "rules", rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}

// cmdUpdate updates the stratus binary via `go install`, then re-execs the new
// binary with `stratus refresh` to update project files from the latest embedded content.
func cmdUpdate() {
	if _, err := exec.LookPath("go"); err != nil {
		fmt.Fprintln(os.Stderr, "error: 'go' not found in PATH — install Go or update manually:")
		fmt.Fprintln(os.Stderr, "  go install github.com/MartinNevlaha/stratus-v2/cmd/stratus@latest")
		os.Exit(1)
	}

	fmt.Println("Updating stratus binary…")
	cmd := exec.Command("go", "install", "github.com/MartinNevlaha/stratus-v2/cmd/stratus@latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("update failed: %v", err)
	}
	fmt.Println("Binary updated.")

	// Locate the newly installed binary and re-exec it with `refresh`
	// so project files are written from the new binary's embedded content.
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}
	newBin := filepath.Join(gopath, "bin", "stratus")

	wd, _ := os.Getwd()
	refresh := exec.Command(newBin, "refresh")
	refresh.Dir = wd
	refresh.Stdout = os.Stdout
	refresh.Stderr = os.Stderr
	if err := refresh.Run(); err != nil {
		log.Fatalf("refresh failed: %v", err)
	}
}

// cmdRefresh re-writes agents, skills, and rules from the current binary's
// embedded content. Safe to run on an already-initialized project — never
// touches .stratus.json or .mcp.json.
func cmdRefresh() {
	wd, _ := os.Getwd()

	if _, err := os.Stat(filepath.Join(wd, ".stratus.json")); err != nil {
		fmt.Fprintln(os.Stderr, "error: stratus not initialized here (no .stratus.json) — run `stratus init` first")
		os.Exit(1)
	}

	if err := writeSkills(wd); err != nil {
		log.Printf("warning: could not write skills: %v", err)
	}
	if err := writeAgents(wd); err != nil {
		log.Printf("warning: could not write agents: %v", err)
	}
	if err := writeRules(wd); err != nil {
		log.Printf("warning: could not write rules: %v", err)
	}

	fmt.Println("stratus refreshed — agents, skills, and rules updated to latest version.")
}

// vexorIndex runs `vexor index` in the project root directory.
// Best-effort: skipped silently if the vexor binary is not installed.
func vexorIndex(projectRoot string) {
	if _, err := exec.LookPath("vexor"); err != nil {
		fmt.Println("vexor not found — skipping code index (install vexor to enable semantic code search)")
		return
	}
	fmt.Print("Indexing codebase with vexor… ")
	cmd := exec.Command("vexor", "index")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("warning: vexor index failed: %v\n", err)
		return
	}
	fmt.Println("done.")
}

func mustOpenDB(cfg config.Config) *db.DB {
	dbPath := filepath.Join(cfg.DataDir, "stratus.db")
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	return database
}
