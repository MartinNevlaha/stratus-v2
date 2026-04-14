package db

import (
	"context"
	"testing"
)

// --- SaveWikiPage / GetWikiPage ---

func TestSaveWikiPage_RoundTrip(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{
		PageType:     "concept",
		Title:        "Dependency Injection",
		Content:      "DI is a design pattern...",
		Status:       "published",
		SourceHashes: []string{"abc123", "def456"},
		Tags:         []string{"go", "patterns"},
		Metadata:     map[string]any{"author": "test"},
		GeneratedBy:  "ingest",
		Version:      1,
	}

	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}
	if page.ID == "" {
		t.Fatal("expected ID to be set after SaveWikiPage")
	}

	got, err := database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got == nil {
		t.Fatal("expected page, got nil")
	}

	if got.Title != page.Title {
		t.Errorf("Title: got %q, want %q", got.Title, page.Title)
	}
	if got.PageType != page.PageType {
		t.Errorf("PageType: got %q, want %q", got.PageType, page.PageType)
	}
	if got.Status != page.Status {
		t.Errorf("Status: got %q, want %q", got.Status, page.Status)
	}
	if got.Content != page.Content {
		t.Errorf("Content: got %q, want %q", got.Content, page.Content)
	}
	if len(got.SourceHashes) != 2 || got.SourceHashes[0] != "abc123" {
		t.Errorf("SourceHashes: got %v", got.SourceHashes)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "go" {
		t.Errorf("Tags: got %v", got.Tags)
	}
	if got.Metadata["author"] != "test" {
		t.Errorf("Metadata: got %v", got.Metadata)
	}
	if got.Version != 1 {
		t.Errorf("Version: got %d, want 1", got.Version)
	}
	if got.CreatedAt == "" {
		t.Error("expected CreatedAt to be set")
	}
}

func TestGetWikiPage_NotFound(t *testing.T) {
	database := openTestDB(t)

	got, err := database.GetWikiPage("nonexistent-id")
	if err != nil {
		t.Fatalf("expected nil error for missing page, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing page, got %+v", got)
	}
}

func TestSaveWikiPage_GeneratesID(t *testing.T) {
	database := openTestDB(t)

	p1 := &WikiPage{Title: "Page 1", PageType: "summary", Status: "draft", GeneratedBy: "ingest"}
	p2 := &WikiPage{Title: "Page 2", PageType: "summary", Status: "draft", GeneratedBy: "ingest"}

	if err := database.SaveWikiPage(p1); err != nil {
		t.Fatalf("SaveWikiPage p1: %v", err)
	}
	if err := database.SaveWikiPage(p2); err != nil {
		t.Fatalf("SaveWikiPage p2: %v", err)
	}

	if p1.ID == "" || p2.ID == "" {
		t.Fatal("expected IDs to be generated")
	}
	if p1.ID == p2.ID {
		t.Fatal("expected unique IDs")
	}
}

// --- UpdateWikiPage ---

func TestUpdateWikiPage_IncrementsVersion(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{
		PageType:    "answer",
		Title:       "What is TDD?",
		Content:     "Original content",
		Status:      "draft",
		GeneratedBy: "query",
		Version:     1,
	}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	page.Content = "Updated content"
	page.Status = "published"
	page.Tags = []string{"tdd", "testing"}
	page.Metadata = map[string]any{"reviewed": true}

	if err := database.UpdateWikiPage(page); err != nil {
		t.Fatalf("UpdateWikiPage: %v", err)
	}

	got, err := database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage after update: %v", err)
	}
	if got == nil {
		t.Fatal("page not found after update")
	}

	if got.Version != 2 {
		t.Errorf("Version: got %d, want 2", got.Version)
	}
	if got.Content != "Updated content" {
		t.Errorf("Content: got %q, want %q", got.Content, "Updated content")
	}
	if got.Status != "published" {
		t.Errorf("Status: got %q, want published", got.Status)
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags: got %v", got.Tags)
	}
}

// --- ListWikiPages ---

