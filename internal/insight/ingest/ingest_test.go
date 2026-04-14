package ingest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
)

// --- fakes ---

type fakeStore struct {
	pages map[string]*db.WikiPage
	refs  []db.WikiPageRef
	links []db.WikiLink
}

func newFakeStore() *fakeStore { return &fakeStore{pages: map[string]*db.WikiPage{}} }

func (f *fakeStore) SavePage(p *db.WikiPage) error    { f.pages[p.ID] = p; return nil }
func (f *fakeStore) UpdatePage(p *db.WikiPage) error  { f.pages[p.ID] = p; return nil }
func (f *fakeStore) GetPage(id string) (*db.WikiPage, error) {
	if p, ok := f.pages[id]; ok {
		return p, nil
	}
	return nil, nil
}
func (f *fakeStore) ListPages(fl db.WikiPageFilters) ([]db.WikiPage, int, error) {
	var out []db.WikiPage
	for _, p := range f.pages {
		if fl.PageType != "" && p.PageType != fl.PageType {
			continue
		}
		if fl.Status != "" && p.Status != fl.Status {
			continue
		}
		out = append(out, *p)
	}
	return out, len(out), nil
}
func (f *fakeStore) SearchPages(q, pt string, l int) ([]db.WikiPage, error) { return nil, nil }
func (f *fakeStore) DeletePage(id string) error                             { delete(f.pages, id); return nil }
func (f *fakeStore) UpdatePageStaleness(id string, s float64) error         { return nil }
func (f *fakeStore) SaveLink(l *db.WikiLink) error                          { f.links = append(f.links, *l); return nil }
func (f *fakeStore) ListLinksFrom(id string) ([]db.WikiLink, error)         { return nil, nil }
func (f *fakeStore) ListLinksTo(id string) ([]db.WikiLink, error)           { return nil, nil }
func (f *fakeStore) DeleteLinks(id string) error                            { return nil }
func (f *fakeStore) GetGraph(pt string, l int) ([]db.WikiPage, []db.WikiLink, error) {
	return nil, nil, nil
}
func (f *fakeStore) SaveRef(r *db.WikiPageRef) error    { f.refs = append(f.refs, *r); return nil }
func (f *fakeStore) ListRefs(id string) ([]db.WikiPageRef, error) { return nil, nil }
func (f *fakeStore) DeleteRefs(id string) error                    { return nil }

type fakeLLM struct{ body string }

func (f *fakeLLM) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: f.body, InputTokens: 1, OutputTokens: 1}, nil
}

// --- tests ---

func TestIngest_MarkdownFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hello.md")
	if err := os.WriteFile(path, []byte("# Hello\n\nWorld body."), 0o644); err != nil {
		t.Fatal(err)
	}

	store := newFakeStore()
	engine := wiki_engine.NewWikiEngine(store, &fakeLLM{body: "# Cleaned\n\nContent."}, func() config.WikiConfig {
		return config.WikiConfig{Enabled: true}
	})
	ing := New(store, engine, func() config.WikiConfig {
		return config.WikiConfig{Enabled: true, LinkSuggesterEnabled: false}
	}, nil)

	res, err := ing.Ingest(context.Background(), path, Options{AutoSynthesize: true, Tags: []string{"test"}})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if res.Kind != KindMarkdown {
		t.Errorf("Kind = %q, want markdown", res.Kind)
	}
	if res.RawPageID == "" {
		t.Error("RawPageID empty")
	}
	if res.WikiPageID == "" {
		t.Error("WikiPageID empty (autosynth should have run)")
	}
	raw := store.pages[res.RawPageID]
	if raw.PageType != "raw" {
		t.Errorf("raw page type = %q, want raw", raw.PageType)
	}
	if !strings.Contains(raw.Content, "World body") {
		t.Errorf("raw content missing source: %q", raw.Content)
	}
	if len(raw.Tags) != 1 || raw.Tags[0] != "test" {
		t.Errorf("tags = %v, want [test]", raw.Tags)
	}
	// Expect exactly one "cites" link raw <- wiki.
	foundCites := false
	for _, l := range store.links {
		if l.LinkType == "cites" {
			foundCites = true
		}
	}
	if !foundCites {
		t.Error("expected cites link from wiki → raw")
	}
}

func TestIngest_EmptySource(t *testing.T) {
	store := newFakeStore()
	ing := New(store, nil, func() config.WikiConfig { return config.WikiConfig{} }, nil)
	_, err := ing.Ingest(context.Background(), "   ", Options{})
	if err != ErrEmptySource {
		t.Errorf("got %v, want ErrEmptySource", err)
	}
}

func TestIngest_TextFileNoSynth(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "notes.txt")
	_ = os.WriteFile(path, []byte("plain notes"), 0o644)

	store := newFakeStore()
	ing := New(store, nil, func() config.WikiConfig { return config.WikiConfig{} }, nil)
	res, err := ing.Ingest(context.Background(), path, Options{AutoSynthesize: false})
	if err != nil {
		t.Fatal(err)
	}
	if res.WikiPageID != "" {
		t.Errorf("WikiPageID should be empty when AutoSynthesize=false, got %q", res.WikiPageID)
	}
	if res.Chars != len("plain notes") {
		t.Errorf("Chars = %d, want %d", res.Chars, len("plain notes"))
	}
}

func TestHTMLToText(t *testing.T) {
	html := `<html><head><title>T</title><script>bad()</script></head><body><p>Hello</p><p>World</p></body></html>`
	got := HTMLToText(html)
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Errorf("HTMLToText missing text: %q", got)
	}
	if strings.Contains(got, "bad()") {
		t.Errorf("script leaked: %q", got)
	}
	if strings.Contains(got, "<p>") {
		t.Errorf("tag leaked: %q", got)
	}
}

func TestExtractHTMLTitle(t *testing.T) {
	html := `<html><head><title>  My Page  </title></head></html>`
	if got := ExtractHTMLTitle(html); got != "My Page" {
		t.Errorf("ExtractHTMLTitle = %q, want 'My Page'", got)
	}
}

func TestDedupeTags(t *testing.T) {
	got := dedupeTags([]string{"a", "", "a", "b", " c ", "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
