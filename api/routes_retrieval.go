package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleRetrieve(w http.ResponseWriter, r *http.Request) {
	query := queryStr(r, "q")
	corpus := queryStr(r, "corpus") // "code" | "governance" | "" (auto)
	topK := queryInt(r, "top_k", 10)

	if query == "" {
		jsonErr(w, http.StatusBadRequest, "q is required")
		return
	}

	type result struct {
		Source   string  `json:"source"`
		FilePath string  `json:"file_path"`
		Title    string  `json:"title"`
		Excerpt  string  `json:"excerpt"`
		Score    float64 `json:"score"`
		DocType  string  `json:"doc_type,omitempty"`
	}

	var results []result

	useCode := corpus == "" || corpus == "code"
	useGov := corpus == "" || corpus == "governance"

	if useCode && s.vexor.Available() {
		hits, err := s.vexor.Search(query, topK, "auto")
		if err == nil {
			for _, h := range hits {
				results = append(results, result{
					Source:   "code",
					FilePath: h.FilePath,
					Title:    h.Heading,
					Excerpt:  h.Excerpt,
					Score:    h.Score,
				})
			}
		}
	}

	if useGov {
		docs, err := s.db.SearchDocs(query, "", s.projectRoot, topK)
		if err == nil {
			for _, d := range docs {
				results = append(results, result{
					Source:   "governance",
					FilePath: d.FilePath,
					Title:    d.Title,
					Excerpt:  truncate(d.Content, 500),
					Score:    d.Score,
					DocType:  d.DocType,
				})
			}
		}
	}

	if results == nil {
		results = []result{}
	}
	json200(w, map[string]any{
		"results": results,
		"count":   len(results),
		"query":   query,
		"corpus":  corpus,
	})
}

func (s *Server) handleRetrieveStatus(w http.ResponseWriter, r *http.Request) {
	stats, _ := s.db.GovernanceStats()
	json200(w, map[string]any{
		"vexor_available":      s.vexor.Available(),
		"governance_available": true,
		"governance_stats":     stats,
	})
}

func (s *Server) handleReIndex(w http.ResponseWriter, r *http.Request) {
	go func() {
		_ = s.db.IndexGovernance(s.projectRoot)
		stats, _ := s.db.GovernanceStats()
		s.hub.BroadcastJSON("governance_indexed", stats)
	}()
	json200(w, map[string]any{"status": "indexing"})
}

func (s *Server) handleMarkDirty(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Paths) == 0 {
		jsonErr(w, http.StatusBadRequest, "paths required")
		return
	}
	s.markDirty(body.Paths)
	json200(w, map[string]any{"status": "queued", "count": len(body.Paths)})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
