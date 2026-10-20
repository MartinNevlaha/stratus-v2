package codex

import (
	"os"
	"path/filepath"
	"strings"
)

// canonicalPath resolves existing ancestors, including for newly created files.
func canonicalPath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) || filepath.Dir(path) == path {
			return "", err
		}
		tail = append(tail, filepath.Base(path))
		path = filepath.Dir(path)
	}
}

// gitIdentity reads Git's own worktree registration rather than trusting a prefix.
func gitIdentity(path string) (root, common string, linked bool) {
	p, err := canonicalPath(path)
	if err != nil {
		return
	}
	for {
		marker := filepath.Join(p, ".git")
		if st, err := os.Stat(marker); err == nil {
			gitdir := marker
			if !st.IsDir() {
				b, err := os.ReadFile(marker)
				if err != nil || !strings.HasPrefix(string(b), "gitdir: ") {
					return "", "", false
				}
				gitdir = strings.TrimSpace(strings.TrimPrefix(string(b), "gitdir: "))
				if !filepath.IsAbs(gitdir) {
					gitdir = filepath.Join(p, gitdir)
				}
				// The registered gitdir must point back to this exact worktree.
				back, err := os.ReadFile(filepath.Join(gitdir, "gitdir"))
				if err != nil {
					return "", "", false
				}
				backPath, err := canonicalPath(strings.TrimSpace(string(back)))
				if err != nil || backPath != marker {
					return "", "", false
				}
				linked = true
			}
			common = gitdir
			if b, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
				common = strings.TrimSpace(string(b))
				if !filepath.IsAbs(common) {
					common = filepath.Join(gitdir, common)
				}
			}
			common, err = canonicalPath(common)
			if err != nil {
				return "", "", false
			}
			if linked {
				registered, err := canonicalPath(gitdir)
				if err != nil || filepath.Dir(registered) != filepath.Join(common, "worktrees") {
					return "", "", false
				}
			}
			return p, common, linked
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", "", false
		}
		p = parent
	}
}

func linkedWorktree(project, path string) (string, bool) {
	if project == "" || path == "" {
		return "", false
	}
	_, common, _ := gitIdentity(project)
	root, other, linked := gitIdentity(path)
	return root, linked && common != "" && common == other
}

// RepositoryPath returns project-relative files or absolute files in a verified
// linked worktree of the same repository. Foreign paths and symlink escapes fail.
func RepositoryPath(root, path string) (string, bool) {
	if root == "" {
		return ProjectPath(root, path)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	canonicalRoot, err := canonicalPath(root)
	if err != nil {
		return "", false
	}
	canonical, err := canonicalPath(path)
	if err != nil {
		return "", false
	}
	if rel, ok := ProjectPath(canonicalRoot, canonical); ok {
		return rel, true
	}
	if _, ok := linkedWorktree(canonicalRoot, canonical); ok {
		return canonical, true
	}
	return "", false
}

// linkedProjectConfig finds an ignored main-checkout config only after verifying
// the caller's reverse Git registration. No arbitrary sibling config is adopted.
func linkedProjectConfig(cwd string) (string, bool) {
	worktree, common, linked := gitIdentity(cwd)
	if !linked || filepath.Base(common) != ".git" {
		return "", false
	}
	main := filepath.Dir(common)
	root, mainCommon, mainLinked := gitIdentity(main)
	if mainLinked || root != main || mainCommon != common {
		return "", false
	}
	canonical, err := canonicalPath(cwd)
	if err != nil {
		return "", false
	}
	rel, ok := ProjectPath(worktree, canonical)
	if !ok {
		return "", false
	}
	dir := filepath.Join(main, filepath.FromSlash(rel))
	for {
		if st, err := os.Stat(filepath.Join(dir, ".stratus.json")); err == nil && !st.IsDir() {
			return dir, true
		}
		if dir == main {
			break
		}
		dir = filepath.Dir(dir)
	}
	return "", false
}
