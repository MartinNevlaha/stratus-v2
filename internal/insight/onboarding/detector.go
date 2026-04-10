package onboarding

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const nonGreenfieldThreshold = 0.4

var sourceExtensions = map[string]bool{
	".go":     true,
	".ts":     true,
	".tsx":    true,
	".js":     true,
	".jsx":    true,
	".py":     true,
	".rs":     true,
	".java":   true,
	".rb":     true,
	".cs":     true,
	".cpp":    true,
	".c":      true,
	".swift":  true,
	".kt":     true,
	".svelte": true,
}

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
}

var projectMarkers = []string{
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"Makefile",
}

// IsNonGreenfield checks if the given root path contains an existing project
// (as opposed to an empty/new directory). Returns a boolean and a confidence
// score between 0.0 and 1.0.
func IsNonGreenfield(rootPath string) (bool, float64) {
	gitScore := scoreGitHistory(rootPath)
	fileScore := scoreSourceFiles(rootPath)
	markerScore := scoreProjectMarkers(rootPath)
	readmeScore := scoreReadme(rootPath)
	ciScore := scoreCIConfig(rootPath)

	total := gitScore*0.30 +
		fileScore*0.25 +
		markerScore*0.20 +
		readmeScore*0.10 +
		ciScore*0.15

	return total >= nonGreenfieldThreshold, total
}

func scoreGitHistory(rootPath string) float64 {
	cmd := exec.Command("git", "-C", rootPath, "rev-list", "--count", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	countStr := strings.TrimSpace(string(out))
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return 0
	}

	switch {
	case count > 50:
		return 1.0
	case count > 10:
		return 0.6
	case count > 3:
		return 0.3
	default:
		return 0
	}
}

func scoreSourceFiles(rootPath string) float64 {
	count := 0

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if sourceExtensions[ext] {
			count++
		}
		return nil
	})
	if err != nil {
		return 0
	}

	switch {
	case count > 100:
		return 1.0
	case count > 30:
		return 0.6
	case count > 10:
		return 0.3
	default:
		return 0
	}
}

func scoreProjectMarkers(rootPath string) float64 {
	for _, marker := range projectMarkers {
		if _, err := os.Stat(filepath.Join(rootPath, marker)); err == nil {
			return 1.0
		}
	}
	return 0
}

func scoreReadme(rootPath string) float64 {
	for _, name := range []string{"README.md", "README"} {
		if _, err := os.Stat(filepath.Join(rootPath, name)); err == nil {
			return 1.0
		}
	}
	return 0
}

func scoreCIConfig(rootPath string) float64 {
	workflowsDir := filepath.Join(rootPath, ".github", "workflows")
	if info, err := os.Stat(workflowsDir); err == nil && info.IsDir() {
		return 1.0
	}

	if _, err := os.Stat(filepath.Join(rootPath, ".gitlab-ci.yml")); err == nil {
		return 1.0
	}

	return 0
}
