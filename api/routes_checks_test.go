package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

const checksHint = `add "checks" to .stratus.json`

func cfgChecks(checks ...config.CheckConfig) *config.Config {
	return &config.Config{Checks: checks}
}

func chk(name string, timeout int, cmd ...string) config.CheckConfig {
	return config.CheckConfig{Name: name, Cmd: cmd, TimeoutSec: timeout}
}

func gateNames(gs []gateSpec) []string {
	var out []string
	for _, g := range gs {
		out = append(out, g.Name)
	}
	return out
}

// nonGoMission is handoffFixture turned into a repository without go.mod, with one
// change on the integration branch so the placeholder scan has a diff to scan.
func nonGoMission(t *testing.T) (*Server, string) {
	t.Helper()
	s, _, wt := handoffFixture(t)
	gateGit(t, wt, "rm", "-q", "go.mod", "main.go")
	gateWrite(t, wt, "README.md", "docs only\n")
	gateGit(t, wt, "add", ".")
	gateGit(t, wt, "commit", "-q", "-m", "not a go project")
	return s, wt
}

func TestResolveGateSet_ConfiguredWinsOverGoModule(t *testing.T) {
	dir := t.TempDir()
	gateWrite(t, dir, "go.mod", "module x\n")
	gates, warns := resolveGateSet(cfgChecks(chk("test", 0, "npm", "test"), chk("lint", 5, "make", "lint")), dir)
	if got := strings.Join(gateNames(gates), ","); got != "test,lint" || len(warns) != 0 {
		t.Fatalf("gates %q warnings %v", got, warns)
	}
	if gates[0].Timeout != 120*1e9 || gates[1].Timeout != 5*1e9 {
		t.Fatalf("timeouts %v %v", gates[0].Timeout, gates[1].Timeout)
	}
}

func TestResolveGateSet_InvalidConfigWarnsAndAutodetects(t *testing.T) {
	dir := t.TempDir()
	gateWrite(t, dir, "go.mod", "module x\n")
	gates, warns := resolveGateSet(cfgChecks(chk("Bad Name", 0, "true")), dir)
	if got := strings.Join(gateNames(gates), ","); got != "build,vet" {
		t.Fatalf("gates %q", got)
	}
	if len(warns) != 1 || !strings.HasPrefix(warns[0], "invalid checks configuration: ") || !strings.Contains(warns[0], "Bad Name") {
		t.Fatalf("warnings %v", warns)
	}
}

func TestResolveGateSet_NothingConfiguredAndNoGoModule(t *testing.T) {
	dir := t.TempDir()
	for _, cfg := range []*config.Config{nil, {}} {
		gates, warns := resolveGateSet(cfg, dir)
		if got := strings.Join(gateNames(gates), ","); got != "checks" {
			t.Fatalf("gates %q", got)
		}
		if len(warns) != 1 || warns[0] != noChecksWarning {
			t.Fatalf("warnings %v", warns)
		}
	}
	if !strings.Contains(noChecksWarning, `{"name":"test","cmd":["npm","test"]}`) {
		t.Fatalf("warning lacks the example: %s", noChecksWarning)
	}
}

func TestResolveGateSet_GoModuleAutodetectsBuildAndVetWithoutWarning(t *testing.T) {
	dir := t.TempDir()
	gateWrite(t, dir, "go.mod", "module x\n")
	gates, warns := resolveGateSet(nil, dir)
	if got := strings.Join(gateNames(gates), ","); got != "build,vet" || len(warns) != 0 {
		t.Fatalf("gates %q warnings %v", got, warns)
	}
}

func TestExpectedGateNames(t *testing.T) {
	dir := t.TempDir()
	gates, _ := resolveGateSet(cfgChecks(chk("test", 0, "true")), dir)
	if got := strings.Join(expectedGateNames(gates), ","); got != "test,placeholder_scan" {
		t.Fatalf("expected %q", got)
	}
}

