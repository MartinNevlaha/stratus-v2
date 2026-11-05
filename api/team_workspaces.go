package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/swarm"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

const (
	integrationMemberID   = "integration"
	maxConflictFiles      = 50
	teamIntegrationBudget = 5 * time.Minute
)

// teamIDPattern matches the UUIDs team IDs are generated from.
var teamIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)

// gitWorkspaces implements teams.WorkspaceProvider with git worktrees under
// <project root>/.stratus/worktrees, the same place swarm workers use.
type gitWorkspaces struct {
	mu sync.Mutex // git is not safe against concurrent worktree add/remove in one repository
}

func newGitWorkspaces() *gitWorkspaces { return &gitWorkspaces{} }

func teamBranch(teamID, id string) string { return "team/" + teamID + "/" + id }
func teamWorktreeBase(root string) string { return filepath.Join(root, ".stratus", "worktrees") }
func teamWorktreeDir(root, teamID, id string) string {
	return filepath.Join(teamWorktreeBase(root), "team-"+teamID+"-"+id)
}

// teamGit runs git with a context, keeping stdout apart from stderr. File contents are
// never part of an error: only git's first stderr line is kept.
func teamGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		return stdout.String(), &gitError{cmd: args[0], line: truncate(first, 200), err: err}
	}
	return stdout.String(), nil
}

// gitError is a failed git command; line is the first line of its stderr.
type gitError struct {
	cmd, line string
	err       error
}

func (e *gitError) Error() string { return fmt.Sprintf("git %s: %v: %s", e.cmd, e.err, e.line) }
func (e *gitError) Unwrap() error { return e.err }

var absPathToken = regexp.MustCompile(`(^|[\s'"(=])/[^\s'"():]+`)

// publicReason is git's first stderr line without absolute paths, safe for an HTTP body.
func publicReason(err error) string {
	var ge *gitError
	if !errors.As(err, &ge) || ge.line == "" {
		return "git reported an error; see the server log"
	}
	return absPathToken.ReplaceAllString(ge.line, "$1<path>")
}

// gitTrue runs a git command whose exit status is the answer (exit 1 means no).
func gitTrue(ctx context.Context, dir string, args ...string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	}
	return false, fmt.Errorf("git %s: %w", args[0], err)
}

func branchExists(ctx context.Context, root, branch string) (bool, error) {
	return gitTrue(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
}

func isAncestor(ctx context.Context, dir, ancestor, of string) (bool, error) {
	return gitTrue(ctx, dir, "merge-base", "--is-ancestor", "--end-of-options", ancestor, of)
}

// checkedWorktreeBase makes sure <root>/.stratus/worktrees exists and stays inside the
// project: neither .stratus nor worktrees may be a symlink.
func checkedWorktreeBase(root string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	base := teamWorktreeBase(root)
	for _, p := range []string{filepath.Join(root, ".stratus"), base} {
		if info, e := os.Lstat(p); e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s must not be a symlink", p)
		}
	}
	if err = os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("create worktree directory: %w", err)
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve worktree directory: %w", err)
	}
	if !strings.HasPrefix(resolvedBase, resolvedRoot+string(filepath.Separator)) {
		return "", errors.New("worktree directory resolves outside the project")
	}
	return resolvedBase, nil
}

// insideWorktreeBase reports whether dir (which must exist) resolves under resolvedBase.
func insideWorktreeBase(resolvedBase, dir string) bool {
	resolved, err := filepath.EvalSymlinks(dir)
	return err == nil && strings.HasPrefix(resolved, resolvedBase+string(filepath.Separator))
}

func validWorkspaceNames(teamID, id string) error {
	if !teamIDPattern.MatchString(teamID) {
		return fmt.Errorf("invalid team id %q", teamID)
	}
	if id != integrationMemberID && !teams.ValidWorkspaceID(id) {
		return fmt.Errorf("invalid member id %q", id)
	}
	return nil
}

// Create adds a worktree on a new branch from the project's current HEAD.
func (g *gitWorkspaces) Create(ctx context.Context, root, teamID, memberID string) (string, string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if memberID == integrationMemberID {
		return "", "", errors.New("create workspace: member id is reserved")
	}
	if err := validWorkspaceNames(teamID, memberID); err != nil {
		return "", "", fmt.Errorf("create workspace: %w", err)
	}
	return g.addWorktree(ctx, root, teamID, memberID)
}

