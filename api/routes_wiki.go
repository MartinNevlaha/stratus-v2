package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

var kebabCaseRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// POST /api/wiki/pages
// Workflow-sourced wiki write — inserts or updates a wiki page identified by
// (workflow_id, feature_slug).
func (s *Server) handleCreateWikiPageFromWorkflow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkflowID  string   `json:"workflow_id"`
		FeatureSlug string   `json:"feature_slug"`
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		PageType    string   `json:"page_type"`
		Tags        []string `json:"tags"`
		Confidence  *float64 `json:"confidence"`
		SourceFiles []string `json:"source_files"`
	}
	if err := decodeBody(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// --- validation ---
	if req.WorkflowID == "" {
		jsonErr(w, http.StatusBadRequest, "workflow_id is required")
		return
	}
	if req.FeatureSlug == "" {
		jsonErr(w, http.StatusBadRequest, "feature_slug is required")
		return
	}
	if !kebabCaseRe.MatchString(req.FeatureSlug) {
		jsonErr(w, http.StatusBadRequest, "feature_slug must be kebab-case (lowercase letters, digits, hyphens)")
		return
	}
	if req.Title == "" {
		jsonErr(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Content == "" {
		jsonErr(w, http.StatusBadRequest, "content is required")
		return
	}
	if req.Confidence != nil && (*req.Confidence < 0 || *req.Confidence > 1) {
		jsonErr(w, http.StatusBadRequest, "confidence must be between 0 and 1")
		return
	}

	// --- defaults ---
	pageType := req.PageType
	if pageType == "" {
		pageType = "feature"
	}

	confidence := 0.0
	if req.Confidence != nil {
		confidence = *req.Confidence
	}

	// --- build metadata ---
	metadata := map[string]any{
		"source":     "workflow:" + req.WorkflowID,
		"confidence": confidence,
	}
	if len(req.SourceFiles) > 0 {
		metadata["source_files"] = req.SourceFiles
	}

	page := &db.WikiPage{
		PageType:    pageType,
		Title:       req.Title,
		Content:     req.Content,
		Status:      "auto-generated",
		GeneratedBy: "workflow",
		Tags:        req.Tags,
		Metadata:    metadata,
	}

	result, err := s.db.UpsertWikiPageByWorkflow(r.Context(), req.WorkflowID, req.FeatureSlug, page)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("upsert wiki page: %v", err))
		return
	}

	action := "inserted"
	if result.Version > 1 {
		action = "updated"
	}

	json200(w, map[string]any{
		"id":         result.ID,
		"version":    result.Version,
		"created_at": result.CreatedAt,
		"updated_at": result.UpdatedAt,
		"action":     action,
	})
}

// GET /api/wiki/pages
func (s *Server) handleListWikiPages(w http.ResponseWriter, r *http.Request) {
	filters := db.WikiPageFilters{
		PageType: queryStr(r, "type"),
		Status:   queryStr(r, "status"),
		Tag:      queryStr(r, "tag"),
		Limit:    queryInt(r, "limit", 50),
		Offset:   queryInt(r, "offset", 0),
	}

	pages, total, err := s.db.ListWikiPages(filters)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list wiki pages: "+err.Error())
		return
	}
	if pages == nil {
		pages = []db.WikiPage{}
	}

	json200(w, map[string]any{
		"pages": pages,
		"count": total,
	})
}

// GET /api/wiki/pages/{id}
func (s *Server) handleGetWikiPage(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing page id")
		return
	}

	page, err := s.db.GetWikiPage(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "get wiki page: "+err.Error())
		return
	}
	if page == nil {
		jsonErr(w, http.StatusNotFound, "wiki page not found")
		return
	}

	linksFrom, err := s.db.ListWikiLinksFrom(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list wiki links from: "+err.Error())
		return
	}
	if linksFrom == nil {
		linksFrom = []db.WikiLink{}
	}

	linksTo, err := s.db.ListWikiLinksTo(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list wiki links to: "+err.Error())
		return
	}
	if linksTo == nil {
		linksTo = []db.WikiLink{}
	}

	refs, err := s.db.ListWikiPageRefs(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list wiki page refs: "+err.Error())
		return
	}
	if refs == nil {
		refs = []db.WikiPageRef{}
	}

	json200(w, map[string]any{
		"page":       page,
		"links_from": linksFrom,
		"links_to":   linksTo,
		"refs":       refs,
	})
}

// DELETE /api/wiki/pages/{id}
// Removes the wiki page from the DB (cascades links + refs) and, when a vault
// sync is configured, also removes the corresponding .md file from disk.
// Vault delete failures are logged but do not block DB deletion (fail-open).
func (s *Server) handleDeleteWikiPage(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing page id")
		return
	}

	page, err := s.db.GetWikiPage(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "get wiki page: "+err.Error())
		return
	}
	if page == nil {
		jsonErr(w, http.StatusNotFound, "wiki page not found")
		return
	}

	if err := s.db.DeleteWikiPage(id); err != nil {
		jsonErr(w, http.StatusInternalServerError, "delete wiki page: "+err.Error())
		return
	}

	vaultDeleted := false
	if vs := s.getVaultSync(); vs != nil {
		if err := vs.DeletePage(page); err != nil {
			// Fail-open: DB row is gone; log and continue.
			fmt.Fprintf(os.Stderr, "warn: delete vault file for page %s: %v\n", id, err)
		} else {
			vaultDeleted = true
		}
	}

	json200(w, map[string]any{"deleted": true, "vault_deleted": vaultDeleted})
}

