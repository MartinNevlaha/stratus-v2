package api

import (
	"net/http"
	"path/filepath"

	"github.com/MartinNevlaha/stratus-v2/config"
)

// GET /api/config/phase-routing
func (s *Server) handleGetPhaseRouting(w http.ResponseWriter, r *http.Request) {
	json200(w, s.cfg.PhaseRouting)
}

// PUT /api/config/phase-routing
func (s *Server) handleUpdatePhaseRouting(w http.ResponseWriter, r *http.Request) {
	var incoming config.PhaseRoutingConfig
	if err := decodeBody(r, &incoming); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	s.cfg.PhaseRouting = incoming
	if err := s.cfg.Save(filepath.Join(s.projectRoot, ".stratus.json")); err != nil {
		jsonErr(w, http.StatusInternalServerError, "save config: "+err.Error())
		return
	}

	json200(w, s.cfg.PhaseRouting)
}