// addWorktree creates the branch and its worktree; on failure it removes only what it created.
func (g *gitWorkspaces) addWorktree(ctx context.Context, root, teamID, id string) (string, string, error) {
	base, err := checkedWorktreeBase(root)
	if err != nil {
		return "", "", err
	}
	branch, dir := teamBranch(teamID, id), teamWorktreeDir(root, teamID, id)
	if exists, e := branchExists(ctx, root, branch); e != nil {
		return "", "", e
	} else if exists {
		return "", "", fmt.Errorf("branch %s already exists", branch)
	}
	if _, e := os.Lstat(dir); e == nil {
		return "", "", fmt.Errorf("worktree directory for %s already exists", id)
	}
	created, err := swarm.NewWorktreeManager(root).Create(branch)
	if err != nil {
		g.removeOne(context.WithoutCancel(ctx), root, dir, branch)
		return "", "", err
	}
	if created != dir || !insideWorktreeBase(base, created) {
		g.removeOne(context.WithoutCancel(ctx), root, created, branch)
		return "", "", errors.New("worktree was created outside the expected directory")
	}
	return created, branch, nil
}

// removeOne deletes a worktree directory and its branch, tolerating either being gone.
func (g *gitWorkspaces) removeOne(ctx context.Context, root, dir, branch string) error {
	var errs []error
	if _, err := os.Lstat(dir); err == nil {
		if _, err = teamGit(ctx, root, nil, "worktree", "remove", "--force", "--", dir); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := teamGit(ctx, root, nil, "worktree", "prune"); err != nil {
		errs = append(errs, err)
	}
	if exists, err := branchExists(ctx, root, branch); err != nil {
		errs = append(errs, err)
	} else if exists {
		if _, err = teamGit(ctx, root, nil, "branch", "-D", "--", branch); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// identityEnv supplies a deterministic committer only when git has none configured,
// so merge commits never fail on a machine without user.name/user.email.
func identityEnv(ctx context.Context, dir string) []string {
	if _, err := teamGit(ctx, dir, nil, "var", "GIT_COMMITTER_IDENT"); err == nil {
		return nil
	}
	return []string{"GIT_AUTHOR_NAME=Stratus", "GIT_AUTHOR_EMAIL=stratus@localhost", "GIT_COMMITTER_NAME=Stratus", "GIT_COMMITTER_EMAIL=stratus@localhost"}
}

// ensureIntegration returns the integration worktree, creating it from the project
// HEAD on first use. base is the commit it started from.
func (g *gitWorkspaces) ensureIntegration(ctx context.Context, team teams.Team) (dir, branch, base string, err error) {
	root := team.ProjectRoot
	if err = validWorkspaceNames(team.ID, integrationMemberID); err != nil {
		return "", "", "", err
	}
	dir, branch = teamWorktreeDir(root, team.ID, integrationMemberID), teamBranch(team.ID, integrationMemberID)
	if team.IntegrationDir != "" && team.IntegrationDir != dir {
		return "", "", "", errors.New("recorded integration directory does not match the team")
	}
	resolvedBase, err := checkedWorktreeBase(root)
	if err != nil {
		return "", "", "", err
	}
	if _, e := os.Lstat(dir); e == nil {
		if !insideWorktreeBase(resolvedBase, dir) {
			return "", "", "", errors.New("integration directory resolves outside the project")
		}
		head, e := teamGit(ctx, dir, nil, "rev-parse", "--abbrev-ref", "HEAD")
		if e != nil || strings.TrimSpace(head) != branch {
			return "", "", "", fmt.Errorf("integration worktree is not on %s", branch)
		}
		return dir, branch, team.IntegrationBase, nil
	}
	head, err := teamGit(ctx, root, nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", "", "", fmt.Errorf("read project HEAD: %w", err)
	}
	base = strings.TrimSpace(head)
	if exists, e := branchExists(ctx, root, branch); e != nil {
		return "", "", "", e
	} else if exists {
		// The worktree directory was deleted but the branch kept its merges: check it out again.
		if _, err = teamGit(ctx, root, nil, "worktree", "prune"); err != nil {
			return "", "", "", err
		}
		if _, err = teamGit(ctx, root, nil, "worktree", "add", "--", dir, branch); err != nil {
			return "", "", "", err
		}
		if team.IntegrationBase != "" {
			base = team.IntegrationBase
		}
		return dir, branch, base, nil
	}
	if _, _, err = g.addWorktree(ctx, root, team.ID, integrationMemberID); err != nil {
		return "", "", "", fmt.Errorf("create integration worktree: %w", err)
	}
	return dir, branch, base, nil
}

// Integrate merges each member branch that is ahead of the integration branch.
func (g *gitWorkspaces) Integrate(ctx context.Context, team teams.Team) (teams.IntegrationResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, teamIntegrationBudget)
	defer cancel()
	res := teams.IntegrationResult{Merged: []string{}, Conflicts: []teams.IntegrationConflict{}, Failed: []teams.IntegrationConflict{}, NothingToMerge: []string{}, Warnings: []string{}}
	dir, branch, base, err := g.ensureIntegration(ctx, team)
	if err != nil {
		return res, err
	}
	res.Dir, res.Branch, res.Base = dir, branch, base
	sha, dirty, err := gitRevision(dir)
	if err != nil {
		return res, fmt.Errorf("read integration revision: %w", err)
	}
	if dirty {
		res.Revision, res.Dirty = sha, true
		res.Warnings = append(res.Warnings, "the integration worktree has uncommitted changes; nothing was merged")
		return res, nil
	}
	env := identityEnv(ctx, dir)
	resolvedBase, err := checkedWorktreeBase(team.ProjectRoot)
	if err != nil {
		return res, err
	}
	for _, member := range team.Members {
		if member.WorkspaceBranch == "" {
			continue
		}
		if err = validWorkspaceNames(team.ID, member.ID); err != nil || member.WorkspaceBranch != teamBranch(team.ID, member.ID) {
			return res, fmt.Errorf("member %s has an invalid workspace record", member.ID)
		}
		if memberDir := teamWorktreeDir(team.ProjectRoot, team.ID, member.ID); insideWorktreeBase(resolvedBase, memberDir) {
			if status, e := runGit(memberDir, "--no-optional-locks", "status", "--porcelain"); e != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("member %s: cannot read workspace status: %v", member.ID, e))
			} else if strings.TrimSpace(status) != "" {
				res.Warnings = append(res.Warnings, fmt.Sprintf("member %s has uncommitted changes in its workspace; they are not merged until committed", member.ID))
			}
		} else {
			res.Warnings = append(res.Warnings, fmt.Sprintf("member %s: workspace directory is missing", member.ID))
		}
		ahead, e := teamGit(ctx, dir, nil, "rev-list", "--count", "--end-of-options", branch+".."+member.WorkspaceBranch)
		if e != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("member %s: cannot compare with the integration branch: %v", member.ID, e))
			continue
		}
		if strings.TrimSpace(ahead) == "0" {
			res.NothingToMerge = append(res.NothingToMerge, member.WorkspaceBranch)
			continue
		}
		if entry, conflict, e := g.mergeBranch(ctx, dir, env, member); e != nil {
			return res, e
		} else if entry != nil && conflict {
			res.Conflicts = append(res.Conflicts, *entry)
		} else if entry != nil {
			res.Failed = append(res.Failed, *entry)
			res.Warnings = append(res.Warnings, fmt.Sprintf("member %s: merge failed without conflicts: %s", member.ID, entry.Reason))
		} else {
			res.Merged = append(res.Merged, member.WorkspaceBranch)
		}
	}
	if res.Revision, res.Dirty, err = gitRevision(dir); err != nil {
		return res, fmt.Errorf("read integration revision: %w", err)
	}
	return res, nil
}

