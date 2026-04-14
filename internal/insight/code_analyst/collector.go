package code_analyst

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Collector gathers per-file signals from the host project filesystem and git history.
type Collector struct {
	projRoot string
}

// NewCollector creates a Collector rooted at projRoot.
func NewCollector(projRoot string) *Collector {
	return &Collector{projRoot: projRoot}
}

// CollectAll gathers signals for all tracked files in the project.
// gitDepth is the number of commits to scan for churn.
func (c *Collector) CollectAll(ctx context.Context, gitDepth int) ([]FileSignals, error) {
	churn, err := c.CollectGitChurn(ctx, gitDepth)
	if err != nil {
		return nil, err
	}
	if len(churn) == 0 {
		return nil, nil
	}

	// Build the list of files that appear in the git log.
	files := make([]string, 0, len(churn))
	for rel := range churn {
		files = append(files, rel)
	}

	// Build absolute paths for line count collection, filtering excluded files.
	absPaths := make([]string, 0, len(files))
	relToAbs := make(map[string]string, len(files))
	for _, rel := range files {
		if isExcluded(rel) {
			continue
		}
		abs := filepath.Join(c.projRoot, rel)
		absPaths = append(absPaths, abs)
		relToAbs[rel] = abs
	}

	lineCounts, err := c.CollectLineCounts(ctx, absPaths)
	if err != nil {
		return nil, err
	}

	debt, err := c.CollectTechDebt(ctx)
	if err != nil {
		return nil, err
	}

	signals := make([]FileSignals, 0, len(relToAbs))
	for rel, abs := range relToAbs {
		lc := lineCounts[abs]
		signals = append(signals, FileSignals{
			FilePath:        rel,
			CommitCount:     churn[rel],
			LineCount:       lc,
			TechDebtMarkers: debt[rel],
			TestFile:        isTestFile(rel),
			Language:        detectLanguage(rel),
		})
	}
	return signals, nil
}

// CollectGitChurn returns a proxy commit-churn count per file from the last
// `depth` commits. The count is the sum of added+removed lines across all
// commits that touched the file.
func (c *Collector) CollectGitChurn(ctx context.Context, depth int) (map[string]int, error) {
	out, err := c.runCmd(ctx, "git", "log", "--numstat", "--format=", fmt.Sprintf("-n%d", depth))
	if err != nil {
		return nil, fmt.Errorf("code analyst: collect git churn: %w", err)
	}

	result := make(map[string]int)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		added, removedStr, filePath := parts[0], parts[1], parts[2]
		// Binary files are represented as "-\t-\tpath" — skip them.
		if added == "-" || removedStr == "-" {
			continue
		}
		a, err1 := strconv.Atoi(added)
		r, err2 := strconv.Atoi(removedStr)
		if err1 != nil || err2 != nil {
			continue
		}
		result[filePath] += a + r
	}
	return result, nil
}

// CollectTechDebt returns TODO/FIXME/HACK marker counts per file (relative to
// projRoot). Only Go, TypeScript, JavaScript, Svelte, Python, Rust, and Java
// source files are scanned.
func (c *Collector) CollectTechDebt(ctx context.Context) (map[string]int, error) {
	// Single-pass: grep -rEcH prints "file:count" lines.
	// grep exits 1 when no matches found — treat as empty, not error.
	out, err := c.runGrepRelaxed(ctx,
		"grep", "-rEcH",
		"--include=*.go",
		"--include=*.ts",
		"--include=*.svelte",
		"--include=*.js",
		"--include=*.py",
		"--include=*.rs",
		"--include=*.java",
		"TODO|FIXME|HACK",
		".",
	)
	if err != nil {
		return nil, fmt.Errorf("code analyst: collect tech debt: %w", err)
	}

	result := make(map[string]int)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Format: ./path/to/file:count
		idx := strings.LastIndex(line, ":")
		if idx < 0 {
			continue
		}
		rawPath := line[:idx]
		countStr := line[idx+1:]
		count, err := strconv.Atoi(countStr)
		if err != nil || count == 0 {
			continue
		}
		// Normalise path: strip leading "./" and make relative.
		rel := filepath.Clean(rawPath)
		if strings.HasPrefix(rel, "./") {
			rel = rel[2:]
		}
		result[rel] = count
	}
	return result, nil
}

