package code_analyst

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// mockStore is a minimal in-memory stub that implements only the file cache
// methods needed by Deduplicator (other methods panic to catch unintended calls).
type mockStore struct {
	cache    map[string]*db.FileCacheEntry
	setCalls []setCacheCall
	getErr   error
	setErr   error
}

type setCacheCall struct {
	path          string
	gitHash       string
	runID         string
	score         float64
	findingsCount int
}

func newMockStore() *mockStore {
	return &mockStore{cache: make(map[string]*db.FileCacheEntry)}
}

func (m *mockStore) GetFileCache(path string) (*db.FileCacheEntry, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	entry, ok := m.cache[path]
	if !ok {
		return nil, nil
	}
	return entry, nil
}

func (m *mockStore) SetFileCache(path, gitHash, runID string, score float64, findingsCount int) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.setCalls = append(m.setCalls, setCacheCall{path, gitHash, runID, score, findingsCount})
	return nil
}

// Implement remaining interface methods as stubs (panic on unexpected calls).
func (m *mockStore) SaveRun(r *db.CodeAnalysisRun) error            { panic("not implemented") }
func (m *mockStore) GetRun(id string) (*db.CodeAnalysisRun, error)  { panic("not implemented") }
func (m *mockStore) ListRuns(limit, offset int) ([]db.CodeAnalysisRun, int, error) {
	panic("not implemented")
}
func (m *mockStore) UpdateRun(r *db.CodeAnalysisRun) error { panic("not implemented") }
func (m *mockStore) SaveFinding(f *db.CodeFinding) error   { panic("not implemented") }
func (m *mockStore) ListFindings(filters db.CodeFindingFilters) ([]db.CodeFinding, int, error) {
	panic("not implemented")
}
func (m *mockStore) SearchFindings(query string, limit int) ([]db.CodeFinding, error) {
	panic("not implemented")
}
func (m *mockStore) SaveMetric(mm *db.CodeQualityMetric) error        { panic("not implemented") }
func (m *mockStore) ListMetrics(days int) ([]db.CodeQualityMetric, error) { panic("not implemented") }

// helper: store a cache entry with a given analyzed time.
func (m *mockStore) seedCache(path, gitHash, runID string, analyzedAt time.Time) {
	m.cache[path] = &db.FileCacheEntry{
		FilePath:       path,
		GitHash:        gitHash,
		LastAnalyzedAt: analyzedAt.UTC().Format(time.RFC3339Nano),
		LastRunID:      runID,
		CompositeScore: 0.5,
		FindingsCount:  2,
		UpdatedAt:      analyzedAt.UTC().Format(time.RFC3339Nano),
	}
}

// TestDeduplicator_FilterUnchanged_AllNew verifies that files with no cache
// entry are all included for analysis.
func TestDeduplicator_FilterUnchanged_AllNew(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"a.go": "package main\n",
		"b.go": "package main\n\nfunc b() {}\n",
	})

	store := newMockStore()
	d := NewDeduplicator(store, dir, 7*24*time.Hour)

	files := []FileScore{
		{FilePath: "a.go"},
		{FilePath: "b.go"},
	}

	got, err := d.FilterUnchanged(context.Background(), files)
	if err != nil {
		t.Fatalf("FilterUnchanged: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 files returned (all new), got %d", len(got))
	}
}

// TestDeduplicator_FilterUnchanged_AllCached verifies that files with matching
// hashes analyzed within maxAge are all skipped.
func TestDeduplicator_FilterUnchanged_AllCached(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"a.go": "package main\n",
	})

	// Compute real hash first.
	hashes, err := computeHashesInDir(t, dir, []string{"a.go"})
	if err != nil {
		t.Fatalf("compute hashes: %v", err)
	}

	store := newMockStore()
	store.seedCache("a.go", hashes["a.go"], "run-1", time.Now().Add(-1*time.Hour))

	d := NewDeduplicator(store, dir, 7*24*time.Hour)
	files := []FileScore{{FilePath: "a.go"}}

	got, err := d.FilterUnchanged(context.Background(), files)
	if err != nil {
		t.Fatalf("FilterUnchanged: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 files returned (all cached), got %d: %v", len(got), got)
	}
}