// mergeBranch merges one member branch. A merge that stops on unmerged files is a
// conflict. Any other failure (a failing hook, untracked files in the way) is returned
// with a reason and is not a conflict. Either way a half-done merge is aborted and the
// tree must be back to what it was.
func (g *gitWorkspaces) mergeBranch(ctx context.Context, dir string, env []string, member teams.Member) (entry *teams.IntegrationConflict, conflict bool, err error) {
	before, err := runGit(dir, "--no-optional-locks", "status", "--porcelain")
	if err != nil {
		return nil, false, fmt.Errorf("read status before merging %s: %w", member.ID, err)
	}
	_, mergeErr := teamGit(ctx, dir, env, "merge", "--no-ff", "--no-edit", "-m", "team: merge "+member.WorkspaceBranch, "--end-of-options", member.WorkspaceBranch)
	if mergeErr == nil {
		return nil, false, nil
	}
	if ctx.Err() != nil {
		return nil, false, fmt.Errorf("merge %s: %w", member.ID, ctx.Err())
	}
	entry = &teams.IntegrationConflict{Member: member.ID, Branch: member.WorkspaceBranch, Files: []string{}}
	if out, e := teamGit(ctx, dir, nil, "diff", "--name-only", "--diff-filter=U", "-z"); e == nil {
		for _, f := range strings.Split(out, "\x00") {
			if f != "" && len(entry.Files) < maxConflictFiles {
				entry.Files = append(entry.Files, f)
			}
		}
		sort.Strings(entry.Files)
	}
	conflict = len(entry.Files) > 0
	if !conflict {
		entry.Reason = publicReason(mergeErr)
		log.Printf("team workspaces: merge of %s failed without conflicts: %v", member.WorkspaceBranch, mergeErr)
	}
	if inProgress, e := gitTrue(ctx, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD"); e != nil {
		return nil, false, fmt.Errorf("check merge state of %s: %w", member.ID, e)
	} else if inProgress {
		if _, e = teamGit(ctx, dir, nil, "merge", "--abort"); e != nil {
			return nil, false, fmt.Errorf("abort merge of %s: %w", member.ID, e)
		}
	}
	if after, e := runGit(dir, "--no-optional-locks", "status", "--porcelain"); e != nil || after != before {
		return nil, false, fmt.Errorf("integration worktree is not back to its state before merging %s: %w", member.ID, errors.Join(e, mergeErr))
	}
	return entry, conflict, nil
}

