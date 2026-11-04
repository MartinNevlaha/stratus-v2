// Package projectidentity derives a local project identity without using display
// names or remote URLs. Linked Git worktrees share an identity only when Git's
// own worktree inventory verifies the current checkout.
package projectidentity

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Resolve returns a stable ID and the canonical actual project root. The ID may
// be shared by linked worktrees; canonical always retains this working directory.
func Resolve(root string) (id, canonical string, err error) {
	if strings.TrimSpace(root) == "" {
		return "", "", fmt.Errorf("project root is required")
	}
	canonical, err = filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	canonical, err = filepath.EvalSymlinks(canonical)
	if err != nil {
		return "", "", err
	}
	st, err := os.Stat(canonical)
	if err != nil {
		return "", "", err
	}
	if !st.IsDir() {
		return "", "", fmt.Errorf("project root is not a directory")
	}
	original := canonical
	top := git(original, "rev-parse", "--show-toplevel")
	if top != "" {
		top, _ = filepath.EvalSymlinks(top)
	}
	found := false
	for p := canonical; ; p = filepath.Dir(p) {
		if st, e := os.Stat(filepath.Join(p, ".stratus.json")); e == nil && st.Mode().IsRegular() {
			canonical = p
			found = true
			break
		}
		if filepath.Dir(p) == p || p == top {
			break
		}
	}
	if !found && top != "" {
		canonical = top
	}
	identityRoot := canonical
	if top != "" {
		relative, e := filepath.Rel(top, canonical)
		if e == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			// The first entry is Git's main worktree, not a name inferred from .git.
			inventory := git(original, "worktree", "list", "--porcelain", "-z")
			main, verified := "", false
			for _, field := range strings.Split(inventory, "\x00") {
				if !strings.HasPrefix(field, "worktree ") {
					continue
				}
				p, e := filepath.EvalSymlinks(strings.TrimPrefix(field, "worktree "))
				if e != nil {
					continue
				}
				if main == "" {
					main = p
				}
				if p == top {
					verified = true
				}
			}
			if verified && main != "" {
				identityRoot = filepath.Join(main, relative)
			}
		}
	}
	sum := sha256.Sum256([]byte(identityRoot))
	return fmt.Sprintf("project-%x", sum[:16]), canonical, nil
}

func git(root string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0")
	b, e := cmd.Output()
	if e != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\r\n")
}
