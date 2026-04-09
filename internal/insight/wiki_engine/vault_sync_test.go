package wiki_engine_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
)

// ---------------------------------------------------------------------------
// local page fixtures
// ---------------------------------------------------------------------------

func vaultSummaryPage(id, title string) *db.WikiPage {
	return &db.WikiPage{
		ID:          id,
		PageType:    "summary",
		Title:       title,
		Content:     "Content for " + title + ".",
		Status:      "published",
		Tags:        []string{"test"},
		GeneratedBy: "ingest",
		Version:     1,
		CreatedAt:   "2026-04-06T12:00:00Z",
		UpdatedAt:   "2026-04-06T12:00:00Z",
	}
}

func vaultEntityPage(id, title string) *db.WikiPage {
	return &db.WikiPage{
		ID:          id,
		PageType:    "entity",
		Title:       title,
		Content:     "Entity content for " + title + ".",
		Status:      "published",
		Tags:        []string{},
		GeneratedBy: "ingest",
		Version:     1,
		CreatedAt:   "2026-04-06T12:00:00Z",
		UpdatedAt:   "2026-04-06T12:00:00Z",
	}
}

func vaultConceptPage(id, title string) *db.WikiPage {
	return &db.WikiPage{
		ID:          id,
		PageType:    "concept",
		Title:       title,
		Content:     "Concept content for " + title + ".",
		Status:      "published",
		Tags:        []string{},
		GeneratedBy: "ingest",
		Version:     1,
		CreatedAt:   "2026-04-06T12:00:00Z",
		UpdatedAt:   "2026-04-06T12:00:00Z",
	}
}

// ---------------------------------------------------------------------------
// TestSyncPage_WritesFile — sync single page, verify file exists with correct content
// ---------------------------------------------------------------------------

