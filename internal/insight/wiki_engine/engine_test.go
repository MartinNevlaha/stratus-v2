package wiki_engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
)

// captureLLM records the last CompletionRequest sent to the mock.
type captureLLM struct {
	lastReq  llm.CompletionRequest
	response string
	err      error
}

func (c *captureLLM) Complete(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	c.lastReq = req
	if c.err != nil {
		return nil, c.err
	}
	return &llm.CompletionResponse{Content: c.response}, nil
}

// ---------------------------------------------------------------------------
// Tests: RunMaintenance
// ---------------------------------------------------------------------------

// TestRunMaintenance_ScoresStaleness verifies that every published page
// receives a staleness score after RunMaintenance.
func TestRunMaintenance_ScoresStaleness(t *testing.T) {
	store := newMockStore()
	p1 := pageUpdatedDaysAgo("p1", 5, 2)
	p2 := pageUpdatedDaysAgo("p2", 10, 3)
	store.pages["p1"] = &p1
	store.pages["p2"] = &p2

	engine := wiki_engine.NewWikiEngine(store, nil, defaultConfig())
	result, err := engine.RunMaintenance(context.Background())
	if err != nil {
		t.Fatalf("RunMaintenance: %v", err)
	}

	if result.PagesScored != 2 {
		t.Errorf("PagesScored: want 2, got %d", result.PagesScored)
	}
	if _, ok := store.stalenessScores["p1"]; !ok {
		t.Error("expected staleness score for p1")
	}
	if _, ok := store.stalenessScores["p2"]; !ok {
		t.Error("expected staleness score for p2")
	}
}

// TestRunMaintenance_MarksStalePages verifies that a page with high staleness
// is counted in PagesMarkedStale and gets status "stale".
func TestRunMaintenance_MarksStalePages(t *testing.T) {
	store := newMockStore()
	// 60-day-old page, version 1, no incoming links — score will exceed 0.7.
	p := pageUpdatedDaysAgo("stale-page", 60, 1)
	store.pages["stale-page"] = &p

	engine := wiki_engine.NewWikiEngine(store, nil, defaultConfig())
	result, err := engine.RunMaintenance(context.Background())
	if err != nil {
		t.Fatalf("RunMaintenance: %v", err)
	}

	if result.PagesMarkedStale == 0 {
		t.Error("expected at least one stale page")
	}
	if status, ok := store.stalenessStatus["stale-page"]; !ok || status != "stale" {
		t.Errorf("expected status 'stale' for stale-page, got %q", status)
	}
}

// TestRunMaintenance_FreshPage_LowScore verifies that a recently-updated
// multi-version page with an incoming link scores below the staleness threshold.
func TestRunMaintenance_FreshPage_LowScore(t *testing.T) {
	store := newMockStore()
	// Updated today, version 3, one incoming link.
	p := pageUpdatedDaysAgo("fresh-page", 0, 3)
	store.pages["fresh-page"] = &p
	store.links["fresh-page"] = []db.WikiLink{
		{ID: "lnk-1", FromPageID: "other", ToPageID: "fresh-page"},
	}

	engine := wiki_engine.NewWikiEngine(store, nil, defaultConfig())
	result, err := engine.RunMaintenance(context.Background())
	if err != nil {
		t.Fatalf("RunMaintenance: %v", err)
	}

	if result.PagesMarkedStale != 0 {
		t.Errorf("expected 0 stale pages, got %d", result.PagesMarkedStale)
	}
	score := store.stalenessScores["fresh-page"]
	if score > 0.7 {
		t.Errorf("expected low staleness score for fresh page, got %f", score)
	}
}

// ---------------------------------------------------------------------------
// Tests: GeneratePageFromData
// ---------------------------------------------------------------------------

// TestGeneratePageFromData_Success verifies that the page is saved and refs
// are attached when the LLM returns content successfully.
func TestGeneratePageFromData_Success(t *testing.T) {
	store := newMockStore()
	lm := &mockLLM{response: "Generated wiki content about the topic."}

	engine := wiki_engine.NewWikiEngine(store, lm, defaultConfig())

	refs := []db.WikiPageRef{
		{SourceType: "event", SourceID: "evt-1", Excerpt: "snippet"},
	}

	page, err := engine.GeneratePageFromData(context.Background(), "My Topic", "raw data", "concept", refs)
	if err != nil {
		t.Fatalf("GeneratePageFromData: %v", err)
	}
	if page == nil {
		t.Fatal("expected page, got nil")
	}
	if page.ID == "" {
		t.Error("expected page.ID to be set")
	}
	if page.Content != "Generated wiki content about the topic." {
		t.Errorf("content: want LLM output, got %q", page.Content)
	}
	if page.Title != "My Topic" {
		t.Errorf("title: want 'My Topic', got %q", page.Title)
	}
	if page.PageType != "concept" {
		t.Errorf("page_type: want 'concept', got %q", page.PageType)
	}

	// Verify page persisted in store.
	if _, ok := store.pages[page.ID]; !ok {
		t.Error("page not found in store after GeneratePageFromData")
	}

	// Verify refs persisted.
	storedRefs := store.refs[page.ID]
	if len(storedRefs) != 1 {
		t.Errorf("expected 1 ref, got %d", len(storedRefs))
	}
}

