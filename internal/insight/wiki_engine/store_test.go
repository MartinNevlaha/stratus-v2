package wiki_engine_test

import (
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
)

// TestNewDBWikiStore_ImplementsInterface verifies that *DBWikiStore satisfies WikiStore.
func TestNewDBWikiStore_ImplementsInterface(t *testing.T) {
	database := newTestDB(t)
	var _ wiki_engine.WikiStore = wiki_engine.NewDBWikiStore(database)
}

// TestDBWikiStore_SavePage_AndGetPage verifies round-trip persistence of a page.
func TestDBWikiStore_SavePage_AndGetPage(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	page := &db.WikiPage{
		PageType:    "summary",
		Title:       "Test Page",
		Content:     "Hello world",
		Status:      "published",
		GeneratedBy: "ingest",
	}

	if err := store.SavePage(page); err != nil {
		t.Fatalf("SavePage: %v", err)
	}
	if page.ID == "" {
		t.Fatal("expected ID to be populated after SavePage")
	}

	got, err := store.GetPage(page.ID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if got == nil {
		t.Fatal("expected page, got nil")
	}
	if got.Title != page.Title {
		t.Errorf("title: want %q, got %q", page.Title, got.Title)
	}
	if got.Content != page.Content {
		t.Errorf("content: want %q, got %q", page.Content, got.Content)
	}
}

// TestDBWikiStore_GetPage_NotFound verifies nil is returned for missing pages.
func TestDBWikiStore_GetPage_NotFound(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	got, err := store.GetPage("nonexistent-id")
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing page, got %+v", got)
	}
}

// TestDBWikiStore_UpdatePage verifies content is updated and version incremented.
func TestDBWikiStore_UpdatePage_UpdatesContent(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	page := &db.WikiPage{
		PageType:    "concept",
		Title:       "Original",
		Content:     "old content",
		Status:      "published",
		GeneratedBy: "ingest",
	}
	if err := store.SavePage(page); err != nil {
		t.Fatalf("SavePage: %v", err)
	}

	page.Content = "updated content"
	if err := store.UpdatePage(page); err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}

	got, err := store.GetPage(page.ID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if got.Content != "updated content" {
		t.Errorf("content: want %q, got %q", "updated content", got.Content)
	}
	if got.Version < 2 {
		t.Errorf("expected version >= 2, got %d", got.Version)
	}
}

// TestDBWikiStore_ListPages_FiltersAndPagination verifies filtering by type.
func TestDBWikiStore_ListPages_FiltersAndPagination(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	for i := 0; i < 3; i++ {
		p := &db.WikiPage{PageType: "summary", Title: "S", Content: "c", Status: "published", GeneratedBy: "ingest"}
		if err := store.SavePage(p); err != nil {
			t.Fatalf("SavePage: %v", err)
		}
	}
	entity := &db.WikiPage{PageType: "entity", Title: "E", Content: "c", Status: "published", GeneratedBy: "ingest"}
	if err := store.SavePage(entity); err != nil {
		t.Fatalf("SavePage: %v", err)
	}

	pages, total, err := store.ListPages(db.WikiPageFilters{PageType: "summary", Limit: 10})
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if total != 3 {
		t.Errorf("total: want 3, got %d", total)
	}
	if len(pages) != 3 {
		t.Errorf("len(pages): want 3, got %d", len(pages))
	}
}

// TestDBWikiStore_SearchPages_ReturnsMatches verifies FTS search finds a page.
func TestDBWikiStore_SearchPages_ReturnsMatches(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	p := &db.WikiPage{
		PageType:    "concept",
		Title:       "Distributed Systems",
		Content:     "Consensus algorithms in distributed computing",
		Status:      "published",
		GeneratedBy: "ingest",
	}
	if err := store.SavePage(p); err != nil {
		t.Fatalf("SavePage: %v", err)
	}

	results, err := store.SearchPages("distributed", "", 10)
	if err != nil {
		t.Fatalf("SearchPages: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result")
	}
}

