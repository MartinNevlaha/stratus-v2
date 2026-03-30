package api

import (
	"net/http"
	"strconv"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// GET /api/kb/solutions
func (s *Server) handleListKBSolutions(w http.ResponseWriter, r *http.Request) {
	filters := db.SolutionPatternFilters{
		ProblemClass: queryStr(r, "problem_class"),
		RepoType:     queryStr(r, "repo_type"),
		Limit:        queryInt(r, "limit", 20),
	}
	if v := queryStr(r, "min_success_rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			filters.MinSuccessRate = f
		}
	}
	patterns, err := s.db.ListSolutionPatterns(filters)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if patterns == nil {
		patterns = []db.SolutionPattern{}
	}
	json200(w, patterns)
}

// GET /api/kb/problems
func (s *Server) handleListKBProblems(w http.ResponseWriter, r *http.Request) {
	filters := db.ProblemStatsFilters{
		ProblemClass: queryStr(r, "problem_class"),
		RepoType:     queryStr(r, "repo_type"),
		Limit:        queryInt(r, "limit", 20),
	}
	stats, err := s.db.ListProblemStats(filters)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if stats == nil {
		stats = []db.ProblemStats{}
	}
	json200(w, stats)
}

// GET /api/kb/recommend?problem_class=X&repo_type=Y
func (s *Server) handleKBRecommend(w http.ResponseWriter, r *http.Request) {
	problemClass := queryStr(r, "problem_class")
	repoType := queryStr(r, "repo_type")
	if problemClass == "" {
		jsonErr(w, http.StatusBadRequest, "problem_class is required")
		return
	}

	solution, _ := s.db.GetBestSolutionForProblem(problemClass, repoType)
	bestAgent, agentRate, _ := s.db.GetBestAgentForProblem(problemClass, repoType)

	json200(w, map[string]any{
		"solution":           solution,
		"best_agent":         bestAgent,
		"agent_success_rate": agentRate,
	})
}

// GET /api/kb/stats
func (s *Server) handleKBStats(w http.ResponseWriter, _ *http.Request) {
	patterns, err := s.db.CountSolutionPatterns()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	problems, err := s.db.CountProblemStats()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	json200(w, map[string]int{
		"solution_patterns": patterns,
		"problem_classes":   problems,
	})
}