func TestAllGatesPassed_PlaceholderOnlyIsNeverOK(t *testing.T) {
	pass := func(string) bool { return true }
	if allGatesPassed([]string{"placeholder_scan"}, pass) {
		t.Fatal("a placeholder-only gate set must not be ok")
	}
	if allGatesPassed(nil, pass) {
		t.Fatal("an empty gate set must not be ok")
	}
	if !allGatesPassed([]string{"test", "placeholder_scan"}, pass) {
		t.Fatal("test + placeholder_scan passing should be ok")
	}
	if allGatesPassed([]string{"test", "placeholder_scan"}, func(n string) bool { return n == "test" }) {
		t.Fatal("a failing placeholder_scan must block")
	}
}

func TestRunQualityGates_ConfiguredChecksRecordNamedRowsInOrder(t *testing.T) {
	s, wt := nonGoMission(t)
	s.cfg = cfgChecks(chk("first", 0, "true"), chk("second", 0, "true"))

	s.runQualityGates("m1")

	var agents, contents []string
	for _, e := range gateEvidence(t, s) {
		agents = append(agents, e.Agent)
		contents = append(contents, e.Content)
	}
	if want := "quality-gate/first,quality-gate/second,quality-gate/placeholder_scan"; strings.Join(agents, ",") != want {
		t.Fatalf("agents %v\n%v", agents, contents)
	}
	if !strings.HasPrefix(contents[0], "first: OK (in "+wt+" [integration] @ ") || !strings.HasPrefix(contents[1], "second: OK (in ") {
		t.Fatalf("contents %q", contents)
	}
	for _, e := range gateEvidence(t, s) {
		if e.Verdict != "pass" {
			t.Errorf("row %q verdict %q", e.Content, e.Verdict)
		}
	}
}

func TestHandoff_ConfiguredChecksOnANonGoProjectAreOK(t *testing.T) {
	s, _ := nonGoMission(t)
	s.cfg = cfgChecks(chk("unit", 0, "true"))
	s.runQualityGates("m1")

	code, h := getHandoff(t, s, "m1")
	if code != http.StatusOK || !h.ChecksOK {
		t.Fatalf("checks_ok=%v (%d) %+v", h.ChecksOK, code, h)
	}
	if strings.Join(h.ExpectedGates, ",") != "unit,placeholder_scan" {
		t.Fatalf("expected_gates %v", h.ExpectedGates)
	}
	if v, cur, ok := h.gate("unit"); !ok || v != "pass" || !cur {
		t.Fatalf("unit gate %q current=%v found=%v", v, cur, ok)
	}
	if hasWarning(h.Warnings, checksHint) {
		t.Fatalf("unexpected no-checks warning: %v", h.Warnings)
	}
}

func TestHandoff_FailingConfiguredCheckRecordsOutputAndBlocksChecksOK(t *testing.T) {
	s, _ := nonGoMission(t)
	s.cfg = cfgChecks(chk("unit", 0, "sh", "-c", `printf '\033[31mred boom\033[0m\n'; exit 3`))
	s.runQualityGates("m1")

	row := findGate(gateEvidence(t, s), "unit FAILED (in ")
	if row == nil || row.Verdict != "fail" || row.Agent != "quality-gate/unit" {
		t.Fatalf("rows %+v", gateEvidence(t, s))
	}
	if !strings.Contains(row.Content, "red boom") || strings.Contains(row.Content, "\x1b") {
		t.Fatalf("output missing or not sanitized: %q", row.Content)
	}
	_, h := getHandoff(t, s, "m1")
	if h.ChecksOK {
		t.Fatal("a failing check must block checks_ok")
	}
	if v, _, _ := h.gate("unit"); v != "fail" {
		t.Fatalf("unit verdict %q", v)
	}
}