// TestDBWikiStore_DeletePage_RemovesPage verifies the page is gone after deletion.
func TestDBWikiStore_DeletePage_RemovesPage(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	p := &db.WikiPage{PageType: "summary", Title: "T", Content: "c", Status: "published", GeneratedBy: "ingest"}
	if err := store.SavePage(p); err != nil {
		t.Fatalf("SavePage: %v", err)
	}

	if err := store.DeletePage(p.ID); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}

	got, err := store.GetPage(p.ID)
	if err != nil {
		t.Fatalf("GetPage after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after deletion, got %+v", got)
	}
}

// TestDBWikiStore_UpdatePageStaleness_SetsScore verifies staleness score is updated.
func TestDBWikiStore_UpdatePageStaleness_SetsScore(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	p := &db.WikiPage{PageType: "summary", Title: "T", Content: "c", Status: "published", GeneratedBy: "ingest"}
	if err := store.SavePage(p); err != nil {
		t.Fatalf("SavePage: %v", err)
	}

	if err := store.UpdatePageStaleness(p.ID, 0.9); err != nil {
		t.Fatalf("UpdatePageStaleness: %v", err)
	}

	got, err := store.GetPage(p.ID)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if got.StalenessScore != 0.9 {
		t.Errorf("staleness_score: want 0.9, got %f", got.StalenessScore)
	}
	if got.Status != "stale" {
		t.Errorf("status: want stale, got %q", got.Status)
	}
}

// TestDBWikiStore_SaveLink_AndListLinksFrom verifies link persistence.
func TestDBWikiStore_SaveLink_AndListLinksFrom(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	p1 := &db.WikiPage{PageType: "summary", Title: "A", Content: "c", Status: "published", GeneratedBy: "ingest"}
	p2 := &db.WikiPage{PageType: "summary", Title: "B", Content: "c", Status: "published", GeneratedBy: "ingest"}
	if err := store.SavePage(p1); err != nil {
		t.Fatalf("SavePage p1: %v", err)
	}
	if err := store.SavePage(p2); err != nil {
		t.Fatalf("SavePage p2: %v", err)
	}

	link := &db.WikiLink{
		FromPageID: p1.ID,
		ToPageID:   p2.ID,
		LinkType:   "related",
		Strength:   0.8,
	}
	if err := store.SaveLink(link); err != nil {
		t.Fatalf("SaveLink: %v", err)
	}
	if link.ID == "" {
		t.Fatal("expected ID to be populated after SaveLink")
	}

	links, err := store.ListLinksFrom(p1.ID)
	if err != nil {
		t.Fatalf("ListLinksFrom: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links): want 1, got %d", len(links))
	}
	if links[0].ToPageID != p2.ID {
		t.Errorf("ToPageID: want %q, got %q", p2.ID, links[0].ToPageID)
	}
}

// TestDBWikiStore_ListLinksTo_ReturnsIncoming verifies incoming link lookup.
func TestDBWikiStore_ListLinksTo_ReturnsIncoming(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	p1 := &db.WikiPage{PageType: "summary", Title: "A", Content: "c", Status: "published", GeneratedBy: "ingest"}
	p2 := &db.WikiPage{PageType: "summary", Title: "B", Content: "c", Status: "published", GeneratedBy: "ingest"}
	if err := store.SavePage(p1); err != nil {
		t.Fatalf("SavePage p1: %v", err)
	}
	if err := store.SavePage(p2); err != nil {
		t.Fatalf("SavePage p2: %v", err)
	}

	link := &db.WikiLink{FromPageID: p1.ID, ToPageID: p2.ID, LinkType: "child", Strength: 0.5}
	if err := store.SaveLink(link); err != nil {
		t.Fatalf("SaveLink: %v", err)
	}

	links, err := store.ListLinksTo(p2.ID)
	if err != nil {
		t.Fatalf("ListLinksTo: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links): want 1, got %d", len(links))
	}
	if links[0].FromPageID != p1.ID {
		t.Errorf("FromPageID: want %q, got %q", p1.ID, links[0].FromPageID)
	}
}

