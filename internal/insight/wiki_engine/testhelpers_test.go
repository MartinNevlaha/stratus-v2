package wiki_engine_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

// ---------------------------------------------------------------------------
// mockLLM — shared LLMClient mock.
// ---------------------------------------------------------------------------

type mockLLM struct {
	response string
	err      error
	calls    int
}

func (m *mockLLM) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return &llm.CompletionResponse{Content: m.response}, nil
}

func (m *mockLLM) Provider() string { return "mock" }
func (m *mockLLM) Model() string    { return "mock-model" }

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

func defaultConfig() func() config.WikiConfig {
	return func() config.WikiConfig {
		return config.WikiConfig{
			Enabled:            true,
			MaxPagesPerIngest:  20,
			StalenessThreshold: 0.7,
		}
	}
}

// pageUpdatedDaysAgo returns a page whose UpdatedAt is set n days in the past.
func pageUpdatedDaysAgo(id string, days int, version int) db.WikiPage {
	updatedAt := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339Nano)
	return db.WikiPage{
		ID:        id,
		PageType:  "summary",
		Title:     id,
		Content:   "content",
		Status:    "published",
		Version:   version,
		UpdatedAt: updatedAt,
		CreatedAt: updatedAt,
	}
}

// ---------------------------------------------------------------------------
// DB helpers
// ---------------------------------------------------------------------------

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// ---------------------------------------------------------------------------
// mockStore — shared WikiStore mock used by linker_test.go and synthesizer_test.go.
//
// Fields prefixed with "search" allow synthesizer tests to inject search
// results; the "savedLinks" slice lets linker tests observe SaveLink calls.
// ---------------------------------------------------------------------------

type mockStore struct {
	pages           map[string]*db.WikiPage
	links           map[string][]db.WikiLink // keyed by ToPageID for ListLinksTo
	refs            map[string][]db.WikiPageRef
	stalenessScores map[string]float64
	stalenessStatus map[string]string
	savedLinks      []db.WikiLink // ordered SaveLink captures

	// error injection
	listPagesErr       error
	listLinksToErr     error
	updateStalenessErr error
	savePageErr        error
	saveRefErr         error
	saveErr            error // SaveLink error

	// search overrides (synthesizer tests)
	searchResult []db.WikiPage
	searchErr    error
}

func newMockStore() *mockStore {
	return &mockStore{
		pages:           make(map[string]*db.WikiPage),
		links:           make(map[string][]db.WikiLink),
		refs:            make(map[string][]db.WikiPageRef),
		stalenessScores: make(map[string]float64),
		stalenessStatus: make(map[string]string),
	}
}

func (s *mockStore) SavePage(p *db.WikiPage) error {
	if s.savePageErr != nil {
		return s.savePageErr
	}
	if p.ID == "" {
		p.ID = fmt.Sprintf("page-%d", len(s.pages)+1)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.Version == 0 {
		p.Version = 1
	}
	clone := *p
	if s.pages == nil {
		s.pages = make(map[string]*db.WikiPage)
	}
	s.pages[p.ID] = &clone
	return nil
}

func (s *mockStore) UpdatePage(p *db.WikiPage) error {
	if _, ok := s.pages[p.ID]; !ok {
		return fmt.Errorf("page not found: %s", p.ID)
	}
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	p.Version++
	clone := *p
	s.pages[p.ID] = &clone
	return nil
}

func (s *mockStore) GetPage(id string) (*db.WikiPage, error) {
	p, ok := s.pages[id]
	if !ok {
		return nil, nil
	}
	clone := *p
	return &clone, nil
}

func (s *mockStore) ListPages(f db.WikiPageFilters) ([]db.WikiPage, int, error) {
	if s.listPagesErr != nil {
		return nil, 0, s.listPagesErr
	}
	var out []db.WikiPage
	for _, p := range s.pages {
		if f.Status != "" && p.Status != f.Status {
			continue
		}
		if f.PageType != "" && p.PageType != f.PageType {
			continue
		}
		out = append(out, *p)
	}
	return out, len(out), nil
}

func (s *mockStore) SearchPages(_ string, _ string, _ int) ([]db.WikiPage, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	if s.searchResult != nil {
		return s.searchResult, nil
	}
	return nil, nil
}

func (s *mockStore) DeletePage(id string) error {
	delete(s.pages, id)
	return nil
}

func (s *mockStore) UpdatePageStaleness(id string, score float64) error {
	if s.updateStalenessErr != nil {
		return s.updateStalenessErr
	}
	if s.stalenessScores == nil {
		s.stalenessScores = make(map[string]float64)
	}
	if s.stalenessStatus == nil {
		s.stalenessStatus = make(map[string]string)
	}
	s.stalenessScores[id] = score
	status := "published"
	if score >= 0.7 {
		status = "stale"
	}
	s.stalenessStatus[id] = status
	return nil
}

func (s *mockStore) SaveLink(l *db.WikiLink) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	if l.ID == "" {
		l.ID = fmt.Sprintf("link-%d", len(s.savedLinks)+1)
	}
	s.savedLinks = append(s.savedLinks, *l)
	if s.links == nil {
		s.links = make(map[string][]db.WikiLink)
	}
	s.links[l.ToPageID] = append(s.links[l.ToPageID], *l)
	return nil
}

func (s *mockStore) ListLinksFrom(_ string) ([]db.WikiLink, error) {
	return nil, nil
}

func (s *mockStore) ListLinksTo(pageID string) ([]db.WikiLink, error) {
	if s.listLinksToErr != nil {
		return nil, s.listLinksToErr
	}
	if s.links == nil {
		return nil, nil
	}
	return s.links[pageID], nil
}

func (s *mockStore) DeleteLinks(pageID string) error {
	if s.links != nil {
		delete(s.links, pageID)
	}
	return nil
}

func (s *mockStore) GetGraph(_ string, _ int) ([]db.WikiPage, []db.WikiLink, error) {
	return nil, nil, nil
}

func (s *mockStore) SaveRef(r *db.WikiPageRef) error {
	if s.saveRefErr != nil {
		return s.saveRefErr
	}
	if r.ID == "" {
		r.ID = fmt.Sprintf("ref-%d", len(s.refs)+1)
	}
	if s.refs == nil {
		s.refs = make(map[string][]db.WikiPageRef)
	}
	s.refs[r.PageID] = append(s.refs[r.PageID], *r)
	return nil
}

func (s *mockStore) ListRefs(pageID string) ([]db.WikiPageRef, error) {
	if s.refs == nil {
		return nil, nil
	}
	return s.refs[pageID], nil
}

func (s *mockStore) DeleteRefs(pageID string) error {
	if s.refs != nil {
		delete(s.refs, pageID)
	}
	return nil
}

func (s *mockStore) FindWikiPageByTitleNewest(title string) (*db.WikiPage, error) {
	var best *db.WikiPage
	for _, p := range s.pages {
		if strings.EqualFold(p.Title, title) {
			if best == nil || p.UpdatedAt > best.UpdatedAt {
				clone := *p
				best = &clone
			}
		}
	}
	return best, nil
}
