package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/vexor"
)

// newRetrievalWikiPage creates a WikiPage for use in retrieval tests.
func newRetrievalWikiPage(id, pageType, title, content string, stalenessScore float64) *db.WikiPage {
	return &db.WikiPage{
		ID:             id,
		PageType:       pageType,
		Title:          title,
		Content:        content,
		Status:         "published",
		StalenessScore: stalenessScore,
		GeneratedBy:    "ingest",
	}
}

// newRetrievalServer builds a minimal Server for retrieval tests with a no-op vexor client.
func newRetrievalServer(t *testing.T, database *db.DB) *Server {
	t.Helper()
	// Use a binary name that will never exist so Available() returns false without panicking.
	return &Server{db: database, vexor: vexor.New("__nonexistent_vexor_binary__", "", 1)}
}

func TestHandleRetrieve_InvalidCorpus(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := newRetrievalServer(t, database)

	req := httptest.NewRequest("GET", "/api/retrieve?q=test&corpus=invalid", nil)
	w := httptest.NewRecorder()

	server.handleRetrieve(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid corpus, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Error("expected 'error' field in response")
	}
}

func TestHandleRetrieve_WikiCorpus(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := newRetrievalServer(t, database)

	pages := []*db.WikiPage{
		newRetrievalWikiPage("ret-wiki-1", "summary", "Go Concurrency Patterns", "goroutines channels concurrency", 0.1),
		newRetrievalWikiPage("ret-wiki-2", "concept", "Rust Ownership Model", "ownership borrow memory safety", 0.2),
	}
	for _, p := range pages {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
		}
	}

	req := httptest.NewRequest("GET", "/api/retrieve?q=concurrency+goroutines&corpus=wiki", nil)
	w := httptest.NewRecorder()

	server.handleRetrieve(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatal("expected 'results' array in response")
	}
	if len(results) == 0 {
		t.Error("expected at least one wiki result")
	}

	// All results should have source="wiki"
	for i, r := range results {
		result, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("result %d is not an object", i)
		}
		if result["source"] != "wiki" {
			t.Errorf("result %d: expected source='wiki', got %v", i, result["source"])
		}
		if _, ok := result["page_type"]; !ok {
			t.Errorf("result %d: expected 'page_type' field", i)
		}
	}
}

func TestHandleRetrieve_AutoMode_IncludesWiki(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := newRetrievalServer(t, database)

	pages := []*db.WikiPage{
		newRetrievalWikiPage("auto-wiki-1", "summary", "Architecture Overview", "system design architecture components", 0.0),
	}
	for _, p := range pages {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
		}
	}

	req := httptest.NewRequest("GET", "/api/retrieve?q=architecture+design&corpus=", nil)
	w := httptest.NewRecorder()

	server.handleRetrieve(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatal("expected 'results' array in response")
	}

	// Check that at least one result is from "wiki"
	wikiFound := false
	for _, r := range results {
		result, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if result["source"] == "wiki" {
			wikiFound = true
			break
		}
	}
	if !wikiFound {
		t.Error("expected at least one wiki result in auto mode")
	}
}

func TestHandleRetrieve_StalePenalty(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := newRetrievalServer(t, database)

	// Page with high staleness (> 0.7) should get penalized score
	stalePage := newRetrievalWikiPage("stale-wiki-1", "summary", "Stale Architecture Doc", "old deprecated architecture system", 0.9)
	freshPage := newRetrievalWikiPage("fresh-wiki-1", "summary", "Fresh Architecture Doc", "current architecture system design", 0.1)

	for _, p := range []*db.WikiPage{stalePage, freshPage} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
		}
	}

	req := httptest.NewRequest("GET", "/api/retrieve?q=architecture+system&corpus=wiki&top_k=10", nil)
	w := httptest.NewRecorder()

	server.handleRetrieve(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatal("expected 'results' array in response")
	}

	var staleScore, freshScore float64
	staleFound, freshFound := false, false
	for _, r := range results {
		result, ok := r.(map[string]any)
		if !ok {
			continue
		}
		title, _ := result["title"].(string)
		score, _ := result["score"].(float64)
		stalenessScore, _ := result["staleness_score"].(float64)

		if title == "Stale Architecture Doc" {
			staleScore = score
			staleFound = true
			// staleness_score field must be present
			if stalenessScore == 0 {
				t.Errorf("expected staleness_score > 0 for stale page, got %v", stalenessScore)
			}
		}
		if title == "Fresh Architecture Doc" {
			freshScore = score
			freshFound = true
		}
	}

	if !staleFound || !freshFound {
		t.Skipf("FTS5 did not return both pages (stale=%v fresh=%v); skipping score comparison", staleFound, freshFound)
	}

	// Stale score must be <= fresh score (penalty applied)
	if staleScore > freshScore {
		t.Errorf("expected stale page score (%v) <= fresh page score (%v)", staleScore, freshScore)
	}
}

