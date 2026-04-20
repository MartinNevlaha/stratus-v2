package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	wiki_engine "github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
	"github.com/google/uuid"
)

type rebuildLinksRequest struct {
	IncludeSuggester *bool `json:"include_suggester,omitempty"`
	MaxPages         *int  `json:"max_pages,omitempty"`
}

type rebuildLinksResult struct {
	PagesScanned        int            `json:"pages_scanned"`
	OrphansBefore       int            `json:"orphans_before"`
	OrphansAfter        int            `json:"orphans_after"`
	LinksSaved          int            `json:"links_saved"`
	ByType              map[string]int `json:"by_type"`
	SuggesterInvokedFor int            `json:"suggester_invoked_for"`
	SuggesterErrors     int            `json:"suggester_errors"`
	DurationMs          int64          `json:"duration_ms"`
}

type createWikiLinkRequest struct {
	FromPageID string   `json:"from_page_id"`
	ToPageID   string   `json:"to_page_id"`
	LinkType   string   `json:"link_type"`
	Strength   *float64 `json:"strength,omitempty"`
}

// POST /api/wiki/links/rebuild
func (s *Server) handleRebuildWikiLinks(w http.ResponseWriter, r *http.Request) {
	var req rebuildLinksRequest
	// Allow empty body — decode only if body is non-empty.
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeBody(r, &req); err != nil {
			jsonErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	// Apply defaults.
	maxPages := 500
	if req.MaxPages != nil {
		maxPages = *req.MaxPages
	}
	includeSuggester := true
	if req.IncludeSuggester != nil {
		includeSuggester = *req.IncludeSuggester
	}

	// Validate maxPages range.
	if maxPages < 1 || maxPages > 2000 {
		jsonErr(w, http.StatusBadRequest, "max_pages must be between 1 and 2000")
		return
	}

	if !s.rebuildMu.TryLock() {
		jsonErr(w, http.StatusConflict, "wiki link rebuild already in progress")
		return
	}
	defer s.rebuildMu.Unlock()

	startTime := time.Now()

	// Load pages up to maxPages limit.
	pages, total, err := s.db.ListWikiPages(db.WikiPageFilters{Limit: maxPages})
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("list wiki pages: %v", err))
		return
	}
	if total > maxPages {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("dataset has %d pages, exceeds max_pages=%d", total, maxPages))
		return
	}

	orphansBefore, err := s.db.CountOrphanWikiPages()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("count orphan wiki pages: %v", err))
		return
	}

	store := wiki_engine.NewDBWikiStore(s.db)
	linker := wiki_engine.NewLinker(store)

	var linksSaved int

	// Step 7: Cross-references.
	for i := range pages {
		links := linker.DetectCrossReferences(&pages[i], pages)
		if len(links) > 0 {
			count, err := linker.SaveDetectedLinks(links)
			linksSaved += count
			if err != nil {
				slog.Warn("rebuild links: save cross-references", "page_id", pages[i].ID, "err", err)
			}
		}
	}

	// Step 8: Shared-source links — fetch refs once per page.
	refsCache := make(map[string][]db.WikiPageRef, len(pages))
	for _, p := range pages {
		refs, err := s.db.ListWikiPageRefs(p.ID)
		if err != nil {
			slog.Warn("rebuild links: list wiki page refs", "page_id", p.ID, "err", err)
			refs = nil
		}
		refsCache[p.ID] = refs
	}
	for i := 0; i < len(pages); i++ {
		for j := i + 1; j < len(pages); j++ {
			link := linker.DetectSharedSourceLinks(
				&pages[i], refsCache[pages[i].ID],
				&pages[j], refsCache[pages[j].ID],
			)
			if link != nil {
				count, err := linker.SaveDetectedLinks([]db.WikiLink{*link})
				linksSaved += count
				if err != nil {
					slog.Warn("rebuild links: save shared-source link", "err", err)
				}
			}
		}
	}

	// Step 9: Contradictions.
	cLinks := linker.FindContradictions(pages)
	if len(cLinks) > 0 {
		count, err := linker.SaveDetectedLinks(cLinks)
		linksSaved += count
		if err != nil {
			slog.Warn("rebuild links: save contradictions", "err", err)
		}
	}

	// Step 10: Suggester (skip if disabled or no LLM).
	var suggesterInvokedFor, suggesterErrors int
	if includeSuggester && s.guardianLLM != nil {
		// Build set of page IDs that already have links after steps 7-9.
		linkedIDs := make(map[string]bool)
		_, graphLinks, err := s.db.GetWikiGraph("", maxPages*2)
		if err == nil {
			for _, lnk := range graphLinks {
				linkedIDs[lnk.FromPageID] = true
				linkedIDs[lnk.ToPageID] = true
			}
		}

		suggester := wiki_engine.NewLinkSuggester(store, s.guardianLLM)
		for i := range pages {
			if linkedIDs[pages[i].ID] {
				continue
			}
			// Check context cancellation before each LLM call.
			if r.Context().Err() != nil {
				break
			}
			suggesterInvokedFor++
			if _, err := suggester.SuggestAndCreateStubs(r.Context(), &pages[i]); err != nil {
				suggesterErrors++
				slog.Warn("rebuild links: suggester error", "page_id", pages[i].ID, "err", err)
			}
		}
	}

	orphansAfter, _ := s.db.CountOrphanWikiPages()

	byType := wikiLinkCountsByType(s.db)

	json200(w, rebuildLinksResult{
		PagesScanned:        len(pages),
		OrphansBefore:       orphansBefore,
		OrphansAfter:        orphansAfter,
		LinksSaved:          linksSaved,
		ByType:              byType,
		SuggesterInvokedFor: suggesterInvokedFor,
		SuggesterErrors:     suggesterErrors,
		DurationMs:          time.Since(startTime).Milliseconds(),
	})
}

