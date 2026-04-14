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
	resp := `[
		{"title": "Existing Concept", "rationale": "already here", "page_type": "concept", "tags": []},
		{"title": "New Concept A", "rationale": "introduced in text", "page_type": "concept", "tags": ["x"]},
		{"title": "Org X", "rationale": "named entity", "page_type": "entity", "tags": []}
	]`
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
