package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/ingest"
)

// POST /api/ingest
// Accepts a raw source (file path or URL) and feeds it into the Karpathy-style
// wiki pipeline. Produces a `raw` wiki page + (optional) cleaned concept page.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source          string   `json:"source"`
		Tags            []string `json:"tags"`
		Title           string   `json:"title"`
		AutoSynthesize  *bool    `json:"auto_synthesize"`
		SkipLinkSuggest bool     `json:"skip_link_suggest"`
	}
	if err := decodeBody(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if strings.TrimSpace(req.Source) == "" {
		jsonErr(w, http.StatusBadRequest, "source is required")
		return
	}
	if len(req.Source) > 2000 {
		jsonErr(w, http.StatusBadRequest, "source exceeds 2000 character limit")
		return
	}
	for _, t := range req.Tags {
		if len(t) > 100 {
			jsonErr(w, http.StatusBadRequest, "tag exceeds 100 character limit")
			return
		}
	}

	if s.insight == nil {
		jsonErr(w, http.StatusServiceUnavailable, "insight engine not initialized")
		return
	}

	autoSynth := true
	if req.AutoSynthesize != nil {
		autoSynth = *req.AutoSynthesize
	}

	res, err := s.insight.Ingest(r.Context(), req.Source, ingest.Options{
		Tags:            req.Tags,
		Title:           req.Title,
		AutoSynthesize:  autoSynth,
		SkipLinkSuggest: req.SkipLinkSuggest,
	})
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("ingest failed: %v", err))
		return
	}

	json200(w, res)
}

// POST /api/wiki/vault/pull — bi-directional sync: pull vault .md edits into DB.
func (s *Server) handleVaultPull(w http.ResponseWriter, r *http.Request) {
	vs := s.getVaultSync()
	if vs == nil {
		jsonErr(w, http.StatusBadRequest, "vault_path not configured")
		return
	}
	status, err := vs.PullAll(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("vault pull: %v", err))
		return
	}
	json200(w, status)
}

// POST /api/wiki/cluster/run — force a cluster synthesis pass.
func (s *Server) handleClusterRun(w http.ResponseWriter, r *http.Request) {
	if s.insight == nil {
		jsonErr(w, http.StatusServiceUnavailable, "insight engine not initialized")
		return
	}
	res, err := s.insight.RunClusterSynthesis(r.Context())
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, fmt.Sprintf("cluster synthesis: %v", err))
		return
	}
	json200(w, res)
}