func TestRunQualityGates_ConfiguredCheckOutputIsRedactedAndCapped(t *testing.T) {
	s, _ := nonGoMission(t)
	s.cfg = cfgChecks(chk("unit", 0, "sh", "-c", `head -c 40000 /dev/zero | tr '\0' 'a'; echo; echo 'password=hunter2hunter2'; exit 1`))
	s.runQualityGates("m1")
	row := findGate(gateEvidence(t, s), "unit FAILED (in ")
	if row == nil {
		t.Fatalf("rows %+v", gateEvidence(t, s))
	}
	if len(row.Content) > 20000 {
		t.Errorf("recorded output is not capped: %d bytes", len(row.Content))
	}
	if strings.Contains(row.Content, "hunter2hunter2") {
		t.Errorf("secret was recorded: %q", row.Content[len(row.Content)-80:])
	}
}

func TestRunQualityGates_ConfiguredCheckTimeout(t *testing.T) {
	s, _ := nonGoMission(t)
	s.cfg = cfgChecks(chk("slow", 1, "sleep", "5"))
	s.runQualityGates("m1")

	row := findGate(gateEvidence(t, s), "slow TIMED OUT after 1s (in ")
	if row == nil || row.Verdict != "fail" || row.Agent != "quality-gate/slow" {
		t.Fatalf("rows %+v", gateEvidence(t, s))
	}
	_, h := getHandoff(t, s, "m1")
	if h.ChecksOK {
		t.Fatal("a timed out check must block checks_ok")
	}
}

func TestRunQualityGates_MissingBinaryIsAFailureNotAPass(t *testing.T) {
	s, _ := nonGoMission(t)
	s.cfg = cfgChecks(chk("ghost", 0, "definitely-not-a-binary-xyz"))
	s.runQualityGates("m1")
	row := findGate(gateEvidence(t, s), "ghost FAILED (in ")
	if row == nil || row.Verdict != "fail" {
		t.Fatalf("rows %+v", gateEvidence(t, s))
	}
}

func TestHandoff_InvalidChecksConfigurationWarnsAndAutodetects(t *testing.T) {
	s, _, wt := handoffFixture(t) // a Go module
	gateCommit(t, wt, "a.txt")
	s.cfg = cfgChecks(chk("Bad", 0, "true"))
	s.runQualityGates("m1")

	_, h := getHandoff(t, s, "m1")
	if !hasWarning(h.Warnings, "invalid checks configuration: ") {
		t.Fatalf("warnings %v", h.Warnings)
	}
	if !h.ChecksOK || strings.Join(h.ExpectedGates, ",") != "build,vet,placeholder_scan" {
		t.Fatalf("autodetect fallback: ok=%v expected=%v", h.ChecksOK, h.ExpectedGates)
	}
}

func TestHandoff_NothingConfiguredOnANonGoProjectIsNeverOK(t *testing.T) {
	s, wt := nonGoMission(t)
	s.runQualityGates("m1")

	row := findGate(gateEvidence(t, s), "checks: none configured (no go.mod in "+wt+")")
	if row == nil || row.Verdict != "info" || !strings.Contains(row.Content, `add a "checks" list to .stratus.json`) {
		t.Fatalf("rows %+v", gateEvidence(t, s))
	}
	_, h := getHandoff(t, s, "m1")
	if h.ChecksOK {
		t.Fatal("no checks configured must never read as ok")
	}
	if !hasWarning(h.Warnings, noChecksWarning) {
		t.Fatalf("warnings %v", h.Warnings)
	}
	if v, _, ok := h.gate("checks"); !ok || v != "info" {
		t.Fatalf("checks row %q found=%v", v, ok)
	}
}

func TestHandoff_PlaceholderOnlyRowsAreNeverOK(t *testing.T) {
	s, wt := nonGoMission(t)
	s.cfg = cfgChecks(chk("unit", 0, "true"))
	head := gateHead(t, wt)
	if _, err := s.swarm.RecordEvidenceAt("t1", "m1", "gate", "Placeholder scan: clean (in x)", "quality-gate/placeholder_scan", "pass", head); err != nil {
		t.Fatal(err)
	}
	_, h := getHandoff(t, s, "m1")
	if h.ChecksOK {
		t.Fatalf("placeholder-only must not be ok: %+v", h)
	}
}

