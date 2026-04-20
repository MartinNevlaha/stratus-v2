package wiki_engine

import (
	"context"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type memStore struct {
	pages []db.WikiPage
	links []db.WikiLink
}

func (m *memStore) SavePage(p *db.WikiPage) error    { m.pages = append(m.pages, *p); return nil }
func (m *memStore) UpdatePage(p *db.WikiPage) error  { return nil }
func (m *memStore) GetPage(id string) (*db.WikiPage, error) {
	for i := range m.pages {
		if m.pages[i].ID == id {
			return &m.pages[i], nil
		}
	}
	return nil, nil
}
func (m *memStore) ListPages(f db.WikiPageFilters) ([]db.WikiPage, int, error) {
	var out []db.WikiPage
	for _, p := range m.pages {
		if f.PageType != "" && p.PageType != f.PageType {
			continue
		}
		if f.Status != "" && p.Status != f.Status {
			continue
		}
		out = append(out, p)
	}
	return out, len(out), nil
}
func (m *memStore) SearchPages(q, pt string, l int) ([]db.WikiPage, error) {
	var out []db.WikiPage
	for _, p := range m.pages {
		if containsIgnoreCase(p.Title, q) {
			out = append(out, p)
		}
	}
	return out, nil
}
func (m *memStore) DeletePage(id string) error             { return nil }
func (m *memStore) UpdatePageStaleness(id string, s float64) error { return nil }
func (m *memStore) SaveLink(l *db.WikiLink) error          { m.links = append(m.links, *l); return nil }
func (m *memStore) ListLinksFrom(id string) ([]db.WikiLink, error) { return nil, nil }
func (m *memStore) ListLinksTo(id string) ([]db.WikiLink, error)   { return nil, nil }
func (m *memStore) DeleteLinks(id string) error                    { return nil }
func (m *memStore) GetGraph(pt string, l int) ([]db.WikiPage, []db.WikiLink, error) {
	return nil, nil, nil
}
func (m *memStore) SaveRef(r *db.WikiPageRef) error              { return nil }
func (m *memStore) ListRefs(id string) ([]db.WikiPageRef, error) { return nil, nil }
func (m *memStore) DeleteRefs(id string) error                   { return nil }
func (m *memStore) FindWikiPageByTitleNewest(title string) (*db.WikiPage, error) {
	var best *db.WikiPage
	for i := range m.pages {
		p := &m.pages[i]
		if toLower(p.Title) == toLower(title) {
			if best == nil || p.UpdatedAt > best.UpdatedAt {
				best = p
			}
		}
	}
	return best, nil
}

func containsIgnoreCase(a, b string) bool {
	if len(b) == 0 {
		return false
	}
	return toLower(a) == toLower(b) || len(a) >= len(b) && indexOf(toLower(a), toLower(b)) >= 0
}
func toLower(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		out[i] = c
	}
	return string(out)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

type canned struct{ body string }

func (c *canned) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: c.body}, nil
}

func TestLinkSuggester_CreatesStubsForMissing(t *testing.T) {
	store := &memStore{pages: []db.WikiPage{
		{ID: "p1", Title: "Existing Concept", PageType: "concept", Status: "published"},
	}}
	resp := `{"links":[
		{"title": "Existing Concept", "rationale": "already here", "page_type": "concept", "tags": []},
		{"title": "New Concept A", "rationale": "introduced in text", "page_type": "concept", "tags": ["x"]},
		{"title": "Org X", "rationale": "named entity", "page_type": "entity", "tags": []}
	]}`
	ls := NewLinkSuggester(store, &canned{body: resp})

	page := &db.WikiPage{ID: "src", Title: "Article", PageType: "concept", Content: "body"}
	n, err := ls.SuggestAndCreateStubs(context.Background(), page)
	if err != nil {
		t.Fatalf("SuggestAndCreateStubs: %v", err)
	}
	if n != 2 {
		t.Errorf("created = %d, want 2 (skipping existing)", n)
	}
	var drafts int
	for _, p := range store.pages {
		if p.Status == "draft" && p.GeneratedBy == db.GeneratedByLinkSuggester {
			drafts++
		}
	}
	if drafts != 2 {
		t.Errorf("draft stubs = %d, want 2", drafts)
	}
	if len(store.links) != 2 {
		t.Errorf("links = %d, want 2", len(store.links))
	}
	for _, l := range store.links {
		if l.FromPageID != "src" || l.LinkType != "related" {
			t.Errorf("unexpected link: %+v", l)
		}
	}
}

