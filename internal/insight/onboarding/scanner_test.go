package onboarding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a test helper that creates a file with content, creating parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestScanProject_GoProject(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/myapp\n\ngo 1.21\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "handler.go"), "package main\n\nfunc handle() {}\n")

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	// Check project name
	if profile.ProjectName != filepath.Base(dir) {
		t.Errorf("ProjectName = %q, want %q", profile.ProjectName, filepath.Base(dir))
	}

	// Check Languages contains "Go"
	found := false
	for _, lang := range profile.Languages {
		if lang.Language == "Go" {
			found = true
			if lang.FileCount < 2 {
				t.Errorf("Go FileCount = %d, want >= 2", lang.FileCount)
			}
		}
	}
	if !found {
		t.Error("Languages does not contain Go")
	}

	// Check EntryPoints has main.go
	foundMain := false
	for _, ep := range profile.EntryPoints {
		if strings.HasSuffix(ep.Path, "main.go") {
			foundMain = true
		}
	}
	if !foundMain {
		t.Errorf("EntryPoints missing main.go, got: %+v", profile.EntryPoints)
	}

	// Check config files contains go.mod
	foundGoMod := false
	for _, cf := range profile.ConfigFiles {
		if strings.HasSuffix(cf.Path, "go.mod") {
			foundGoMod = true
			if cf.Type == "" {
				t.Error("ConfigFile.Type is empty for go.mod")
			}
		}
	}
	if !foundGoMod {
		t.Error("ConfigFiles missing go.mod")
	}
}

func TestScanProject_MultiLanguage(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "app.ts"), "const x: number = 1;\n")
	writeFile(t, filepath.Join(dir, "script.py"), "def main():\n    pass\n")

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	langMap := make(map[string]LanguageStat)
	for _, l := range profile.Languages {
		langMap[l.Language] = l
	}

	for _, expected := range []string{"Go", "TypeScript", "Python"} {
		if _, ok := langMap[expected]; !ok {
			t.Errorf("missing language %q in profile", expected)
		}
	}

	// Each should have exactly 1 file
	for lang, stat := range langMap {
		if stat.FileCount != 1 {
			t.Errorf("language %q: FileCount = %d, want 1", lang, stat.FileCount)
		}
	}

	// Percentages should sum to ~100
	total := 0.0
	for _, l := range profile.Languages {
		total += l.Percentage
	}
	if total < 99.0 || total > 101.0 {
		t.Errorf("percentages sum = %.2f, want ~100", total)
	}
}

func TestScanProject_SkipsSecrets(t *testing.T) {
	dir := t.TempDir()

	// Create a legitimate config file
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.21\n")

	// Create secret files that must NOT appear in ConfigFiles
	writeFile(t, filepath.Join(dir, ".env"), "SECRET=hunter2\n")
	writeFile(t, filepath.Join(dir, ".env.production"), "DB_PASS=password\n")
	writeFile(t, filepath.Join(dir, "credentials.json"), `{"key":"secret"}`+"\n")
	writeFile(t, filepath.Join(dir, "server.pem"), "-----BEGIN CERTIFICATE-----\n")
	writeFile(t, filepath.Join(dir, "server.key"), "-----BEGIN PRIVATE KEY-----\n")
	writeFile(t, filepath.Join(dir, "id_rsa"), "-----BEGIN RSA PRIVATE KEY-----\n")

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	secretNames := []string{".env", ".env.production", "credentials.json", "server.pem", "server.key", "id_rsa"}
	for _, cf := range profile.ConfigFiles {
		base := filepath.Base(cf.Path)
		for _, secret := range secretNames {
			if base == secret {
				t.Errorf("secret file %q appeared in ConfigFiles", cf.Path)
			}
		}
	}
}

func TestScanProject_DepthLimitsTree(t *testing.T) {
	dir := t.TempDir()

	// Create 6 levels of nesting: dir/l1/l2/l3/l4/l5/l6/deep.go
	deepPath := filepath.Join(dir, "l1", "l2", "l3", "l4", "l5", "l6")
	writeFile(t, filepath.Join(deepPath, "deep.go"), "package deep\n")

	shallowProfile, err := ScanProject(dir, "shallow")
	if err != nil {
		t.Fatalf("ScanProject shallow: %v", err)
	}

	// With depth "shallow" (limit 3), level 4+ directories should not appear in the tree
	// The tree is indented text; level 4 would have depth indicator beyond limit
	// We check that "l4" does not appear in the directory tree
	if strings.Contains(shallowProfile.DirectoryTree, "l4") {
		t.Errorf("shallow tree should not contain l4 (depth 4), tree:\n%s", shallowProfile.DirectoryTree)
	}

	// With depth "deep" (limit 5), l5 should appear but not l6
	deepProfile, err := ScanProject(dir, "deep")
	if err != nil {
		t.Fatalf("ScanProject deep: %v", err)
	}
	if !strings.Contains(deepProfile.DirectoryTree, "l5") {
		t.Errorf("deep tree should contain l5, tree:\n%s", deepProfile.DirectoryTree)
	}
	if strings.Contains(deepProfile.DirectoryTree, "l6") {
		t.Errorf("deep tree should not contain l6 (depth 6 > limit 5), tree:\n%s", deepProfile.DirectoryTree)
	}
}