func TestRunQualityGates_IgnoresChecksFromAWorktreeStratusJSON(t *testing.T) {
	s, wt := nonGoMission(t)
	marker := filepath.Join(t.TempDir(), "ran")
	gateWrite(t, wt, ".stratus.json", `{"checks":[{"name":"evil","cmd":["touch","`+marker+`"]}]}`)
	gateGit(t, wt, "add", ".")
	gateGit(t, wt, "commit", "-q", "-m", "agent adds its own checks")

	s.runQualityGates("m1")

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a command from a worktree .stratus.json was executed")
	}
	if findGate(gateEvidence(t, s), "evil") != nil {
		t.Fatalf("rows %+v", gateEvidence(t, s))
	}
	_, h := getHandoff(t, s, "m1")
	if h.ChecksOK || strings.Join(h.ExpectedGates, ",") != "checks,placeholder_scan" {
		t.Fatalf("ok=%v expected=%v", h.ChecksOK, h.ExpectedGates)
	}
}

func TestHandoff_GoProjectWithoutConfigKeepsTodaysRows(t *testing.T) {
	s, _, wt := handoffFixture(t)
	gateCommit(t, wt, "a.txt")
	s.runQualityGates("m1")

	ev := gateEvidence(t, s)
	build, vet := findGate(ev, "go build: OK (in "+wt+" [integration] @ "), findGate(ev, "go vet: OK (in ")
	if build == nil || vet == nil || build.Agent != "quality-gate/build" || vet.Agent != "quality-gate/vet" {
		t.Fatalf("rows %+v", ev)
	}
	_, h := getHandoff(t, s, "m1")
	if !h.ChecksOK || strings.Join(h.ExpectedGates, ",") != "build,vet,placeholder_scan" {
		t.Fatalf("ok=%v expected=%v", h.ChecksOK, h.ExpectedGates)
	}
}

func TestGateName(t *testing.T) {
	cases := []struct {
		agent, content, want string
	}{
		{"quality-gate", "go build: OK (in x)", "build"},
		{"quality-gate", "go vet FAILED (in x)", "vet"},
		{"quality-gate", "Placeholder scan: clean (in x)", "placeholder_scan"},
		{"quality-gate", "Found 2 placeholder markers in recent changes (in x)", "placeholder_scan"},
		{"quality-gate", "unit: OK (in x)", ""}, // legacy agent never names configured gates
		{"quality-gate/build", "go build: OK (in x)", "build"},
		{"quality-gate/unit", "unit: OK (in x)", "unit"},
		{"quality-gate/placeholder_scan", "Placeholder scan: clean", "placeholder_scan"},
		{"quality-gate/Bad Name", "x", ""},
		{"quality-gate/", "x", ""},
		{"quality-gatex", "go build: OK", ""},
		{"worker-1", "go build: OK", ""},
		{"worker-1", "unit: OK", ""},
	}
	for _, c := range cases {
		if got := gateName(db.SwarmEvidence{Agent: c.agent, Content: c.content}); got != c.want {
			t.Errorf("gateName(%q, %q) = %q, want %q", c.agent, c.content, got, c.want)
		}
	}
}

func TestHandleRecordEvidence_RejectsForgedQualityGateAgents(t *testing.T) {
	s, _ := gateFixture(t)
	for _, agent := range []string{"quality-gate", "quality-gate/unit"} {
		body := `{"type":"gate","content":"unit: OK (in x)","agent":"` + agent + `","verdict":"pass"}`
		if w := postEvidence(t, s, "t1", body); w.Code != http.StatusForbidden {
			t.Errorf("agent %q: status %d %s", agent, w.Code, w.Body)
		}
	}
	if w := postEvidence(t, s, "t1", `{"type":"gate","content":"x","agent":"worker-1","verdict":"pass"}`); w.Code != http.StatusOK {
		t.Errorf("ordinary agent: %d %s", w.Code, w.Body)
	}
}

