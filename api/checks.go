package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
)

const (
	placeholderGate = "placeholder_scan"
	// noChecksGate names the info row recorded when nothing can be checked.
	noChecksGate = "checks"

	checkOutputCap = 16 << 10
)

// checkWaitDelay is how long a finished check may keep its output pipes open; a variable so tests can shorten it.
var checkWaitDelay = 3 * time.Second

// noChecksWarning is shown with a handoff whose tree has no automated checks.
const noChecksWarning = `no automated checks configured for this project; add "checks" to .stratus.json (example: {"name":"test","cmd":["npm","test"]})`

type gateKind int

const (
	gateConfigured gateKind = iota // a command from the "checks" config
	gateGoBuild
	gateGoVet
	gateNone // nothing to run: records the "none configured" notice
)

// gateSpec is one gate of a handoff's gate set, other than the placeholder scan.
type gateSpec struct {
	Name    string
	Kind    gateKind
	Argv    []string
	Timeout time.Duration
}

// resolveGateSet decides which gates run in dir. The checks come from the server's loaded
// config (the project root's .stratus.json at startup), never from a file inside dir, which
// agents can edit. An invalid configuration is reported as a warning and replaced by
// autodetection: a go.mod gives build and vet, anything else gives the "none configured" row.
func resolveGateSet(cfg *config.Config, dir string) (gates []gateSpec, warnings []string) {
	if cfg != nil && len(cfg.Checks) > 0 {
		if err := config.ValidateChecks(cfg.Checks); err != nil {
			warnings = append(warnings, "invalid checks configuration: "+strings.TrimPrefix(err.Error(), config.ErrInvalidChecks.Error()+": "))
		} else {
			for _, c := range cfg.Checks {
				timeout := time.Duration(c.TimeoutSec) * time.Second
				if c.TimeoutSec == 0 {
					timeout = config.DefaultCheckTimeoutSec * time.Second
				}
				gates = append(gates, gateSpec{Name: c.Name, Kind: gateConfigured, Argv: c.Cmd, Timeout: timeout})
			}
			return gates, nil
		}
	}
	if !noGoModule(dir) {
		return []gateSpec{{Name: "build", Kind: gateGoBuild}, {Name: "vet", Kind: gateGoVet}}, warnings
	}
	return []gateSpec{{Name: noChecksGate, Kind: gateNone}}, append(warnings, noChecksWarning)
}

// expectedGateNames lists the gate names a handoff expects: the resolved gates and the
// placeholder scan, which runs for every project.
func expectedGateNames(gates []gateSpec) []string {
	names := make([]string, 0, len(gates)+1)
	for _, g := range gates {
		names = append(names, g.Name)
	}
	return append(names, placeholderGate)
}

// allGatesPassed reports whether every expected gate passed and at least one of them is a
// real check: the placeholder scan alone, or the "none configured" row, never counts.
func allGatesPassed(expected []string, passed func(name string) bool) bool {
	real := false
	for _, name := range expected {
		if !passed(name) {
			return false
		}
		if name != placeholderGate {
			real = true
		}
	}
	return real
}

// gateRunSlack is added to the sum of the check timeouts to form the deadline of a whole gate run.
var gateRunSlack = 30 * time.Second

// goGateTimeout is how long runGateCmd lets one built-in command run.
const goGateTimeout = 120 * time.Second

// gateRunDeadline is the time a whole gate run may take: the configured timeouts (the
// built-in Go gates count as runGateCmd's limit each) plus gateRunSlack.
func gateRunDeadline(gates []gateSpec) time.Duration {
	total := gateRunSlack
	for _, g := range gates {
		switch g.Kind {
		case gateConfigured:
			total += g.Timeout
		case gateGoBuild, gateGoVet:
			total += goGateTimeout
		}
	}
	return total
}

