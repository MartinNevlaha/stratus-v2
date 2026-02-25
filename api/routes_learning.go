package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

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

func (s *Server) handleSaveProposal(w http.ResponseWriter, r *http.Request) {
	var p db.Proposal
	if err := decodeBody(r, &p); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	id, err := s.db.SaveProposal(p)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.BroadcastJSON("learning_update", map[string]string{"type": "proposal", "id": id})
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

	// Fetch proposal before recording decision so we can apply it if accepted.
	proposal, err := s.db.GetProposal(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}

	if err := s.db.DecideProposal(id, body.Decision); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Auto-apply: write proposed_content to proposed_path when accepted.
	applied := false
	if body.Decision == "accept" && proposal.ProposedPath != nil && *proposal.ProposedPath != "" && proposal.ProposedContent != "" {
		absPath := filepath.Join(s.projectRoot, *proposal.ProposedPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			log.Printf("learn apply: mkdir %s: %v", absPath, err)
		} else if err := os.WriteFile(absPath, []byte(proposal.ProposedContent), 0644); err != nil {
			log.Printf("learn apply: write %s: %v", absPath, err)
		} else {
			applied = true
			// Re-index governance async so retrieve() finds the new file.
			go func() {
				if err := s.db.IndexGovernance(s.projectRoot); err != nil {
					log.Printf("learn apply: governance reindex: %v", err)
				}
				s.hub.BroadcastJSON("governance_indexed", map[string]string{"source": "learn_apply", "path": *proposal.ProposedPath})
			}()
		}
	}

	s.hub.BroadcastJSON("learning_update", map[string]any{
		"type":        "decision",
		"proposal_id": id,
		"decision":    body.Decision,
		"applied":     applied,
	})
	json200(w, map[string]any{"status": "ok", "proposal_id": id, "decision": body.Decision, "applied": applied})
}
