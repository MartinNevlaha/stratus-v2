package baseline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// mockVexor implements baseline.VexorClient for tests.
type mockVexor struct {
	hits  []baseline.VexorHit
	calls []struct{ root, query string; topK int }
	err   error
}

func (m *mockVexor) Search(_ context.Context, root, query string, topK int) ([]baseline.VexorHit, error) {
	m.calls = append(m.calls, struct{ root, query string; topK int }{root, query, topK})
	if m.err != nil {
		return nil, m.err
	}
	if topK < len(m.hits) {
		return m.hits[:topK], nil
	}
	return m.hits, nil
}

// makeFakeProject creates a temp dir with a small project structure.
func makeFakeProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// main.go with a TODO
	writeFile(t, filepath.Join(root, "main.go"), `package main

// TODO: add proper error handling here
func main() {}
`)

	// pkg/service.go with a FIXME
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "pkg", "service.go"), `package pkg

// FIXME: this is broken
func DoWork() {}
`)

	// pkg/service_test.go
	writeFile(t, filepath.Join(root, "pkg", "service_test.go"), `package pkg

import "testing"

func TestDoWork(t *testing.T) {}
`)

	// vendor dir (should be ignored in TODO scan)
	if err := os.MkdirAll(filepath.Join(root, "vendor", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "vendor", "lib", "lib.go"), `package lib
// TODO: this should be ignored
func Noop() {}
`)

	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

func defaultLimits() config.BaselineLimits {
	return config.BaselineLimits{
		VexorTopK:     10,
		GitLogCommits: 50,
		TODOMax:       20,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestBuild_WithNilVexor_SkipsVexor ensures nil VexorClient produces an empty
// VexorHits slice without error.
func TestBuild_WithNilVexor_SkipsVexor(t *testing.T) {
	root := makeFakeProject(t)
	deps := baseline.Dependencies{
		Vexor:       nil,
		DB:          nil,
		CommitQuery: "recent changes",
	}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundle.VexorHits) != 0 {
		t.Errorf("expected 0 VexorHits with nil client, got %d", len(bundle.VexorHits))
	}
}

// TestBuild_VexorTopK_PropagatedToClient verifies that limits.VexorTopK is
// passed through to the VexorClient.Search call.
func TestBuild_VexorTopK_PropagatedToClient(t *testing.T) {
	root := makeFakeProject(t)
	mv := &mockVexor{
		hits: []baseline.VexorHit{
			{Path: "a.go", Snippet: "code", Score: 0.9},
			{Path: "b.go", Snippet: "more", Score: 0.8},
			{Path: "c.go", Snippet: "even more", Score: 0.7},
		},
	}
	deps := baseline.Dependencies{
		Vexor:       mv,
		CommitQuery: "hotspots",
	}
	limits := config.BaselineLimits{VexorTopK: 2, GitLogCommits: 10, TODOMax: 10}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, limits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mv.calls) == 0 {
		t.Fatal("expected at least one call to VexorClient.Search")
	}
	if mv.calls[0].topK != 2 {
		t.Errorf("expected topK=2, got %d", mv.calls[0].topK)
	}
	if len(bundle.VexorHits) > 2 {
		t.Errorf("expected at most 2 VexorHits, got %d", len(bundle.VexorHits))
	}
}

// TestBuild_VexorError_SkipsGracefully ensures a VexorClient error is
// tolerated and Build still succeeds with empty hits.
func TestBuild_VexorError_SkipsGracefully(t *testing.T) {
	root := makeFakeProject(t)
	mv := &mockVexor{err: errVexorDown}
	deps := baseline.Dependencies{Vexor: mv, CommitQuery: "q"}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundle.VexorHits) != 0 {
		t.Errorf("expected 0 VexorHits on error, got %d", len(bundle.VexorHits))
	}
}

// sentinel for mock
var errVexorDown = &vexorError{"vexor unavailable"}

type vexorError struct{ msg string }

func (e *vexorError) Error() string { return e.msg }

// TestBuild_FileTree_Depth2Only verifies the file tree is exactly 2 levels deep.
func TestBuild_FileTree_Depth2Only(t *testing.T) {
	root := makeFakeProject(t)
	// Add a 3-level-deep file — it must NOT appear in FileTree children.
	deep := filepath.Join(root, "a", "b", "deep.go")
	if err := os.MkdirAll(filepath.Dir(deep), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, deep, "package b\n")

	deps := baseline.Dependencies{}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// root node has children (depth 1); children may have children (depth 2).
	// None of those children's children should have children.
	for _, child := range bundle.FileTree.Children {
		for _, grandchild := range child.Children {
			if len(grandchild.Children) > 0 {
				t.Errorf("FileTree depth > 2: grandchild %q has children", grandchild.Name)
			}
		}
	}
}

// TestBuild_TODOCap_Honored verifies TODOMax caps the number of TODOs returned.
func TestBuild_TODOCap_Honored(t *testing.T) {
	root := t.TempDir()
	// Write a file with 10 TODOs.
	var content string
	for i := 0; i < 10; i++ {
		content += "// TODO: item\nfunc f() {}\n"
	}
	writeFile(t, filepath.Join(root, "many.go"), content)

	limits := config.BaselineLimits{VexorTopK: 5, GitLogCommits: 10, TODOMax: 3}
	deps := baseline.Dependencies{}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, limits)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundle.TODOs) > 3 {
		t.Errorf("expected at most 3 TODOs, got %d", len(bundle.TODOs))
	}
}