// workspaceTarget is one worktree and branch to remove.
type workspaceTarget struct {
	name, dir, branch string
	integration       bool
}

// Remove deletes the member and integration worktrees and their branches. Without
// force nothing is removed unless every worktree is clean and its commits are already
// part of the project's HEAD (or, for a member, of the integration branch).
func (g *gitWorkspaces) Remove(ctx context.Context, team teams.Team, force bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, teamIntegrationBudget)
	defer cancel()
	root := team.ProjectRoot
	resolvedBase, err := checkedWorktreeBase(root)
	if err != nil {
		return err
	}
	var targets []workspaceTarget
	for _, member := range team.Members {
		if member.WorkspaceBranch == "" {
			continue
		}
		if err = validWorkspaceNames(team.ID, member.ID); err != nil || member.WorkspaceBranch != teamBranch(team.ID, member.ID) {
			return fmt.Errorf("member %s has an invalid workspace record", member.ID)
		}
		targets = append(targets, workspaceTarget{name: member.ID, dir: teamWorktreeDir(root, team.ID, member.ID), branch: member.WorkspaceBranch})
	}
	integrationBranch := teamBranch(team.ID, integrationMemberID)
	integrationExists, err := branchExists(ctx, root, integrationBranch)
	if err != nil {
		return err
	}
	if integrationExists || team.IntegrationDir != "" {
		targets = append(targets, workspaceTarget{name: "integration", dir: teamWorktreeDir(root, team.ID, integrationMemberID), branch: integrationBranch, integration: true})
	}
	if !force {
		var blockers []string
		for _, tg := range targets {
			blockers = append(blockers, g.blockers(ctx, root, tg, integrationExists, integrationBranch)...)
		}
		if len(blockers) > 0 {
			return &teams.BlockedError{Blockers: blockers}
		}
	}
	var errs []error
	for _, tg := range targets {
		if _, e := os.Lstat(tg.dir); e == nil && !insideWorktreeBase(resolvedBase, tg.dir) {
			errs = append(errs, fmt.Errorf("%s: directory resolves outside the project; not removed", tg.name))
			continue
		}
		if e := g.removeOne(ctx, root, tg.dir, tg.branch); e != nil {
			errs = append(errs, fmt.Errorf("%s: %w", tg.name, e))
		}
	}
	return errors.Join(errs...)
}

