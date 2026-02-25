package api

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/terminal"
	"github.com/MartinNevlaha/stratus-v2/vexor"
)

// Server holds all dependencies and the HTTP mux.
type Server struct {
	db            *db.DB
	coordinator   *orchestration.Coordinator
	vexor         *vexor.Client
	hub           *Hub
	terminal      *terminal.Manager
	projectRoot   string
	sttEndpoint   string
	sttModel      string
	staticFiles   fs.FS
	version       string
	syncedVersion string   // version when assets were last refreshed to disk
	skippedFiles  []string // asset files skipped in last refresh (user-customized)

	dirtyFiles map[string]struct{}
	dirtyMu    sync.Mutex
	dirtyCh    chan struct{} // buffered(1) signal channel
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
	sttModel string,
	staticFiles fs.FS,
	version string,
	syncedVersion string,
	skippedFiles []string,
) *Server {
	s := &Server{
		db:            database,
		coordinator:   coord,
		vexor:         vexorClient,
		hub:           hub,
		terminal:      termMgr,
		projectRoot:   projectRoot,
		sttEndpoint:   sttEndpoint,
		sttModel:      sttModel,
		staticFiles:   staticFiles,
		version:       version,
		syncedVersion: syncedVersion,
		skippedFiles:  skippedFiles,
		dirtyFiles:    make(map[string]struct{}),
		dirtyCh:       make(chan struct{}, 1),
	}
	go s.indexWorker()
	return s
}

// markDirty adds file paths to the dirty set and signals the index worker.
func (s *Server) markDirty(paths []string) {
	s.dirtyMu.Lock()
	for _, p := range paths {
		s.dirtyFiles[p] = struct{}{}
	}
	s.dirtyMu.Unlock()
	select {
	case s.dirtyCh <- struct{}{}:
	default:
	}
}

// indexWorker runs a debounced background loop that calls vexor.Index whenever
// files are marked dirty. It waits for a 5s quiet period after the last dirty
// signal before indexing, and falls back to a full reindex when the batch
// exceeds 20 files.
func (s *Server) indexWorker() {
	const quietPeriod = 5 * time.Second
	const maxBatch = 20

	for {
		<-s.dirtyCh // wait for first dirty signal

		timer := time.NewTimer(quietPeriod)
	debounce:
		for {
			select {
			case <-s.dirtyCh:
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(quietPeriod)
			case <-timer.C:
				break debounce
			}
		}

		s.dirtyMu.Lock()
		files := make([]string, 0, len(s.dirtyFiles))
		for f := range s.dirtyFiles {
			files = append(files, f)
		}
		s.dirtyFiles = make(map[string]struct{})
		s.dirtyMu.Unlock()

		if len(files) == 0 || !s.vexor.Available() {
			continue
		}
		if len(files) > maxBatch {
			files = nil // full reindex
		}
		if err := s.vexor.Index(files); err != nil {
			log.Printf("vexor reindex: %v", err)
		}
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
	mux.HandleFunc("POST /api/retrieve/dirty", s.handleMarkDirty)

	// Orchestration
	mux.HandleFunc("GET /api/workflows", s.handleListWorkflows)
	mux.HandleFunc("POST /api/workflows", s.handleStartWorkflow)
	mux.HandleFunc("GET /api/workflows/{id}", s.handleGetWorkflow)
	mux.HandleFunc("PUT /api/workflows/{id}/phase", s.handleTransitionPhase)
	mux.HandleFunc("POST /api/workflows/{id}/delegate", s.handleRecordDelegation)
	mux.HandleFunc("POST /api/workflows/{id}/tasks", s.handleSetTasks)
	mux.HandleFunc("POST /api/workflows/{id}/tasks/{index}/start", s.handleStartTask)
	mux.HandleFunc("POST /api/workflows/{id}/tasks/{index}/complete", s.handleCompleteTask)
	mux.HandleFunc("DELETE /api/workflows/{id}", s.handleDeleteWorkflow)
	mux.HandleFunc("PATCH /api/workflows/{id}/session", s.handleSetWorkflowSession)
	mux.HandleFunc("GET /api/workflows/{id}/dispatch", s.handleDispatch)

	// Learning
	mux.HandleFunc("GET /api/learning/candidates", s.handleListCandidates)
	mux.HandleFunc("POST /api/learning/candidates", s.handleSaveCandidate)
	mux.HandleFunc("GET /api/learning/proposals", s.handleListProposals)
	mux.HandleFunc("POST /api/learning/proposals", s.handleSaveProposal)
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
