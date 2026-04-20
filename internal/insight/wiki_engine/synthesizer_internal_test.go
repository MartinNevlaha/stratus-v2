package wiki_engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type mockLLM struct{}

func (m *mockLLM) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{}, nil
}

func (m *mockLLM) Provider() string { return "mock" }
func (m *mockLLM) Model() string    { return "mock" }

type testStore struct {
	pages map[string]*db.WikiPage
	links map[string][]db.WikiLink
}

func newTestStore() *testStore {
	return &testStore{
		pages: make(map[string]*db.WikiPage),
		links: make(map[string][]db.WikiLink),
	}
}

func (s *testStore) SavePage(p *db.WikiPage) error {
	s.pages[p.ID] = p
	return nil
}

func (s *testStore) UpdatePage(p *db.WikiPage) error {
	s.pages[p.ID] = p
	return nil
}

func (s *testStore) GetPage(id string) (*db.WikiPage, error) {
	return s.pages[id], nil
}

func (s *testStore) ListPages(f db.WikiPageFilters) ([]db.WikiPage, int, error) {
	var out []db.WikiPage
	for _, p := range s.pages {
		out = append(out, *p)
	}
	return out, len(out), nil
}

func (s *testStore) SearchPages(_ string, _ string, _ int) ([]db.WikiPage, error) {
	return nil, nil
}

func (s *testStore) DeletePage(_ string) error { return nil }

func (s *testStore) UpdatePageStaleness(_ string, _ float64) error { return nil }

func (s *testStore) SaveLink(l *db.WikiLink) error {
	s.links[l.ToPageID] = append(s.links[l.ToPageID], *l)
	return nil
}

func (s *testStore) ListLinksFrom(pageID string) ([]db.WikiLink, error) {
	var out []db.WikiLink
	for _, links := range s.links {
		for _, l := range links {
			if l.FromPageID == pageID {
				out = append(out, l)
			}
		}
	}
	return out, nil
}

func (s *testStore) ListLinksTo(pageID string) ([]db.WikiLink, error) {
	return s.links[pageID], nil
}

func (s *testStore) DeleteLinks(_ string) error { return nil }

func (s *testStore) GetGraph(_ string, _ int) ([]db.WikiPage, []db.WikiLink, error) {
	return nil, nil, nil
}

func (s *testStore) SaveRef(_ *db.WikiPageRef) error { return nil }

func (s *testStore) ListRefs(_ string) ([]db.WikiPageRef, error) { return nil, nil }

func (s *testStore) DeleteRefs(_ string) error { return nil }

func (s *testStore) FindWikiPageByTitleNewest(title string) (*db.WikiPage, error) {
	var best *db.WikiPage
	for _, p := range s.pages {
		if strings.EqualFold(p.Title, title) {
			if best == nil || p.UpdatedAt > best.UpdatedAt {
				best = p
			}
		}
	}
	return best, nil
}