func TestScanProject_NoGitRepo(t *testing.T) {
	dir := t.TempDir()

	// No .git directory — not a git repo
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	if profile.GitStats != nil {
		t.Errorf("GitStats should be nil for non-git directory, got: %+v", profile.GitStats)
	}
}

func TestScanProject_ConfigFileCap(t *testing.T) {
	dir := t.TempDir()

	// Create a go.mod larger than 4KB
	largContent := strings.Repeat("// comment line\n", 300) // ~5100 bytes
	writeFile(t, filepath.Join(dir, "go.mod"), largContent)

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	found := false
	for _, cf := range profile.ConfigFiles {
		if strings.HasSuffix(cf.Path, "go.mod") {
			found = true
			const maxBytes = 4 * 1024
			if len(cf.Content) > maxBytes {
				t.Errorf("ConfigFile content length = %d, want <= %d", len(cf.Content), maxBytes)
			}
		}
	}
	if !found {
		t.Error("ConfigFiles missing go.mod")
	}
}

func TestScanProject_ReadmeTruncation(t *testing.T) {
	dir := t.TempDir()

	// Create a README.md larger than 2000 chars
	longReadme := strings.Repeat("A", 3000)
	writeFile(t, filepath.Join(dir, "README.md"), longReadme)

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	const maxChars = 2000
	if len(profile.ReadmeContent) > maxChars {
		t.Errorf("ReadmeContent length = %d, want <= %d", len(profile.ReadmeContent), maxChars)
	}
	if len(profile.ReadmeContent) == 0 {
		t.Error("ReadmeContent is empty, want truncated content")
	}
}

func TestScanProject_DetectedPatterns(t *testing.T) {
	dir := t.TempDir()

	// Create a structure that triggers "cli-app" (cmd/ directory) and "docker" (Dockerfile)
	writeFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM ubuntu:22.04\n")

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	patternSet := make(map[string]bool)
	for _, p := range profile.DetectedPatterns {
		patternSet[p] = true
	}

	if !patternSet["cli-app"] {
		t.Errorf("expected 'cli-app' pattern, got: %v", profile.DetectedPatterns)
	}
	if !patternSet["docker"] {
		t.Errorf("expected 'docker' pattern, got: %v", profile.DetectedPatterns)
	}
}

func TestScanProject_CIProvider(t *testing.T) {
	dir := t.TempDir()

	// Create .github/workflows/ci.yml
	writeFile(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), "name: CI\non: push\n")

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	if profile.CIProvider != "github-actions" {
		t.Errorf("CIProvider = %q, want %q", profile.CIProvider, "github-actions")
	}
}

func TestScanProject_TestStructure(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(dir, "main_test.go"), "package main\n\nfunc TestFoo(t *testing.T) {}\n")
	writeFile(t, filepath.Join(dir, "handler_test.go"), "package main\n\nfunc TestBar(t *testing.T) {}\n")

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	if profile.TestStructure.TestFiles < 2 {
		t.Errorf("TestFiles = %d, want >= 2", profile.TestStructure.TestFiles)
	}
	if profile.TestStructure.Framework != "go-test" {
		t.Errorf("Framework = %q, want %q", profile.TestStructure.Framework, "go-test")
	}
}

func TestScanProject_SkipsNodeModules(t *testing.T) {
	dir := t.TempDir()

	// Create a file inside node_modules — should be ignored
	writeFile(t, filepath.Join(dir, "node_modules", "lodash", "index.js"), "module.exports = {};\n")
	writeFile(t, filepath.Join(dir, "src", "app.ts"), "const x = 1;\n")

	profile, err := ScanProject(dir, "standard")
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}

	// Should find TypeScript from src/, not from node_modules
	for _, lang := range profile.Languages {
		if lang.Language == "JavaScript" {
			t.Errorf("found JavaScript (from node_modules) which should be skipped")
		}
	}
}