func TestTeamVerifyAndHandoff_ConfiguredChecksOnANonGoProject(t *testing.T) {
	s, _, team, _ := workspaceFixtureWith(t, false)
	s.cfg = cfgChecks(chk("unit", 0, "true"))
	wsCommit(t, wsMember(t, team, "dev1").WorkspaceDir, "notes.md", "hello\n")
	integrate(t, s, team.ID)

	res := verifyOK(t, s, team.ID)
	checks := res["checks"].(map[string]any)
	if checks["checks_ok"] != true {
		t.Fatalf("verify %v", res)
	}
	if got := strings.Join(strs(checks["expected_gates"]), ","); got != "unit,placeholder_scan" {
		t.Fatalf("verify expected_gates %q", got)
	}
	_, h := wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	hc := h["checks"].(map[string]any)
	if hc["checks_ok"] != true || strings.Join(strs(hc["expected_gates"]), ",") != "unit,placeholder_scan" {
		t.Fatalf("handoff checks %v", hc)
	}
}

func TestTeamVerify_FailingConfiguredCheckIsNotOKAndIsNotCached(t *testing.T) {
	s, _, team, _ := workspaceFixtureWith(t, false)
	s.cfg = cfgChecks(chk("unit", 0, "false"))
	wsCommit(t, wsMember(t, team, "dev1").WorkspaceDir, "notes.md", "hello\n")
	integrate(t, s, team.ID)

	for i := 0; i < 2; i++ {
		res := verifyOK(t, s, team.ID)
		if res["checks"].(map[string]any)["checks_ok"] != false || res["cached"] != false {
			t.Fatalf("run %d: %v", i, res)
		}
	}
}

func TestTeamHandoff_NothingConfiguredWarnsWithTheNewText(t *testing.T) {
	s, _, team, _ := workspaceFixtureWith(t, false)
	wsCommit(t, wsMember(t, team, "dev1").WorkspaceDir, "notes.md", "hello\n")
	integrate(t, s, team.ID)
	_, h := wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	if !strings.Contains(strings.Join(strs(h["warnings"]), "\n"), noChecksWarning) {
		t.Fatalf("warnings %v", h["warnings"])
	}
}

// leftover reports whether a process whose command line matches the pgrep pattern is alive.
func leftover(pattern string) bool {
	return exec.Command("pgrep", "-f", pattern).Run() == nil
}

func waitGone(pattern string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !leftover(pattern) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !leftover(pattern)
}

func TestRunQualityGates_TimeoutKillsTheWholeProcessGroup(t *testing.T) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not available")
	}
	const pattern = "sleep 3133[78]"
	t.Cleanup(func() { _ = exec.Command("pkill", "-f", pattern).Run() })
	s, _ := nonGoMission(t)
	s.cfg = cfgChecks(chk("slow", 1, "sh", "-c", "sleep 31337 & sleep 31338; true"))

	s.runQualityGates("m1")

	if findGate(gateEvidence(t, s), "slow TIMED OUT after 1s (in ") == nil {
		t.Fatalf("rows %+v", gateEvidence(t, s))
	}
	if !waitGone(pattern, 3*time.Second) {
		t.Fatal("the timed out check left its child processes running")
	}
}

