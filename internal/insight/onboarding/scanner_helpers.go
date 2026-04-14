package onboarding

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// isSecretFile returns true if the path looks like a secret that should not be read.
func isSecretFile(name string) bool {
	base := filepath.Base(name)
	lower := strings.ToLower(base)

	// Exact matches (case-insensitive)
	exactSecrets := map[string]bool{
		".env":       true,
		"id_rsa":     true,
		"id_ed25519": true,
	}
	if exactSecrets[lower] {
		return true
	}

	// .env.* pattern
	if strings.HasPrefix(lower, ".env.") {
		return true
	}

	// credentials* and secrets*
	if strings.HasPrefix(lower, "credentials") || strings.HasPrefix(lower, "secrets") {
		return true
	}

	// secret file extensions
	secretExts := map[string]bool{
		".pem":    true,
		".key":    true,
		".p12":    true,
		".pfx":    true,
		".secret": true,
	}
	ext := strings.ToLower(filepath.Ext(base))
	return secretExts[ext]
}

// depthLimit returns the walk depth for the given depth string.
func depthLimit(depth string) int {
	switch depth {
	case "shallow":
		return 3
	case "deep", "auto":
		return 5
	default: // "standard"
		return 4
	}
}

// detectEntryPoints appends detected entry points to the slice based on file path.
func detectEntryPoints(eps *[]EntryPoint, rel string, parts []string, base string) {
	switch {
	case base == "main.go" && len(parts) == 1:
		*eps = append(*eps, EntryPoint{
			Path:        rel,
			Type:        "main",
			Description: "Go main package entry point",
		})
	case base == "main.go" && len(parts) >= 3 && parts[0] == "cmd":
		*eps = append(*eps, EntryPoint{
			Path:        rel,
			Type:        "cli",
			Description: fmt.Sprintf("Go CLI command: %s", parts[1]),
		})
	case (base == "index.ts" || base == "index.js") && len(parts) == 1:
		*eps = append(*eps, EntryPoint{
			Path:        rel,
			Type:        "index",
			Description: "Node.js entry point",
		})
	case len(parts) == 2 && parts[0] == "src" && (strings.HasPrefix(base, "index.")):
		*eps = append(*eps, EntryPoint{
			Path:        rel,
			Type:        "index",
			Description: "Source entry point",
		})
	case base == "main.py":
		*eps = append(*eps, EntryPoint{
			Path:        rel,
			Type:        "main",
			Description: "Python main module",
		})
	case base == "app.py":
		*eps = append(*eps, EntryPoint{
			Path:        rel,
			Type:        "server",
			Description: "Python app module",
		})
	case base == "manage.py":
		*eps = append(*eps, EntryPoint{
			Path:        rel,
			Type:        "cli",
			Description: "Django management script",
		})
	}
}

// countLines counts the number of lines in a file.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("count lines: open: %w", err)
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("count lines: scan: %w", err)
	}
	return count, nil
}

// readCapped reads up to maxBytes from a file and returns the content as a string.
func readCapped(path string, maxBytes int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read capped: open: %w", err)
	}
	defer f.Close()

	buf := make([]byte, maxBytes)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", fmt.Errorf("read capped: read: %w", err)
	}
	return string(buf[:n]), nil
}

// buildTree produces an indented directory tree string, limited to maxDepth levels below root.
func buildTree(root string, maxDepth int) string {
	var sb strings.Builder
	buildTreeLevel(&sb, root, 0, maxDepth)
	return sb.String()
}

// buildTreeLevel recursively writes directory entries up to maxDepth.
// currentDepth is 0-indexed: depth 0 = immediate children of root.
// We only print and recurse when currentDepth < maxDepth, so maxDepth=3 shows
// exactly 3 levels of nesting (l1, l2, l3 — not l4).
func buildTreeLevel(sb *strings.Builder, dir string, currentDepth, maxDepth int) {
	if currentDepth >= maxDepth {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	indent := strings.Repeat("  ", currentDepth)
	for _, entry := range entries {
		if scanSkipDirs[entry.Name()] {
			continue
		}
		sb.WriteString(indent)
		sb.WriteString(entry.Name())
		sb.WriteString("\n")

		if entry.IsDir() {
			buildTreeLevel(sb, filepath.Join(dir, entry.Name()), currentDepth+1, maxDepth)
		}
	}
}

// collectGitStats runs git commands to gather repository statistics.
// Returns nil if the directory is not a git repo or git is unavailable.
func collectGitStats(repoPath string) *GitStats {
	// Check if this is a git repo
	checkCmd := exec.Command("git", "-C", repoPath, "rev-parse", "--git-dir")
	if err := checkCmd.Run(); err != nil {
		return nil
	}

	stats := &GitStats{}

	// Commit count
	if out, err := runGit(repoPath, "rev-list", "--count", "HEAD"); err == nil {
		if n, parseErr := strconv.Atoi(strings.TrimSpace(out)); parseErr == nil {
			stats.CommitCount = n
		}
	}

	// Contributor count
	if out, err := runGit(repoPath, "shortlog", "-sn", "HEAD"); err == nil {
		count := 0
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if strings.TrimSpace(line) != "" {
				count++
			}
		}
		stats.Contributors = count
	}

	// First commit date
	if out, err := runGit(repoPath, "log", "--format=%aI", "--reverse", "-1"); err == nil {
		if t, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(out)); parseErr == nil {
			stats.FirstCommit = t
		}
	}

	// Last commit date
	if out, err := runGit(repoPath, "log", "-1", "--format=%aI"); err == nil {
		if t, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(out)); parseErr == nil {
			stats.LastCommit = t
		}
	}

	// Age in days
	if !stats.FirstCommit.IsZero() {
		stats.AgeInDays = int(time.Since(stats.FirstCommit).Hours() / 24)
	}

	// Default branch
	if out, err := runGit(repoPath, "symbolic-ref", "--short", "HEAD"); err == nil {
		stats.DefaultBranch = strings.TrimSpace(out)
	}

	return stats
}

// runGit runs a git command in the specified repo directory and returns stdout.
func runGit(repoPath string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", fullArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out.String(), nil
}

// detectTestFramework returns the test framework name based on project signals.
func detectTestFramework(hasGo bool, packageJSONContent, pyprojectContent string) string {
	if hasGo {
		return "go-test"
	}
	if strings.Contains(packageJSONContent, "jest") {
		return "jest"
	}
	if strings.Contains(pyprojectContent, "pytest") {
		return "pytest"
	}
	return ""
}
