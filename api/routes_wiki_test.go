package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