func TestSyncPage_WritesFile(t *testing.T) {
	vaultDir := t.TempDir()
	store := newMockStore()
	vs := wiki_engine.NewVaultSync(store, vaultDir)

	page := vaultSummaryPage("wp-1", "My Summary Page")
	refs := []db.WikiPageRef{
		{ID: "ref-1", PageID: "wp-1", SourceType: "event", SourceID: "evt-100"},
	}
	linked := []db.WikiPage{}

	if err := vs.SyncPage(page, refs, linked); err != nil {
		t.Fatalf("SyncPage returned error: %v", err)
	}

	expectedPath := filepath.Join(vaultDir, "summaries", "my-summary-page.md")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("expected file at %s, got error: %v", expectedPath, err)
	}

	content := string(data)

	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("file content must start with ---\\n, got: %q", content[:clamp(20, len(content))])
	}
	if !strings.Contains(content, "# My Summary Page") {
		t.Errorf("expected heading '# My Summary Page' in file, got:\n%s", content)
	}
	if !strings.Contains(content, "type: event") {
		t.Errorf("expected 'type: event' in frontmatter sources, got:\n%s", content)
	}
	if !strings.Contains(content, "id: wp-1") {
		t.Errorf("expected 'id: wp-1' in frontmatter, got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// TestSyncPage_CreatesSubdirectory — verify directory created for each page type
// ---------------------------------------------------------------------------

func TestSyncPage_CreatesSubdirectory(t *testing.T) {
	cases := []struct {
		pageType string
		title    string
		wantDir  string
	}{
		{"summary", "My Summary", "summaries"},
		{"entity", "My Entity", "entities"},
		{"concept", "My Concept", "concepts"},
		{"answer", "My Answer", "answers"},
	}

	for _, tc := range cases {
		t.Run(tc.pageType, func(t *testing.T) {
			vaultDir := t.TempDir()
			store := newMockStore()
			vs := wiki_engine.NewVaultSync(store, vaultDir)

			page := &db.WikiPage{
				ID:          "wp-sub-" + tc.pageType,
				PageType:    tc.pageType,
				Title:       tc.title,
				Content:     "Content.",
				Status:      "published",
				Tags:        []string{},
				GeneratedBy: "ingest",
				Version:     1,
				CreatedAt:   "2026-04-06T12:00:00Z",
				UpdatedAt:   "2026-04-06T12:00:00Z",
			}

			if err := vs.SyncPage(page, nil, nil); err != nil {
				t.Fatalf("SyncPage returned error: %v", err)
			}

			subDir := filepath.Join(vaultDir, tc.wantDir)
			info, err := os.Stat(subDir)
			if err != nil {
				t.Fatalf("expected subdirectory %s to exist, got error: %v", subDir, err)
			}
			if !info.IsDir() {
				t.Errorf("expected %s to be a directory", subDir)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestSyncAll_MultiplePages — sync 3 pages of different types, verify all files written
// ---------------------------------------------------------------------------

func TestSyncAll_MultiplePages(t *testing.T) {
	vaultDir := t.TempDir()
	store := newMockStore()

	pages := []*db.WikiPage{
		vaultSummaryPage("wp-s1", "Alpha Summary"),
		vaultEntityPage("wp-e1", "Beta Entity"),
		vaultConceptPage("wp-c1", "Gamma Concept"),
	}
	for _, p := range pages {
		if err := store.SavePage(p); err != nil {
			t.Fatalf("SavePage: %v", err)
		}
	}

	vs := wiki_engine.NewVaultSync(store, vaultDir)

	status, err := vs.SyncAll(t.Context())
	if err != nil {
		t.Fatalf("SyncAll returned error: %v", err)
	}

	if status.FileCount != 3 {
		t.Errorf("expected FileCount=3, got %d", status.FileCount)
	}

	expectedFiles := []string{
		filepath.Join(vaultDir, "summaries", "alpha-summary.md"),
		filepath.Join(vaultDir, "entities", "beta-entity.md"),
		filepath.Join(vaultDir, "concepts", "gamma-concept.md"),
	}
	for _, path := range expectedFiles {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist, got error: %v", path, err)
		}
	}

	if len(status.Errors) != 0 {
		t.Errorf("expected no errors, got: %v", status.Errors)
	}
}

// ---------------------------------------------------------------------------
// TestGetStatus_CountsFiles — write .md files manually, verify count (ignores non-.md)
// ---------------------------------------------------------------------------

func TestGetStatus_CountsFiles(t *testing.T) {
	vaultDir := t.TempDir()
	store := newMockStore()
	vs := wiki_engine.NewVaultSync(store, vaultDir)

	if err := vs.EnsureVaultDirs(); err != nil {
		t.Fatalf("EnsureVaultDirs: %v", err)
	}

	mdFiles := []string{
		filepath.Join(vaultDir, "summaries", "one.md"),
		filepath.Join(vaultDir, "entities", "two.md"),
		filepath.Join(vaultDir, "concepts", "three.md"),
	}
	nonMdFile := filepath.Join(vaultDir, "summaries", "ignore.txt")

	for _, f := range mdFiles {
		if err := os.WriteFile(f, []byte("content"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", f, err)
		}
	}
	if err := os.WriteFile(nonMdFile, []byte("not markdown"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", nonMdFile, err)
	}

	status, err := vs.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if status.FileCount != 3 {
		t.Errorf("expected FileCount=3 (only .md files counted), got %d", status.FileCount)
	}
	if status.VaultPath != vaultDir {
		t.Errorf("expected VaultPath=%q, got %q", vaultDir, status.VaultPath)
	}
}

// ---------------------------------------------------------------------------
// TestEnsureVaultDirs_CreatesStructure — verify all standard subdirs are created
// ---------------------------------------------------------------------------

func TestEnsureVaultDirs_CreatesStructure(t *testing.T) {
	vaultDir := t.TempDir()
	store := newMockStore()
	vs := wiki_engine.NewVaultSync(store, vaultDir)

	if err := vs.EnsureVaultDirs(); err != nil {
		t.Fatalf("EnsureVaultDirs returned error: %v", err)
	}

	for _, dir := range []string{"summaries", "entities", "concepts", "answers"} {
		path := filepath.Join(vaultDir, dir)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %s to exist, got error: %v", path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %s to be a directory, got a file", path)
		}
	}
}

// ---------------------------------------------------------------------------
// TestEnsureVaultDirs_Idempotent — calling twice must not error
// ---------------------------------------------------------------------------

func TestEnsureVaultDirs_Idempotent(t *testing.T) {
	vaultDir := t.TempDir()
	store := newMockStore()
	vs := wiki_engine.NewVaultSync(store, vaultDir)

	if err := vs.EnsureVaultDirs(); err != nil {
		t.Fatalf("first EnsureVaultDirs: %v", err)
	}
	if err := vs.EnsureVaultDirs(); err != nil {
		t.Fatalf("second EnsureVaultDirs (idempotent): %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestSyncPage_AtomicWrite — verify file not corrupt on concurrent writes
// ---------------------------------------------------------------------------

func TestSyncPage_AtomicWrite(t *testing.T) {
	vaultDir := t.TempDir()
	store := newMockStore()
	vs := wiki_engine.NewVaultSync(store, vaultDir)

	// Pre-create summaries dir so goroutines do not race on MkdirAll.
	if err := vs.EnsureVaultDirs(); err != nil {
		t.Fatalf("EnsureVaultDirs: %v", err)
	}

	page := vaultSummaryPage("wp-concurrent", "Concurrent Page")

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = vs.SyncPage(page, nil, nil)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: SyncPage error: %v", i, err)
		}
	}

	expectedPath := filepath.Join(vaultDir, "summaries", "concurrent-page.md")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("expected file at %s, got error: %v", expectedPath, err)
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Errorf("file appears corrupt, does not start with frontmatter: %q",
			string(data)[:clamp(40, len(data))])
	}
}

// ---------------------------------------------------------------------------
// TestSyncAll_ListError — store ListPages error propagates as fatal error
// ---------------------------------------------------------------------------

func TestSyncAll_ListError(t *testing.T) {
	vaultDir := t.TempDir()
	store := newMockStore()
	store.listPagesErr = errVaultSentinel("injected list error")

	vs := wiki_engine.NewVaultSync(store, vaultDir)

	_, err := vs.SyncAll(t.Context())
	if err == nil {
		t.Fatal("expected error when store.ListPages fails, got nil")
	}
	if !strings.Contains(err.Error(), "vault sync") {
		t.Errorf("expected error to contain 'vault sync', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestSyncAll_CollectsPerPageErrors — per-page write failures collected, not fatal
// ---------------------------------------------------------------------------

func TestSyncAll_CollectsPerPageErrors(t *testing.T) {
	vaultDir := t.TempDir()

	// Block the summaries subdirectory by creating a file in its place.
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	blocker := filepath.Join(vaultDir, "summaries")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("setup blocker file: %v", err)
	}

	store := newMockStore()
	page := vaultSummaryPage("wp-err", "Error Page")
	if err := store.SavePage(page); err != nil {
		t.Fatalf("SavePage: %v", err)
	}

	vs := wiki_engine.NewVaultSync(store, vaultDir)
	status, err := vs.SyncAll(t.Context())

	// SyncAll must not return a fatal error — per-page failures are collected.
	if err != nil {
		t.Fatalf("SyncAll should not return fatal error for per-page write failures, got: %v", err)
	}
	if len(status.Errors) == 0 {
		t.Error("expected at least one collected error for the failed page write")
	}
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

type errVaultSentinel string

func (e errVaultSentinel) Error() string { return string(e) }

// clamp returns a if a <= b, otherwise b (avoids panic slicing short strings).
func clamp(a, b int) int {
	if a <= b {
		return a
	}
	return b
}