// TestBuild_TestRatios_Computed verifies TestRatios are calculated per
// top-level directory.
func TestBuild_TestRatios_Computed(t *testing.T) {
	root := makeFakeProject(t)
	deps := baseline.Dependencies{}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The "pkg" directory has 1 source + 1 test file.
	var pkgRatio *baseline.TestRatio
	for i := range bundle.TestRatios {
		if bundle.TestRatios[i].Dir == "pkg" {
			pkgRatio = &bundle.TestRatios[i]
			break
		}
	}
	if pkgRatio == nil {
		t.Fatal("expected TestRatio for 'pkg' directory")
	}
	if pkgRatio.SourceFiles != 1 {
		t.Errorf("expected 1 source file in pkg, got %d", pkgRatio.SourceFiles)
	}
	if pkgRatio.TestFiles != 1 {
		t.Errorf("expected 1 test file in pkg, got %d", pkgRatio.TestFiles)
	}
	if pkgRatio.Ratio != 1.0 {
		t.Errorf("expected ratio 1.0, got %f", pkgRatio.Ratio)
	}
}

// TestBuild_TODOs_Found verifies TODOs are discovered and vendor is skipped.
func TestBuild_TODOs_Found(t *testing.T) {
	root := makeFakeProject(t)
	deps := baseline.Dependencies{}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have at least 2 TODOs (main.go TODO and pkg/service.go FIXME).
	if len(bundle.TODOs) < 2 {
		t.Errorf("expected >= 2 TODOs, got %d: %+v", len(bundle.TODOs), bundle.TODOs)
	}

	// Vendor TODOs must not appear.
	for _, item := range bundle.TODOs {
		if filepath.ToSlash(item.Path) != "" && len(item.Path) > 0 {
			if contains(item.Path, "vendor") {
				t.Errorf("vendor TODO leaked through: %+v", item)
			}
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAny(s, substr))
}

func containsAny(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestBuild_WikiTitles_WithDB verifies wiki titles are loaded from DB when
// non-nil.
func TestBuild_WikiTitles_WithDB(t *testing.T) {
	root := makeFakeProject(t)
	database := openTestDB(t)

	// Insert a wiki page directly via raw SQL.
	_, err := database.SQL().Exec(`
		INSERT INTO wiki_pages (id, title, staleness_score, status, content)
		VALUES ('wp-1', 'Architecture Overview', 0.8, 'published', 'content here')
	`)
	if err != nil {
		t.Fatalf("insert wiki page: %v", err)
	}

	deps := baseline.Dependencies{DB: database}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundle.WikiTitles) == 0 {
		t.Error("expected at least one WikiTitle from DB")
	}
	if bundle.WikiTitles[0].ID != "wp-1" {
		t.Errorf("expected wiki page ID wp-1, got %q", bundle.WikiTitles[0].ID)
	}
}

// TestBuild_WikiTitles_NilDB_Empty verifies no panic and empty titles when DB
// is nil.
func TestBuild_WikiTitles_NilDB_Empty(t *testing.T) {
	root := makeFakeProject(t)
	deps := baseline.Dependencies{DB: nil}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundle.WikiTitles) != 0 {
		t.Errorf("expected 0 WikiTitles with nil DB, got %d", len(bundle.WikiTitles))
	}
}

// TestBuild_GovernanceRefs_WithDB verifies governance refs are loaded when
// the docs table has rule/adr entries.
func TestBuild_GovernanceRefs_WithDB(t *testing.T) {
	root := makeFakeProject(t)
	database := openTestDB(t)

	_, err := database.SQL().Exec(`
		INSERT INTO docs (file_path, title, content, doc_type, file_hash)
		VALUES ('.claude/rules/error-handling.md', 'Error Handling', 'Use fmt.Errorf', 'rule', 'abc123')
	`)
	if err != nil {
		t.Fatalf("insert doc: %v", err)
	}

	deps := baseline.Dependencies{DB: database}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundle.GovernanceRefs) == 0 {
		t.Error("expected at least one GovernanceRef from DB")
	}
}

// TestBuild_GeneratedAt_Set verifies GeneratedAt is populated.
func TestBuild_GeneratedAt_Set(t *testing.T) {
	root := makeFakeProject(t)
	deps := baseline.Dependencies{}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.GeneratedAt.IsZero() {
		t.Error("expected GeneratedAt to be set")
	}
}

// TestBuild_ProjectRoot_Set verifies ProjectRoot is stored.
func TestBuild_ProjectRoot_Set(t *testing.T) {
	root := makeFakeProject(t)
	deps := baseline.Dependencies{}
	b := baseline.New(deps)
	bundle, err := b.Build(context.Background(), root, defaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.ProjectRoot != root {
		t.Errorf("expected ProjectRoot=%q, got %q", root, bundle.ProjectRoot)
	}
}
