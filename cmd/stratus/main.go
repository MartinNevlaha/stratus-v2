package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/MartinNevlaha/stratus-v2/api"
	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/hooks"
	"github.com/MartinNevlaha/stratus-v2/mcp"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/swarm"
	"github.com/MartinNevlaha/stratus-v2/terminal"
	"github.com/MartinNevlaha/stratus-v2/vexor"
)

const (
	sttContainerName = "stratus-stt"
	sttImage         = "ghcr.io/speaches-ai/speaches:latest-cpu"
	sttDefaultModel  = "Systran/faster-whisper-small"
	sttHost          = "http://localhost:8011"
)

//go:embed skills
var skillsFS embed.FS

//go:embed agents
var agentsFS embed.FS

//go:embed agents-opencode
var agentsOpenCodeFS embed.FS

//go:embed commands-opencode
var commandsOpenCodeFS embed.FS

//go:embed plugins-opencode
var pluginsOpenCodeFS embed.FS

//go:embed rules
var rulesFS embed.FS

//go:embed static
var staticFiles embed.FS

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
	case "statusline":
		cmdStatusline()
	case "version":
		fmt.Println("stratus v" + Version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `stratus - Claude Code / OpenCode extension framework

Commands:
  serve       Start HTTP API server + dashboard
  mcp-serve   Start MCP stdio server
  hook <name> Run a Claude Code hook handler
  init        Initialize stratus in the current project
              Flags: --force (re-run), --target [claude-code|opencode|both]
  update      Update stratus binary and refresh project files
  refresh     Refresh agents, skills, and rules from the current binary
              Flags: --target [claude-code|opencode|both]
  statusline  Emit ANSI status bar (invoked by Claude Code via settings.json)
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

	// Strip the "static/" prefix so the FS root is the build output directory.
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("static fs: %v", err)
	}

	var syncedVersion string
	var skippedFiles []string
	if cfg.SyncState != nil {
		syncedVersion = cfg.SyncState.SyncedVersion
		skippedFiles = cfg.SyncState.SkippedFiles
	}
	swarmStore := swarm.NewStore(database, cfg.ProjectRoot)
	srv := api.NewServer(database, coord, vexorClient, hub, termMgr, cfg.ProjectRoot, cfg.STT.Endpoint, cfg.STT.Model, staticFS, Version, syncedVersion, skippedFiles, swarmStore)

	// Start STT container (best-effort).
	sttOwned := sttStart(cfg.STT.Model)

	// Handle SIGINT/SIGTERM for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("stratus shutting down…")
		if sttOwned {
			sttStop()
		}
		os.Exit(0)
	}()

	log.Printf("stratus serving on http://localhost:%d", cfg.Port)
	if err := srv.ListenAndServe(cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// sttStart starts the speaches STT container. Returns true if this process
// owns the container (i.e. it started it) so it knows to stop it on exit.
func sttStart(model string) bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	// "whisper-1" is the OpenAI API alias; speaches needs the HuggingFace model ID.
	if model == "" || model == "whisper-1" {
		model = sttDefaultModel
	}

	// Check existing container state.
	out, _ := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", sttContainerName).Output()
	switch strings.TrimSpace(string(out)) {
	case "true":
		log.Printf("STT: container %q already running", sttContainerName)
		return false // not owned by us
	case "false":
		// Stale stopped container — remove so we can recreate with current config.
		exec.Command("docker", "rm", sttContainerName).Run()
	}

	args := []string{
		"run", "-d",
		"--name", sttContainerName,
		"-p", "8011:8000",
		"-e", "WHISPER__MODEL=" + model,
		"-v", "stratus-whisper-cache:/root/.cache/huggingface",
		sttImage,
	}
	if err := exec.Command("docker", args...).Run(); err != nil {
		log.Printf("STT: could not start container: %v", err)
		return false
	}
	log.Printf("STT: container started (model: %s)", model)

	// Install the model in speaches in the background — speaches tracks
	// installed models separately from the HuggingFace file cache.
	// POST /v1/models/{id} triggers download; subsequent calls are no-ops.
	go sttInstallModel(sttHost, model)
	return true
}

// sttInstallModel waits for the speaches health endpoint then installs the model.
func sttInstallModel(host, model string) {
	client := &http.Client{Timeout: 5 * time.Second}
	// Wait up to 30s for the container to be healthy.
	for range 30 {
		resp, err := client.Get(host + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(time.Second)
	}

	// POST /v1/models/{model_id} — triggers async download. Use a longer timeout
	// since speaches may need time to start the download before responding.
	installClient := &http.Client{Timeout: 120 * time.Second}
	encoded := strings.ReplaceAll(model, "/", "%2F")
	req, err := http.NewRequest(http.MethodPost, host+"/v1/models/"+encoded, nil)
	if err != nil {
		log.Printf("STT: model install request error: %v", err)
		return
	}
	resp, err := installClient.Do(req)
	if err != nil {
		log.Printf("STT: model install failed: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		log.Printf("STT: model %s installing/ready (status %d)", model, resp.StatusCode)
	} else {
		log.Printf("STT: model install returned %d", resp.StatusCode)
	}
}

// sttStop stops and removes the speaches container.
func sttStop() {
	exec.Command("docker", "stop", sttContainerName).Run()
	exec.Command("docker", "rm", sttContainerName).Run()
	log.Println("STT: container stopped")
}

// sttPullImage pulls the speaches Docker image during init (best-effort).
func sttPullImage() {
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("docker not found — skipping STT image pull (install Docker to enable voice input)")
		return
	}
	fmt.Printf("Pulling STT image (%s)… \n", sttImage)
	cmd := exec.Command("docker", "pull", sttImage)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("warning: docker pull failed: %v\n", err)
		return
	}
	fmt.Println("STT image ready.")
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
		"watcher":           hooks.Watcher,
		"teammate_idle":     hooks.TeammateIdle,
		"task_completed":    hooks.TaskCompleted,
	}
	hooks.Run(hookName, handlers)
}

func parseInitFlags() (force bool, target string) {
	target = "claude-code"
	for i := 2; i < len(os.Args); i++ {
		switch {
		case os.Args[i] == "--force":
			force = true
		case os.Args[i] == "--target" && i+1 < len(os.Args):
			target = os.Args[i+1]
			i++
		case strings.HasPrefix(os.Args[i], "--target="):
			target = strings.TrimPrefix(os.Args[i], "--target=")
		}
	}
	return
}

func cmdInit() {
	force, target := parseInitFlags()
	if target != "claude-code" && target != "opencode" && target != "both" {
		fmt.Fprintf(os.Stderr, "warning: unknown --target %q, defaulting to 'claude-code'\n", target)
		target = "claude-code"
	}
	wd, _ := os.Getwd()
	cfgPath := filepath.Join(wd, ".stratus.json")

	if _, err := os.Stat(cfgPath); err == nil && !force {
		fmt.Println("stratus already initialized (.stratus.json exists) — use --force to re-run")
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
    "endpoint": "http://localhost:8011"
  }
}
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		log.Fatalf("write .stratus.json: %v", err)
	}

	allHashes := make(map[string]string)
	switch target {
	case "opencode":
		initOpenCode(wd, allHashes)
	case "both":
		initClaudeCode(wd, allHashes)
		initOpenCode(wd, allHashes)
	default: // "claude-code"
		initClaudeCode(wd, allHashes)
	}

	// Record sync state so future refreshes can detect user customizations.
	initCfg := config.Load()
	initCfg.SyncState = &config.SyncState{
		SyncedVersion: Version,
		AssetHashes:   allHashes,
	}
	if err := initCfg.Save(cfgPath); err != nil {
		log.Printf("warning: could not save sync state: %v", err)
	}

	// Pull STT Docker image (best-effort — skip if docker not installed)
	sttPullImage()

	// Run initial Vexor index (best-effort — skip if vexor not installed)
	vexorIndex(wd)

	// Index governance docs into the DB (best-effort)
	governanceIndex(wd)

	printInitSummary(target)
}

// initClaudeCode writes all Claude Code integration files: .mcp.json,
// .claude/skills|agents|rules, and registers hooks in .claude/settings.json.
func initClaudeCode(wd string, allHashes map[string]string) {
	if err := writeMCP(wd); err != nil {
		log.Printf("warning: could not write .mcp.json: %v", err)
	}
	for _, spec := range []struct {
		fsys   embed.FS
		root   string
		subdir string
	}{
		{skillsFS, "skills", "skills"},
		{agentsFS, "agents", "agents"},
		{rulesFS, "rules", "rules"},
	} {
		res, err := writeAssetsFS(spec.fsys, spec.root, spec.subdir, wd, nil)
		if err != nil {
			log.Printf("warning: could not write %s: %v", spec.root, err)
		}
		for k, v := range res.hashes {
			allHashes[k] = v
		}
	}
	if err := writeHooks(wd); err != nil {
		log.Printf("warning: could not register hooks: %v", err)
	}
}

// initOpenCode writes all OpenCode integration files: opencode.json (MCP + plugin),
// .opencode/agents|commands|plugin, and .claude/skills|rules (shared with Claude Code).
func initOpenCode(wd string, allHashes map[string]string) {
	if err := writeOpenCodeConfig(wd); err != nil {
		log.Printf("warning: could not write opencode.json: %v", err)
	}
	openCodeDir := filepath.Join(wd, ".opencode")

	// Shared assets: skills and rules go to .claude/ (OpenCode reads .claude/skills/ natively).
	for _, spec := range []struct {
		fsys   embed.FS
		root   string
		subdir string
	}{
		{skillsFS, "skills", "skills"},
		{rulesFS, "rules", "rules"},
	} {
		res, err := writeAssetsFS(spec.fsys, spec.root, spec.subdir, wd, nil)
		if err != nil {
			log.Printf("warning: could not write %s: %v", spec.root, err)
		}
		for k, v := range res.hashes {
			allHashes[k] = v
		}
	}

	// OpenCode-specific assets.
	for _, spec := range []struct {
		fsys    embed.FS
		root    string
		destDir string
	}{
		{agentsOpenCodeFS, "agents-opencode", filepath.Join(openCodeDir, "agents")},
		{commandsOpenCodeFS, "commands-opencode", filepath.Join(openCodeDir, "commands")},
		{pluginsOpenCodeFS, "plugins-opencode", filepath.Join(openCodeDir, "plugin")},
	} {
		res, err := writeAssetsTo(spec.fsys, spec.root, spec.destDir, nil)
		if err != nil {
			log.Printf("warning: could not write %s: %v", spec.root, err)
		}
		for k, v := range res.hashes {
			allHashes[k] = v
		}
	}
}

func printInitSummary(target string) {
	const skills = `Skills written to .claude/skills/:
  /spec                        — spec-driven development
  /spec-complex                — complex spec (discovery→design→plan→implement→verify→learn)
  /bug                         — bug-fixing workflow
  /learn                       — pattern learning
  /sync-stratus                — installation health check
  /vexor-cli                   — semantic file discovery
  /governance-db               — query governance docs and ADRs
  /create-architecture         — design ADRs, component diagrams, interfaces
  /explain-architecture        — read-only architecture explanation
  /run-tests                   — auto-detect and run test suite
  /code-review                 — structured code review (PASS/FAIL)
  /find-bugs                   — systematic bug diagnosis (read-only)
  /security-review             — security audit (OWASP, secrets, injection)
  /frontend-design             — distinctive UI design guidance
  /react-native-best-practices — React Native / Expo performance patterns`

	const ccAgents = `Agents written to .claude/agents/:
  delivery-implementation-expert  — general-purpose implementation
  delivery-backend-engineer       — API, services, handlers
  delivery-frontend-engineer      — UI, components, pages
  delivery-ux-designer            — UI/UX design specs and design systems
  delivery-database-engineer      — schema, migrations, queries
  delivery-devops-engineer        — CI/CD, Docker, infrastructure
  delivery-mobile-engineer        — React Native / Expo (iOS + Android)
  delivery-system-architect       — component designs, API contracts (read-only)
  delivery-strategic-architect    — ADRs, technology selection (read-only)
  delivery-qa-engineer            — tests, coverage, lint
  delivery-code-reviewer          — code quality + security review
  delivery-governance-checker     — governance & ADR compliance
  delivery-debugger               — root cause diagnosis`

	const ocAgents = `Agents written to .opencode/agents/:
  delivery-implementation-expert  — general-purpose implementation
  delivery-backend-engineer       — API, services, handlers
  delivery-frontend-engineer      — UI, components, pages
  delivery-ux-designer            — UI/UX design specs and design systems
  delivery-database-engineer      — schema, migrations, queries
  delivery-devops-engineer        — CI/CD, Docker, infrastructure
  delivery-mobile-engineer        — React Native / Expo (iOS + Android)
  delivery-system-architect       — component designs, API contracts (read-only)
  delivery-strategic-architect    — ADRs, technology selection (read-only)
  delivery-qa-engineer            — tests, coverage, lint
  delivery-code-reviewer          — code quality + security review (read-only)
  delivery-governance-checker     — governance & ADR compliance (read-only)
  delivery-debugger               — root cause diagnosis (read-only)`

	const ocCommands = `Commands written to .opencode/commands/:
  /spec          — spec-driven development
  /spec-complex  — complex spec workflow
  /bug           — bug-fixing workflow
  /learn         — pattern learning
  /sync-stratus  — installation health check
  /team          — parallel delivery`

	const ocPlugin = `Plugin written to .opencode/plugin/stratus.ts:
  phase_guard — blocks write tools during verify/review phases
  watcher     — queues modified files for vexor reindexing`

	const rules = `Rules written to .claude/rules/:
  review-verdict-format — structured PASS/FAIL verdicts
  tdd-requirements      — test-driven development
  error-handling        — consistent error patterns`

	const ccHooks = `Hooks registered in .claude/settings.json:
  PreToolUse  phase_guard       — blocks write tools during review/verify
  PreToolUse  delegation_guard  — requires active workflow for delivery agents
  PostToolUse watcher           — queues modified files for vexor reindexing

Statusline registered in .claude/settings.json — workflow status visible in Claude Code status bar`

	fmt.Println("stratus initialized!")
	fmt.Println()
	fmt.Println(skills)
	fmt.Println()

	switch target {
	case "opencode":
		fmt.Println(ocAgents)
		fmt.Println()
		fmt.Println(ocCommands)
		fmt.Println()
		fmt.Println(ocPlugin)
		fmt.Println()
		fmt.Println(rules)
		fmt.Println()
		fmt.Println("MCP server registered in opencode.json")
	case "both":
		fmt.Println(ccAgents)
		fmt.Println()
		fmt.Println(ocAgents)
		fmt.Println()
		fmt.Println(ocCommands)
		fmt.Println()
		fmt.Println(ocPlugin)
		fmt.Println()
		fmt.Println(rules)
		fmt.Println()
		fmt.Println(ccHooks)
		fmt.Println()
		fmt.Println("MCP server registered in .mcp.json (Claude Code) and opencode.json (OpenCode)")
	default: // "claude-code"
		fmt.Println(ccAgents)
		fmt.Println()
		fmt.Println(rules)
		fmt.Println()
		fmt.Println(ccHooks)
		fmt.Println()
		fmt.Println("Governance docs indexed into DB (CLAUDE.md, rules, skills, agents, ADRs)")
	}
}

// sha256hex returns the hex-encoded SHA-256 digest of data.
func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// diskSHA256 returns the SHA-256 hex digest of the file at path, or "" if the
// file cannot be read (does not exist, permission error, etc.).
func diskSHA256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return sha256hex(data)
}

// assetWriteResult holds the outcome of a writeAssetsFS call.
type assetWriteResult struct {
	hashes  map[string]string // embedded path -> embedded hash (files written)
	skipped []string          // embedded paths skipped (user-customized)
}

// writeAssetsTo walks fsys starting at fsRoot, writing each file under destDir.
// When storedHashes is nil (force mode, used by init) every file is written
// unconditionally.  In smart mode (storedHashes != nil) the 3-way comparison is
// applied:
//
//   - stored hash == ""       → first-time, write and record hash
//   - embedded == stored      → unchanged in new version, skip write
//   - embedded != stored AND disk == stored  → user hasn't touched, safe to overwrite
//   - embedded != stored AND disk != stored  → user customized, skip and report
func writeAssetsTo(
	fsys embed.FS, fsRoot, destDir string,
	storedHashes map[string]string,
) (assetWriteResult, error) {
	result := assetWriteResult{hashes: make(map[string]string)}
	err := fs.WalkDir(fsys, fsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fsys.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(fsRoot, path)
		dest := filepath.Join(destDir, rel)

		embeddedHash := sha256hex(data)

		if storedHashes != nil {
			storedHash := storedHashes[path]
			if storedHash != "" {
				if embeddedHash == storedHash {
					// Content unchanged in new binary — no need to write.
					return nil
				}
				// Content changed in new binary — check if user modified the disk file.
				if diskSHA256(dest) != storedHash {
					// Disk differs from what we last wrote → user customized it, skip.
					result.skipped = append(result.skipped, path)
					return nil
				}
				// Disk matches what we wrote → safe to overwrite with new content.
			}
			// storedHash == "" means this file is new in this version; write it.
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return err
		}
		result.hashes[path] = embeddedHash
		return nil
	})
	return result, err
}

// writeAssetsFS is a convenience wrapper around writeAssetsTo that writes to
// <projectRoot>/.claude/<claudeSubdir>/.
func writeAssetsFS(
	fsys embed.FS, fsRoot, claudeSubdir, projectRoot string,
	storedHashes map[string]string,
) (assetWriteResult, error) {
	return writeAssetsTo(fsys, fsRoot, filepath.Join(projectRoot, ".claude", claudeSubdir), storedHashes)
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
// touches .stratus.json or .mcp.json / opencode.json.
//
// Smart mode: if sync_state.asset_hashes is present in .stratus.json, files
// that the user has customized (disk hash differs from stored hash) are skipped
// rather than overwritten.
//
// Flags: --target [claude-code|opencode|both]
func cmdRefresh() {
	_, target := parseInitFlags() // reuse flag parser; force is ignored for refresh
	if target != "claude-code" && target != "opencode" && target != "both" {
		fmt.Fprintf(os.Stderr, "warning: unknown --target %q, defaulting to 'claude-code'\n", target)
		target = "claude-code"
	}
	wd, _ := os.Getwd()
	cfgPath := filepath.Join(wd, ".stratus.json")

	if _, err := os.Stat(cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "error: stratus not initialized here (no .stratus.json) — run `stratus init` first")
		os.Exit(1)
	}

	cfg := config.Load()
	var storedHashes map[string]string
	if cfg.SyncState != nil {
		storedHashes = cfg.SyncState.AssetHashes
	}

	allHashes := make(map[string]string)
	var allSkipped []string

	switch target {
	case "opencode":
		skipped := refreshOpenCode(wd, storedHashes, allHashes)
		allSkipped = append(allSkipped, skipped...)
	case "both":
		skipped := refreshClaudeCode(wd, storedHashes, allHashes)
		allSkipped = append(allSkipped, skipped...)
		skipped = refreshOpenCode(wd, storedHashes, allHashes)
		allSkipped = append(allSkipped, skipped...)
	default: // "claude-code"
		skipped := refreshClaudeCode(wd, storedHashes, allHashes)
		allSkipped = append(allSkipped, skipped...)
	}

	// Persist updated sync state.
	if cfg.SyncState == nil {
		cfg.SyncState = &config.SyncState{AssetHashes: make(map[string]string)}
	}
	for k, v := range allHashes {
		cfg.SyncState.AssetHashes[k] = v
	}
	cfg.SyncState.SyncedVersion = Version
	cfg.SyncState.SkippedFiles = allSkipped
	if err := cfg.Save(cfgPath); err != nil {
		log.Printf("warning: could not save sync state: %v", err)
	}

	if len(allSkipped) > 0 {
		fmt.Printf("stratus refreshed — %d customized file(s) skipped (your changes preserved).\n", len(allSkipped))
		for _, f := range allSkipped {
			fmt.Printf("  ⚠ skipped: %s\n", f)
		}
		fmt.Println("Run /sync-stratus to review the new asset versions.")
	} else {
		fmt.Println("stratus refreshed — agents, skills, rules, and hooks updated to latest version.")
	}
}

// refreshClaudeCode refreshes Claude Code assets (skills, agents, rules, hooks).
func refreshClaudeCode(wd string, storedHashes map[string]string, allHashes map[string]string) []string {
	var allSkipped []string
	for _, spec := range []struct {
		fsys   embed.FS
		root   string
		subdir string
	}{
		{skillsFS, "skills", "skills"},
		{agentsFS, "agents", "agents"},
		{rulesFS, "rules", "rules"},
	} {
		res, err := writeAssetsFS(spec.fsys, spec.root, spec.subdir, wd, storedHashes)
		if err != nil {
			log.Printf("warning: %v", err)
		}
		for k, v := range res.hashes {
			allHashes[k] = v
		}
		allSkipped = append(allSkipped, res.skipped...)
	}
	if err := writeHooks(wd); err != nil {
		log.Printf("warning: could not register hooks: %v", err)
	}
	return allSkipped
}

// refreshOpenCode refreshes OpenCode assets (skills, rules to .claude/; agents,
// commands, plugin to .opencode/).
func refreshOpenCode(wd string, storedHashes map[string]string, allHashes map[string]string) []string {
	var allSkipped []string
	openCodeDir := filepath.Join(wd, ".opencode")

	// Shared assets.
	for _, spec := range []struct {
		fsys   embed.FS
		root   string
		subdir string
	}{
		{skillsFS, "skills", "skills"},
		{rulesFS, "rules", "rules"},
	} {
		res, err := writeAssetsFS(spec.fsys, spec.root, spec.subdir, wd, storedHashes)
		if err != nil {
			log.Printf("warning: %v", err)
		}
		for k, v := range res.hashes {
			allHashes[k] = v
		}
		allSkipped = append(allSkipped, res.skipped...)
	}

	// OpenCode-specific.
	for _, spec := range []struct {
		fsys    embed.FS
		root    string
		destDir string
	}{
		{agentsOpenCodeFS, "agents-opencode", filepath.Join(openCodeDir, "agents")},
		{commandsOpenCodeFS, "commands-opencode", filepath.Join(openCodeDir, "commands")},
		{pluginsOpenCodeFS, "plugins-opencode", filepath.Join(openCodeDir, "plugin")},
	} {
		res, err := writeAssetsTo(spec.fsys, spec.root, spec.destDir, storedHashes)
		if err != nil {
			log.Printf("warning: %v", err)
		}
		for k, v := range res.hashes {
			allHashes[k] = v
		}
		allSkipped = append(allSkipped, res.skipped...)
	}
	return allSkipped
}

// writeMCP merges the stratus MCP server entry into <project>/.mcp.json.
// If the file already contains other MCP servers they are preserved.
func writeMCP(projectRoot string) error {
	mcpPath := filepath.Join(projectRoot, ".mcp.json")

	// Read existing config (best-effort).
	var existing map[string]any
	if data, err := os.ReadFile(mcpPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = map[string]any{}
	}

	// Get or create "mcpServers" object.
	servers, _ := existing["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}

	// Only add stratus if not already present.
	if _, ok := servers["stratus"]; !ok {
		servers["stratus"] = map[string]any{
			"type":    "stdio",
			"command": "stratus",
			"args":    []string{"mcp-serve"},
		}
	}

	existing["mcpServers"] = servers
	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(mcpPath, append(out, '\n'), 0o644)
}

// writeOpenCodeConfig merges the stratus MCP server entry into
// <project>/opencode.json. If the file already contains other configuration it
// is preserved — only "mcp.stratus" is added/updated.
// Local plugins are auto-discovered from .opencode/plugins/ and do not need
// a config entry.
func writeOpenCodeConfig(projectRoot string) error {
	ocPath := filepath.Join(projectRoot, "opencode.json")

	// Read existing config (best-effort).
	var existing map[string]any
	if data, err := os.ReadFile(ocPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = map[string]any{
			"$schema": "https://opencode.ai/config.json",
		}
	}

	// Get or create "mcp" object.
	mcpSection, _ := existing["mcp"].(map[string]any)
	if mcpSection == nil {
		mcpSection = map[string]any{}
	}
	if _, ok := mcpSection["stratus"]; !ok {
		mcpSection["stratus"] = map[string]any{
			"type":    "local",
			"command": []string{"stratus", "mcp-serve"},
			"enabled": true,
		}
	}
	existing["mcp"] = mcpSection

	// Remove stale "plugins" key from older Stratus versions — OpenCode
	// auto-discovers local plugins from .opencode/plugins/.
	delete(existing, "plugins")

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ocPath, append(out, '\n'), 0o644)
}

// writeHooks registers stratus hooks in <project>/.claude/settings.json.
// It reads the existing file (if any), merges the stratus hooks without
// disturbing user-defined hooks, then writes the result back.
func writeHooks(projectRoot string) error {
	claudeDir := filepath.Join(projectRoot, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return err
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Read existing settings (best-effort; start with empty map on miss).
	var settings map[string]any
	if data, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = map[string]any{}
	}

	// Extract or create the top-level "hooks" object.
	hooksSection, _ := settings["hooks"].(map[string]any)
	if hooksSection == nil {
		hooksSection = map[string]any{}
	}

	// Stratus hooks: {event → [{matcher, command}]}.
	type hookDef struct{ matcher, command string }
	defs := []struct {
		event string
		hooks []hookDef
	}{
		{
			event: "PreToolUse",
			hooks: []hookDef{
				{"Write|Edit|Bash|NotebookEdit|MultiEdit", "stratus hook phase_guard"},
				{"Task", "stratus hook delegation_guard"},
			},
		},
		{
			event: "PostToolUse",
			hooks: []hookDef{
				{"Write|Edit|MultiEdit|NotebookEdit", "stratus hook watcher"},
			},
		},
		{
			event: "TeammateIdle",
			hooks: []hookDef{
				{"", "stratus hook teammate_idle"},
			},
		},
		{
			event: "TaskCompleted",
			hooks: []hookDef{
				{"", "stratus hook task_completed"},
			},
		},
	}

	for _, d := range defs {
		groups, _ := hooksSection[d.event].([]any)
		for _, h := range d.hooks {
			if hasStratusHook(groups, h.command) {
				continue // already registered
			}
			groups = append(groups, map[string]any{
				"matcher": h.matcher,
				"hooks":   []any{map[string]any{"type": "command", "command": h.command}},
			})
		}
		hooksSection[d.event] = groups
	}

	settings["hooks"] = hooksSection

	// Register env non-destructively (preserve user customisation).
	envSection, _ := settings["env"].(map[string]any)
	if envSection == nil {
		envSection = map[string]any{}
	}
	if _, ok := envSection["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"]; !ok {
		envSection["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] = "1"
	}
	settings["env"] = envSection

	// Register statusLine non-destructively (preserve user customisation).
	if _, ok := settings["statusLine"]; !ok {
		settings["statusLine"] = map[string]any{
			"type":    "command",
			"command": `bash -c 'input=$(cat); echo "$input" | stratus statusline'`,
		}
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(out, '\n'), 0o644)
}

// hasStratusHook returns true when command is already present in the hook groups slice.
func hasStratusHook(groups []any, command string) bool {
	for _, g := range groups {
		group, ok := g.(map[string]any)
		if !ok {
			continue
		}
		hooks, _ := group["hooks"].([]any)
		for _, h := range hooks {
			entry, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, _ := entry["command"].(string); cmd == command {
				return true
			}
		}
	}
	return false
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

// governanceIndex indexes governance docs into the DB at init time.
// Best-effort: errors are printed as warnings, never fatal.
func governanceIndex(projectRoot string) {
	fmt.Print("Indexing governance docs… ")
	cfg := config.Load()
	database := mustOpenDB(cfg)
	defer database.Close()
	if err := database.IndexGovernance(projectRoot); err != nil {
		fmt.Printf("warning: governance index failed: %v\n", err)
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
