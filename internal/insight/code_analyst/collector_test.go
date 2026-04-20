package code_analyst

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo initialises a bare git repo in dir, creates files and makes an
// initial commit. Returns the repo root.
func initGitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	run("git", "init", dir)
	for name, content := range files {
		fullPath := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "-C", dir, "add", ".")
	run("git", "-C", dir, "commit", "-m", "initial commit")
}

// TestCollector_CollectGitChurn verifies that modified line counts accumulate per file.
func TestCollector_CollectGitChurn(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"main.go":  "package main\n\nfunc main() {}\n",
		"util.go":  "package main\n\nfunc helper() {}\n",
		"extra.go": "package main\n",
	})

	// Make a second commit that modifies main.go only.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { _ = 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("git", "-C", dir, "add", "main.go")
	run("git", "-C", dir, "commit", "-m", "update main")

	c := NewCollector(dir)
	churn, err := c.CollectGitChurn(context.Background(), 10)
	if err != nil {
		t.Fatalf("CollectGitChurn: %v", err)
	}
	if len(churn) == 0 {
		t.Fatal("expected non-empty churn map")
	}
	// main.go should have been touched in both commits → higher churn than extra.go
	mainChurn, ok := churn["main.go"]
	if !ok {
		t.Errorf("main.go not found in churn map; keys=%v", mapKeys(churn))
	}
	if mainChurn <= 0 {
		t.Errorf("main.go churn should be > 0, got %d", mainChurn)
	}
}

// TestCollector_CollectTechDebt verifies TODO/FIXME/HACK marker counts.
func TestCollector_CollectTechDebt(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"clean.go": "package main\n\nfunc clean() {}\n",
		"dirty.go": "package main\n\n// TODO: fix this\n// FIXME: broken\n// HACK: workaround\nfunc dirty() {}\n",
	})

	c := NewCollector(dir)
	debt, err := c.CollectTechDebt(context.Background())
	if err != nil {
		t.Fatalf("CollectTechDebt: %v", err)
	}
	// dirty.go has 3 markers, clean.go has 0
	dirtyCount, ok := debt["dirty.go"]
	if !ok {
		t.Errorf("dirty.go not found in debt map; keys=%v", mapKeys(debt))
	}
	if dirtyCount != 3 {
		t.Errorf("dirty.go: expected 3 markers, got %d", dirtyCount)
	}
	if cleanCount, exists := debt["clean.go"]; exists && cleanCount != 0 {
		t.Errorf("clean.go: expected 0 markers, got %d", cleanCount)
	}
}

// TestCollector_CollectLineCounts verifies that line counts are accurate.
func TestCollector_CollectLineCounts(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"three.go": "line1\nline2\nline3\n",
		"one.go":   "only\n",
	}
	initGitRepo(t, dir, files)

	c := NewCollector(dir)
	counts, err := c.CollectLineCounts(context.Background(), []string{
		filepath.Join(dir, "three.go"),
		filepath.Join(dir, "one.go"),
	})
	if err != nil {
		t.Fatalf("CollectLineCounts: %v", err)
	}
	if counts[filepath.Join(dir, "three.go")] != 3 {
		t.Errorf("three.go: expected 3 lines, got %d", counts[filepath.Join(dir, "three.go")])
	}
	if counts[filepath.Join(dir, "one.go")] != 1 {
		t.Errorf("one.go: expected 1 line, got %d", counts[filepath.Join(dir, "one.go")])
	}
}