func TestRunQualityGates_CheckThatLeavesFilesKeepsTheRevisionAndWarns(t *testing.T) {
	s, wt := nonGoMission(t)
	s.cfg = cfgChecks(chk("unit", 0, "sh", "-c", "echo r > report.xml"))
	head := gateHead(t, wt)

	s.runQualityGates("m1")

	ev := gateEvidence(t, s)
	for _, e := range ev {
		if strings.Contains(e.Content, "revision changed") {
			t.Errorf("a file left in the tree is not a moved revision: %+v", e)
		}
		if gateName(e) != "" && e.Revision != head {
			t.Errorf("row %q revision %q, want the plain sha %q", e.Content, e.Revision, head)
		}
	}
	art := findGate(ev, "check(s) left files in the working tree: report.xml; add them to .gitignore or make the check clean up")
	if art == nil || art.Verdict != "info" || art.Agent != "quality-gate/checks_artifacts" {
		t.Fatalf("rows %+v", ev)
	}
	if _, err := os.Stat(filepath.Join(wt, "report.xml")); err != nil {
		t.Fatalf("the file must never be deleted: %v", err)
	}
	_, h := getHandoff(t, s, "m1")
	if h.ChecksOK || !h.Dirty {
		t.Fatalf("a dirty tree must not be ok: ok=%v dirty=%v", h.ChecksOK, h.Dirty)
	}
	if !hasWarning(h.Warnings, "report.xml; add them to .gitignore") || !hasWarning(h.Warnings, "uncommitted changes") {
		t.Fatalf("warnings %v", h.Warnings)
	}
	if len(h.ExpectedGates) != 2 {
		t.Fatalf("checks_artifacts must not be an expected gate: %v", h.ExpectedGates)
	}

	// Once the tree is clean again the very same rows count.
	if err := os.Remove(filepath.Join(wt, "report.xml")); err != nil {
		t.Fatal(err)
	}
	_, h = getHandoff(t, s, "m1")
	if !h.ChecksOK || hasWarning(h.Warnings, "left files") {
		t.Fatalf("after cleanup: ok=%v warnings=%v", h.ChecksOK, h.Warnings)
	}
}

func TestRunQualityGates_CheckThatMovesHeadIsStillMoved(t *testing.T) {
	s, wt := nonGoMission(t)
	s.cfg = cfgChecks(chk("unit", 0, "git", "commit", "--allow-empty", "-q", "-m", "from a check"))
	before := gateHead(t, wt)

	s.runQualityGates("m1")

	var info bool
	for _, e := range gateEvidence(t, s) {
		if gateName(e) != "" && e.Revision != before+"+moved" {
			t.Errorf("row %q revision %q, want %q", e.Content, e.Revision, before+"+moved")
		}
		if strings.Contains(e.Content, "revision changed while gates ran") {
			info = true
		}
	}
	if !info {
		t.Fatalf("rows %+v", gateEvidence(t, s))
	}
	if _, h := getHandoff(t, s, "m1"); h.ChecksOK {
		t.Fatal("moved rows must not be ok")
	}
}

func TestTeamVerify_CheckThatLeavesFilesKeepsTheRevisionAndWarns(t *testing.T) {
	s, _, team, _ := workspaceFixtureWith(t, false)
	s.cfg = cfgChecks(chk("unit", 0, "sh", "-c", "echo r > report.xml"))
	wsCommit(t, wsMember(t, team, "dev1").WorkspaceDir, "notes.md", "hello\n")
	integrate(t, s, team.ID)

	res := verifyOK(t, s, team.ID)
	checks := res["checks"].(map[string]any)
	if rev, _ := checks["revision"].(string); strings.Contains(rev, "+") {
		t.Fatalf("revision %q must stay the plain sha", rev)
	}
	if checks["checks_ok"] != false {
		t.Fatalf("the tree is dirty now: %v", checks)
	}
	_, h := wsCall(t, s.handleTeamHandoff, "GET", "/api/teams/"+team.ID+"/handoff", team.ID)
	if !strings.Contains(strings.Join(strs(h["warnings"]), "\n"), "check(s) left files in the working tree: report.xml; add them to .gitignore") {
		t.Fatalf("warnings %v", h["warnings"])
	}
}

func TestRunQualityGates_BackgroundProcessHoldingOutputFails(t *testing.T) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not available")
	}
	const pattern = "sleep 3134[0]"
	t.Cleanup(func() { _ = exec.Command("pkill", "-f", pattern).Run() })
	orig := checkWaitDelay
	checkWaitDelay = 200 * time.Millisecond
	t.Cleanup(func() { checkWaitDelay = orig })
	s, _ := nonGoMission(t)
	s.cfg = cfgChecks(chk("bg", 0, "sh", "-c", "sleep 31340 & exit 0"))

	s.runQualityGates("m1")

	row := findGate(gateEvidence(t, s), "bg FAILED: the command left a background process holding its output; make it exit cleanly")
	if row == nil || row.Verdict != "fail" {
		t.Fatalf("rows %+v", gateEvidence(t, s))
	}
	if !waitGone(pattern, 3*time.Second) {
		t.Fatal("the background process was left running")
	}
}