func TestLinkSuggester_InvalidJSON(t *testing.T) {
	ls := NewLinkSuggester(&memStore{}, &canned{body: "not json at all"})
	_, err := ls.Suggest(context.Background(), &db.WikiPage{Title: "t"})
	if err == nil {
		t.Error("expected parse error")
	}
}

func TestLinkSuggester_ParsesTypedEdges(t *testing.T) {
	existing := db.WikiPage{ID: "ep1", Title: "Existing Page", PageType: "concept", Status: "published", UpdatedAt: "2026-01-01T00:00:00Z"}
	store := &memStore{pages: []db.WikiPage{existing}}
	resp := `{"links":[{"to_title":"Existing Page","link_type":"parent","strength":0.9,"rationale":"parent relationship"}]}`
	ls := NewLinkSuggester(store, &canned{body: resp})

	src := &db.WikiPage{ID: "src1", Title: "Child Page", PageType: "concept", Content: "body"}
	n, err := ls.SuggestAndCreateStubs(context.Background(), src)
	if err != nil {
		t.Fatalf("SuggestAndCreateStubs: %v", err)
	}
	// No stub created — linked to existing page.
	if n != 0 {
		t.Errorf("created = %d, want 0 (linked to existing, no stub)", n)
	}
	// One page existed already; no new pages.
	if len(store.pages) != 1 {
		t.Errorf("pages = %d, want 1", len(store.pages))
	}
	if len(store.links) != 1 {
		t.Fatalf("links = %d, want 1", len(store.links))
	}
	l := store.links[0]
	if l.LinkType != "parent" {
		t.Errorf("LinkType = %q, want %q", l.LinkType, "parent")
	}
	if l.ToPageID != "ep1" {
		t.Errorf("ToPageID = %q, want %q", l.ToPageID, "ep1")
	}
}

func TestLinkSuggester_ParsesChildAndCites(t *testing.T) {
	p1 := db.WikiPage{ID: "p1", Title: "Module A", PageType: "concept", Status: "published", UpdatedAt: "2026-01-01T00:00:00Z"}
	p2 := db.WikiPage{ID: "p2", Title: "Source B", PageType: "concept", Status: "published", UpdatedAt: "2026-01-01T00:00:00Z"}
	store := &memStore{pages: []db.WikiPage{p1, p2}}
	resp := `{"links":[
		{"to_title":"Module A","link_type":"child","strength":0.8,"rationale":"child of"},
		{"to_title":"Source B","link_type":"cites","strength":0.7,"rationale":"cites source"}
	]}`
	ls := NewLinkSuggester(store, &canned{body: resp})

	src := &db.WikiPage{ID: "src2", Title: "Current", PageType: "concept", Content: "body"}
	_, err := ls.SuggestAndCreateStubs(context.Background(), src)
	if err != nil {
		t.Fatalf("SuggestAndCreateStubs: %v", err)
	}
	if len(store.links) != 2 {
		t.Fatalf("links = %d, want 2", len(store.links))
	}
	types := map[string]bool{}
	for _, l := range store.links {
		types[l.LinkType] = true
	}
	if !types["child"] {
		t.Error("expected link_type 'child' in saved links")
	}
	if !types["cites"] {
		t.Error("expected link_type 'cites' in saved links")
	}
}

func TestLinkSuggester_FallsBackToRelatedOnInvalidType(t *testing.T) {
	store := &memStore{}
	resp := `{"links":[{"to_title":"","title":"New Concept","link_type":"friend","strength":0.5,"rationale":"unknown type","page_type":"concept","tags":[]}]}`
	ls := NewLinkSuggester(store, &canned{body: resp})

	src := &db.WikiPage{ID: "src3", Title: "Current", PageType: "concept", Content: "body"}
	_, err := ls.SuggestAndCreateStubs(context.Background(), src)
	if err != nil {
		t.Fatalf("SuggestAndCreateStubs: %v", err)
	}
	if len(store.links) != 1 {
		t.Fatalf("links = %d, want 1", len(store.links))
	}
	if store.links[0].LinkType != "related" {
		t.Errorf("LinkType = %q, want %q", store.links[0].LinkType, "related")
	}
}

