package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/insight"
)

func newWikiPage(id, pageType, title, status string, tags []string) *db.WikiPage {
	return &db.WikiPage{
		ID:          id,
		PageType:    pageType,
		Title:       title,
		Content:     "content for " + title,
		Status:      status,
		GeneratedBy: "ingest",
		Tags:        tags,
	}
}

func TestHandleListWikiPages(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	pages := []*db.WikiPage{
		newWikiPage("wiki-1", "summary", "Summary One", "published", []string{"go", "backend"}),
		newWikiPage("wiki-2", "concept", "Concept Two", "published", []string{"frontend"}),
		newWikiPage("wiki-3", "entity", "Entity Three", "draft", []string{"go"}),
	}
	for _, p := range pages {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
		}
	}

	tests := []struct {
		name          string
		query         string
		expectedCount int
	}{
		{"all pages", "", 3},
		{"filter by type summary", "?type=summary", 1},
		{"filter by type concept", "?type=concept", 1},
		{"filter by status draft", "?status=draft", 1},
		{"filter by status published", "?status=published", 2},
		{"filter by tag go", "?tag=go", 2},
		{"limit 1", "?limit=1", 1},
		{"offset 2", "?offset=2&limit=50", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/wiki/pages"+tt.query, nil)
			w := httptest.NewRecorder()

			server.handleListWikiPages(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}

			var resp map[string]any
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			pages, ok := resp["pages"].([]any)
			if !ok {
				t.Fatal("expected 'pages' array in response")
			}
			if len(pages) != tt.expectedCount {
				t.Errorf("expected %d pages, got %d", tt.expectedCount, len(pages))
			}

			count, ok := resp["count"].(float64)
			if !ok {
				t.Fatal("expected 'count' in response")
			}
			_ = count // count is total, not just this page
		})
	}
}

func TestHandleGetWikiPage(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	page := newWikiPage("wiki-get-1", "summary", "Test Page", "published", []string{"test"})
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	// Save the target page so the foreign-key constraint is satisfied.
	targetPage := newWikiPage("wiki-get-other", "concept", "Other Page", "published", nil)
	if err := database.SaveWikiPage(targetPage); err != nil {
		t.Fatalf("SaveWikiPage wiki-get-other: %v", err)
	}

	// Add links
	link := &db.WikiLink{
		ID:         "link-1",
		FromPageID: "wiki-get-1",
		ToPageID:   "wiki-get-other",
		LinkType:   "related",
		Strength:   0.8,
	}
	if err := database.SaveWikiLink(link); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}

	// Add a ref
	ref := &db.WikiPageRef{
		ID:         "ref-1",
		PageID:     "wiki-get-1",
		SourceType: "event",
		SourceID:   "evt-123",
		Excerpt:    "Some excerpt",
	}
	if err := database.SaveWikiPageRef(ref); err != nil {
		t.Fatalf("SaveWikiPageRef: %v", err)
	}

	t.Run("existing page", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/wiki/pages/wiki-get-1", nil)
		req.SetPathValue("id", "wiki-get-1")
		w := httptest.NewRecorder()

		server.handleGetWikiPage(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		p, ok := resp["page"].(map[string]any)
		if !ok {
			t.Fatal("expected 'page' object in response")
		}
		if p["id"] != "wiki-get-1" {
			t.Errorf("expected id wiki-get-1, got %v", p["id"])
		}

		linksFrom, ok := resp["links_from"].([]any)
		if !ok {
			t.Fatal("expected 'links_from' array")
		}
		if len(linksFrom) != 1 {
			t.Errorf("expected 1 link_from, got %d", len(linksFrom))
		}

		if _, ok := resp["links_to"].([]any); !ok {
			t.Fatal("expected 'links_to' array")
		}

		refs, ok := resp["refs"].([]any)
		if !ok {
			t.Fatal("expected 'refs' array")
		}
		if len(refs) != 1 {
			t.Errorf("expected 1 ref, got %d", len(refs))
		}
	})
}