// TestDeduplicator_FilterUnchanged_MixedCacheHits verifies that only uncached
// files are returned when some files have up-to-date cache entries.
func TestDeduplicator_FilterUnchanged_MixedCacheHits(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"cached.go": "package main\n",
		"new.go":    "package main\n\nfunc n() {}\n",
	})

	hashes, err := computeHashesInDir(t, dir, []string{"cached.go", "new.go"})
	if err != nil {
		t.Fatalf("compute hashes: %v", err)
	}

	store := newMockStore()
	// cached.go has a fresh matching entry; new.go has no entry.
	store.seedCache("cached.go", hashes["cached.go"], "run-1", time.Now().Add(-1*time.Hour))

	d := NewDeduplicator(store, dir, 7*24*time.Hour)
	files := []FileScore{
		{FilePath: "cached.go"},
		{FilePath: "new.go"},
	}

	got, err := d.FilterUnchanged(context.Background(), files)
	if err != nil {
		t.Fatalf("FilterUnchanged: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 file returned (new.go only), got %d: %v", len(got), got)
	}
	if got[0].FilePath != "new.go" {
		t.Errorf("expected new.go, got %s", got[0].FilePath)
	}
}

// TestDeduplicator_FilterUnchanged_HashChanged verifies that a file whose git
// hash differs from the cached value is included for re-analysis.
func TestDeduplicator_FilterUnchanged_HashChanged(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"mod.go": "package main\n",
	})

	store := newMockStore()
	// Store a stale hash that won't match the real file hash.
	store.seedCache("mod.go", "0000000000000000000000000000000000000000", "run-1", time.Now().Add(-1*time.Hour))

	d := NewDeduplicator(store, dir, 7*24*time.Hour)
	files := []FileScore{{FilePath: "mod.go"}}

	got, err := d.FilterUnchanged(context.Background(), files)
	if err != nil {
		t.Fatalf("FilterUnchanged: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 file (hash changed), got %d", len(got))
	}
}

// TestDeduplicator_FilterUnchanged_StaleCache verifies that a file whose cache
// entry is older than maxAge is re-analyzed even if the hash matches.
func TestDeduplicator_FilterUnchanged_StaleCache(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"old.go": "package main\n",
	})

	hashes, err := computeHashesInDir(t, dir, []string{"old.go"})
	if err != nil {
		t.Fatalf("compute hashes: %v", err)
	}

	store := newMockStore()
	// Hash matches but was analyzed 8 days ago (> 7-day maxAge).
	store.seedCache("old.go", hashes["old.go"], "run-1", time.Now().Add(-8*24*time.Hour))

	d := NewDeduplicator(store, dir, 7*24*time.Hour)
	files := []FileScore{{FilePath: "old.go"}}

	got, err := d.FilterUnchanged(context.Background(), files)
	if err != nil {
		t.Fatalf("FilterUnchanged: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 file (stale cache), got %d", len(got))
	}
}

// TestDeduplicator_FilterUnchanged_SetsLastGitHash verifies that returned
// FileScore entries have LastGitHash populated from the hash map.
func TestDeduplicator_FilterUnchanged_SetsLastGitHash(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"a.go": "package main\n",
	})

	store := newMockStore()
	d := NewDeduplicator(store, dir, 7*24*time.Hour)
	files := []FileScore{{FilePath: "a.go"}}

	got, err := d.FilterUnchanged(context.Background(), files)
	if err != nil {
		t.Fatalf("FilterUnchanged: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 file, got %d", len(got))
	}
	if got[0].LastGitHash == nil || *got[0].LastGitHash == "" {
		t.Error("expected LastGitHash to be set on returned FileScore")
	}
}

// TestDeduplicator_FilterUnchanged_SetsLastAnalyzedAtFromCache verifies that
// cached files that ARE returned (due to hash change) get LastAnalyzedAt from cache.
func TestDeduplicator_FilterUnchanged_SetsLastAnalyzedAtFromCache(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"x.go": "package main\n",
	})

	analyzedAt := time.Now().Add(-2 * time.Hour)
	store := newMockStore()
	// Seed with wrong hash so it gets returned, but LastAnalyzedAt should be from cache.
	store.seedCache("x.go", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "run-1", analyzedAt)

	d := NewDeduplicator(store, dir, 7*24*time.Hour)
	files := []FileScore{{FilePath: "x.go"}}

	got, err := d.FilterUnchanged(context.Background(), files)
	if err != nil {
		t.Fatalf("FilterUnchanged: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 file, got %d", len(got))
	}
	if got[0].LastAnalyzedAt == nil {
		t.Error("expected LastAnalyzedAt to be populated from cache")
	}
}