// blockers lists why a worktree cannot be discarded without force.
func (g *gitWorkspaces) blockers(ctx context.Context, root string, tg workspaceTarget, integrationExists bool, integrationBranch string) []string {
	var out []string
	if _, err := os.Lstat(tg.dir); err == nil {
		status, e := runGit(tg.dir, "--no-optional-locks", "status", "--porcelain")
		if e != nil {
			out = append(out, fmt.Sprintf("%s: cannot read workspace status", tg.name))
		} else if strings.TrimSpace(status) != "" {
			out = append(out, fmt.Sprintf("%s has uncommitted changes", tg.name))
		}
	}
	exists, err := branchExists(ctx, root, tg.branch)
	if err != nil || !exists {
		return out
	}
	inProject, err := isAncestor(ctx, root, tg.branch, "HEAD")
	if err != nil {
		return append(out, fmt.Sprintf("%s: cannot compare branch with the project HEAD", tg.name))
	}
	if inProject {
		return out
	}
	if !tg.integration && integrationExists {
		if inIntegration, e := isAncestor(ctx, root, tg.branch, integrationBranch); e == nil && inIntegration {
			return out
		}
	}
	if tg.integration {
		return append(out, "the integration branch has commits that are not in the project's HEAD")
	}
	return append(out, fmt.Sprintf("%s has commits that are not merged", tg.name))
}

// runCodeGates runs the given gates and the placeholder scan in rev.Dir, the checks a swarm
// mission and an isolated team both run on their integration tree. The placeholder scan
// looks at the lines changed since baseRef.
func runCodeGates(rev missionRevision, baseRef string, gates []gateSpec) []gateOutcome {
	dir, where := rev.Dir, rev.where()
	var outcomes []gateOutcome
	run, cancel := context.WithTimeout(context.Background(), gateRunDeadline(gates))
	defer cancel()

	for _, g := range gates {
		switch g.Kind {
		case gateNone:
			// Nothing to run: report that nothing was checked rather than a failure or a pass.
			content := fmt.Sprintf("checks: none configured (no go.mod in %s); add a \"checks\" list to .stratus.json", dir)
			outcomes = append(outcomes, gateOutcome{name: g.Name, content: content, verdict: "info",
				event: map[string]any{"gate": g.Name, "skipped": true, "output": truncate(content, 500)}})
		case gateGoBuild:
			// -o /dev/null: a lone main package would otherwise leave its binary in the tree, dirtying it.
			if out, err := runGateCmd(dir, "go", "build", "-o", "/dev/null", "./..."); err != nil {
				outcomes = append(outcomes, gateOutcome{name: g.Name, content: fmt.Sprintf("go build FAILED (in %s):\n%s", where, out), verdict: "fail",
					event: map[string]any{"gate": g.Name, "passed": false, "output": truncate(out, 500)}})
			} else {
				outcomes = append(outcomes, gateOutcome{name: g.Name, content: "go build: OK (in " + where + ")", verdict: "pass",
					event: map[string]any{"gate": g.Name, "passed": true}})
			}
		case gateGoVet:
			if out, err := runGateCmd(dir, "go", "vet", "./..."); err != nil {
				outcomes = append(outcomes, gateOutcome{name: g.Name, content: fmt.Sprintf("go vet FAILED (in %s):\n%s", where, out), verdict: "fail",
					event: map[string]any{"gate": g.Name, "passed": false, "output": truncate(out, 500)}})
			} else {
				outcomes = append(outcomes, gateOutcome{name: g.Name, content: "go vet: OK (in " + where + ")", verdict: "pass",
					event: map[string]any{"gate": g.Name, "passed": true}})
			}
		default:
			outcomes = append(outcomes, runConfiguredCheck(run, rev, g))
		}
	}

	// Placeholder scan (TODO/FIXME/PLACEHOLDER in lines changed since the base)
	if o := runPlaceholderGate(rev, baseRef); o != nil {
		outcomes = append(outcomes, *o)
	}
	return outcomes
}

// noGoModule reports whether dir holds no Go module, so go build and go vet cannot apply.
func noGoModule(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return errors.Is(err, os.ErrNotExist)
}