func TestHandleDeleteWikiPage_Success(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	tmp := t.TempDir()
	cfg := config.Default()
	cfg.Wiki.VaultPath = tmp
	server := &Server{db: database, cfg: &cfg, vaultSync: newVaultSyncForConfig(&cfg, database)}

	page := newWikiPage("wiki-del-1", "summary", "Delete Me", "published", nil)
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/wiki/pages/wiki-del-1", nil)
	req.SetPathValue("id", "wiki-del-1")
	w := httptest.NewRecorder()

	server.handleDeleteWikiPage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// DB row must be gone.
	got, err := database.GetWikiPage("wiki-del-1")
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got != nil {
		t.Error("page should be deleted from DB")
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["deleted"] != true {
		t.Errorf("expected deleted=true; got %v", resp["deleted"])
	}
}

func TestHandleDeleteWikiPage_NotFound(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	server := &Server{db: database, cfg: &cfg}

	req := httptest.NewRequest(http.MethodDelete, "/api/wiki/pages/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	server.handleDeleteWikiPage(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleDeleteWikiPage_MissingID(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	server := &Server{db: database, cfg: &cfg}

	req := httptest.NewRequest(http.MethodDelete, "/api/wiki/pages/", nil)
	req.SetPathValue("id", "")
	w := httptest.NewRecorder()

	server.handleDeleteWikiPage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleDeleteWikiPage_NoVault_StillDeletesDB(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// No vault path configured — vaultSync is nil.
	cfg := config.Default()
	cfg.Wiki.VaultPath = ""
	server := &Server{db: database, cfg: &cfg, vaultSync: nil}

	page := newWikiPage("wiki-del-novault", "summary", "No Vault", "published", nil)
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/wiki/pages/wiki-del-novault", nil)
	req.SetPathValue("id", "wiki-del-novault")
	w := httptest.NewRecorder()

	server.handleDeleteWikiPage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got, _ := database.GetWikiPage("wiki-del-novault")
	if got != nil {
		t.Error("page should be deleted even without vault")
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["vault_deleted"] != false {
		t.Errorf("expected vault_deleted=false; got %v", resp["vault_deleted"])
	}
}

func TestHandleGetWikiPage_NotFound(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	req := httptest.NewRequest("GET", "/api/wiki/pages/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	server.handleGetWikiPage(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleSearchWikiPages(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	pages := []*db.WikiPage{
		newWikiPage("search-1", "summary", "Go Programming Language", "published", nil),
		newWikiPage("search-2", "concept", "Rust Memory Safety", "published", nil),
		newWikiPage("search-3", "entity", "TypeScript Static Typing", "published", nil),
	}
	for _, p := range pages {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
		}
	}

	t.Run("search returns results", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/wiki/search?q=Go+Programming", nil)
		w := httptest.NewRecorder()

		server.handleSearchWikiPages(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		if _, ok := resp["results"]; !ok {
			t.Fatal("expected 'results' in response")
		}
		if _, ok := resp["query"]; !ok {
			t.Fatal("expected 'query' in response")
		}
		if _, ok := resp["count"]; !ok {
			t.Fatal("expected 'count' in response")
		}
		if resp["query"] != "Go Programming" {
			t.Errorf("expected query 'Go Programming', got %v", resp["query"])
		}
	})

	t.Run("missing query returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/wiki/search", nil)
		w := httptest.NewRecorder()

		server.handleSearchWikiPages(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("query too long returns 400", func(t *testing.T) {
		longQ := make([]byte, 2001)
		for i := range longQ {
			longQ[i] = 'a'
		}
		req := httptest.NewRequest("GET", "/api/wiki/search?q="+string(longQ), nil)
		w := httptest.NewRecorder()

		server.handleSearchWikiPages(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestHandleWikiQuery_NoInsight(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// insight is nil — should return 503
	server := &Server{db: database, insight: nil}

	body := map[string]any{
		"query":       "What is the wiki about?",
		"persist":     false,
		"max_sources": 10,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/wiki/query", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	server.handleWikiQuery(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleWikiQuery_Validation(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database, insight: nil}

	t.Run("empty query returns 400", func(t *testing.T) {
		body := map[string]any{"query": ""}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/wiki/query", bytes.NewReader(bodyBytes))
		w := httptest.NewRecorder()

		server.handleWikiQuery(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("query too long returns 400", func(t *testing.T) {
		longQ := make([]byte, 2001)
		for i := range longQ {
			longQ[i] = 'a'
		}
		body := map[string]any{"query": string(longQ)}
		bodyBytes, _ := json.Marshal(body)

		req := httptest.NewRequest("POST", "/api/wiki/query", bytes.NewReader(bodyBytes))
		w := httptest.NewRecorder()

		server.handleWikiQuery(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid body returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/wiki/query", bytes.NewReader([]byte("not json")))
		w := httptest.NewRecorder()

		server.handleWikiQuery(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestHandleVaultSync_NoVaultPath(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	cfg.Wiki.VaultPath = ""
	server := &Server{db: database, cfg: &cfg}

	req := httptest.NewRequest(http.MethodPost, "/api/wiki/vault/sync", nil)
	w := httptest.NewRecorder()

	server.handleVaultSync(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when vault_path not configured, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["error"]; !ok {
		t.Error("expected 'error' field in response")
	}
}

func TestHandleVaultSync_WithVaultPath(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	tmp := t.TempDir()
	cfg := config.Default()
	cfg.Wiki.VaultPath = tmp
	server := &Server{db: database, cfg: &cfg, vaultSync: newVaultSyncForConfig(&cfg, database)}

	page := newWikiPage("vault-sync-1", "summary", "Vault Page", "published", nil)
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/wiki/vault/sync", nil)
	w := httptest.NewRecorder()

	server.handleVaultSync(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "synced" {
		t.Errorf("expected status 'synced', got %v", resp["status"])
	}
	if _, ok := resp["file_count"]; !ok {
		t.Error("expected 'file_count' in response")
	}
	if _, ok := resp["message"]; !ok {
		t.Error("expected 'message' in response")
	}
}

func TestHandleVaultStatus_NoVaultPath(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	cfg := config.Default()
	cfg.Wiki.VaultPath = ""
	server := &Server{db: database, cfg: &cfg}

	req := httptest.NewRequest(http.MethodGet, "/api/wiki/vault/status", nil)
	w := httptest.NewRecorder()

	server.handleVaultStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["vault_path"] != "" {
		t.Errorf("expected empty vault_path, got %v", resp["vault_path"])
	}
	count, ok := resp["file_count"].(float64)
	if !ok {
		t.Fatal("expected 'file_count' to be a number")
	}
	if int(count) != 0 {
		t.Errorf("expected file_count 0, got %v", count)
	}
	if _, ok := resp["errors"]; !ok {
		t.Error("expected 'errors' in response")
	}
}

func TestHandleVaultStatus_WithVaultPath(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	tmp := t.TempDir()
	cfg := config.Default()
	cfg.Wiki.VaultPath = tmp
	server := &Server{db: database, cfg: &cfg, vaultSync: newVaultSyncForConfig(&cfg, database)}

	req := httptest.NewRequest(http.MethodGet, "/api/wiki/vault/status", nil)
	w := httptest.NewRecorder()

	server.handleVaultStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["vault_path"] != tmp {
		t.Errorf("expected vault_path %q, got %v", tmp, resp["vault_path"])
	}
	if _, ok := resp["file_count"]; !ok {
		t.Error("expected 'file_count' in response")
	}
	if _, ok := resp["errors"]; !ok {
		t.Error("expected 'errors' in response")
	}
}

// TestHandleVaultSync_LastSyncPersistsBetweenCalls verifies that the lastSync
// timestamp set during a sync is still visible when GetStatus is called
// afterwards — i.e. the two handlers share the same VaultSync instance.
func TestHandleVaultSync_LastSyncPersistsBetweenCalls(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	tmp := t.TempDir()
	cfg := config.Default()
	cfg.Wiki.VaultPath = tmp

	// Construct server the same way the production code does so that the
	// shared vaultSync instance is wired up.
	vs := newVaultSyncForConfig(&cfg, database)
	server := &Server{db: database, cfg: &cfg, vaultSync: vs}

	// First: call sync
	req := httptest.NewRequest(http.MethodPost, "/api/wiki/vault/sync", nil)
	w := httptest.NewRecorder()
	server.handleVaultSync(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sync: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Second: call status — lastSync must not be nil because the same instance was used
	req2 := httptest.NewRequest(http.MethodGet, "/api/wiki/vault/status", nil)
	w2 := httptest.NewRecorder()
	server.handleVaultStatus(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if resp["last_sync"] == nil {
		t.Error("expected last_sync to be set after sync, got nil — handlers are not sharing the same VaultSync instance")
	}
}

// TestHandleWikiQuery_WithInsightEngine_CallsSynthesize verifies that when
// insight is non-nil the handler delegates to SynthesizeWikiAnswer and does NOT
// return 503. (With no LLM configured, the synthesizer returns an error which
// the handler translates to 500 — not 503.)
func TestHandleWikiQuery_WithInsightEngine_CallsSynthesize(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// NewEngine without LLM — wikiSynth stays nil → SynthesizeWikiAnswer returns error.
	insightCfg := config.InsightConfig{Enabled: true, Interval: 1}
	eng := insight.NewEngine(database, insightCfg)

	server := &Server{db: database, insight: eng}

	body := map[string]any{
		"query":       "What is the architecture?",
		"persist":     false,
		"max_sources": 5,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/wiki/query", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	server.handleWikiQuery(w, req)

	// insight is non-nil, so handler must NOT return 503 "insight engine not initialized".
	// wikiSynth is nil on a default engine → returns error → handler returns 500.
	if w.Code == http.StatusServiceUnavailable {
		t.Errorf("expected handler to delegate to SynthesizeWikiAnswer (not return 503 insight-not-initialized), got 503: %s", w.Body.String())
	}
	// Should be 500 because synthesizer is not initialized inside the engine.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 from nil synthesizer error, got %d: %s", w.Code, w.Body.String())
	}
}

// --- POST /api/wiki/pages (workflow-sourced write) ---

func TestHandleCreateWikiPageFromWorkflow_HappyPath_Insert(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	body := map[string]any{
		"workflow_id":  "wf-happy-1",
		"feature_slug": "auth-service",
		"title":        "Auth Service Overview",
		"content":      "# Auth Service\n\nHandles authentication.",
		"page_type":    "feature",
		"tags":         []string{"auth", "backend"},
		"confidence":   0.9,
		"source_files": []string{"auth/handler.go", "auth/service.go"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/wiki/pages", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	server.handleCreateWikiPageFromWorkflow(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["id"] == nil || resp["id"] == "" {
		t.Error("expected non-empty 'id' in response")
	}
	version, ok := resp["version"].(float64)
	if !ok {
		t.Fatal("expected 'version' numeric in response")
	}
	if int(version) != 1 {
		t.Errorf("expected version=1 on insert, got %v", version)
	}
	if resp["created_at"] == nil {
		t.Error("expected 'created_at' in response")
	}
	if resp["updated_at"] == nil {
		t.Error("expected 'updated_at' in response")
	}
	if resp["action"] != "inserted" {
		t.Errorf("expected action='inserted', got %v", resp["action"])
	}
}

func TestHandleCreateWikiPageFromWorkflow_HappyPath_Update_VersionIncrements(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	doRequest := func(content string) map[string]any {
		body := map[string]any{
			"workflow_id":  "wf-update-1",
			"feature_slug": "payment-service",
			"title":        "Payment Service",
			"content":      content,
		}
		bodyBytes, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/wiki/pages", bytes.NewReader(bodyBytes))
		w := httptest.NewRecorder()
		server.handleCreateWikiPageFromWorkflow(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	first := doRequest("Initial content.")
	if first["action"] != "inserted" {
		t.Errorf("first call: expected action='inserted', got %v", first["action"])
	}
	if int(first["version"].(float64)) != 1 {
		t.Errorf("first call: expected version=1, got %v", first["version"])
	}

	second := doRequest("Updated content.")
	if second["action"] != "updated" {
		t.Errorf("second call: expected action='updated', got %v", second["action"])
	}
	if int(second["version"].(float64)) != 2 {
		t.Errorf("second call: expected version=2, got %v", second["version"])
	}

	// IDs must be the same (same row was upserted)
	if first["id"] != second["id"] {
		t.Errorf("expected same id across upsert, got %v vs %v", first["id"], second["id"])
	}
}

func TestHandleCreateWikiPageFromWorkflow_Validation(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	type tc struct {
		name string
		body map[string]any
	}

	tests := []tc{
		{
			name: "missing workflow_id",
			body: map[string]any{
				"feature_slug": "my-slug",
				"title":        "Title",
				"content":      "Content",
			},
		},
		{
			name: "empty workflow_id",
			body: map[string]any{
				"workflow_id":  "",
				"feature_slug": "my-slug",
				"title":        "Title",
				"content":      "Content",
			},
		},
		{
			name: "missing feature_slug",
			body: map[string]any{
				"workflow_id": "wf-1",
				"title":       "Title",
				"content":     "Content",
			},
		},
		{
			name: "empty feature_slug",
			body: map[string]any{
				"workflow_id":  "wf-1",
				"feature_slug": "",
				"title":        "Title",
				"content":      "Content",
			},
		},
		{
			name: "invalid feature_slug (spaces)",
			body: map[string]any{
				"workflow_id":  "wf-1",
				"feature_slug": "not valid slug",
				"title":        "Title",
				"content":      "Content",
			},
		},
		{
			name: "invalid feature_slug (uppercase)",
			body: map[string]any{
				"workflow_id":  "wf-1",
				"feature_slug": "NotKebabCase",
				"title":        "Title",
				"content":      "Content",
			},
		},
		{
			name: "missing title",
			body: map[string]any{
				"workflow_id":  "wf-1",
				"feature_slug": "my-slug",
				"content":      "Content",
			},
		},
		{
			name: "empty title",
			body: map[string]any{
				"workflow_id":  "wf-1",
				"feature_slug": "my-slug",
				"title":        "",
				"content":      "Content",
			},
		},
		{
			name: "missing content",
			body: map[string]any{
				"workflow_id":  "wf-1",
				"feature_slug": "my-slug",
				"title":        "Title",
			},
		},
		{
			name: "empty content",
			body: map[string]any{
				"workflow_id":  "wf-1",
				"feature_slug": "my-slug",
				"title":        "Title",
				"content":      "",
			},
		},
		{
			name: "confidence below 0",
			body: map[string]any{
				"workflow_id":  "wf-1",
				"feature_slug": "my-slug",
				"title":        "Title",
				"content":      "Content",
				"confidence":   -0.1,
			},
		},
		{
			name: "confidence above 1",
			body: map[string]any{
				"workflow_id":  "wf-1",
				"feature_slug": "my-slug",
				"title":        "Title",
				"content":      "Content",
				"confidence":   1.1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/wiki/pages", bytes.NewReader(bodyBytes))
			w := httptest.NewRecorder()

			server.handleCreateWikiPageFromWorkflow(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleCreateWikiPageFromWorkflow_DefaultPageType(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	body := map[string]any{
		"workflow_id":  "wf-default-type",
		"feature_slug": "some-feature",
		"title":        "Some Feature",
		"content":      "Content here.",
		// page_type omitted → should default to "feature"
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/wiki/pages", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	server.handleCreateWikiPageFromWorkflow(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetWikiGraph(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	page1 := newWikiPage("graph-1", "summary", "Graph Page 1", "published", nil)
	page2 := newWikiPage("graph-2", "concept", "Graph Page 2", "published", nil)
	if err := database.SaveWikiPage(page1); err != nil {
		t.Fatalf("SaveWikiPage graph-1: %v", err)
	}
	if err := database.SaveWikiPage(page2); err != nil {
		t.Fatalf("SaveWikiPage graph-2: %v", err)
	}

	link := &db.WikiLink{
		ID:         "graph-link-1",
		FromPageID: "graph-1",
		ToPageID:   "graph-2",
		LinkType:   "related",
		Strength:   0.9,
	}
	if err := database.SaveWikiLink(link); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}

	t.Run("returns nodes and edges", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/wiki/graph", nil)
		w := httptest.NewRecorder()

		server.handleGetWikiGraph(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		nodes, ok := resp["nodes"].([]any)
		if !ok {
			t.Fatal("expected 'nodes' array")
		}
		if len(nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(nodes))
		}

		edges, ok := resp["edges"].([]any)
		if !ok {
			t.Fatal("expected 'edges' array")
		}
		if len(edges) != 1 {
			t.Errorf("expected 1 edge, got %d", len(edges))
		}
	})

	t.Run("filter by type returns subset", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/wiki/graph?type=summary", nil)
		w := httptest.NewRecorder()

		server.handleGetWikiGraph(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		nodes, ok := resp["nodes"].([]any)
		if !ok {
			t.Fatal("expected 'nodes' array")
		}
		if len(nodes) != 1 {
			t.Errorf("expected 1 node (summary type), got %d", len(nodes))
		}
	})
}

// --- POST /api/wiki/links/rebuild ---

func doRebuildPost(t *testing.T, server *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/wiki/links/rebuild", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()
	server.handleRebuildWikiLinks(w, req)
	return w
}

func TestRebuildLinks_AllRelatedIncreases(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	// Page A's content mentions page B's title.
	pageA := &db.WikiPage{
		ID: "rebuild-a", PageType: "concept", Title: "Alpha Feature",
		Content: "This page describes the Beta Feature in detail.", Status: "published",
		GeneratedBy: "ingest",
	}
	pageB := &db.WikiPage{
		ID: "rebuild-b", PageType: "concept", Title: "Beta Feature",
		Content: "This is the beta feature page.", Status: "published",
		GeneratedBy: "ingest",
	}
	pageC := &db.WikiPage{
		ID: "rebuild-c", PageType: "concept", Title: "Gamma Feature",
		Content: "Gamma has nothing to do with others.", Status: "published",
		GeneratedBy: "ingest",
	}
	for _, p := range []*db.WikiPage{pageA, pageB, pageC} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
		}
	}

	w := doRebuildPost(t, server, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result rebuildLinksResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.LinksSaved < 1 {
		t.Errorf("expected at least 1 link saved (A->B cross-ref), got %d", result.LinksSaved)
	}
	if result.ByType["related"] < 1 {
		t.Errorf("expected at least 1 related link, got %d", result.ByType["related"])
	}
	if result.PagesScanned != 3 {
		t.Errorf("expected 3 pages scanned, got %d", result.PagesScanned)
	}
}

func TestRebuildLinks_OrphansDecrease(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	// 2 pages that reference each other (will get linked by cross-ref).
	pageA := &db.WikiPage{
		ID: "orphan-a", PageType: "concept", Title: "Orphan Alpha",
		Content: "This mentions Orphan Beta in depth.", Status: "published", GeneratedBy: "ingest",
	}
	pageB := &db.WikiPage{
		ID: "orphan-b", PageType: "concept", Title: "Orphan Beta",
		Content: "Main content.", Status: "published", GeneratedBy: "ingest",
	}
	// 3 truly isolated pages with unrelated content and titles.
	pageC := &db.WikiPage{
		ID: "orphan-c", PageType: "concept", Title: "Unrelated One",
		Content: "Completely isolated page.", Status: "published", GeneratedBy: "ingest",
	}
	pageD := &db.WikiPage{
		ID: "orphan-d", PageType: "concept", Title: "Unrelated Two",
		Content: "Also completely isolated.", Status: "published", GeneratedBy: "ingest",
	}
	pageE := &db.WikiPage{
		ID: "orphan-e", PageType: "concept", Title: "Unrelated Three",
		Content: "Nothing in common with others.", Status: "published", GeneratedBy: "ingest",
	}
	for _, p := range []*db.WikiPage{pageA, pageB, pageC, pageD, pageE} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
		}
	}

	// All 5 are orphans before rebuild.
	before, err := database.CountOrphanWikiPages()
	if err != nil {
		t.Fatalf("CountOrphanWikiPages: %v", err)
	}
	if before != 5 {
		t.Fatalf("expected 5 orphans before rebuild, got %d", before)
	}

	w := doRebuildPost(t, server, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result rebuildLinksResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.OrphansBefore != 5 {
		t.Errorf("expected OrphansBefore=5, got %d", result.OrphansBefore)
	}
	// After rebuild at least the cross-ref (A->B) should reduce orphans.
	if result.OrphansAfter > result.OrphansBefore {
		t.Errorf("orphans increased: before=%d after=%d", result.OrphansBefore, result.OrphansAfter)
	}
}

func TestRebuildLinks_DetectsContradictions(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	// Two pages with the same 3-word title prefix but different content.
	pageA := &db.WikiPage{
		ID: "contra-a", PageType: "concept", Title: "Auth Service Overview",
		Content: "This is the first description of auth.", Status: "published", GeneratedBy: "ingest",
	}
	pageB := &db.WikiPage{
		ID: "contra-b", PageType: "concept", Title: "Auth Service Overview",
		Content: "This is a completely different and contradicting description of auth.", Status: "published", GeneratedBy: "ingest",
	}
	for _, p := range []*db.WikiPage{pageA, pageB} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
		}
	}

	w := doRebuildPost(t, server, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result rebuildLinksResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.ByType["contradicts"] < 1 {
		t.Errorf("expected at least 1 contradicts link, got %d", result.ByType["contradicts"])
	}
}

func TestRebuildLinks_MaxPagesExceeded_Returns400(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	// Seed 10 pages.
	for i := 0; i < 10; i++ {
		p := &db.WikiPage{
			ID:          "maxp-" + string(rune('a'+i)),
			PageType:    "concept",
			Title:       "Page " + string(rune('A'+i)),
			Content:     "Content.",
			Status:      "published",
			GeneratedBy: "ingest",
		}
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage: %v", err)
		}
	}

	w := doRebuildPost(t, server, map[string]any{"max_pages": 5})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRebuildLinks_MaxPagesOutOfRange_Returns400(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	for _, maxPages := range []int{0, 5000} {
		w := doRebuildPost(t, server, map[string]any{"max_pages": maxPages})
		if w.Code != http.StatusBadRequest {
			t.Errorf("max_pages=%d: expected 400, got %d: %s", maxPages, w.Code, w.Body.String())
		}
	}
}

func TestRebuildLinks_NoLLMClient_SkipsSuggester(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// guardianLLM is nil — suggester must be skipped even when include_suggester=true.
	server := &Server{db: database, guardianLLM: nil}

	page := &db.WikiPage{
		ID: "nollm-1", PageType: "concept", Title: "No LLM Page",
		Content: "Some content.", Status: "published", GeneratedBy: "ingest",
	}
	if err := database.SaveWikiPage(page); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}

	trueVal := true
	body := map[string]any{"include_suggester": trueVal}
	w := doRebuildPost(t, server, body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result rebuildLinksResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if result.SuggesterInvokedFor != 0 {
		t.Errorf("expected SuggesterInvokedFor=0, got %d", result.SuggesterInvokedFor)
	}
	// Deterministic linker still ran — PagesScanned should reflect this.
	if result.PagesScanned != 1 {
		t.Errorf("expected PagesScanned=1, got %d", result.PagesScanned)
	}
}

func TestRebuildLinks_ConcurrentRequests_SecondReturns409(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	// Hold the rebuild mutex to simulate an in-progress rebuild.
	server.rebuildMu.Lock()

	// Second request must get 409 immediately.
	w := doRebuildPost(t, server, nil)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 while mutex held, got %d: %s", w.Code, w.Body.String())
	}

	// Release the mutex.
	server.rebuildMu.Unlock()

	// Wait briefly and retry — should now succeed (0 pages = 200).
	var wg sync.WaitGroup
	wg.Add(1)
	var retryCode int
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		rw := doRebuildPost(t, server, nil)
		retryCode = rw.Code
	}()
	wg.Wait()

	if retryCode != http.StatusOK {
		t.Errorf("expected 200 after mutex released, got %d", retryCode)
	}
}

// --- POST /api/wiki/links ---

func doCreateLink(t *testing.T, server *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/wiki/links", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()
	server.handleCreateWikiLink(w, req)
	return w
}

func seedTwoPages(t *testing.T, database *db.DB) (*db.WikiPage, *db.WikiPage) {
	t.Helper()
	pageA := &db.WikiPage{
		ID: "link-from-1", PageType: "concept", Title: "From Page",
		Content: "Content.", Status: "published", GeneratedBy: "ingest",
	}
	pageB := &db.WikiPage{
		ID: "link-to-1", PageType: "concept", Title: "To Page",
		Content: "Content.", Status: "published", GeneratedBy: "ingest",
	}
	for _, p := range []*db.WikiPage{pageA, pageB} {
		if err := database.SaveWikiPage(p); err != nil {
			t.Fatalf("SaveWikiPage %s: %v", p.ID, err)
		}
	}
	return pageA, pageB
}

func TestCreateWikiLink_ValidatesLinkType(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	pageA, pageB := seedTwoPages(t, database)

	// Valid link type → 201.
	w := doCreateLink(t, server, map[string]any{
		"from_page_id": pageA.ID,
		"to_page_id":   pageB.ID,
		"link_type":    "related",
	})
	if w.Code != http.StatusCreated {
		t.Errorf("valid link_type: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Invalid link type → 400.
	w = doCreateLink(t, server, map[string]any{
		"from_page_id": pageA.ID,
		"to_page_id":   pageB.ID,
		"link_type":    "invalid-type",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid link_type: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWikiLink_RejectsUnknownType(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	pageA, pageB := seedTwoPages(t, database)

	for _, lt := range []string{"friend", "", "x"} {
		w := doCreateLink(t, server, map[string]any{
			"from_page_id": pageA.ID,
			"to_page_id":   pageB.ID,
			"link_type":    lt,
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("link_type=%q: expected 400, got %d: %s", lt, w.Code, w.Body.String())
		}
	}
}

func TestCreateWikiLink_RejectsSelfLoop(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	pageA, _ := seedTwoPages(t, database)

	w := doCreateLink(t, server, map[string]any{
		"from_page_id": pageA.ID,
		"to_page_id":   pageA.ID,
		"link_type":    "related",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("self-loop: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWikiLink_RejectsInvalidStrength(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	pageA, pageB := seedTwoPages(t, database)

	for _, strength := range []float64{-0.1, 1.1} {
		w := doCreateLink(t, server, map[string]any{
			"from_page_id": pageA.ID,
			"to_page_id":   pageB.ID,
			"link_type":    "related",
			"strength":     strength,
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("strength=%v: expected 400, got %d: %s", strength, w.Code, w.Body.String())
		}
	}

	// nil strength → should default to 0.5 and succeed.
	w := doCreateLink(t, server, map[string]any{
		"from_page_id": pageA.ID,
		"to_page_id":   pageB.ID,
		"link_type":    "related",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("nil strength: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var link db.WikiLink
	if err := json.NewDecoder(w.Body).Decode(&link); err != nil {
		t.Fatalf("decode link: %v", err)
	}
	if link.Strength != 0.5 {
		t.Errorf("expected default strength 0.5, got %v", link.Strength)
	}
}

func TestCreateWikiLink_FromPageNotFound_Returns404(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	_, pageB := seedTwoPages(t, database)

	w := doCreateLink(t, server, map[string]any{
		"from_page_id": "nonexistent-from",
		"to_page_id":   pageB.ID,
		"link_type":    "related",
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWikiLink_ToPageNotFound_Returns404(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	pageA, _ := seedTwoPages(t, database)

	w := doCreateLink(t, server, map[string]any{
		"from_page_id": pageA.ID,
		"to_page_id":   "nonexistent-to",
		"link_type":    "related",
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWikiLink_DuplicateReturnsUpsert(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	pageA, pageB := seedTwoPages(t, database)

	// First call.
	s1 := 0.3
	w1 := doCreateLink(t, server, map[string]any{
		"from_page_id": pageA.ID,
		"to_page_id":   pageB.ID,
		"link_type":    "related",
		"strength":     s1,
	})
	if w1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second call with a different strength — SaveWikiLink upserts on conflict.
	s2 := 0.9
	w2 := doCreateLink(t, server, map[string]any{
		"from_page_id": pageA.ID,
		"to_page_id":   pageB.ID,
		"link_type":    "related",
		"strength":     s2,
	})
	if w2.Code != http.StatusCreated && w2.Code != http.StatusOK {
		t.Fatalf("second create: expected 201 or 200, got %d: %s", w2.Code, w2.Body.String())
	}

	// DB should reflect the second strength value via the upsert.
	links, err := database.ListWikiLinksFrom(pageA.ID)
	if err != nil {
		t.Fatalf("ListWikiLinksFrom: %v", err)
	}
	if len(links) == 0 {
		t.Fatal("expected at least one link")
	}
	found := false
	for _, l := range links {
		if l.FromPageID == pageA.ID && l.ToPageID == pageB.ID && l.LinkType == "related" {
			if l.Strength != s2 {
				t.Errorf("expected strength %v after upsert, got %v", s2, l.Strength)
			}
			found = true
		}
	}
	if !found {
		t.Error("expected related link from pageA to pageB")
	}
}

// --- DELETE /api/wiki/links/{id} ---

func TestDeleteWikiLink_Success(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}
	pageA, pageB := seedTwoPages(t, database)

	link := &db.WikiLink{
		ID:         "del-link-1",
		FromPageID: pageA.ID,
		ToPageID:   pageB.ID,
		LinkType:   "related",
		Strength:   0.5,
	}
	if err := database.SaveWikiLink(link); err != nil {
		t.Fatalf("SaveWikiLink: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/wiki/links/del-link-1", nil)
	req.SetPathValue("id", "del-link-1")
	w := httptest.NewRecorder()
	server.handleDeleteWikiLink(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["deleted"] != true {
		t.Errorf("expected deleted=true, got %v", resp["deleted"])
	}

	// Confirm DB row is gone.
	linksFrom, err := database.ListWikiLinksFrom(pageA.ID)
	if err != nil {
		t.Fatalf("ListWikiLinksFrom: %v", err)
	}
	for _, l := range linksFrom {
		if l.ID == "del-link-1" {
			t.Error("link should have been deleted from DB")
		}
	}
}

func TestDeleteWikiLink_UnknownID_Returns404(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	server := &Server{db: database}

	req := httptest.NewRequest(http.MethodDelete, "/api/wiki/links/does-not-exist", nil)
	req.SetPathValue("id", "does-not-exist")
	w := httptest.NewRecorder()
	server.handleDeleteWikiLink(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