func TestHandleUpdateMissionStatus_SecondVerifyingWhileGatesRunStartsNoSecondRun(t *testing.T) {
	s, _ := gateFixture(t)
	// The test database is ":memory:", which is private to each pooled connection: the
	// gate goroutine and the handler must share the one.
	s.db.SQL().SetMaxOpenConns(1)
	orig := runGateCmd
	t.Cleanup(func() { runGateCmd = orig })
	var builds atomic.Int32
	started, release := make(chan struct{}, 4), make(chan struct{})
	runGateCmd = func(dir, name string, args ...string) (string, error) {
		if name == "go" && len(args) > 0 && args[0] == "build" {
			builds.Add(1)
			started <- struct{}{}
			<-release
		}
		return orig(dir, name, args...)
	}
	put := func() int {
		req := httptest.NewRequest(http.MethodPut, "/api/swarm/missions/m1/status", strings.NewReader(`{"status":"verifying"}`))
		req.SetPathValue("id", "m1")
		w := httptest.NewRecorder()
		s.handleUpdateMissionStatus(w, req)
		return w.Code
	}
	if code := put(); code != http.StatusOK {
		t.Fatalf("first PUT %d", code)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the gates did not start")
	}
	if code := put(); code != http.StatusOK {
		t.Fatalf("second PUT must still succeed, got %d", code)
	}
	time.Sleep(300 * time.Millisecond)
	close(release)
	deadline := time.Now().Add(10 * time.Second)
	for !s.gateRuns.begin("m1") {
		if time.Now().After(deadline) {
			t.Fatal("the first run never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.gateRuns.end("m1")
	if n := builds.Load(); n != 1 {
		t.Fatalf("go build ran %d times, want 1", n)
	}

}

func TestGateRunDeadline_IsTheSumOfTheTimeoutsPlusSlack(t *testing.T) {
	gates := []gateSpec{
		{Name: "a", Kind: gateConfigured, Timeout: 10 * time.Second},
		{Name: "b", Kind: gateConfigured, Timeout: 5 * time.Second},
	}
	if got := gateRunDeadline(gates); got != 15*time.Second+gateRunSlack || gateRunSlack != 30*time.Second {
		t.Fatalf("deadline %v slack %v", got, gateRunSlack)
	}
	if got := gateRunDeadline([]gateSpec{{Name: "build", Kind: gateGoBuild}, {Name: "vet", Kind: gateGoVet}}); got != 240*time.Second+gateRunSlack {
		t.Fatalf("go gates deadline %v", got)
	}
}

func TestRunConfiguredCheck_ExpiredRunDeadlineIsRecordedWithoutRunning(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	o := runConfiguredCheck(ctx, missionRevision{Dir: dir, Kind: dirKindIntegration}, gateSpec{Name: "late", Kind: gateConfigured, Argv: []string{"touch", marker}, Timeout: time.Minute})
	if o.verdict != "fail" || !strings.HasPrefix(o.content, "late TIMED OUT (run deadline) (in ") {
		t.Fatalf("outcome %q %q", o.verdict, o.content)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the command ran after the run deadline")
	}
}

func TestRunConfiguredCheck_RunDeadlineDuringACheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	o := runConfiguredCheck(ctx, missionRevision{Dir: t.TempDir(), Kind: dirKindIntegration}, gateSpec{Name: "slow", Kind: gateConfigured, Argv: []string{"sleep", "5"}, Timeout: time.Minute})
	if o.verdict != "fail" || !strings.HasPrefix(o.content, "slow TIMED OUT (run deadline) (in ") || time.Since(start) > 3*time.Second {
		t.Fatalf("outcome %q %q after %v", o.verdict, o.content, time.Since(start))
	}
}