func TestLinkSuggester_StubModeStillWorks(t *testing.T) {
	store := &memStore{}
	resp := `{"links":[{"to_title":"","title":"New Concept","link_type":"related","page_type":"concept","strength":0.5,"rationale":"worth its own page","tags":[]}]}`
	ls := NewLinkSuggester(store, &canned{body: resp})

	src := &db.WikiPage{ID: "src4", Title: "Article", PageType: "concept", Content: "body"}
	n, err := ls.SuggestAndCreateStubs(context.Background(), src)
	if err != nil {
		t.Fatalf("SuggestAndCreateStubs: %v", err)
	}
	if n != 1 {
		t.Errorf("created = %d, want 1", n)
	}
	if len(store.pages) != 1 {
		t.Errorf("pages = %d, want 1 stub", len(store.pages))
	}
	if len(store.links) != 1 {
		t.Errorf("links = %d, want 1", len(store.links))
	}
}

func TestLinkSuggester_ParsesObjectWrappedResponse(t *testing.T) {
	t.Run("populated", func(t *testing.T) {
		store := &memStore{}
		resp := `{"links":[{"to_title":"","title":"Alpha","link_type":"related","page_type":"concept","strength":0.6,"rationale":"r","tags":[]}]}`
		ls := NewLinkSuggester(store, &canned{body: resp})
		sugs, err := ls.Suggest(context.Background(), &db.WikiPage{Title: "T", Content: "c"})
		if err != nil {
			t.Fatalf("Suggest: %v", err)
		}
		if len(sugs) != 1 {
			t.Errorf("len = %d, want 1", len(sugs))
		}
	})
	t.Run("empty", func(t *testing.T) {
		store := &memStore{}
		resp := `{"links":[]}`
		ls := NewLinkSuggester(store, &canned{body: resp})
		sugs, err := ls.Suggest(context.Background(), &db.WikiPage{Title: "T", Content: "c"})
		if err != nil {
			t.Fatalf("Suggest: %v", err)
		}
		if len(sugs) != 0 {
			t.Errorf("len = %d, want 0", len(sugs))
		}
	})
}

func TestLinkSuggester_AmbiguousToTitle_PicksNewest(t *testing.T) {
	older := db.WikiPage{ID: "old1", Title: "Shared Title", PageType: "concept", Status: "published", UpdatedAt: "2025-01-01T00:00:00Z"}
	newer := db.WikiPage{ID: "new1", Title: "Shared Title", PageType: "concept", Status: "published", UpdatedAt: "2026-01-01T00:00:00Z"}
	store := &memStore{pages: []db.WikiPage{older, newer}}
	resp := `{"links":[{"to_title":"Shared Title","link_type":"related","strength":0.5,"rationale":"ambiguous"}]}`
	ls := NewLinkSuggester(store, &canned{body: resp})

	src := &db.WikiPage{ID: "src6", Title: "Current", PageType: "concept", Content: "body"}
	_, err := ls.SuggestAndCreateStubs(context.Background(), src)
	if err != nil {
		t.Fatalf("SuggestAndCreateStubs: %v", err)
	}
	if len(store.links) != 1 {
		t.Fatalf("links = %d, want 1", len(store.links))
	}
	if store.links[0].ToPageID != "new1" {
		t.Errorf("ToPageID = %q, want %q (newest)", store.links[0].ToPageID, "new1")
	}
}

func TestFilterSuggestions_CapsAndDedupes(t *testing.T) {
	in := []StubSuggestion{
		{Title: "A"}, {Title: "a"}, {Title: "B"}, {Title: ""},
		{Title: "C"}, {Title: "D"}, {Title: "E"}, {Title: "F"}, {Title: "G"},
	}
	got := filterSuggestions(in)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	titles := map[string]bool{}
	for _, s := range got {
		if titles[s.Title] {
			t.Errorf("duplicate %q", s.Title)
		}
		titles[s.Title] = true
	}
}