func TestRankPages(t *testing.T) {
	cases := []struct {
		name     string
		pages    []db.WikiPage
		wantIDs  []string
	}{
		{
			name: "empty slice",
			pages: []db.WikiPage{},
			wantIDs: []string{},
		},
		{
			name: "single page",
			pages: []db.WikiPage{
				{ID: "page-1", StalenessScore: 0.5, PageType: "concept"},
			},
			wantIDs: []string{"page-1"},
		},
		{
			name: "different staleness - fresh first",
			pages: []db.WikiPage{
				{ID: "page-2", StalenessScore: 0.8, PageType: "concept"},
				{ID: "page-1", StalenessScore: 0.3, PageType: "concept"},
				{ID: "page-3", StalenessScore: 0.5, PageType: "concept"},
			},
			wantIDs: []string{"page-1", "page-3", "page-2"},
		},
		{
			name: "page type priority - concept > entity > summary",
			pages: []db.WikiPage{
				{ID: "page-2", StalenessScore: 0.5, PageType: "summary"},
				{ID: "page-1", StalenessScore: 0.5, PageType: "concept"},
				{ID: "page-3", StalenessScore: 0.5, PageType: "entity"},
			},
			wantIDs: []string{"page-1", "page-3", "page-2"},
		},
		{
			name: "all page types in priority order",
			pages: []db.WikiPage{
				{ID: "page-index", StalenessScore: 0.5, PageType: "index"},
				{ID: "page-answer", StalenessScore: 0.5, PageType: "answer"},
				{ID: "page-raw", StalenessScore: 0.5, PageType: "raw"},
				{ID: "page-topic", StalenessScore: 0.5, PageType: "topic"},
				{ID: "page-summary", StalenessScore: 0.5, PageType: "summary"},
				{ID: "page-entity", StalenessScore: 0.5, PageType: "entity"},
				{ID: "page-concept", StalenessScore: 0.5, PageType: "concept"},
			},
			wantIDs: []string{"page-concept", "page-entity", "page-summary", "page-topic", "page-raw", "page-answer", "page-index"},
		},
		{
			name: "evolution pages get freshness boost",
			pages: []db.WikiPage{
				{ID: "page-2", StalenessScore: 0.5, PageType: "concept", GeneratedBy: "evolution"},
				{ID: "page-1", StalenessScore: 0.5, PageType: "concept"},
				{ID: "page-3", StalenessScore: 0.5, PageType: "concept", GeneratedBy: "evolution"},
			},
			wantIDs: []string{"page-2", "page-3", "page-1"},
		},
		{
			name: "equal staleness and type - stable sort",
			pages: []db.WikiPage{
				{ID: "page-1", StalenessScore: 0.5, PageType: "concept"},
				{ID: "page-2", StalenessScore: 0.5, PageType: "concept"},
				{ID: "page-3", StalenessScore: 0.5, PageType: "concept"},
			},
			wantIDs: []string{"page-1", "page-2", "page-3"},
		},
		{
			name: "unknown page type goes last",
			pages: []db.WikiPage{
				{ID: "page-2", StalenessScore: 0.5, PageType: "unknown"},
				{ID: "page-1", StalenessScore: 0.5, PageType: "concept"},
			},
			wantIDs: []string{"page-1", "page-2"},
		},
		{
			name: "staleness overrides type priority",
			pages: []db.WikiPage{
				{ID: "page-2", StalenessScore: 0.8, PageType: "concept"},
				{ID: "page-1", StalenessScore: 0.3, PageType: "index"},
			},
			wantIDs: []string{"page-1", "page-2"},
		},
		{
			name: "evolution boost can overcome type difference",
			pages: []db.WikiPage{
				{ID: "page-2", StalenessScore: 0.45, PageType: "summary", GeneratedBy: "evolution"},
				{ID: "page-1", StalenessScore: 0.5, PageType: "concept"},
			},
			wantIDs: []string{"page-2", "page-1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rankPages(tc.pages)
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("got %d pages, want %d", len(got), len(tc.wantIDs))
			}
			for i, wantID := range tc.wantIDs {
				if got[i].ID != wantID {
					t.Errorf("position %d: got ID %q, want %q", i, got[i].ID, wantID)
				}
			}
		})
	}
}

func TestExtractSummary(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		wantStart string
	}{
		{
			name:      "empty content",
			content:   "",
			wantStart: "",
		},
		{
			name:      "content shorter than 300 chars",
			content:   "Short content",
			wantStart: "Short content",
		},
		{
			name:      "content exactly 300 chars",
			content:   string(make([]byte, 300)),
			wantStart: string(make([]byte, 300)),
		},
		{
			name:      "no headings - first 300 chars",
			content:   strings.Repeat("x", 400),
			wantStart: strings.Repeat("x", 300),
		},
		{
			name:      "TL;DR heading present",
			content:   "Some preamble\n## TL;DR\nThis is the summary.\nMore content",
			wantStart: "## TL;DR\nThis is the summary.",
		},
		{
			name:      "extracts first heading section",
			content:   "## First Section\nContent here\n## Second Section\nMore content",
			wantStart: "## First Section\nContent here",
		},
		{
			name:      "heading section longer than 300 chars - capped",
			content:   "## Section\n" + strings.Repeat("x", 400) + "\n## Next",
			wantStart: "## Section\n" + strings.Repeat("x", 288),
		},
		{
			name:      "heading at position 0",
			content:   "## Heading\nContent\n## Next",
			wantStart: "## Heading\nContent",
		},
		{
			name:      "only heading section - no next heading",
			content:   "## Heading\nThis is the only section",
			wantStart: "## Heading\nThis is the only section",
		},
		{
			name:      "heading section exactly 300 chars",
			content:   "## H\n" + strings.Repeat("x", 295),
			wantStart: "## H\n" + strings.Repeat("x", 295),
		},
		{
			name:      "single line heading section",
			content:   "## Heading\n## Next",
			wantStart: "## Heading",
		},
		{
			name:      "heading with special chars",
			content:   "## Heading: Test\nContent here\n## Next",
			wantStart: "## Heading: Test\nContent here",
		},
		{
			name:      "no heading with unicode content - rune-based truncation",
			content:   strings.Repeat("世", 400),
			wantStart: strings.Repeat("世", 300),
		},
		{
			name:      "heading at end of content",
			content:   "Preamble\n## Final\nEnd",
			wantStart: "## Final\nEnd",
		},
		{
			name:      "heading with very long line - still capped at 300",
			content:   "## H\n" + strings.Repeat("a", 500) + "\n## Next",
			wantStart: "## H\n" + strings.Repeat("a", 288),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSummary(tc.content)
			if !strings.HasPrefix(got, tc.wantStart) {
				t.Errorf("got %q, want start %q", got, tc.wantStart)
			}
			if utf8.RuneCountInString(got) > 300 {
				t.Errorf("got rune length %d, want at most 300", utf8.RuneCountInString(got))
			}
		})
	}
}

