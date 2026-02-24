package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/terminal"
	"github.com/MartinNevlaha/stratus-v2/vexor"
)

// Server holds all dependencies and the HTTP mux.
type Server struct {
	db          *db.DB
	coordinator *orchestration.Coordinator
	vexor       *vexor.Client
	hub         *Hub
	terminal    *terminal.Manager
	projectRoot string
	sttEndpoint string
	staticFiles fs.FS
	version     string
}

// NewServer creates the HTTP server with all routes wired up.
func NewServer(
	database *db.DB,
	coord *orchestration.Coordinator,
	vexorClient *vexor.Client,
	hub *Hub,
	termMgr *terminal.Manager,
	projectRoot string,
	sttEndpoint string,
	staticFiles fs.FS,
	version string,
) *Server {
	return &Server{
		db:          database,
		coordinator: coord,
		vexor:       vexorClient,
		hub:         hub,
		terminal:    termMgr,
		projectRoot: projectRoot,
		sttEndpoint: sttEndpoint,
		staticFiles: staticFiles,
		version:     version,
	}
}

// Handler builds and returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/stats", s.handleStats)

	// Memory / events
	mux.HandleFunc("POST /api/events", s.handleSaveEvent)
	mux.HandleFunc("GET /api/events/search", s.handleSearchEvents)
	mux.HandleFunc("GET /api/events/{id}/timeline", s.handleTimeline)
	mux.HandleFunc("POST /api/events/batch", s.handleBatchEvents)

	// Sessions
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)

	// Retrieval
	mux.HandleFunc("GET /api/retrieve", s.handleRetrieve)
	mux.HandleFunc("GET /api/retrieve/status", s.handleRetrieveStatus)
	mux.HandleFunc("POST /api/retrieve/index", s.handleReIndex)

	// Orchestration
	mux.HandleFunc("POST /api/workflows", s.handleStartWorkflow)
	mux.HandleFunc("GET /api/workflows/{id}", s.handleGetWorkflow)
	mux.HandleFunc("PUT /api/workflows/{id}/phase", s.handleTransitionPhase)
	mux.HandleFunc("POST /api/workflows/{id}/delegate", s.handleRecordDelegation)
	mux.HandleFunc("POST /api/workflows/{id}/tasks", s.handleSetTasks)
	mux.HandleFunc("POST /api/workflows/{id}/tasks/{index}/start", s.handleStartTask)
	mux.HandleFunc("POST /api/workflows/{id}/tasks/{index}/complete", s.handleCompleteTask)
	mux.HandleFunc("DELETE /api/workflows/{id}", s.handleAbortWorkflow)
	mux.HandleFunc("GET /api/workflows/{id}/dispatch", s.handleDispatch)

	// Learning
	mux.HandleFunc("GET /api/learning/candidates", s.handleListCandidates)
	mux.HandleFunc("POST /api/learning/candidates", s.handleSaveCandidate)
	mux.HandleFunc("GET /api/learning/proposals", s.handleListProposals)
	mux.HandleFunc("POST /api/learning/proposals/{id}/decide", s.handleDecideProposal)

	// Dashboard
	mux.HandleFunc("GET /api/dashboard/state", s.handleDashboardState)

	// System
	mux.HandleFunc("GET /api/system/version", s.handleVersion)
	mux.HandleFunc("POST /api/system/update", s.handleUpdate)

	// STT
	mux.HandleFunc("POST /api/stt/transcribe", s.handleSTTTranscribe)
	mux.HandleFunc("GET /api/stt/status", s.handleSTTStatus)

	// WebSocket
	mux.HandleFunc("/api/ws", s.hub.ServeWS)
	mux.HandleFunc("/api/terminal/ws", s.terminal.ServeWS)

	// Static files (embedded Svelte SPA) with SPA fallback to index.html
	mux.Handle("/", s.spaHandler())

	return corsMiddleware(mux)
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(port int) error {
	addr := fmt.Sprintf(":%d", port)
	return http.ListenAndServe(addr, s.Handler())
}

// spaHandler serves the embedded Svelte frontend. For any path that doesn't
// correspond to a real file (e.g. /memory, /retrieval), it falls back to
// index.html so the client-side router can take over.
func (s *Server) spaHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.staticFiles))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip leading slash for fs.Stat
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(s.staticFiles, path); err != nil {
			// File not found — serve index.html for SPA routing
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