// CollectLineCounts returns the number of lines for each absolute file path
// provided. Files that cannot be read are skipped (count 0 is not returned for
// unreadable files, so callers should treat missing keys as 0).
func (c *Collector) CollectLineCounts(ctx context.Context, files []string) (map[string]int, error) {
	result := make(map[string]int, len(files))
	for _, abs := range files {
		// Validate the path is under projRoot.
		rel, err := filepath.Rel(c.projRoot, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("code analyst: collect line counts: path %q is outside projRoot", abs)
		}
		n, err := countLines(abs)
		if err != nil {
			// Skip unreadable files silently.
			continue
		}
		result[abs] = n
	}
	return result, nil
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

// runCmd executes a command in projRoot with the given context, applying a
// 30-second default timeout if the context has no deadline.
func (c *Collector) runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := ensureTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = c.projRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %v: %w — output: %s", name, args, err, string(out))
	}
	return out, nil
}

// runGrepRelaxed is like runCmd but tolerates a non-zero exit code when
// grep produces output (grep exits 1 on "no matches").
func (c *Collector) runGrepRelaxed(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := ensureTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = c.projRoot
	out, err := cmd.CombinedOutput()
	// grep exits 1 when no matches — that is not a real error.
	if err != nil && len(out) > 0 {
		return out, nil
	}
	return out, err
}

// ensureTimeout attaches a timeout to ctx only when ctx has no deadline yet.
func ensureTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

// countLines counts the number of newline characters in the file at path.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

// isExcluded reports whether a relative path should be excluded from
// collection (vendor dirs, sensitive filenames, etc.).
func isExcluded(rel string) bool {
	// Directory prefixes to skip.
	excludedDirs := []string{
		".git/",
		"node_modules/",
		"vendor/",
		".next/",
		"dist/",
		"build/",
	}
	for _, prefix := range excludedDirs {
		if strings.HasPrefix(rel, prefix) || strings.Contains(rel, "/"+prefix[:len(prefix)-1]+"/") {
			return true
		}
	}

	base := filepath.Base(rel)
	ext := strings.ToLower(filepath.Ext(base))

	// Sensitive file extensions.
	sensitiveExts := []string{".env", ".key", ".pem"}
	for _, s := range sensitiveExts {
		if ext == s {
			return true
		}
	}

	// Sensitive filename patterns.
	baseLower := strings.ToLower(base)
	sensitivePatterns := []string{"credentials", "secret"}
	for _, p := range sensitivePatterns {
		if strings.Contains(baseLower, p) {
			return true
		}
	}

	return false
}

// isTestFile reports whether the file at relPath is a test file.
func isTestFile(relPath string) bool {
	base := filepath.Base(relPath)
	// Go test files
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	// TypeScript / JavaScript test files
	if strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".test.js") {
		return true
	}
	// TypeScript / JavaScript spec files
	if strings.HasSuffix(base, ".spec.ts") || strings.HasSuffix(base, ".spec.js") {
		return true
	}
	// Python test files
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return true
	}
	return false
}

// detectLanguage returns a human-readable language name based on file extension.
func detectLanguage(relPath string) string {
	ext := strings.ToLower(filepath.Ext(relPath))
	switch ext {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".js":
		return "javascript"
	case ".svelte":
		return "svelte"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".tsx":
		return "typescript"
	case ".jsx":
		return "javascript"
	case ".rb":
		return "ruby"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".c", ".h":
		return "c"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".sh":
		return "shell"
	case ".sql":
		return "sql"
	default:
		return "unknown"
	}
}