// TestDeduplicator_ComputeFileHashes verifies that hashes are 40-char SHA1 strings.
func TestDeduplicator_ComputeFileHashes(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"foo.go": "package main\nfunc foo() {}\n",
		"bar.go": "package main\nfunc bar() {}\n",
	})

	store := newMockStore()
	d := NewDeduplicator(store, dir, 7*24*time.Hour)

	files := []FileScore{
		{FilePath: "foo.go"},
		{FilePath: "bar.go"},
	}

	hashes, err := d.ComputeFileHashes(context.Background(), files)
	if err != nil {
		t.Fatalf("ComputeFileHashes: %v", err)
	}
	if len(hashes) != 2 {
		t.Errorf("expected 2 hashes, got %d", len(hashes))
	}
	for _, path := range []string{"foo.go", "bar.go"} {
		h, ok := hashes[path]
		if !ok {
			t.Errorf("missing hash for %s", path)
			continue
		}
		if len(h) != 40 {
			t.Errorf("%s: expected 40-char hash, got %q (len=%d)", path, h, len(h))
		}
	}
	// Different content → different hashes.
	if hashes["foo.go"] == hashes["bar.go"] {
		t.Error("foo.go and bar.go have different content but identical hashes")
	}
}

// TestDeduplicator_ComputeFileHashes_MissingFile verifies that a missing file is
// skipped without error.
func TestDeduplicator_ComputeFileHashes_MissingFile(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"present.go": "package main\n",
	})

	store := newMockStore()
	d := NewDeduplicator(store, dir, 7*24*time.Hour)

	files := []FileScore{
		{FilePath: "present.go"},
		{FilePath: "ghost.go"}, // does not exist on disk
	}

	hashes, err := d.ComputeFileHashes(context.Background(), files)
	if err != nil {
		t.Fatalf("ComputeFileHashes returned error for missing file: %v", err)
	}
	if _, ok := hashes["present.go"]; !ok {
		t.Error("present.go should be in hashes map")
	}
	if _, ok := hashes["ghost.go"]; ok {
		t.Error("ghost.go (missing) should not be in hashes map")
	}
}

// TestDeduplicator_UpdateCache verifies that SetFileCache is called with the
// correct arguments.
func TestDeduplicator_UpdateCache(t *testing.T) {
	store := newMockStore()
	d := NewDeduplicator(store, "/tmp/proj", 7*24*time.Hour)

	err := d.UpdateCache("internal/foo.go", "abc123hash", "run-xyz", 0.75, 3)
	if err != nil {
		t.Fatalf("UpdateCache: %v", err)
	}
	if len(store.setCalls) != 1 {
		t.Fatalf("expected 1 SetFileCache call, got %d", len(store.setCalls))
	}
	call := store.setCalls[0]
	if call.path != "internal/foo.go" {
		t.Errorf("path: got %q, want %q", call.path, "internal/foo.go")
	}
	if call.gitHash != "abc123hash" {
		t.Errorf("gitHash: got %q, want %q", call.gitHash, "abc123hash")
	}
	if call.runID != "run-xyz" {
		t.Errorf("runID: got %q, want %q", call.runID, "run-xyz")
	}
	if call.score != 0.75 {
		t.Errorf("score: got %v, want 0.75", call.score)
	}
	if call.findingsCount != 3 {
		t.Errorf("findingsCount: got %d, want 3", call.findingsCount)
	}
}

