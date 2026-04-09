package api

import (
	"fmt"
	"net/http"

	"github.com/MartinNevlaha/stratus-v2/db"
)

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
	if s.vaultSync == nil {
		jsonErr(w, http.StatusBadRequest, "vault_path not configured")
		return
	}

	result, err := s.vaultSync.SyncAll(r.Context())
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
	if s.vaultSync == nil {
		json200(w, map[string]any{
			"last_sync":  nil,
			"file_count": 0,
			"vault_path": "",
			"errors":     []string{},
		})
		return
	}

	status, err := s.vaultSync.GetStatus()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("get vault status: %v", err))
		return
	}

	json200(w, status)
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