// TestGeneratePageFromData_LLMFailure verifies that the error from the LLM is
// propagated and no page is saved.
func TestGeneratePageFromData_LLMFailure(t *testing.T) {
	store := newMockStore()
	llmErr := errors.New("upstream LLM timeout")
	lm := &mockLLM{err: llmErr}

	engine := wiki_engine.NewWikiEngine(store, lm, defaultConfig())

	page, err := engine.GeneratePageFromData(context.Background(), "Title", "data", "summary", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, llmErr) {
		t.Errorf("expected llmErr in chain, got: %v", err)
	}
	if page != nil {
		t.Errorf("expected nil page on error, got %+v", page)
	}
	if len(store.pages) != 0 {
		t.Errorf("expected no pages in store on LLM failure, got %d", len(store.pages))
	}
}

// ---------------------------------------------------------------------------
// Tests: RunIngest
// ---------------------------------------------------------------------------

// TestNewWikiEngine_NilLLM verifies that RunIngest handles a nil LLM
// gracefully (fail-open: returns empty result, no error).
func TestNewWikiEngine_NilLLM(t *testing.T) {
	store := newMockStore()

	engine := wiki_engine.NewWikiEngine(store, nil, defaultConfig())
	result, err := engine.RunIngest(context.Background())
	if err != nil {
		t.Fatalf("RunIngest with nil LLM: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil IngestResult")
	}
}

// TestRunIngest_DisabledConfig verifies that RunIngest returns an empty result
// when the wiki is disabled in config.
func TestRunIngest_DisabledConfig(t *testing.T) {
	store := newMockStore()
	lm := &mockLLM{response: "content"}

	disabledConfig := func() config.WikiConfig {
		return config.WikiConfig{Enabled: false}
	}

	engine := wiki_engine.NewWikiEngine(store, lm, disabledConfig)
	result, err := engine.RunIngest(context.Background())
	if err != nil {
		t.Fatalf("RunIngest disabled: %v", err)
	}
	if result.PagesCreated != 0 || result.PagesUpdated != 0 || result.LinksCreated != 0 {
		t.Errorf("expected empty result for disabled config, got %+v", result)
	}
}

// TestRunIngest_ListPagesError verifies that store errors are propagated correctly.
func TestRunIngest_ListPagesError(t *testing.T) {
	store := newMockStore()
	store.listPagesErr = errors.New("db unavailable")
	lm := &mockLLM{response: "content"}

	engine := wiki_engine.NewWikiEngine(store, lm, defaultConfig())
	_, err := engine.RunIngest(context.Background())
	if err == nil {
		t.Fatal("expected error from list pages failure, got nil")
	}
	if !errors.Is(err, store.listPagesErr) {
		t.Errorf("expected store error in chain, got: %v", err)
	}
}

