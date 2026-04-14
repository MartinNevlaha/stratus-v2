package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// retrieveResult is a single hit returned by runRetrieve, shared between the
// HTTP handler and internal callers (e.g. the phase-transition prefetcher).
type retrieveResult struct {
	Source         string  `json:"source"`
	FilePath       string  `json:"file_path"`
	Title          string  `json:"title"`
	Excerpt        string  `json:"excerpt"`
	Score          float64 `json:"score"`
	DocType        string  `json:"doc_type,omitempty"`
	PageType       string  `json:"page_type,omitempty"`
	StalenessScore float64 `json:"staleness_score,omitempty"`
}

// runRetrieve is the core search logic shared by handleRetrieve and internal
// callers. corpus is one of "code" | "governance" | "wiki" | "" (auto = all).
// In auto mode, wiki results are capped at topK/3 so they don't drown out code
// and governance hits.
func (s *Server) runRetrieve(query, corpus string, topK int) []retrieveResult {
	var results []retrieveResult

	useCode := corpus == "" || corpus == "code"
	useGov := corpus == "" || corpus == "governance"
	useWiki := corpus == "" || corpus == "wiki"

	if useCode && s.vexor.Available() {
		hits, err := s.vexor.Search(query, topK, "auto")
		if err == nil {
			log.Printf("[vexor search] query=%q results=%d", query, len(hits))
			for _, h := range hits {
				results = append(results, retrieveResult{
					Source:   "code",
					FilePath: h.FilePath,
					Title:    h.Heading,
					Excerpt:  h.Excerpt,
					Score:    h.Score,
				})
			}
		} else {
			log.Printf("[vexor search] query=%q error=%v", query, err)
		}
	}

	if useGov {
		docs, err := s.db.SearchDocs(query, "", s.projectRoot, topK)
		if err == nil {
			log.Printf("[governance search] query=%q results=%d", query, len(docs))
			for _, d := range docs {
				results = append(results, retrieveResult{
					Source:   "governance",
					FilePath: d.FilePath,
					Title:    d.Title,
					Excerpt:  truncate(d.Content, 500),
					Score:    d.Score,
					DocType:  d.DocType,
				})
			}
		} else {
			log.Printf("[governance search] query=%q error=%v", query, err)
		}
	}

	if useWiki {
		wikiLimit := topK
		if corpus == "" {
			wikiLimit = topK / 3
			if wikiLimit < 1 {
				wikiLimit = 1
			}
		}

		wikiPages, err := s.db.SearchWikiPages(query, "", wikiLimit)
		if err == nil {
			log.Printf("[wiki search] query=%q results=%d", query, len(wikiPages))
			for i, p := range wikiPages {
				score := 1.0 - float64(i)*0.1
				if score < 0.1 {
					score = 0.1
				}
				if p.StalenessScore > 0.7 {
					score *= 0.5
				}
				// Evolution findings boost — validated system-generated insights
				// rank slightly higher than user-written pages.
				if p.GeneratedBy == "evolution" {
					score *= 1.2
				}
				results = append(results, retrieveResult{
					Source:         "wiki",
					Title:          p.Title,
					Excerpt:        truncate(p.Content, 500),
					Score:          score,
					PageType:       p.PageType,
					StalenessScore: p.StalenessScore,
				})
			}
		} else {
			log.Printf("[wiki search] query=%q error=%v", query, err)
		}
	}

	return results
}

func (s *Server) handleRetrieve(w http.ResponseWriter, r *http.Request) {
	query := queryStr(r, "q")
	corpus := queryStr(r, "corpus") // "code" | "governance" | "wiki" | "" (auto)
	topK := queryInt(r, "top_k", 10)

	if query == "" {
		jsonErr(w, http.StatusBadRequest, "q is required")
		return
	}

	if corpus != "" && corpus != "code" && corpus != "governance" && corpus != "wiki" {
		jsonErr(w, http.StatusBadRequest, "invalid corpus value, must be: code, governance, wiki, or empty")
		return
	}

	results := s.runRetrieve(query, corpus, topK)
	if results == nil {
		results = []retrieveResult{}
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
	wikiCount, _ := s.db.WikiPageCount()
	json200(w, map[string]any{
		"vexor_available":      s.vexor.Available(),
		"governance_available": true,
		"wiki_available":       wikiCount > 0,
		"wiki_page_count":      wikiCount,
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
