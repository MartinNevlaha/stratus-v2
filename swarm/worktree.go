package swarm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeManager handles git worktree creation and removal.
type WorktreeManager struct {
	projectRoot string
	worktreeDir string
}

// NewWorktreeManager creates a worktree manager.
// Worktrees are stored in <projectRoot>/.stratus/worktrees/.
func NewWorktreeManager(projectRoot string) *WorktreeManager {
	return &WorktreeManager{
		projectRoot: projectRoot,
		worktreeDir: filepath.Join(projectRoot, ".stratus", "worktrees"),
	}
}

// Create creates a new git worktree with a dedicated branch.
// The branch is created from HEAD.
// Returns the absolute worktree path.
func (wm *WorktreeManager) Create(branch string) (string, error) {
	dirName := sanitizeBranchForDir(branch)
	wtPath := filepath.Join(wm.worktreeDir, dirName)

	if err := os.MkdirAll(wm.worktreeDir, 0o755); err != nil {
		return "", fmt.Errorf("create worktree dir: %w", err)
	}

	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath)
	cmd.Dir = wm.projectRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return wtPath, nil
}

// Remove removes a worktree and optionally deletes the branch.
func (wm *WorktreeManager) Remove(wtPath, branch string) error {
	var errs []string

	// Remove worktree
	cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
	cmd.Dir = wm.projectRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		errs = append(errs, fmt.Sprintf("worktree remove: %s: %v", strings.TrimSpace(string(out)), err))
	}

	// Delete branch
	if branch != "" {
		cmd = exec.Command("git", "branch", "-D", branch)
		cmd.Dir = wm.projectRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("branch delete: %s: %v", strings.TrimSpace(string(out)), err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("worktree cleanup: %s", strings.Join(errs, "; "))
	}
	return nil
}

// List returns all active worktree paths.
func (wm *WorktreeManager) List() ([]string, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = wm.projectRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if after, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, after)
		}
	}
	return paths, nil
}

// WorktreeDir returns the base directory for all worktrees.
func (wm *WorktreeManager) WorktreeDir() string {
	return wm.worktreeDir
}

// sanitizeBranchForDir converts a branch name to a safe directory name.
func sanitizeBranchForDir(branch string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "..", "-")
	result := replacer.Replace(branch)
	// Trim leading/trailing dashes and dots
	return strings.Trim(result, "-.")
}