// TestDeduplicator_UpdateCache_PropagatesError verifies that store errors are
// wrapped and returned.
func TestDeduplicator_UpdateCache_PropagatesError(t *testing.T) {
	store := newMockStore()
	store.setErr = &stubError{"db connection lost"}

	d := NewDeduplicator(store, "/tmp/proj", 7*24*time.Hour)
	err := d.UpdateCache("f.go", "hash", "run", 0, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !containsStr(err.Error(), "dedup") {
		t.Errorf("error should contain 'dedup', got: %v", err)
	}
}

// TestDeduplicator_FilterUnchanged_EmptyInput verifies that empty input returns
// an empty slice without error.
func TestDeduplicator_FilterUnchanged_EmptyInput(t *testing.T) {
	store := newMockStore()
	d := NewDeduplicator(store, t.TempDir(), 7*24*time.Hour)

	got, err := d.FilterUnchanged(context.Background(), []FileScore{})
	if err != nil {
		t.Fatalf("FilterUnchanged on empty input: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %d", len(got))
	}
}

// stubError is a simple error type for testing.
type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

// containsStr checks whether s contains substr.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}

// computeHashesInDir is a test-only helper that runs git hash-object directly.
func computeHashesInDir(t *testing.T, dir string, relPaths []string) (map[string]string, error) {
	t.Helper()
	store := newMockStore()
	d := NewDeduplicator(store, dir, 7*24*time.Hour)
	files := make([]FileScore, len(relPaths))
	for i, p := range relPaths {
		files[i] = FileScore{FilePath: p}
	}
	return d.ComputeFileHashes(context.Background(), files)
}

// TestDeduplicator_ComputeFileHashes_AbsolutePath verifies that absolute file
// paths work the same way as relative paths.
func TestDeduplicator_ComputeFileHashes_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"abs.go": "package main\n",
	})

	store := newMockStore()
	d := NewDeduplicator(store, dir, 7*24*time.Hour)

	// Use absolute path.
	absPath := filepath.Join(dir, "abs.go")
	files := []FileScore{{FilePath: absPath}}

	hashes, err := d.ComputeFileHashes(context.Background(), files)
	if err != nil {
		t.Fatalf("ComputeFileHashes with absolute path: %v", err)
	}
	h, ok := hashes[absPath]
	if !ok {
		t.Fatal("expected hash for absolute path")
	}
	if len(h) != 40 {
		t.Errorf("expected 40-char hash, got %q", h)
	}
}

// TestDeduplicator_FilterUnchanged_GetCacheErrorIsFailOpen verifies that a store
// error on GetFileCache causes the file to be included (fail-open for individual lookups).
func TestDeduplicator_FilterUnchanged_GetCacheErrorIsFailOpen(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"a.go": "package main\n",
	})

	store := newMockStore()
	store.getErr = &stubError{"transient db error"}

	d := NewDeduplicator(store, dir, 7*24*time.Hour)
	files := []FileScore{{FilePath: "a.go"}}

	got, err := d.FilterUnchanged(context.Background(), files)
	// The function should succeed (no error returned for individual cache misses).
	if err != nil {
		t.Fatalf("FilterUnchanged should not return error on cache lookup failure, got: %v", err)
	}
	// File should be included (fail-open).
	if len(got) != 1 {
		t.Errorf("expected 1 file (fail-open), got %d", len(got))
	}
}

// TestDeduplicator_New_DefaultMaxAge verifies that a zero maxAge is replaced
// with the 7-day default inside NewDeduplicator.
func TestDeduplicator_New_DefaultMaxAge(t *testing.T) {
	store := newMockStore()
	d := NewDeduplicator(store, "/tmp", 0)
	if d.maxAge != 7*24*time.Hour {
		t.Errorf("expected default maxAge 7d, got %v", d.maxAge)
	}
}

// Verify that non-empty files still have their path accessible.
func TestDeduplicator_FilterUnchanged_PreservesFileScoreFields(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir, map[string]string{
		"z.go": "package main\n",
	})

	store := newMockStore()
	d := NewDeduplicator(store, dir, 7*24*time.Hour)

	input := FileScore{
		FilePath:       "z.go",
		CompositeScore: 0.9,
		CommitCount:    5,
	}

	got, err := d.FilterUnchanged(context.Background(), []FileScore{input})
	if err != nil {
		t.Fatalf("FilterUnchanged: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].CompositeScore != 0.9 {
		t.Errorf("CompositeScore not preserved: got %v, want 0.9", got[0].CompositeScore)
	}
	if got[0].CommitCount != 5 {
		t.Errorf("CommitCount not preserved: got %v, want 5", got[0].CommitCount)
	}

	// Verify os.Stat skipping does not apply to existing files.
	if got[0].LastGitHash == nil {
		t.Error("LastGitHash should be set for an existing file")
	}
}