func TestListWikiPages_NoFilters(t *testing.T) {
	database := openTestDB(t)

	for i := 0; i < 3; i++ {
		p := &WikiPage{PageType: "summary", Title: "Page", Status: "published", GeneratedBy: "ingest"}
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	pages, total, err := database.ListWikiPages(WikiPageFilters{})
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	if total != 3 {
		t.Errorf("total: got %d, want 3", total)
	}
	if len(pages) != 3 {
		t.Errorf("len(pages): got %d, want 3", len(pages))
	}
}

func TestListWikiPages_FilterByPageType(t *testing.T) {
	database := openTestDB(t)

	types := []string{"summary", "concept", "concept", "entity"}
	for _, pt := range types {
		p := &WikiPage{PageType: pt, Title: "T", Status: "published", GeneratedBy: "ingest"}
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	pages, total, err := database.ListWikiPages(WikiPageFilters{PageType: "concept"})
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
	if len(pages) != 2 {
		t.Errorf("len(pages): got %d, want 2", len(pages))
	}
}

func TestListWikiPages_FilterByStatus(t *testing.T) {
	database := openTestDB(t)

	statuses := []string{"draft", "published", "published", "stale"}
	for _, s := range statuses {
		p := &WikiPage{PageType: "summary", Title: "T", Status: s, GeneratedBy: "ingest"}
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	pages, total, err := database.ListWikiPages(WikiPageFilters{Status: "published"})
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
	if len(pages) != 2 {
		t.Errorf("len(pages): got %d, want 2", len(pages))
	}
}

func TestListWikiPages_FilterByTag(t *testing.T) {
	database := openTestDB(t)

	p1 := &WikiPage{PageType: "summary", Title: "T1", Status: "published", GeneratedBy: "ingest", Tags: []string{"go", "api"}}
	p2 := &WikiPage{PageType: "summary", Title: "T2", Status: "published", GeneratedBy: "ingest", Tags: []string{"python"}}
	p3 := &WikiPage{PageType: "summary", Title: "T3", Status: "published", GeneratedBy: "ingest", Tags: []string{"go", "db"}}

	for _, p := range []*WikiPage{p1, p2, p3} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	pages, total, err := database.ListWikiPages(WikiPageFilters{Tag: "go"})
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
	if len(pages) != 2 {
		t.Errorf("len(pages): got %d, want 2", len(pages))
	}
}

func TestListWikiPages_DefaultLimit(t *testing.T) {
	database := openTestDB(t)

	// Limit defaults to 50 when zero.
	_, _, err := database.ListWikiPages(WikiPageFilters{Limit: 0})
	if err != nil {
		t.Fatalf("ListWikiPages with zero limit: %v", err)
	}
}

func TestListWikiPages_Empty(t *testing.T) {
	database := openTestDB(t)

	pages, total, err := database.ListWikiPages(WikiPageFilters{})
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	if total != 0 {
		t.Errorf("total: got %d, want 0", total)
	}
	if len(pages) != 0 {
		t.Errorf("len(pages): got %d, want 0", len(pages))
	}
}

// --- SearchWikiPages (FTS5) ---

func TestSearchWikiPages_FTS5Match(t *testing.T) {
	database := openTestDB(t)

	pages := []*WikiPage{
		{PageType: "concept", Title: "Dependency Injection", Content: "A software design pattern", Status: "published", GeneratedBy: "ingest"},
		{PageType: "concept", Title: "Interface Segregation", Content: "SOLID principles for interfaces", Status: "published", GeneratedBy: "ingest"},
		{PageType: "summary", Title: "Go Modules", Content: "Module system for Go", Status: "published", GeneratedBy: "ingest"},
	}
	for _, p := range pages {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	results, err := database.SearchWikiPages("dependency", "", 10)
	if err != nil {
		t.Fatalf("SearchWikiPages: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if len(results) > 0 && results[0].Title != "Dependency Injection" {
		t.Errorf("unexpected result: %q", results[0].Title)
	}
}

func TestSearchWikiPages_FilterByPageType(t *testing.T) {
	database := openTestDB(t)

	pages := []*WikiPage{
		{PageType: "concept", Title: "Interface Basics", Content: "Go interfaces explained", Status: "published", GeneratedBy: "ingest"},
		{PageType: "summary", Title: "Interface Overview", Content: "A summary of interfaces", Status: "published", GeneratedBy: "ingest"},
	}
	for _, p := range pages {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	results, err := database.SearchWikiPages("interface", "concept", 10)
	if err != nil {
		t.Fatalf("SearchWikiPages: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result filtered by type, got %d", len(results))
	}
	if len(results) > 0 && results[0].PageType != "concept" {
		t.Errorf("wrong page_type returned: %q", results[0].PageType)
	}
}

func TestSearchWikiPages_EmptyQuery(t *testing.T) {
	database := openTestDB(t)

	results, err := database.SearchWikiPages("", "", 10)
	if err != nil {
		t.Fatalf("SearchWikiPages empty query: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

// --- DeleteWikiPage (cascade) ---

func TestDeleteWikiPage_CascadesToLinksAndRefs(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{PageType: "summary", Title: "To Delete", Status: "draft", GeneratedBy: "ingest"}
	other := &WikiPage{PageType: "summary", Title: "Other", Status: "draft", GeneratedBy: "ingest"}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage page: %v", err)
	}
	if err := database.SaveWikiPage(other); err != nil {
		t.Fatalf("SaveWikiPage other: %v", err)
	}

	link := &WikiLink{FromPageID: page.ID, ToPageID: other.ID, LinkType: "related", Strength: 0.5}
	if err := database.SaveWikiLink(link); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}

	ref := &WikiPageRef{PageID: page.ID, SourceType: "event", SourceID: "evt-1", Excerpt: "x"}
	if err := database.SaveWikiPageRef(ref); err != nil {
		t.Fatalf("SaveWikiPageRef: %v", err)
	}

	if err := database.DeleteWikiPage(page.ID); err != nil {
		t.Fatalf("DeleteWikiPage: %v", err)
	}

	got, err := database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage after delete: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after delete, got page")
	}

	links, err := database.ListWikiLinksFrom(page.ID)
	if err != nil {
		t.Fatalf("ListWikiLinksFrom after delete: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected 0 links after cascade delete, got %d", len(links))
	}

	refs, err := database.ListWikiPageRefs(page.ID)
	if err != nil {
		t.Fatalf("ListWikiPageRefs after delete: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs after cascade delete, got %d", len(refs))
	}
}

// --- UpdateWikiPageStaleness ---

func TestUpdateWikiPageStaleness_BelowThreshold(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{PageType: "summary", Title: "Fresh", Status: "published", GeneratedBy: "ingest"}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	if err := database.UpdateWikiPageStaleness(page.ID, 0.3); err != nil {
		t.Fatalf("UpdateWikiPageStaleness: %v", err)
	}

	got, err := database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got.Status != "published" {
		t.Errorf("Status: got %q, want published", got.Status)
	}
	if got.StalenessScore != 0.3 {
		t.Errorf("StalenessScore: got %f, want 0.3", got.StalenessScore)
	}
}

func TestUpdateWikiPageStaleness_AboveThreshold_SetsStale(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{PageType: "summary", Title: "Stale", Status: "published", GeneratedBy: "ingest"}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	if err := database.UpdateWikiPageStaleness(page.ID, 0.9); err != nil {
		t.Fatalf("UpdateWikiPageStaleness: %v", err)
	}

	got, err := database.GetWikiPage(page.ID)
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got.Status != "stale" {
		t.Errorf("Status: got %q, want stale", got.Status)
	}
	if got.StalenessScore != 0.9 {
		t.Errorf("StalenessScore: got %f, want 0.9", got.StalenessScore)
	}
}

// --- SaveWikiLink / ListWikiLinksFrom / ListWikiLinksTo ---

func TestSaveWikiLink_AndListFromTo(t *testing.T) {
	database := openTestDB(t)

	p1 := &WikiPage{PageType: "concept", Title: "A", Status: "published", GeneratedBy: "ingest"}
	p2 := &WikiPage{PageType: "concept", Title: "B", Status: "published", GeneratedBy: "ingest"}
	p3 := &WikiPage{PageType: "concept", Title: "C", Status: "published", GeneratedBy: "ingest"}

	for _, p := range []*WikiPage{p1, p2, p3} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	l1 := &WikiLink{FromPageID: p1.ID, ToPageID: p2.ID, LinkType: "related", Strength: 0.8}
	l2 := &WikiLink{FromPageID: p1.ID, ToPageID: p3.ID, LinkType: "cites", Strength: 0.6}
	l3 := &WikiLink{FromPageID: p3.ID, ToPageID: p2.ID, LinkType: "parent", Strength: 0.9}

	for _, l := range []*WikiLink{l1, l2, l3} {
		if err := database.SaveWikiLink(l); err != nil {
			t.Fatalf("SaveWikiLink: %v", err)
		}
	}

	from1, err := database.ListWikiLinksFrom(p1.ID)
	if err != nil {
		t.Fatalf("ListWikiLinksFrom p1: %v", err)
	}
	if len(from1) != 2 {
		t.Errorf("expected 2 links from p1, got %d", len(from1))
	}

	to2, err := database.ListWikiLinksTo(p2.ID)
	if err != nil {
		t.Fatalf("ListWikiLinksTo p2: %v", err)
	}
	if len(to2) != 2 {
		t.Errorf("expected 2 links to p2, got %d", len(to2))
	}

	from3, err := database.ListWikiLinksFrom(p3.ID)
	if err != nil {
		t.Fatalf("ListWikiLinksFrom p3: %v", err)
	}
	if len(from3) != 1 {
		t.Errorf("expected 1 link from p3, got %d", len(from3))
	}
}

func TestSaveWikiLink_UpsertUpdatesStrength(t *testing.T) {
	database := openTestDB(t)

	p1 := &WikiPage{PageType: "summary", Title: "A", Status: "published", GeneratedBy: "ingest"}
	p2 := &WikiPage{PageType: "summary", Title: "B", Status: "published", GeneratedBy: "ingest"}
	for _, p := range []*WikiPage{p1, p2} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	l1 := &WikiLink{FromPageID: p1.ID, ToPageID: p2.ID, LinkType: "related", Strength: 0.4}
	if err := database.SaveWikiLink(l1); err != nil {
		t.Fatalf("SaveWikiLink first: %v", err)
	}

	// Same from/to/type — should update strength.
	l2 := &WikiLink{FromPageID: p1.ID, ToPageID: p2.ID, LinkType: "related", Strength: 0.9}
	if err := database.SaveWikiLink(l2); err != nil {
		t.Fatalf("SaveWikiLink upsert: %v", err)
	}

	links, err := database.ListWikiLinksFrom(p1.ID)
	if err != nil {
		t.Fatalf("ListWikiLinksFrom: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link after upsert, got %d", len(links))
	}
	if links[0].Strength != 0.9 {
		t.Errorf("Strength after upsert: got %f, want 0.9", links[0].Strength)
	}
}

// --- SaveWikiPageRef / ListWikiPageRefs ---

func TestSaveWikiPageRef_AndList(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{PageType: "summary", Title: "X", Status: "draft", GeneratedBy: "ingest"}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	r1 := &WikiPageRef{PageID: page.ID, SourceType: "event", SourceID: "evt-1", Excerpt: "excerpt 1"}
	r2 := &WikiPageRef{PageID: page.ID, SourceType: "artifact", SourceID: "art-2", Excerpt: "excerpt 2"}

	for _, r := range []*WikiPageRef{r1, r2} {
		if err := database.SaveWikiPageRef(r); err != nil {
			t.Fatalf("SaveWikiPageRef: %v", err)
		}
	}

	refs, err := database.ListWikiPageRefs(page.ID)
	if err != nil {
		t.Fatalf("ListWikiPageRefs: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("expected 2 refs, got %d", len(refs))
	}
}

func TestSaveWikiPageRef_DuplicateIgnored(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{PageType: "summary", Title: "Y", Status: "draft", GeneratedBy: "ingest"}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	r := &WikiPageRef{PageID: page.ID, SourceType: "event", SourceID: "evt-1", Excerpt: "x"}
	if err := database.SaveWikiPageRef(r); err != nil {
		t.Fatalf("SaveWikiPageRef first: %v", err)
	}
	// Duplicate — should be silently ignored.
	r2 := &WikiPageRef{PageID: page.ID, SourceType: "event", SourceID: "evt-1", Excerpt: "different"}
	if err := database.SaveWikiPageRef(r2); err != nil {
		t.Fatalf("SaveWikiPageRef duplicate: %v", err)
	}

	refs, err := database.ListWikiPageRefs(page.ID)
	if err != nil {
		t.Fatalf("ListWikiPageRefs: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("expected 1 ref after duplicate insert, got %d", len(refs))
	}
}

// --- DeleteWikiLinks / DeleteWikiPageRefs ---

func TestDeleteWikiLinks_RemovesAllForPage(t *testing.T) {
	database := openTestDB(t)

	p1 := &WikiPage{PageType: "summary", Title: "A", Status: "published", GeneratedBy: "ingest"}
	p2 := &WikiPage{PageType: "summary", Title: "B", Status: "published", GeneratedBy: "ingest"}
	p3 := &WikiPage{PageType: "summary", Title: "C", Status: "published", GeneratedBy: "ingest"}
	for _, p := range []*WikiPage{p1, p2, p3} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	if err := database.SaveWikiLink(&WikiLink{FromPageID: p1.ID, ToPageID: p2.ID, LinkType: "related", Strength: 0.5}); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}
	if err := database.SaveWikiLink(&WikiLink{FromPageID: p3.ID, ToPageID: p1.ID, LinkType: "cites", Strength: 0.5}); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}
	if err := database.SaveWikiLink(&WikiLink{FromPageID: p2.ID, ToPageID: p3.ID, LinkType: "parent", Strength: 0.5}); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}

	if err := database.DeleteWikiLinks(p1.ID); err != nil {
		t.Fatalf("DeleteWikiLinks: %v", err)
	}

	from1, _ := database.ListWikiLinksFrom(p1.ID)
	to1, _ := database.ListWikiLinksTo(p1.ID)
	if len(from1) != 0 || len(to1) != 0 {
		t.Errorf("expected 0 links for p1, got from=%d to=%d", len(from1), len(to1))
	}

	// p2 → p3 link must survive.
	from2, _ := database.ListWikiLinksFrom(p2.ID)
	if len(from2) != 1 {
		t.Errorf("expected 1 surviving link from p2, got %d", len(from2))
	}
}

func TestDeleteWikiPageRefs_RemovesAllForPage(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{PageType: "summary", Title: "Z", Status: "draft", GeneratedBy: "ingest"}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	for i, src := range []string{"evt-1", "evt-2", "art-3"} {
		r := &WikiPageRef{PageID: page.ID, SourceType: "event", SourceID: src, Excerpt: ""}
		if i == 2 {
			r.SourceType = "artifact"
		}
		if err := database.SaveWikiPageRef(r); err != nil {
			t.Fatalf("SaveWikiPageRef: %v", err)
		}
	}

	if err := database.DeleteWikiPageRefs(page.ID); err != nil {
		t.Fatalf("DeleteWikiPageRefs: %v", err)
	}

	refs, err := database.ListWikiPageRefs(page.ID)
	if err != nil {
		t.Fatalf("ListWikiPageRefs: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("expected 0 refs after delete, got %d", len(refs))
	}
}

// --- GetWikiGraph ---

func TestGetWikiGraph_ReturnsNodesAndEdges(t *testing.T) {
	database := openTestDB(t)

	p1 := &WikiPage{PageType: "concept", Title: "A", Status: "published", GeneratedBy: "ingest"}
	p2 := &WikiPage{PageType: "concept", Title: "B", Status: "published", GeneratedBy: "ingest"}
	p3 := &WikiPage{PageType: "summary", Title: "C", Status: "published", GeneratedBy: "ingest"}

	for _, p := range []*WikiPage{p1, p2, p3} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	if err := database.SaveWikiLink(&WikiLink{FromPageID: p1.ID, ToPageID: p2.ID, LinkType: "related", Strength: 0.7}); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}
	if err := database.SaveWikiLink(&WikiLink{FromPageID: p2.ID, ToPageID: p3.ID, LinkType: "cites", Strength: 0.5}); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}

	pages, links, err := database.GetWikiGraph("", 100)
	if err != nil {
		t.Fatalf("GetWikiGraph: %v", err)
	}
	if len(pages) != 3 {
		t.Errorf("expected 3 pages, got %d", len(pages))
	}
	if len(links) != 2 {
		t.Errorf("expected 2 links, got %d", len(links))
	}
}

func TestGetWikiGraph_FilterByPageType(t *testing.T) {
	database := openTestDB(t)

	p1 := &WikiPage{PageType: "concept", Title: "A", Status: "published", GeneratedBy: "ingest"}
	p2 := &WikiPage{PageType: "concept", Title: "B", Status: "published", GeneratedBy: "ingest"}
	p3 := &WikiPage{PageType: "summary", Title: "C", Status: "published", GeneratedBy: "ingest"}

	for _, p := range []*WikiPage{p1, p2, p3} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	if err := database.SaveWikiLink(&WikiLink{FromPageID: p1.ID, ToPageID: p2.ID, LinkType: "related", Strength: 0.7}); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}
	if err := database.SaveWikiLink(&WikiLink{FromPageID: p1.ID, ToPageID: p3.ID, LinkType: "cites", Strength: 0.5}); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}

	pages, links, err := database.GetWikiGraph("concept", 100)
	if err != nil {
		t.Fatalf("GetWikiGraph filtered: %v", err)
	}
	if len(pages) != 2 {
		t.Errorf("expected 2 concept pages, got %d", len(pages))
	}
	// Both links touch at least one concept page (p1 is concept), so both are returned.
	if len(links) != 2 {
		t.Errorf("expected 2 links touching concept pages, got %d", len(links))
	}
}

func TestGetWikiGraph_EmptyDB(t *testing.T) {
	database := openTestDB(t)

	pages, links, err := database.GetWikiGraph("", 100)
	if err != nil {
		t.Fatalf("GetWikiGraph empty: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("expected 0 pages, got %d", len(pages))
	}
	if len(links) != 0 {
		t.Errorf("expected 0 links, got %d", len(links))
	}
}

// --- WikiPageCount ---

func TestWikiPageCount(t *testing.T) {
	database := openTestDB(t)

	for i := 0; i < 3; i++ {
		p := &WikiPage{PageType: "summary", Title: "Page", Status: "published", GeneratedBy: "ingest"}
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	count, err := database.WikiPageCount()
	if err != nil {
		t.Fatalf("WikiPageCount: %v", err)
	}
	if count != 3 {
		t.Errorf("WikiPageCount: got %d, want 3", count)
	}
}

func TestWikiPageCount_Empty(t *testing.T) {
	database := openTestDB(t)

	count, err := database.WikiPageCount()
	if err != nil {
		t.Fatalf("WikiPageCount: %v", err)
	}
	if count != 0 {
		t.Errorf("WikiPageCount: got %d, want 0", count)
	}
}

// --- FindPagesBySourceFiles ---

func TestFindPagesBySourceFiles(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{PageType: "summary", Title: "Main", Status: "published", GeneratedBy: "ingest"}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	ref := &WikiPageRef{PageID: page.ID, SourceType: "artifact", SourceID: "main.go", Excerpt: ""}
	if err := database.SaveWikiPageRef(ref); err != nil {
		t.Fatalf("SaveWikiPageRef: %v", err)
	}

	ids, err := database.FindPagesBySourceFiles([]string{"main.go"})
	if err != nil {
		t.Fatalf("FindPagesBySourceFiles: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 page ID, got %d", len(ids))
	}
	if ids[0] != page.ID {
		t.Errorf("got page ID %q, want %q", ids[0], page.ID)
	}
}

func TestFindPagesBySourceFiles_Empty(t *testing.T) {
	database := openTestDB(t)

	ids, err := database.FindPagesBySourceFiles([]string{})
	if err != nil {
		t.Fatalf("FindPagesBySourceFiles empty: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected nil/empty result, got %v", ids)
	}
}

func TestFindPagesBySourceFiles_NoMatches(t *testing.T) {
	database := openTestDB(t)

	page := &WikiPage{PageType: "summary", Title: "Other", Status: "published", GeneratedBy: "ingest"}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	ref := &WikiPageRef{PageID: page.ID, SourceType: "artifact", SourceID: "server.go", Excerpt: ""}
	if err := database.SaveWikiPageRef(ref); err != nil {
		t.Fatalf("SaveWikiPageRef: %v", err)
	}

	ids, err := database.FindPagesBySourceFiles([]string{"nonexistent.go"})
	if err != nil {
		t.Fatalf("FindPagesBySourceFiles: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty result, got %v", ids)
	}
}

// --- UpsertWikiPageByWorkflow ---

func TestUpsertWikiPageByWorkflow_Insert(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	page := &WikiPage{
		PageType:    "concept",
		Title:       "Auth Service",
		Content:     "Initial content",
		Status:      "draft",
		GeneratedBy: "ingest",
		Tags:        []string{"auth"},
		Metadata:    map[string]any{"priority": "high"},
	}

	got, err := database.UpsertWikiPageByWorkflow(ctx, "wf-123", "auth-service", page)
	if err != nil {
		t.Fatalf("UpsertWikiPageByWorkflow insert: %v", err)
	}
	if got.ID == "" {
		t.Fatal("expected ID to be set after insert")
	}
	if got.WorkflowID != "wf-123" {
		t.Errorf("WorkflowID: got %q, want %q", got.WorkflowID, "wf-123")
	}
	if got.FeatureSlug != "auth-service" {
		t.Errorf("FeatureSlug: got %q, want %q", got.FeatureSlug, "auth-service")
	}
	if got.Version != 1 {
		t.Errorf("Version: got %d, want 1", got.Version)
	}
	if got.Content != "Initial content" {
		t.Errorf("Content: got %q, want %q", got.Content, "Initial content")
	}
	if got.CreatedAt == "" {
		t.Error("expected CreatedAt to be set")
	}
}

func TestUpsertWikiPageByWorkflow_UpdateSameKey_SingleRow(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	first := &WikiPage{
		PageType:    "concept",
		Title:       "Auth Service",
		Content:     "First content",
		Status:      "draft",
		GeneratedBy: "ingest",
	}
	inserted, err := database.UpsertWikiPageByWorkflow(ctx, "wf-abc", "auth-service", first)
	if err != nil {
		t.Fatalf("UpsertWikiPageByWorkflow first: %v", err)
	}
	createdAt := inserted.CreatedAt

	// Second call with same (workflow_id, feature_slug) — should update.
	second := &WikiPage{
		PageType:    "concept",
		Title:       "Auth Service v2",
		Content:     "Updated content",
		Status:      "published",
		GeneratedBy: "evolution",
	}
	updated, err := database.UpsertWikiPageByWorkflow(ctx, "wf-abc", "auth-service", second)
	if err != nil {
		t.Fatalf("UpsertWikiPageByWorkflow second: %v", err)
	}

	// Must be same row ID.
	if updated.ID != inserted.ID {
		t.Errorf("expected same ID after upsert: got %q, want %q", updated.ID, inserted.ID)
	}
	// Version must be incremented.
	if updated.Version != 2 {
		t.Errorf("Version after upsert: got %d, want 2", updated.Version)
	}
	// Content must be updated.
	if updated.Content != "Updated content" {
		t.Errorf("Content: got %q, want %q", updated.Content, "Updated content")
	}
	// created_at must be preserved.
	if updated.CreatedAt != createdAt {
		t.Errorf("CreatedAt changed: got %q, want %q", updated.CreatedAt, createdAt)
	}

	// Confirm single row in DB.
	pages, total, err := database.ListWikiPages(WikiPageFilters{})
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 row in DB, got %d", total)
	}
	if len(pages) != 1 {
		t.Errorf("expected 1 page returned, got %d", len(pages))
	}
}

func TestUpsertWikiPageByWorkflow_DifferentFeatureSlug_NewRow(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	base := &WikiPage{
		PageType:    "summary",
		Title:       "Feature A",
		Content:     "Content A",
		Status:      "draft",
		GeneratedBy: "ingest",
	}
	_, err := database.UpsertWikiPageByWorkflow(ctx, "wf-xyz", "feature-a", base)
	if err != nil {
		t.Fatalf("UpsertWikiPageByWorkflow feature-a: %v", err)
	}

	other := &WikiPage{
		PageType:    "summary",
		Title:       "Feature B",
		Content:     "Content B",
		Status:      "draft",
		GeneratedBy: "ingest",
	}
	_, err = database.UpsertWikiPageByWorkflow(ctx, "wf-xyz", "feature-b", other)
	if err != nil {
		t.Fatalf("UpsertWikiPageByWorkflow feature-b: %v", err)
	}

	pages, total, err := database.ListWikiPages(WikiPageFilters{})
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 rows for different feature slugs, got %d", total)
	}
	if len(pages) != 2 {
		t.Errorf("expected 2 pages, got %d", len(pages))
	}
}

func TestUpsertWikiPageByWorkflow_MissingWorkflowID_Error(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	page := &WikiPage{Title: "X", PageType: "summary", Status: "draft", GeneratedBy: "ingest"}
	_, err := database.UpsertWikiPageByWorkflow(ctx, "", "slug", page)
	if err == nil {
		t.Fatal("expected error for empty workflow_id, got nil")
	}
}

func TestUpsertWikiPageByWorkflow_MissingFeatureSlug_Error(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	page := &WikiPage{Title: "X", PageType: "summary", Status: "draft", GeneratedBy: "ingest"}
	_, err := database.UpsertWikiPageByWorkflow(ctx, "wf-1", "", page)
	if err == nil {
		t.Fatal("expected error for empty feature_slug, got nil")
	}
}

func TestUpsertWikiPageByWorkflow_MultipleUpserts_VersionIncrementsCorrectly(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	page := &WikiPage{
		PageType:    "answer",
		Title:       "Auth Flow",
		Content:     "v1",
		Status:      "draft",
		GeneratedBy: "ingest",
	}
	r1, err := database.UpsertWikiPageByWorkflow(ctx, "wf-multi", "auth-flow", page)
	if err != nil {
		t.Fatalf("upsert v1: %v", err)
	}
	if r1.Version != 1 {
		t.Errorf("v1 Version: got %d, want 1", r1.Version)
	}

	page.Content = "v2"
	r2, err := database.UpsertWikiPageByWorkflow(ctx, "wf-multi", "auth-flow", page)
	if err != nil {
		t.Fatalf("upsert v2: %v", err)
	}
	if r2.Version != 2 {
		t.Errorf("v2 Version: got %d, want 2", r2.Version)
	}

	page.Content = "v3"
	r3, err := database.UpsertWikiPageByWorkflow(ctx, "wf-multi", "auth-flow", page)
	if err != nil {
		t.Fatalf("upsert v3: %v", err)
	}
	if r3.Version != 3 {
		t.Errorf("v3 Version: got %d, want 3", r3.Version)
	}

	// created_at must never change across upserts.
	if r2.CreatedAt != r1.CreatedAt {
		t.Errorf("CreatedAt changed between v1 and v2: %q vs %q", r1.CreatedAt, r2.CreatedAt)
	}
	if r3.CreatedAt != r1.CreatedAt {
		t.Errorf("CreatedAt changed between v1 and v3: %q vs %q", r1.CreatedAt, r3.CreatedAt)
	}
}
