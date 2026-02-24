package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	json200(w, map[string]any{
		"status": "ok",
		"ts":     time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	govStats, _ := s.db.GovernanceStats()
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	json200(w, map[string]any{
		"governance":   govStats,
		"ws_clients":   s.hub.ClientCount(),
		"vexor_ok":     s.vexor.Available(),
		"memory_mb":    memStats.Alloc / 1024 / 1024,
	})
}

func (s *Server) handleDashboardState(w http.ResponseWriter, r *http.Request) {
	// Aggregate: active workflows, recent events, learning summary, retrieval status
	workflows, _ := s.coordinator.ListActive()
	if workflows == nil {
		workflows = []*orchestration.WorkflowState{}
	}

	recentEvents, _ := s.db.SearchEvents(searchEventsDefaults())

	candidates, _ := s.db.ListCandidates("pending", 5)
	proposals, _ := s.db.ListProposals("pending", 5)
	govStats, _ := s.db.GovernanceStats()

	json200(w, map[string]any{
		"workflows":        workflows,
		"recent_events":    nilSlice(recentEvents),
		"pending_candidates": nilSlice(candidates),
		"pending_proposals":  nilSlice(proposals),
		"governance":       govStats,
		"vexor_available":  s.vexor.Available(),
		"ws_clients":       s.hub.ClientCount(),
		"ts":               time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleSTTTranscribe(w http.ResponseWriter, r *http.Request) {
	if s.sttEndpoint == "" {
		jsonErr(w, http.StatusServiceUnavailable, "STT not configured")
		return
	}
	// Proxy the multipart audio form to the STT endpoint
	// Use stdlib net/http as a simple proxy
	proxyReq, err := newProxyRequest(r, s.sttEndpoint+"/transcribe")
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, "STT endpoint unavailable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	copyBody(w, resp.Body)
}

func (s *Server) handleSTTStatus(w http.ResponseWriter, r *http.Request) {
	available := false
	if s.sttEndpoint != "" {
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(s.sttEndpoint + "/health")
		if err == nil {
			resp.Body.Close()
			available = resp.StatusCode == http.StatusOK
		}
	}
	json200(w, map[string]any{"available": available, "endpoint": s.sttEndpoint})
}