// runConfiguredCheck runs one configured check as an argv (no shell) in rev.Dir with the
// check's timeout, or until the run's deadline, whichever is first. A timeout kills the
// whole process group. The output is cut to the last checkOutputCap bytes, stripped of
// terminal escapes and control characters and redacted before it is recorded.
func runConfiguredCheck(run context.Context, rev missionRevision, g gateSpec) gateOutcome {
	where := rev.where()
	event := map[string]any{"gate": g.Name, "passed": false}
	runDeadline := func(out string) gateOutcome {
		content := fmt.Sprintf("%s TIMED OUT (run deadline) (in %s)", g.Name, where)
		if out != "" {
			content += ":\n" + out
		}
		event["timed_out"], event["output"] = true, truncate(out, 500)
		return gateOutcome{name: g.Name, content: content, verdict: "fail", event: event}
	}
	if run.Err() != nil {
		return runDeadline("")
	}
	ctx, cancel := context.WithTimeout(run, g.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.Argv[0], g.Argv[1:]...)
	cmd.Dir = rev.Dir
	cmd.WaitDelay = checkWaitDelay // a child holding the pipes open cannot stall the gate past this
	isolateProcessGroup(cmd)
	tail := &tailBuffer{max: checkOutputCap}
	cmd.Stdout, cmd.Stderr = tail, tail
	err := cmd.Run()

	out := strings.TrimSpace(redactCILog(sanitizeCIText(tail.String())))
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) && run.Err() != nil:
		return runDeadline(out)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		content := fmt.Sprintf("%s TIMED OUT after %ds (in %s)", g.Name, int(g.Timeout/time.Second), where)
		if out != "" {
			content += ":\n" + out
		}
		event["timed_out"], event["output"] = true, truncate(out, 500)
		return gateOutcome{name: g.Name, content: content, verdict: "fail", event: event}
	case errors.Is(err, exec.ErrWaitDelay):
		// The command exited 0 but a background child still holds its output.
		if killErr := killProcessGroup(cmd); killErr != nil {
			log.Printf("warning: check %s: kill leftover process group: %v", g.Name, killErr)
		}
		content := g.Name + " FAILED: the command left a background process holding its output; make it exit cleanly"
		event["output"] = truncate(content, 500)
		return gateOutcome{name: g.Name, content: content, verdict: "fail", event: event}
	case err != nil:
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			out = strings.TrimSpace(out + "\n" + sanitizeCIText(err.Error()))
		}
		event["output"] = truncate(out, 500)
		return gateOutcome{name: g.Name, content: fmt.Sprintf("%s FAILED (in %s):\n%s", g.Name, where, out), verdict: "fail", event: event}
	}
	event["passed"] = true
	return gateOutcome{name: g.Name, content: fmt.Sprintf("%s: OK (in %s)", g.Name, where), verdict: "pass", event: event}
}

// artifactsGate names the info row recorded when the checks left files in the working tree.
const artifactsGate = "checks_artifacts"

// gateRun is a finished set of gates together with what happened to the tree meanwhile.
type gateRun struct {
	Outcomes []gateOutcome
	Recorded string // the revision every row is tied to: the SHA, "+dirty" or "+moved" suffixed
	Moved    string // set when HEAD moved while the gates ran: the text of the info row
	ReadErr  error  // the revision could not be re-read after the run
	// Artifacts is set when the checks left new or changed files in the working tree
	// (HEAD unchanged): the warning text, naming up to maxArtifactPaths of them.
	Artifacts string
}

const maxArtifactPaths = 5

// runObservedGates runs the gates and compares the tree before and after. A moved HEAD
// ties the rows to a "+moved" revision that never matches. Files created by the checks
// leave the revision alone (the rows describe the clean tree they ran on), and are
// reported in Artifacts; the tree then reads as dirty, so checks_ok waits for a cleanup.
// Nothing is ever deleted.
func runObservedGates(rev missionRevision, baseRef string, gates []gateSpec) gateRun {
	before, beforeErr := gitStatusPorcelain(rev.Dir)
	run := gateRun{Outcomes: runCodeGates(rev, baseRef, gates), Recorded: rev.recorded()}
	if rev.SHA == "" {
		return run
	}
	sha, status, err := gitHeadAndStatus(rev.Dir)
	if err != nil {
		run.ReadErr = err
		return run
	}
	if sha != rev.SHA {
		run.Recorded = rev.SHA + "+moved"
		after := missionRevision{Dir: rev.Dir, Kind: rev.Kind, SHA: sha, Dirty: status != "", ShortSHA: shortSHA(sha)}
		run.Moved = fmt.Sprintf("revision changed while gates ran: %s -> %s", rev.where(), after.where())
		return run
	}
	if beforeErr == nil && status != before {
		if paths := newStatusPaths(before, status); len(paths) > 0 {
			run.Artifacts = fmt.Sprintf("check(s) left files in the working tree: %s; add them to .gitignore or make the check clean up", strings.Join(paths, ", "))
		}
	}
	return run
}

// gitStatusPorcelain is `git status --porcelain` without refreshing the index.
func gitStatusPorcelain(dir string) (string, error) {
	out, err := runGit(dir, "--no-optional-locks", "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("read status: %w", err)
	}
	return out, nil
}

func gitHeadAndStatus(dir string) (sha, status string, err error) {
	head, err := runGit(dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("read HEAD: %w", err)
	}
	status, err = gitStatusPorcelain(dir)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(head), status, nil
}

// newStatusPaths lists the paths of the status lines in after that were not in before,
// at most maxArtifactPaths.
func newStatusPaths(before, after string) []string {
	had := map[string]bool{}
	for _, line := range strings.Split(before, "\n") {
		had[line] = true
	}
	var paths []string
	for _, line := range strings.Split(after, "\n") {
		if len(line) > 3 && !had[line] {
			paths = append(paths, line[3:])
		}
	}
	if len(paths) > maxArtifactPaths {
		paths = append(paths[:maxArtifactPaths], "…")
	}
	return paths
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max     int
	buf     []byte
	dropped bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2*t.max {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.max:]...)
		t.dropped = true
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	b := t.buf
	if len(b) > t.max {
		b, t.dropped = b[len(b)-t.max:], true
	}
	if t.dropped {
		return "[output truncated; showing the last 16 KiB]\n" + string(b)
	}
	return string(b)
}
