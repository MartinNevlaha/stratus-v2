package wiki_engine

import "github.com/MartinNevlaha/stratus-v2/db"

// WikiStore abstracts wiki persistence for testability.
type WikiStore interface {
	SavePage(p *db.WikiPage) error
	UpdatePage(p *db.WikiPage) error
	GetPage(id string) (*db.WikiPage, error)
	ListPages(f db.WikiPageFilters) ([]db.WikiPage, int, error)
	SearchPages(query string, pageType string, limit int) ([]db.WikiPage, error)
	DeletePage(id string) error
	UpdatePageStaleness(id string, score float64) error

	SaveLink(l *db.WikiLink) error
	ListLinksFrom(pageID string) ([]db.WikiLink, error)
	ListLinksTo(pageID string) ([]db.WikiLink, error)
	DeleteLinks(pageID string) error
	GetGraph(pageType string, limit int) ([]db.WikiPage, []db.WikiLink, error)

	SaveRef(r *db.WikiPageRef) error
	ListRefs(pageID string) ([]db.WikiPageRef, error)
	DeleteRefs(pageID string) error
}

// DBWikiStore delegates to *db.DB.
type DBWikiStore struct {
	db *db.DB
}

// NewDBWikiStore returns a DBWikiStore backed by the given database connection.
func NewDBWikiStore(database *db.DB) *DBWikiStore {
	return &DBWikiStore{db: database}
}

// --- page methods ---

func (s *DBWikiStore) SavePage(p *db.WikiPage) error {
	return s.db.SaveWikiPage(p)
}

func (s *DBWikiStore) UpdatePage(p *db.WikiPage) error {
	return s.db.UpdateWikiPage(p)
}

func (s *DBWikiStore) GetPage(id string) (*db.WikiPage, error) {
	return s.db.GetWikiPage(id)
}

func (s *DBWikiStore) ListPages(f db.WikiPageFilters) ([]db.WikiPage, int, error) {
	return s.db.ListWikiPages(f)
}

func (s *DBWikiStore) SearchPages(query string, pageType string, limit int) ([]db.WikiPage, error) {
	return s.db.SearchWikiPages(query, pageType, limit)
}

func (s *DBWikiStore) DeletePage(id string) error {
	return s.db.DeleteWikiPage(id)
}

func (s *DBWikiStore) UpdatePageStaleness(id string, score float64) error {
	return s.db.UpdateWikiPageStaleness(id, score)
}

// --- link methods ---

func (s *DBWikiStore) SaveLink(l *db.WikiLink) error {
	return s.db.SaveWikiLink(l)
}

func (s *DBWikiStore) ListLinksFrom(pageID string) ([]db.WikiLink, error) {
	return s.db.ListWikiLinksFrom(pageID)
}

func (s *DBWikiStore) ListLinksTo(pageID string) ([]db.WikiLink, error) {
	return s.db.ListWikiLinksTo(pageID)
}

func (s *DBWikiStore) DeleteLinks(pageID string) error {
	return s.db.DeleteWikiLinks(pageID)
}

func (s *DBWikiStore) GetGraph(pageType string, limit int) ([]db.WikiPage, []db.WikiLink, error) {
	return s.db.GetWikiGraph(pageType, limit)
}

// --- ref methods ---

func (s *DBWikiStore) SaveRef(r *db.WikiPageRef) error {
	return s.db.SaveWikiPageRef(r)
}

func (s *DBWikiStore) ListRefs(pageID string) ([]db.WikiPageRef, error) {
	return s.db.ListWikiPageRefs(pageID)
}

func (s *DBWikiStore) DeleteRefs(pageID string) error {
	return s.db.DeleteWikiPageRefs(pageID)
}
