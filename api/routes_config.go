package api

import (
	"net/http"
	"path/filepath"

	"github.com/MartinNevlaha/stratus-v2/config"
)

// GET /api/config/language — returns the current UI language setting.
func (s *Server) handleGetLanguage(w http.ResponseWriter, r *http.Request) {
	json200(w, map[string]string{"language": s.cfg.Language})
}

// PUT /api/config/language — updates the UI language setting.
// Body: {"language":"sk"} — only "en" and "sk" are accepted.
func (s *Server) handlePutLanguage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Language string `json:"language"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid language")
		return
	}

	if !config.ValidLanguage(body.Language) {
		jsonErr(w, http.StatusBadRequest, "invalid language")
		return
	}

	prev := s.cfg.Language
	s.cfg.Language = body.Language
	if err := s.cfg.Save(filepath.Join(s.projectRoot, ".stratus.json")); err != nil {
		s.cfg.Language = prev
		jsonErr(w, http.StatusInternalServerError, "save config: "+err.Error())
		return
	}

	json200(w, map[string]string{"language": s.cfg.Language})
}
