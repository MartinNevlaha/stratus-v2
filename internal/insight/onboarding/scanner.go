package onboarding

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LanguageStat holds statistics for a single programming language.
type LanguageStat struct {
	Language   string  `json:"language"`
	Extension  string  `json:"extension"`
	FileCount  int     `json:"file_count"`
	LineCount  int     `json:"line_count"`
	Percentage float64 `json:"percentage"`
}

// EntryPoint represents a detected application entry point.
type EntryPoint struct {
	Path        string `json:"path"`
	Type        string `json:"type"`        // "main", "index", "cli", "server"
	Description string `json:"description"`
}

// ConfigFile represents a detected configuration file with its content.
type ConfigFile struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	Content string `json:"content"` // capped at 4KB
}

// GitStats holds high-level git repository statistics.
type GitStats struct {
	CommitCount   int       `json:"commit_count"`
	Contributors  int       `json:"contributors"`
	FirstCommit   time.Time `json:"first_commit"`
	LastCommit    time.Time `json:"last_commit"`
	AgeInDays     int       `json:"age_in_days"`
	DefaultBranch string    `json:"default_branch"`
}

// TestStructure describes the project's test setup.
type TestStructure struct {
	TestDirs  []string `json:"test_dirs"`
	TestFiles int      `json:"test_files"`
	Framework string   `json:"framework"`
}

// ProjectProfile is the result of a full project scan.
type ProjectProfile struct {
	RootPath         string         `json:"root_path"`
	ProjectName      string         `json:"project_name"`
	Languages        []LanguageStat `json:"languages"`
	EntryPoints      []EntryPoint   `json:"entry_points"`
	DirectoryTree    string         `json:"directory_tree"`
	ReadmeContent    string         `json:"readme_content"`
	ConfigFiles      []ConfigFile   `json:"config_files"`
	GitStats         *GitStats      `json:"git_stats"`
	TestStructure    TestStructure  `json:"test_structure"`
	DetectedPatterns []string       `json:"detected_patterns"`
	CIProvider       string         `json:"ci_provider"`
	ScannedAt        time.Time      `json:"scanned_at"`
}

// extensionToLanguage maps file extensions to language names.
var extensionToLanguage = map[string]string{
	".go":     "Go",
	".ts":     "TypeScript",
	".tsx":    "TypeScript",
	".js":     "JavaScript",
	".jsx":    "JavaScript",
	".py":     "Python",
	".rs":     "Rust",
	".java":   "Java",
	".rb":     "Ruby",
	".cs":     "C#",
	".cpp":    "C++",
	".cc":     "C++",
	".cxx":    "C++",
	".c":      "C",
	".h":      "C",
	".swift":  "Swift",
	".kt":     "Kotlin",
	".svelte": "Svelte",
}

// knownConfigFiles maps filenames to their type label.
var knownConfigFiles = map[string]string{
	"go.mod":              "go-module",
	"package.json":        "npm",
	"tsconfig.json":       "typescript",
	"pyproject.toml":      "python",
	"Cargo.toml":          "rust",
	"Dockerfile":          "docker",
	"docker-compose.yml":  "docker-compose",
	"Makefile":            "makefile",
}

// scanSkipDirs are directories that the scanner never walks into.
// Note: detector.go defines skipDirs for its own walk; this set is the full list
// needed by ScanProject.
var scanSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".venv":        true,
	"dist":         true,
	"build":        true,
	".next":        true,
	".cache":       true,
	"target":       true,
	"bin":          true,
	"obj":          true,
}

const (
	configFileCap = 4 * 1024 // 4KB
	readmeCap     = 2000     // 2000 characters
)