// wikiLinkCountsByType runs a GROUP BY query and returns a map with all 6
// canonical types (defaulting to 0 for types not present).
func wikiLinkCountsByType(database *db.DB) map[string]int {
	result := make(map[string]int, len(db.AllowedWikiLinkTypes))
	for t := range db.AllowedWikiLinkTypes {
		result[t] = 0
	}

	rows, err := database.SQL().Query(
		`SELECT link_type, COUNT(*) FROM wiki_links GROUP BY link_type`,
	)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var lt string
		var cnt int
		if err := rows.Scan(&lt, &cnt); err != nil {
			continue
		}
		result[lt] = cnt
	}
	return result
}

// POST /api/wiki/links
func (s *Server) handleCreateWikiLink(w http.ResponseWriter, r *http.Request) {
	var req createWikiLinkRequest
	if err := decodeBody(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.FromPageID == "" {
		jsonErr(w, http.StatusBadRequest, "from_page_id is required")
		return
	}
	if req.ToPageID == "" {
		jsonErr(w, http.StatusBadRequest, "to_page_id is required")
		return
	}
	if req.FromPageID == req.ToPageID {
		jsonErr(w, http.StatusBadRequest, "self-loops not allowed")
		return
	}

	linkType := strings.ToLower(strings.TrimSpace(req.LinkType))
	if !db.IsValidWikiLinkType(linkType) {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("invalid link_type: %s", req.LinkType))
		return
	}

	if req.Strength != nil && (*req.Strength < 0 || *req.Strength > 1) {
		jsonErr(w, http.StatusBadRequest, "strength must be between 0 and 1")
		return
	}

	fromPage, err := s.db.GetWikiPage(req.FromPageID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("get from_page: %v", err))
		return
	}
	if fromPage == nil {
		jsonErr(w, http.StatusNotFound, "from_page_id not found")
		return
	}

	toPage, err := s.db.GetWikiPage(req.ToPageID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("get to_page: %v", err))
		return
	}
	if toPage == nil {
		jsonErr(w, http.StatusNotFound, "to_page_id not found")
		return
	}

	strength := 0.5
	if req.Strength != nil {
		strength = *req.Strength
	}

	link := &db.WikiLink{
		ID:         uuid.NewString(),
		FromPageID: req.FromPageID,
		ToPageID:   req.ToPageID,
		LinkType:   linkType,
		Strength:   strength,
	}

	if err := s.db.SaveWikiLink(link); err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("save wiki link: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(link)
}

// DELETE /api/wiki/links/{id}
func (s *Server) handleDeleteWikiLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "missing link id")
		return
	}

	deleted, err := s.db.DeleteWikiLinkByID(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("delete wiki link: %v", err))
		return
	}
	if !deleted {
		jsonErr(w, http.StatusNotFound, "link not found")
		return
	}

	json200(w, map[string]bool{"deleted": true})
}
