package api

import (
	"net/http"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func (s *Server) handleListCandidates(w http.ResponseWriter, r *http.Request) {
	status := queryStr(r, "status")
	limit := queryInt(r, "limit", 50)
	candidates, err := s.db.ListCandidates(status, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if candidates == nil {
		candidates = []db.Candidate{}
	}
	json200(w, map[string]any{"candidates": candidates, "count": len(candidates)})
}

func (s *Server) handleSaveCandidate(w http.ResponseWriter, r *http.Request) {
	var c db.Candidate
	if err := decodeBody(r, &c); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	id, err := s.db.SaveCandidate(c)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("learning_update", map[string]string{"type": "candidate", "id": id})
	json200(w, map[string]any{"id": id})
}

func (s *Server) handleListProposals(w http.ResponseWriter, r *http.Request) {
	status := queryStr(r, "status")
	limit := queryInt(r, "limit", 50)
	proposals, err := s.db.ListProposals(status, limit)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if proposals == nil {
		proposals = []db.Proposal{}
	}
	json200(w, map[string]any{"proposals": proposals, "count": len(proposals)})
}

func (s *Server) handleDecideProposal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Decision string `json:"decision"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := s.db.DecideProposal(id, body.Decision); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.BroadcastJSON("learning_update", map[string]string{"type": "decision", "proposal_id": id, "decision": body.Decision})
	json200(w, map[string]any{"status": "ok", "proposal_id": id, "decision": body.Decision})
}