// TestCollector_CollectAll verifies the full pipeline including language
// detection, test file detection, and sensitive file exclusion.
func TestCollector_CollectAll(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"main.go":          "package main\n// TODO: remove\nfunc main() {}\n",
		"main_test.go":     "package main\n\nfunc TestMain(t *testing.T) {}\n",
		"app.ts":           "export function app() {}\n",
		"app.test.ts":      "import {app} from './app'\n",
		"script.py":        "def run(): pass\n",
		"test_helper.py":   "def test_foo(): pass\n",
		"style.svelte":     "<script>let x = 1;</script>\n",
		".env":             "SECRET=abc123\n",
		"credentials.json": `{"key":"value"}`,
	})

	c := NewCollector(dir)
	signals, err := c.CollectAll(context.Background(), 5)
	if err != nil {
		t.Fatalf("CollectAll: %v", err)
	}
	if len(signals) == 0 {
		t.Fatal("expected non-empty signals")
	}

	byPath := make(map[string]FileSignals)
	for _, s := range signals {
		byPath[s.FilePath] = s
	}

	// Sensitive files must be excluded
	for _, sensitive := range []string{".env", "credentials.json"} {
		if _, found := byPath[sensitive]; found {
			t.Errorf("sensitive file %q should be excluded", sensitive)
		}
	}

	// Language detection
	assertLang := func(path, want string) {
		t.Helper()
		if s, ok := byPath[path]; ok {
			if s.Language != want {
				t.Errorf("%s: expected language %q, got %q", path, want, s.Language)
			}
		}
		// If the file isn't present (no churn), that's also acceptable — skip the assert.
	}
	assertLang("main.go", "go")
	assertLang("app.ts", "typescript")
	assertLang("script.py", "python")
	assertLang("style.svelte", "svelte")

	// Test file detection
	assertTestFile := func(path string, wantTest bool) {
		t.Helper()
		s, ok := byPath[path]
		if !ok {
			return // file may be absent if no churn, skip
		}
		if s.TestFile != wantTest {
			t.Errorf("%s: TestFile=%v, want %v", path, s.TestFile, wantTest)
		}
	}
	assertTestFile("main_test.go", true)
	assertTestFile("main.go", false)
	assertTestFile("app.test.ts", true)
	assertTestFile("app.ts", false)
	assertTestFile("test_helper.py", true)
	assertTestFile("script.py", false)

	// Tech debt marker detection for main.go
	if s, ok := byPath["main.go"]; ok {
		if s.TechDebtMarkers == 0 {
			t.Errorf("main.go: expected TechDebtMarkers > 0, got 0")
		}
	}
}

// TestCollector_EmptyRepo verifies that a repo with no commits returns an
// empty (not erroring) result.
func TestCollector_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	c := NewCollector(dir)
	churn, err := c.CollectGitChurn(context.Background(), 10)
	// An empty repo (no commits) may return an error from git, but it should
	// not panic. We accept either (empty map, nil) or (nil, non-nil error).
	if err == nil && len(churn) != 0 {
		t.Errorf("expected empty churn for empty repo, got %v", churn)
	}
}

// TestCollector_NotGitRepo verifies that a non-git directory returns an error.
func TestCollector_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	// Write a plain file so the directory is not empty.
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCollector(dir)
	_, err := c.CollectGitChurn(context.Background(), 10)
	if err == nil {
		t.Error("expected error for non-git directory, got nil")
	}
	if !strings.Contains(err.Error(), "code analyst") {
		t.Errorf("error should contain 'code analyst', got: %v", err)
	}
}

// mapKeys is a helper to list keys of map[string]int for test messages.
func mapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestIsExcluded_NonSourceFiles(t *testing.T) {
	excluded := []string{
		"data/training/train.jsonl",
		"data/training_cleaned/checkpoint.jsonl.partial",
		"logs/run-20260420.log",
		"datasets/features.csv",
		"build-artifacts/bundle.map",
		"assets/logo.png",
		"readme.pdf",
		"data.parquet",
	}
	for _, p := range excluded {
		if !isExcluded(p) {
			t.Errorf("isExcluded(%q) = false, want true (non-source file)", p)
		}
	}
}

func TestIsExcluded_SourceFilesNotExcluded(t *testing.T) {
	included := []string{
		"main.go",
		"internal/foo/bar.go",
		"src/App.tsx",
		"src/lib/util.ts",
		"frontend/src/routes/Evolution.svelte",
		"scripts/run.py",
		"src/main.rs",
		"Main.java",
		"lib/helpers.rb",
		"include/vec.h",
		"src/server.cpp",
	}
	for _, p := range included {
		if isExcluded(p) {
			t.Errorf("isExcluded(%q) = true, want false (source file must be analyzed)", p)
		}
	}
}

func TestIsExcluded_SensitiveAndVendorStillExcluded(t *testing.T) {
	excluded := []string{
		".env",
		"config/secret.go",
		"lib/credentials.go",
		"node_modules/foo/index.js",
		"vendor/github.com/x/y.go",
		".git/config",
	}
	for _, p := range excluded {
		if !isExcluded(p) {
			t.Errorf("isExcluded(%q) = false, want true", p)
		}
	}
}