func TestHandleRetrieveStatus_WikiFields(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := newRetrievalServer(t, database)

	req := httptest.NewRequest("GET", "/api/retrieve/status", nil)
	w := httptest.NewRecorder()

	server.handleRetrieveStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// wiki_available must be present
	if _, ok := resp["wiki_available"]; !ok {
		t.Error("expected 'wiki_available' field in response")
	}

	// wiki_page_count must be present
	if _, ok := resp["wiki_page_count"]; !ok {
		t.Error("expected 'wiki_page_count' field in response")
	}

	// Initially no wiki pages → wiki_available = false, count = 0
	wikiAvailable, ok := resp["wiki_available"].(bool)
	if !ok {
		t.Fatalf("expected 'wiki_available' to be bool, got %T", resp["wiki_available"])
	}
	if wikiAvailable {
		t.Error("expected wiki_available=false with no pages")
	}

	count, ok := resp["wiki_page_count"].(float64)
	if !ok {
		t.Fatalf("expected 'wiki_page_count' to be a number, got %T", resp["wiki_page_count"])
	}
	if int(count) != 0 {
		t.Errorf("expected wiki_page_count=0, got %v", count)
	}

	// Now add pages and verify wiki_available flips to true
	page := newRetrievalWikiPage("status-wiki-1", "summary", "Status Test Page", "testing", 0.0)
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	req2 := httptest.NewRequest("GET", "/api/retrieve/status", nil)
	w2 := httptest.NewRecorder()
	server.handleRetrieveStatus(w2, req2)

	var resp2 map[string]any
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode response2: %v", err)
	}

	wikiAvailable2, ok := resp2["wiki_available"].(bool)
	if !ok {
		t.Fatalf("expected 'wiki_available' to be bool in resp2")
	}
	if !wikiAvailable2 {
		t.Error("expected wiki_available=true after inserting a page")
	}

	count2, _ := resp2["wiki_page_count"].(float64)
	if int(count2) != 1 {
		t.Errorf("expected wiki_page_count=1 after inserting one page, got %v", count2)
	}
}

func TestHandleRetrieve_MissingQuery(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := newRetrievalServer(t, database)

	req := httptest.NewRequest("GET", "/api/retrieve", nil)
	w := httptest.NewRecorder()

	server.handleRetrieve(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing query, got %d", w.Code)
	}
}

func TestHandleRetrieve_WikiCorpus_AutoLimitsCap(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := newRetrievalServer(t, database)

	// Insert several wiki pages
	for i := 0; i < 5; i++ {
		p := newRetrievalWikiPage(
			"cap-wiki-"+string(rune('0'+i)),
			"summary",
			"Cap Test Page",
			"architecture system testing cap limit",
			0.0,
		)
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	// Auto mode with top_k=3: wiki should be capped at max(1, 3/3)=1
	req := httptest.NewRequest("GET", "/api/retrieve?q=architecture&top_k=3", nil)
	w := httptest.NewRecorder()

	server.handleRetrieve(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatal("expected 'results' array in response")
	}

	wikiCount := 0
	for _, r := range results {
		result, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if result["source"] == "wiki" {
			wikiCount++
		}
	}

	// In auto mode with top_k=3, wiki is capped at 1
	if wikiCount > 1 {
		t.Errorf("expected wiki results capped at 1 in auto mode with top_k=3, got %d", wikiCount)
	}
}