// TestDBWikiStore_DeleteLinks_RemovesLinks verifies links are removed.
func TestDBWikiStore_DeleteLinks_RemovesLinks(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	p1 := &db.WikiPage{PageType: "summary", Title: "A", Content: "c", Status: "published", GeneratedBy: "ingest"}
	p2 := &db.WikiPage{PageType: "summary", Title: "B", Content: "c", Status: "published", GeneratedBy: "ingest"}
	if err := store.SavePage(p1); err != nil {
		t.Fatalf("SavePage p1: %v", err)
	}
	if err := store.SavePage(p2); err != nil {
		t.Fatalf("SavePage p2: %v", err)
	}

	link := &db.WikiLink{FromPageID: p1.ID, ToPageID: p2.ID, LinkType: "related", Strength: 0.5}
	if err := store.SaveLink(link); err != nil {
		t.Fatalf("SaveLink: %v", err)
	}

	if err := store.DeleteLinks(p1.ID); err != nil {
		t.Fatalf("DeleteLinks: %v", err)
	}

	links, err := store.ListLinksFrom(p1.ID)
	if err != nil {
		t.Fatalf("ListLinksFrom: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected 0 links after deletion, got %d", len(links))
	}
}

// TestDBWikiStore_GetGraph_ReturnsPagesAndLinks verifies graph retrieval.
func TestDBWikiStore_GetGraph_ReturnsPagesAndLinks(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	p1 := &db.WikiPage{PageType: "concept", Title: "A", Content: "c", Status: "published", GeneratedBy: "ingest"}
	p2 := &db.WikiPage{PageType: "concept", Title: "B", Content: "c", Status: "published", GeneratedBy: "ingest"}
	if err := store.SavePage(p1); err != nil {
		t.Fatalf("SavePage p1: %v", err)
	}
	if err := store.SavePage(p2); err != nil {
		t.Fatalf("SavePage p2: %v", err)
	}

	link := &db.WikiLink{FromPageID: p1.ID, ToPageID: p2.ID, LinkType: "related", Strength: 1.0}
	if err := store.SaveLink(link); err != nil {
		t.Fatalf("SaveLink: %v", err)
	}

	pages, links, err := store.GetGraph("concept", 50)
	if err != nil {
		t.Fatalf("GetGraph: %v", err)
	}
	if len(pages) != 2 {
		t.Errorf("len(pages): want 2, got %d", len(pages))
	}
	if len(links) != 1 {
		t.Errorf("len(links): want 1, got %d", len(links))
	}
}

// TestDBWikiStore_SaveRef_AndListRefs verifies ref persistence.
func TestDBWikiStore_SaveRef_AndListRefs(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	p := &db.WikiPage{PageType: "summary", Title: "T", Content: "c", Status: "published", GeneratedBy: "ingest"}
	if err := store.SavePage(p); err != nil {
		t.Fatalf("SavePage: %v", err)
	}

	ref := &db.WikiPageRef{
		PageID:     p.ID,
		SourceType: "event",
		SourceID:   "evt-123",
		Excerpt:    "relevant snippet",
	}
	if err := store.SaveRef(ref); err != nil {
		t.Fatalf("SaveRef: %v", err)
	}
	if ref.ID == "" {
		t.Fatal("expected ID to be populated after SaveRef")
	}

	refs, err := store.ListRefs(p.ID)
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("len(refs): want 1, got %d", len(refs))
	}
	if refs[0].SourceID != "evt-123" {
		t.Errorf("SourceID: want %q, got %q", "evt-123", refs[0].SourceID)
	}
}

// TestDBWikiStore_DeleteRefs_RemovesRefs verifies refs are removed.
func TestDBWikiStore_DeleteRefs_RemovesRefs(t *testing.T) {
	store := wiki_engine.NewDBWikiStore(newTestDB(t))

	p := &db.WikiPage{PageType: "summary", Title: "T", Content: "c", Status: "published", GeneratedBy: "ingest"}
	if err := store.SavePage(p); err != nil {
		t.Fatalf("SavePage: %v", err)
	}

	ref := &db.WikiPageRef{PageID: p.ID, SourceType: "event", SourceID: "evt-1", Excerpt: "x"}
	if err := store.SaveRef(ref); err != nil {
		t.Fatalf("SaveRef: %v", err)
	}

	if err := store.DeleteRefs(p.ID); err != nil {
		t.Fatalf("DeleteRefs: %v", err)
	}

	refs, err := store.ListRefs(p.ID)
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs after deletion, got %d", len(refs))
	}
}
