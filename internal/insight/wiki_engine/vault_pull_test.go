package wiki_engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func TestParseFrontmatter(t *testing.T) {
	in := "---\nid: abc\ntitle: Hello\nversion: 3\n---\n# body\n\ntext"
	fm, body, ok := parseFrontmatter(in)
	if !ok {
		t.Fatal("expected frontmatter")
	}
	if fm["id"] != "abc" {
		t.Errorf("id = %q", fm["id"])
	}
	if fm["title"] != "Hello" {
		t.Errorf("title = %q", fm["title"])
	}
	if !strings.HasPrefix(body, "# body") {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	_, _, ok := parseFrontmatter("no frontmatter here")
	if ok {
		t.Error("expected false")
	}
}

func TestPullPage_UpdatesWhenFileNewer(t *testing.T) {
	store := &memStore{pages: []db.WikiPage{{
		ID: "abc", Title: "Old", Content: "old body", Version: 1,
		Status: "published", PageType: "concept",
		UpdatedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano),
	}}}
	tmp := t.TempDir()
	vs := NewVaultSync(store, tmp)

	newer := time.Now().UTC().Format(time.RFC3339Nano)
	md := "---\nid: abc\ntitle: NewTitle\nversion: 1\npage_type: concept\nupdated_at: " + newer + "\n---\n\nfresh body"
	path := filepath.Join(tmp, "foo.md")
	_ = os.WriteFile(path, []byte(md), 0o644)

	_, updated, conflict, err := vs.PullPage(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !updated || conflict {
		t.Fatalf("updated=%v conflict=%v", updated, conflict)
	}
	got := store.pages[0]
	if got.Content != "fresh body" {
		t.Errorf("content = %q", got.Content)
	}
	if got.Version != 2 {
		t.Errorf("version = %d, want 2", got.Version)
	}
	if got.GeneratedBy != db.GeneratedByUserEdit {
		t.Errorf("generated_by = %q", got.GeneratedBy)
	}
}

func TestPullPage_ConflictWhenOlder(t *testing.T) {
	now := time.Now().UTC()
	store := &memStore{pages: []db.WikiPage{{
		ID: "abc", Title: "t", Content: "db body", Version: 5,
		Status: "published", PageType: "concept",
		UpdatedAt: now.Format(time.RFC3339Nano),
	}}}
	tmp := t.TempDir()
	vs := NewVaultSync(store, tmp)

	older := now.Add(-time.Hour).Format(time.RFC3339Nano)
	md := "---\nid: abc\nversion: 2\nupdated_at: " + older + "\n---\n\nstale body"
	path := filepath.Join(tmp, "foo.md")
	_ = os.WriteFile(path, []byte(md), 0o644)

	_, updated, conflict, err := vs.PullPage(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if updated {
		t.Error("should not have updated on conflict")
	}
	if !conflict {
		t.Error("expected conflict")
	}
	if store.pages[0].Content != "db body" {
		t.Errorf("db content overwritten: %q", store.pages[0].Content)
	}
	// conflict file should exist
	conflicts, _ := os.ReadDir(filepath.Join(tmp, "_conflicts"))
	if len(conflicts) != 1 {
		t.Errorf("_conflicts entries = %d, want 1", len(conflicts))
	}
}

func TestPullPage_CreatesNewWhenNoID(t *testing.T) {
	store := &memStore{}
	tmp := t.TempDir()
	vs := NewVaultSync(store, tmp)

	path := filepath.Join(tmp, "brand-new.md")
	_ = os.WriteFile(path, []byte("# New\n\nbody"), 0o644)

	created, _, _, err := vs.PullPage(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("expected created=true")
	}
	if len(store.pages) != 1 {
		t.Fatalf("pages = %d", len(store.pages))
	}
	if store.pages[0].GeneratedBy != db.GeneratedByUserEdit {
		t.Errorf("generated_by = %q", store.pages[0].GeneratedBy)
	}
}