// TestGeneratePageFromData_NilLLM verifies that an error is returned when the
// LLM client is nil.
func TestGeneratePageFromData_NilLLM(t *testing.T) {
	store := newMockStore()
	engine := wiki_engine.NewWikiEngine(store, nil, defaultConfig())

	_, err := engine.GeneratePageFromData(context.Background(), "Title", "data", "summary", nil)
	if err == nil {
		t.Fatal("expected error when LLM is nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: RunIngest — index page generation
// ---------------------------------------------------------------------------

// TestRunIngest_CreatesIndexPage verifies that RunIngest creates an index page
// when published pages exist and no index page exists yet.
func TestRunIngest_CreatesIndexPage(t *testing.T) {
	store := newMockStore()
	lm := &mockLLM{response: "any"}

	// Seed two published non-index pages.
	p1 := pageUpdatedDaysAgo("p-1", 1, 2)
	p1.PageType = "summary"
	p1.Status = "published"
	p2 := pageUpdatedDaysAgo("p-2", 2, 1)
	p2.PageType = "concept"
	p2.Status = "published"
	store.pages["p-1"] = &p1
	store.pages["p-2"] = &p2

	engine := wiki_engine.NewWikiEngine(store, lm, defaultConfig())
	result, err := engine.RunIngest(context.Background())
	if err != nil {
		t.Fatalf("RunIngest: %v", err)
	}
	if result.PagesCreated != 1 {
		t.Errorf("PagesCreated: want 1, got %d", result.PagesCreated)
	}

	// Verify an index page was saved to the store.
	var indexFound bool
	for _, p := range store.pages {
		if p.PageType == "index" {
			indexFound = true
			if p.Title == "" {
				t.Error("expected non-empty index page title")
			}
			if p.Status != "published" {
				t.Errorf("expected index page status 'published', got %q", p.Status)
			}
		}
	}
	if !indexFound {
		t.Error("expected an index page to be created in the store")
	}
}

// TestRunIngest_EmptyStore_SkipsIndexCreation verifies that no index page is
// created when there are no existing published pages to index.
func TestRunIngest_EmptyStore_SkipsIndexCreation(t *testing.T) {
	store := newMockStore()
	lm := &mockLLM{response: "any"}

	engine := wiki_engine.NewWikiEngine(store, lm, defaultConfig())
	result, err := engine.RunIngest(context.Background())
	if err != nil {
		t.Fatalf("RunIngest on empty store: %v", err)
	}
	if result.PagesCreated != 0 {
		t.Errorf("PagesCreated: want 0 on empty store, got %d", result.PagesCreated)
	}
	if len(store.pages) != 0 {
		t.Errorf("expected no pages created on empty store, got %d", len(store.pages))
	}
}

// TestRunIngest_UpdatesExistingIndexPage verifies that RunIngest updates an
// already-existing index page rather than creating a duplicate.
func TestRunIngest_UpdatesExistingIndexPage(t *testing.T) {
	store := newMockStore()
	lm := &mockLLM{response: "any"}

	// Seed an existing index page.
	idx := pageUpdatedDaysAgo("idx-1", 5, 1)
	idx.PageType = "index"
	idx.Status = "published"
	idx.Title = "Knowledge Wiki Index"
	store.pages["idx-1"] = &idx

	// Seed a published summary page to appear in the updated index.
	p1 := pageUpdatedDaysAgo("p-1", 1, 2)
	p1.PageType = "summary"
	p1.Status = "published"
	store.pages["p-1"] = &p1

	engine := wiki_engine.NewWikiEngine(store, lm, defaultConfig())
	result, err := engine.RunIngest(context.Background())
	if err != nil {
		t.Fatalf("RunIngest: %v", err)
	}
	if result.PagesUpdated != 1 {
		t.Errorf("PagesUpdated: want 1, got %d", result.PagesUpdated)
	}
	if result.PagesCreated != 0 {
		t.Errorf("PagesCreated: want 0 when index already exists, got %d", result.PagesCreated)
	}

	// The index page version should have been incremented by UpdatePage.
	updated := store.pages["idx-1"]
	if updated.Version <= 1 {
		t.Errorf("expected index page version to be incremented, got %d", updated.Version)
	}
}

// ---------------------------------------------------------------------------
// Tests: GeneratePageFromData — system prompt uses obsidian prompt library
// ---------------------------------------------------------------------------

// TestGeneratePageFromData_SystemPromptContainsObsidianMarkdown verifies that
// the system prompt sent to the LLM includes obsidian-compatible wikilink syntax.
func TestGeneratePageFromData_SystemPromptContainsObsidianMarkdown(t *testing.T) {
	store := newMockStore()
	lm := &captureLLM{response: "wiki content"}

	engine := wiki_engine.NewWikiEngine(store, lm, defaultConfig())
	_, err := engine.GeneratePageFromData(context.Background(), "Topic", "data", "concept", nil)
	if err != nil {
		t.Fatalf("GeneratePageFromData: %v", err)
	}

	prompt := lm.lastReq.SystemPrompt
	if !strings.Contains(prompt, "[[") {
		t.Errorf("system prompt should contain obsidian wikilink syntax '[[', got: %q", prompt)
	}
	if !strings.Contains(prompt, "wikilink") {
		t.Errorf("system prompt should contain 'wikilink', got: %q", prompt)
	}
}

// TestGeneratePageFromData_SystemPromptContainsWikiAuthorInstruction verifies
// that the WikiPageGeneration prompt is present in the composed system prompt.
func TestGeneratePageFromData_SystemPromptContainsWikiAuthorInstruction(t *testing.T) {
	store := newMockStore()
	lm := &captureLLM{response: "wiki content"}

	engine := wiki_engine.NewWikiEngine(store, lm, defaultConfig())
	_, err := engine.GeneratePageFromData(context.Background(), "Topic", "data", "concept", nil)
	if err != nil {
		t.Fatalf("GeneratePageFromData: %v", err)
	}

	prompt := lm.lastReq.SystemPrompt
	if !strings.Contains(prompt, "wiki") {
		t.Errorf("system prompt should contain 'wiki' from WikiPageGeneration, got: %q", prompt)
	}
}