// GET /api/wiki/search
func (s *Server) handleSearchWikiPages(w http.ResponseWriter, r *http.Request) {
	q := queryStr(r, "q")
	if q == "" {
		jsonErr(w, http.StatusBadRequest, "q is required")
		return
	}
	if len(q) > 2000 {
		jsonErr(w, http.StatusBadRequest, "q exceeds 2000 character limit")
		return
	}

	pageType := queryStr(r, "type")
	limit := queryInt(r, "limit", 20)

	results, err := s.db.SearchWikiPages(q, pageType, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "search wiki pages: "+err.Error())
		return
	}
	if results == nil {
		results = []db.WikiPage{}
	}

	json200(w, map[string]any{
		"results": results,
		"count":   len(results),
		"query":   q,
	})
}

// POST /api/wiki/query
func (s *Server) handleWikiQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string `json:"query"`
		Persist    bool   `json:"persist"`
		MaxSources int    `json:"max_sources"`
	}
	if err := decodeBody(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Query == "" {
		jsonErr(w, http.StatusBadRequest, "query is required")
		return
	}
	if len(req.Query) > 2000 {
		jsonErr(w, http.StatusBadRequest, "query exceeds 2000 character limit")
		return
	}
	if req.MaxSources > 50 {
		req.MaxSources = 50
	}

	if s.insight == nil {
		jsonErr(w, http.StatusServiceUnavailable, "insight engine not initialized")
		return
	}

	result, err := s.insight.SynthesizeWikiAnswer(r.Context(), req.Query, req.MaxSources, req.Persist)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("synthesis failed: %v", err))
		return
	}
	json200(w, result)
}

// POST /api/wiki/vault/sync
func (s *Server) handleVaultSync(w http.ResponseWriter, r *http.Request) {
	vs := s.getVaultSync()
	if vs == nil {
		jsonErr(w, http.StatusBadRequest, "vault_path not configured")
		return
	}

	result, err := vs.SyncAll(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("vault sync: %v", err))
		return
	}

	json200(w, map[string]any{
		"status":     "synced",
		"message":    fmt.Sprintf("synced %d files to vault", result.FileCount),
		"file_count": result.FileCount,
		"errors":     result.Errors,
	})
}

// GET /api/wiki/vault/status
func (s *Server) handleVaultStatus(w http.ResponseWriter, r *http.Request) {
	vs := s.getVaultSync()
	if vs == nil {
		json200(w, map[string]any{
			"last_sync":  nil,
			"file_count": 0,
			"vault_path": "",
			"errors":     []string{},
		})
		return
	}

	status, err := vs.GetStatus()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("get vault status: %v", err))
		return
	}

	json200(w, status)
}

// GET /api/wiki/config
func (s *Server) handleGetWikiConfig(w http.ResponseWriter, r *http.Request) {
	json200(w, s.cfg.Wiki)
}

// PUT /api/wiki/config
func (s *Server) handleUpdateWikiConfig(w http.ResponseWriter, r *http.Request) {
	var incoming config.WikiConfig
	if err := decodeBody(r, &incoming); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	if err := validateWikiConfig(&incoming); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	pathChanged := incoming.VaultPath != s.cfg.Wiki.VaultPath
	s.cfg.Wiki = incoming
	if err := s.cfg.Save(filepath.Join(s.projectRoot, ".stratus.json")); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save config: "+err.Error())
		return
	}

	if pathChanged {
		s.rebuildVaultSync()
	}

	json200(w, s.cfg.Wiki)
}

// validateWikiConfig enforces bounds on numeric and enum fields and resolves
// the vault_path to an absolute, writable directory.
// For MaxPagesPerIngest, OnboardingMaxPages, and IngestTokenBudget the value 0
// is the unlimited sentinel and is always accepted; negative values are rejected.
func validateWikiConfig(cfg *config.WikiConfig) error {
	// Delegate numeric-bound checks (0=unlimited sentinel, negatives rejected)
	// to the config package so the API and config loader stay in lockstep.
	if err := config.ValidateWikiConfig(cfg); err != nil {
		return err
	}
	if cfg.StalenessThreshold < 0 || cfg.StalenessThreshold > 1 {
		return fmt.Errorf("staleness_threshold must be between 0 and 1")
	}
	if cfg.MaxPageSizeTokens < 0 || cfg.MaxPageSizeTokens > 100000 {
		return fmt.Errorf("max_page_size_tokens must be between 0 and 100000")
	}
	switch cfg.OnboardingDepth {
	case "", "shallow", "standard", "deep":
	default:
		return fmt.Errorf("onboarding_depth must be one of: shallow, standard, deep")
	}

	cfg.VaultPath = strings.TrimSpace(cfg.VaultPath)
	if cfg.VaultPath == "" {
		return nil
	}
	if !filepath.IsAbs(cfg.VaultPath) {
		return fmt.Errorf("vault_path must be an absolute path")
	}
	info, err := os.Stat(cfg.VaultPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("vault_path: %v", err)
		}
		if err := os.MkdirAll(cfg.VaultPath, 0o755); err != nil {
			return fmt.Errorf("vault_path: create directory: %v", err)
		}
		return nil
	}
	if !info.IsDir() {
		return fmt.Errorf("vault_path must point to a directory")
	}
	return nil
}

// GET /api/wiki/graph
func (s *Server) handleGetWikiGraph(w http.ResponseWriter, r *http.Request) {
	pageType := queryStr(r, "type")
	limit := queryInt(r, "limit", 100)

	pages, links, err := s.db.GetWikiGraph(pageType, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "get wiki graph: "+err.Error())
		return
	}
	if pages == nil {
		pages = []db.WikiPage{}
	}
	if links == nil {
		links = []db.WikiLink{}
	}

	json200(w, map[string]any{
		"nodes": pages,
		"edges": links,
	})
}
