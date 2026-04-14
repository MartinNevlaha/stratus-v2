package api

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/insight/events"
)

const (
	prefetchDedupeWindow = 60 * time.Second
	prefetchTimeout      = 10 * time.Second
	prefetchTopK         = 5
)

// prefetchQuery is one retrieve invocation the prefetch worker executes.
type prefetchQuery struct {
	Corpus string `json:"corpus"` // "code" | "governance" | "wiki"
	Query  string `json:"query"`
}

// prefetcher listens for phase-transition events and pre-fetches relevant
// context (code + governance + wiki) so the next delegated agent can read it
// from a single memory event instead of re-querying from scratch.
type prefetcher struct {
	server  *Server
	mu      sync.Mutex
	lastRun map[string]time.Time // key: "wfID|phase"
}

func newPrefetcher(s *Server) *prefetcher {
	return &prefetcher{
		server:  s,
		lastRun: make(map[string]time.Time),
	}
}

// handleEvent is the events.Handler registered on the event bus.
func (p *prefetcher) handleEvent(_ context.Context, evt events.Event) {
	if evt.Type != events.EventPhaseTransition {
		return
	}
	wfID, _ := evt.Payload["workflow_id"].(string)
	wfType, _ := evt.Payload["workflow_type"].(string)
	toPhase, _ := evt.Payload["to_phase"].(string)
	title, _ := evt.Payload["title"].(string)
	if wfID == "" || toPhase == "" {
		return
	}
	// handleAnalyzeWorkflow (routes_orchestration.go) already prefetches on
	// bug.analyze via direct vexor/governance calls — skip to avoid duplicates.
	if wfType == "bug" && toPhase == "analyze" {
		return
	}

	key := wfID + "|" + toPhase
	p.mu.Lock()
	if last, ok := p.lastRun[key]; ok && time.Since(last) < prefetchDedupeWindow {
		p.mu.Unlock()
		return
	}
	p.lastRun[key] = time.Now()
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
	defer cancel()
	p.runPrefetch(ctx, wfID, wfType, toPhase, title)
}

func (p *prefetcher) runPrefetch(ctx context.Context, wfID, wfType, toPhase, title string) {
	queries := buildPrefetchQueries(wfType, toPhase, title)
	if len(queries) == 0 {
		return
	}

	var allResults []retrieveResult
	for _, q := range queries {
		if ctx.Err() != nil {
			return
		}
		hits := p.server.runRetrieve(q.Query, q.Corpus, prefetchTopK)
		allResults = append(allResults, hits...)
	}

	if len(allResults) == 0 {
		return
	}

	refs := map[string]any{
		"workflow_id":   wfID,
		"workflow_type": wfType,
		"phase":         toPhase,
		"queries":       queries,
		"results":       allResults,
	}

	_, err := p.server.db.SaveEvent(db.SaveEventInput{
		Actor:      "stratus",
		Scope:      "workflow",
		Type:       "context_prefetch",
		Title:      fmt.Sprintf("Phase prefetch: %s/%s", wfID, toPhase),
		Text:       summarizePrefetch(allResults),
		Tags:       []string{"context_prefetch", wfType, toPhase},
		Refs:       refs,
		Importance: 0.5,
	})
	if err != nil {
		log.Printf("prefetch: save event failed (wf=%s phase=%s): %v", wfID, toPhase, err)
	}
}

// buildPrefetchQueries returns per-phase query heuristics. Returns nil if the
// phase has no prefetch strategy (or title is empty).
func buildPrefetchQueries(wfType, toPhase, title string) []prefetchQuery {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}

	switch wfType + "." + toPhase {
	case "spec.plan", "spec.discovery", "spec.design":
		return []prefetchQuery{
			{Corpus: "governance", Query: title},
			{Corpus: "wiki", Query: title},
		}
	case "spec.implement":
		return []prefetchQuery{
			{Corpus: "code", Query: title},
			{Corpus: "wiki", Query: title},
		}
	case "spec.verify", "bug.review", "e2e.heal":
		return []prefetchQuery{
			{Corpus: "governance", Query: "test review security " + title},
			{Corpus: "code", Query: title},
		}
	case "spec.governance", "spec.accept":
		return []prefetchQuery{
			{Corpus: "governance", Query: "rule ADR " + title},
		}
	case "bug.fix":
		return []prefetchQuery{
			{Corpus: "code", Query: title},
			{Corpus: "wiki", Query: title},
		}
	case "e2e.setup", "e2e.plan", "e2e.generate":
		return []prefetchQuery{
			{Corpus: "code", Query: "test e2e " + title},
			{Corpus: "wiki", Query: title},
		}
	}
	return nil
}

func summarizePrefetch(results []retrieveResult) string {
	if len(results) == 0 {
		return "no results"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d prefetch results", len(results))
	for i, r := range results {
		if i >= 3 {
			break
		}
		sb.WriteString("\n- ")
		switch {
		case r.Title != "":
			sb.WriteString(r.Title)
		case r.FilePath != "":
			sb.WriteString(r.FilePath)
		default:
			sb.WriteString(r.Source)
		}
	}
	return sb.String()
}