func TestBuildSourceContext(t *testing.T) {
	store := newTestStore()
	s := &Synthesizer{store: store, llmClient: &mockLLM{}}

	cases := []struct {
		name         string
		pages        []db.WikiPage
		wantContains []string
	}{
		{
			name:  "empty pages",
			pages: []db.WikiPage{},
			wantContains: []string{},
		},
		{
			name: "single page with short content",
			pages: []db.WikiPage{
				{ID: "page-1", Title: "Test Page", PageType: "concept", Content: "Short content"},
			},
			wantContains: []string{"Test Page", "page-1", "concept", "Short content"},
		},
		{
			name: "single page with TL;DR",
			pages: []db.WikiPage{
				{ID: "page-1", Title: "Summary Page", PageType: "summary", Content: "Preamble\n## TL;DR\nThis is summary\nMore"},
			},
			wantContains: []string{"Summary Page", "page-1", "summary", "## TL;DR", "This is summary"},
		},
		{
			name: "multiple pages - respects budget",
			pages: []db.WikiPage{
				{ID: "page-1", Title: "Page One", PageType: "concept", Content: "Content one"},
				{ID: "page-2", Title: "Page Two", PageType: "entity", Content: "Content two"},
				{ID: "page-3", Title: "Page Three", PageType: "summary", Content: "Content three"},
			},
			wantContains: []string{"Page One", "Page Two", "Page Three", "page-1", "page-2", "page-3"},
		},
		{
			name: "long content - should be truncated in detail layer",
			pages: []db.WikiPage{
				{ID: "page-1", Title: "Long Page", PageType: "concept", Content: strings.Repeat("x", 10000)},
			},
			wantContains: []string{"Long Page", "page-1"},
		},
		{
			name: "summary layer includes page types",
			pages: []db.WikiPage{
				{ID: "page-1", Title: "Concept", PageType: "concept", Content: "Content"},
				{ID: "page-2", Title: "Entity", PageType: "entity", Content: "Content"},
				{ID: "page-3", Title: "Summary", PageType: "summary", Content: "Content"},
			},
			wantContains: []string{"[concept]", "[entity]", "[summary]"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := s.buildSourceContext(context.Background(), tc.pages)
			if tc.wantContains == nil {
				if got != "" {
					t.Errorf("got %q, want empty", got)
				}
				return
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("context missing %q\ngot:\n%s", want, got)
				}
			}
		})
	}
}

