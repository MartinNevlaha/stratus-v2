package codex

import (
	"fmt"
	"path/filepath"
	"strings"
)

func PatchPaths(command string) ([]string, error) {
	lines := strings.Split(strings.TrimSpace(command), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "*** Begin Patch" || strings.TrimSpace(lines[len(lines)-1]) != "*** End Patch" {
		return nil, fmt.Errorf("invalid apply_patch envelope")
	}
	var paths []string
	seen := map[string]bool{}
	for _, line := range lines {
		for _, prefix := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "} {
			if strings.HasPrefix(line, prefix) {
				p := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				clean := filepath.Clean(p)
				if p == "" || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
					return nil, fmt.Errorf("invalid patch path")
				}
				if !seen[clean] {
					paths = append(paths, clean)
					seen[clean] = true
				}
			}
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("patch has no file operations")
	}
	return paths, nil
}
func ProjectPath(root, path string) (string, bool) {
	if root == "" {
		if filepath.IsAbs(path) {
			return "", false
		}
		return filepath.ToSlash(filepath.Clean(path)), path != ".." && !strings.HasPrefix(filepath.Clean(path), "../")
	}
	root, e := filepath.Abs(root)
	if e != nil {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// Resolve against the operation CWD before expressing a path relative to the project.
func operationProjectPath(root, cwd, path string) (string, bool) {
	if cwd == "" {
		cwd = root
	}
	if !filepath.IsAbs(path) && cwd != "" {
		path = filepath.Join(cwd, path)
	}
	return RepositoryPath(root, path)
}
