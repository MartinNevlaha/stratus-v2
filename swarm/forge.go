package swarm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// MergeResult reports the outcome of merging one worker branch.
type MergeResult struct {
	EntryID       string   `json:"entry_id"`
	BranchName    string   `json:"branch_name"`
	Status        string   `json:"status"` // merged | conflict | failed
	ConflictFiles []string `json:"conflict_files,omitempty"`
}

// ForgeExecutor performs sequential git merges for a mission's forge queue.
type ForgeExecutor struct {
	projectRoot string
}

func newForgeExecutor(projectRoot string) *ForgeExecutor {
	return &ForgeExecutor{projectRoot: projectRoot}
}

// ExecuteQueue merges each pending forge entry sequentially into integrationDir.
// It stashes local changes before each merge and pops them afterward, so
// untracked files from a previous merge don't block the next one.
func (fe *ForgeExecutor) ExecuteQueue(integrationDir string, entries []db.SwarmForgeEntry) []MergeResult {
	var results []MergeResult
	for _, e := range entries {
		if e.Status != ForgePending && e.Status != ForgeMerging {
			continue
		}
		result := fe.mergeBranch(integrationDir, e)
		results = append(results, result)
	}
	return results
}

func (fe *ForgeExecutor) mergeBranch(dir string, e db.SwarmForgeEntry) MergeResult {
	res := MergeResult{EntryID: e.ID, BranchName: e.BranchName}

	// Stash any local changes (including untracked) so they don't block the merge.
	stashed := fe.stash(dir)

	// Attempt the merge.
	cmd := exec.Command("git", "merge", "--no-ff", e.BranchName, "-m", "forge: merge "+e.BranchName)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	mergeErr := cmd.Run()

	if mergeErr == nil {
		res.Status = ForgeMerged
		fe.stashPop(dir, stashed)
		return res
	}

	// Merge failed — collect conflict files then abort.
	conflictFiles := fe.listConflictFiles(dir)
	_ = fe.runGit(dir, "merge", "--abort")
	fe.stashPop(dir, stashed)

	if len(conflictFiles) > 0 {
		res.Status = ForgeConflict
		res.ConflictFiles = conflictFiles
	} else {
		res.Status = ForgeFailed
		log.Printf("forge: merge %s failed: %s", e.BranchName, strings.TrimSpace(out.String()))
	}
	return res
}

// stash runs `git stash --include-untracked`. Returns true if a stash was created.
func (fe *ForgeExecutor) stash(dir string) bool {
	out, err := fe.runGitOutput(dir, "stash", "--include-untracked")
	if err != nil {
		return false
	}
	return !strings.Contains(out, "No local changes to save")
}

// stashPop runs `git stash pop` only if a stash was previously created.
func (fe *ForgeExecutor) stashPop(dir string, stashed bool) {
	if !stashed {
		return
	}
	if err := fe.runGit(dir, "stash", "pop"); err != nil {
		log.Printf("forge: stash pop failed in %s: %v", dir, err)
	}
}

// listConflictFiles returns files with unresolved conflicts in the working tree.
func (fe *ForgeExecutor) listConflictFiles(dir string) []string {
	out, err := fe.runGitOutput(dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// VerifyCommits checks that each branch's HEAD commit is reachable from the
// integration branch HEAD. Returns branches whose commits are NOT present.
func (fe *ForgeExecutor) VerifyCommits(dir string, mergedBranches []string) []string {
	var missing []string
	for _, branch := range mergedBranches {
		// Get HEAD of the worker branch (from the main repo, not the worktree).
		sha, err := fe.runGitOutput(fe.projectRoot, "rev-parse", branch)
		if err != nil {
			missing = append(missing, branch)
			continue
		}
		sha = strings.TrimSpace(sha)
		// Check if that commit is an ancestor of the integration worktree HEAD.
		err = fe.runGit(dir, "merge-base", "--is-ancestor", sha, "HEAD")
		if err != nil {
			missing = append(missing, branch)
		}
	}
	return missing
}

func (fe *ForgeExecutor) runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func (fe *ForgeExecutor) runGitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ExecuteForge creates (or reuses) an integration worktree for the mission's merge
// branch, runs the forge queue, updates DB entries, and verifies commits.
func (s *Store) ExecuteForge(missionID string) ([]MergeResult, []string, error) {
	mission, err := s.db.GetMission(missionID)
	if err != nil {
		return nil, nil, fmt.Errorf("get mission: %w", err)
	}

	entries, err := s.db.ListForgeEntries(missionID)
	if err != nil {
		return nil, nil, fmt.Errorf("list forge entries: %w", err)
	}

	// Filter to pending entries only.
	var pending []db.SwarmForgeEntry
	for _, e := range entries {
		if e.Status == ForgePending || e.Status == ForgeMerging {
			pending = append(pending, e)
		}
	}
	if len(pending) == 0 {
		return nil, nil, nil
	}

	// Ensure the integration worktree exists.
	integrationDir, err := s.ensureIntegrationWorktree(mission)
	if err != nil {
		return nil, nil, fmt.Errorf("integration worktree: %w", err)
	}

	executor := newForgeExecutor(s.worktree.projectRoot)
	results := executor.ExecuteQueue(integrationDir, pending)

	// Persist results to DB.
	var mergedBranches []string
	for _, r := range results {
		conflictJSON := "[]"
		if len(r.ConflictFiles) > 0 {
			b, _ := jsonMarshalCompact(r.ConflictFiles)
			conflictJSON = string(b)
		}
		if dbErr := s.db.UpdateForgeEntry(r.EntryID, r.Status, conflictJSON); dbErr != nil {
			log.Printf("forge: update entry %s: %v", r.EntryID, dbErr)
		}
		if r.Status == ForgeMerged {
			mergedBranches = append(mergedBranches, r.BranchName)
		}
	}

	// Verify all merged commits are reachable.
	var missingCommits []string
	if len(mergedBranches) > 0 {
		missingCommits = executor.VerifyCommits(integrationDir, mergedBranches)
	}

	return results, missingCommits, nil
}

func jsonMarshalCompact(v any) ([]byte, error) {
	return json.Marshal(v)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ensureIntegrationWorktree returns the path to the integration worktree, creating
// it if it doesn't already exist.
func (s *Store) ensureIntegrationWorktree(mission *db.SwarmMission) (string, error) {
	dirName := sanitizeBranchForDir(mission.MergeBranch)
	wtPath := s.worktree.worktreeDir + "/" + dirName

	// Check if the worktree directory already exists.
	if isDir(wtPath) {
		return wtPath, nil
	}

	// Create a new worktree for the integration branch, branching from base.
	// First, ensure the base branch is checked out somewhere reachable.
	cmd := exec.Command("git", "worktree", "add", "-b", mission.MergeBranch, wtPath, mission.BaseBranch)
	cmd.Dir = s.worktree.projectRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		// If the branch already exists (e.g., from a previous partial run), use it.
		cmd2 := exec.Command("git", "worktree", "add", wtPath, mission.MergeBranch)
		cmd2.Dir = s.worktree.projectRoot
		if err2 := cmd2.Run(); err2 != nil {
			return "", fmt.Errorf("create integration worktree: %s: %w", strings.TrimSpace(out.String()), err)
		}
	}
	return wtPath, nil
}