// ScanProject scans the project at rootPath and returns a ProjectProfile.
// depth controls how deep the directory tree is rendered: "shallow" (3), "standard" (4), "deep" (5).
func ScanProject(rootPath string, depth string) (*ProjectProfile, error) {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("scan project: resolve path: %w", err)
	}

	maxDepth := depthLimit(depth)

	profile := &ProjectProfile{
		RootPath:    absRoot,
		ProjectName: filepath.Base(absRoot),
		ScannedAt:   time.Now().UTC(),
	}

	// State accumulated during the walk
	type langKey struct{ lang, ext string }
	langStats := make(map[langKey]*LanguageStat)

	var entryPoints []EntryPoint
	var configFiles []ConfigFile

	testDirSet := make(map[string]bool)
	testFileCount := 0

	hasGoFiles := false
	var packageJSONContent string
	var pyprojectContent string

	// Pattern signals
	goModCount := 0
	pkgJSONSubdirCount := 0
	hasCmdDir := false
	hasFrontendDir := false
	hasSrcPagesDir := false
	hasDockerfile := false

	err = filepath.WalkDir(absRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}

		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			return nil
		}

		if rel == "." {
			return nil
		}

		parts := strings.Split(rel, string(filepath.Separator))

		// Skip if any path component is in scanSkipDirs
		for _, part := range parts {
			if scanSkipDirs[part] {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if d.IsDir() {
			base := d.Name()
			depth := len(parts) // depth of this directory relative to root
			if base == "cmd" && depth == 1 {
				hasCmdDir = true
			}
			if base == "frontend" && depth == 1 {
				hasFrontendDir = true
			}
			if base == "pages" && len(parts) >= 2 && parts[0] == "src" {
				hasSrcPagesDir = true
			}
			return nil
		}

		// It's a file from here on
		base := d.Name()
		lowerBase := strings.ToLower(base)

		// Test file detection
		switch {
		case strings.HasSuffix(base, "_test.go"):
			testFileCount++
			testDirSet[filepath.Dir(path)] = true
		case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".spec.ts"):
			testFileCount++
			testDirSet[filepath.Dir(path)] = true
		case strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
			testFileCount++
			testDirSet[filepath.Dir(path)] = true
		}

		// Entry point detection
		detectEntryPoints(&entryPoints, rel, parts, base)

		// Language detection (skip secret files)
		if !isSecretFile(path) {
			ext := strings.ToLower(filepath.Ext(base))
			if lang, ok := extensionToLanguage[ext]; ok {
				key := langKey{lang, ext}
				if langStats[key] == nil {
					langStats[key] = &LanguageStat{Language: lang, Extension: ext}
				}
				ls := langStats[key]
				ls.FileCount++
				if lines, countErr := countLines(path); countErr == nil {
					ls.LineCount += lines
				}
				if lang == "Go" {
					hasGoFiles = true
				}
			}
		}

		// Config file detection
		cfgType, isKnownCfg := knownConfigFiles[base]
		isGHWorkflow := strings.HasPrefix(rel, filepath.Join(".github", "workflows")+string(filepath.Separator)) &&
			strings.HasSuffix(lowerBase, ".yml")
		// Also handle forward-slash paths (cross-platform)
		if !isGHWorkflow {
			isGHWorkflow = strings.HasPrefix(rel, ".github/workflows/") && strings.HasSuffix(lowerBase, ".yml")
		}

		if (isKnownCfg || isGHWorkflow) && !isSecretFile(path) {
			if isGHWorkflow {
				cfgType = "github-workflow"
			}
			if content, readErr := readCapped(path, configFileCap); readErr == nil {
				configFiles = append(configFiles, ConfigFile{
					Path:    rel,
					Type:    cfgType,
					Content: content,
				})

				// Track specific file contents for framework and pattern detection
				switch base {
				case "go.mod":
					goModCount++
				case "package.json":
					if len(parts) == 1 {
						packageJSONContent = content
					} else {
						pkgJSONSubdirCount++
					}
				case "Dockerfile":
					hasDockerfile = true
				case "pyproject.toml":
					pyprojectContent = content
				}
			}
		}

		// README
		if lowerBase == "readme.md" && len(parts) == 1 {
			if content, readErr := readCapped(path, readmeCap); readErr == nil {
				profile.ReadmeContent = content
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan project: walk: %w", err)
	}

	// Build Languages sorted by line count descending, compute percentages
	totalLines := 0
	for _, ls := range langStats {
		totalLines += ls.LineCount
	}
	langs := make([]LanguageStat, 0, len(langStats))
	for _, ls := range langStats {
		if totalLines > 0 {
			ls.Percentage = float64(ls.LineCount) / float64(totalLines) * 100.0
		}
		langs = append(langs, *ls)
	}
	sort.Slice(langs, func(i, j int) bool {
		return langs[i].LineCount > langs[j].LineCount
	})
	profile.Languages = langs
	profile.EntryPoints = entryPoints
	profile.ConfigFiles = configFiles

	// Directory tree
	profile.DirectoryTree = buildTree(absRoot, maxDepth)

	// Git stats
	profile.GitStats = collectGitStats(absRoot)

	// Test structure
	testDirs := make([]string, 0, len(testDirSet))
	for d := range testDirSet {
		rel, _ := filepath.Rel(absRoot, d)
		testDirs = append(testDirs, rel)
	}
	sort.Strings(testDirs)

	profile.TestStructure = TestStructure{
		TestDirs:  testDirs,
		TestFiles: testFileCount,
		Framework: detectTestFramework(hasGoFiles, packageJSONContent, pyprojectContent),
	}

	// Detected patterns
	var patterns []string
	if goModCount > 1 || pkgJSONSubdirCount > 1 {
		patterns = append(patterns, "monorepo")
	}
	if hasCmdDir {
		patterns = append(patterns, "cli-app")
	}
	if hasFrontendDir || hasSrcPagesDir {
		patterns = append(patterns, "web-app")
	}
	if hasDockerfile {
		patterns = append(patterns, "docker")
	}
	profile.DetectedPatterns = patterns

	// CI provider
	if _, statErr := os.Stat(filepath.Join(absRoot, ".github", "workflows")); statErr == nil {
		profile.CIProvider = "github-actions"
	} else if _, statErr := os.Stat(filepath.Join(absRoot, ".gitlab-ci.yml")); statErr == nil {
		profile.CIProvider = "gitlab-ci"
	}

	return profile, nil
}