func TestExpandLinkedPages(t *testing.T) {
	cases := []struct {
		name          string
		primaryPages  []db.WikiPage
		setupLinks    func(*testStore)
		wantExpanded  int
		wantContains  []string
	}{
		{
			name:         "no links - empty expansion",
			primaryPages: []db.WikiPage{{ID: "page-1"}},
			setupLinks:   func(s *testStore) {},
			wantExpanded: 0,
			wantContains: []string{},
		},
		{
			name:         "outgoing links - fetches linked pages",
			primaryPages: []db.WikiPage{{ID: "page-1"}},
			setupLinks: func(s *testStore) {
				s.pages["page-1"] = &db.WikiPage{ID: "page-1", Title: "Page 1", Content: "Content"}
				s.pages["page-2"] = &db.WikiPage{ID: "page-2", Title: "Page 2", Content: "Content"}
				s.links["page-2"] = []db.WikiLink{{ID: "link-1", FromPageID: "page-1", ToPageID: "page-2"}}
			},
			wantExpanded: 1,
			wantContains: []string{"page-2"},
		},
		{
			name:         "incoming links - fetches linked pages",
			primaryPages: []db.WikiPage{{ID: "page-2"}},
			setupLinks: func(s *testStore) {
				s.pages["page-1"] = &db.WikiPage{ID: "page-1", Title: "Page 1", Content: "Content"}
				s.pages["page-2"] = &db.WikiPage{ID: "page-2", Title: "Page 2", Content: "Content"}
				s.links["page-2"] = []db.WikiLink{{ID: "link-1", FromPageID: "page-1", ToPageID: "page-2"}}
			},
			wantExpanded: 1,
			wantContains: []string{"page-1"},
		},
		{
			name:         "deduplication - excludes primary pages",
			primaryPages: []db.WikiPage{{ID: "page-1"}, {ID: "page-2"}},
			setupLinks: func(s *testStore) {
				s.pages["page-1"] = &db.WikiPage{ID: "page-1", Title: "Page 1", Content: "Content"}
				s.pages["page-2"] = &db.WikiPage{ID: "page-2", Title: "Page 2", Content: "Content"}
				s.links["page-2"] = []db.WikiLink{{ID: "link-1", FromPageID: "page-1", ToPageID: "page-2"}}
			},
			wantExpanded: 0,
			wantContains: []string{},
		},
		{
			name:         "max pages cap - stops at limit",
			primaryPages: []db.WikiPage{{ID: "page-1"}},
			setupLinks: func(s *testStore) {
				s.pages["page-1"] = &db.WikiPage{ID: "page-1", Title: "Page 1", Content: "Content"}
				for i := 2; i <= 10; i++ {
					id := fmt.Sprintf("page-%d", i)
					s.pages[id] = &db.WikiPage{ID: id, Title: id, Content: "Content"}
					s.links[id] = []db.WikiLink{{ID: fmt.Sprintf("link-%d", i), FromPageID: "page-1", ToPageID: id}}
				}
			},
			wantExpanded: 5,
			wantContains: []string{},
		},
		{
			name:         "both incoming and outgoing links",
			primaryPages: []db.WikiPage{{ID: "page-2"}},
			setupLinks: func(s *testStore) {
				s.pages["page-1"] = &db.WikiPage{ID: "page-1", Title: "Page 1", Content: "Content"}
				s.pages["page-2"] = &db.WikiPage{ID: "page-2", Title: "Page 2", Content: "Content"}
				s.pages["page-3"] = &db.WikiPage{ID: "page-3", Title: "Page 3", Content: "Content"}
				s.links["page-2"] = []db.WikiLink{{ID: "link-1", FromPageID: "page-1", ToPageID: "page-2"}}
				s.links["page-3"] = []db.WikiLink{{ID: "link-2", FromPageID: "page-2", ToPageID: "page-3"}}
			},
			wantExpanded: 2,
			wantContains: []string{"page-1", "page-3"},
		},
		{
			name:         "multiple primary pages with overlapping links",
			primaryPages: []db.WikiPage{{ID: "page-1"}, {ID: "page-2"}},
			setupLinks: func(s *testStore) {
				s.pages["page-1"] = &db.WikiPage{ID: "page-1", Title: "Page 1", Content: "Content"}
				s.pages["page-2"] = &db.WikiPage{ID: "page-2", Title: "Page 2", Content: "Content"}
				s.pages["page-3"] = &db.WikiPage{ID: "page-3", Title: "Page 3", Content: "Content"}
				s.pages["page-4"] = &db.WikiPage{ID: "page-4", Title: "Page 4", Content: "Content"}
				s.links["page-3"] = []db.WikiLink{{ID: "link-1", FromPageID: "page-1", ToPageID: "page-3"}}
				s.links["page-4"] = []db.WikiLink{{ID: "link-2", FromPageID: "page-2", ToPageID: "page-4"}}
			},
			wantExpanded: 2,
			wantContains: []string{"page-3", "page-4"},
		},
		{
			name:         "missing page in expansion - skipped",
			primaryPages: []db.WikiPage{{ID: "page-1"}},
			setupLinks: func(s *testStore) {
				s.pages["page-1"] = &db.WikiPage{ID: "page-1", Title: "Page 1", Content: "Content"}
				s.links["page-2"] = []db.WikiLink{{ID: "link-1", FromPageID: "page-1", ToPageID: "page-2"}}
			},
			wantExpanded: 0,
			wantContains: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore()
			s := &Synthesizer{store: store, llmClient: &mockLLM{}}
			tc.setupLinks(store)
			got := s.expandLinkedPages(context.Background(), tc.primaryPages, 5)
			if len(got) != tc.wantExpanded {
				t.Errorf("got %d expanded pages, want %d", len(got), tc.wantExpanded)
			}
			for _, wantID := range tc.wantContains {
				found := false
				for _, p := range got {
					if p.ID == wantID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expanded pages missing %q, got %v", wantID, gotIDs(got))
				}
			}
		})
	}
}

func gotIDs(pages []db.WikiPage) []string {
	ids := make([]string, len(pages))
	for i, p := range pages {
		ids[i] = p.ID
	}
	return ids
}
