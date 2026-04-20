package wiki_engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/prompts"
	"github.com/google/uuid"
)

// LLMClient abstracts LLM completion calls for testability.
// It is the common interface shared by WikiEngine and Synthesizer.
type LLMClient interface {
	Complete(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error)
}

// IngestResult holds counts from a RunIngest call.
type IngestResult struct {
	PagesCreated int
	PagesUpdated int
	LinksCreated int
}

// MaintenanceResult holds counts from a RunMaintenance call.
type MaintenanceResult struct {
	PagesScored      int
	PagesMarkedStale int
}

// WikiEngine orchestrates wiki ingest and maintenance workflows.
type WikiEngine struct {
	store      WikiStore
	llmClient  LLMClient
	config     func() config.WikiConfig
	mu         sync.Mutex
	lastIngest time.Time
}

// NewWikiEngine creates a WikiEngine. llm may be nil (fail-open: ingest logs a
// warning and returns an empty result rather than erroring).
func NewWikiEngine(store WikiStore, llm LLMClient, configFn func() config.WikiConfig) *WikiEngine {
	return &WikiEngine{
		store:     store,
		llmClient: llm,
		config:    configFn,
	}
}

// RunIngest is the main ingest workflow. It checks configuration, then ensures
// a knowledge index page exists (creating or updating it). If the LLM client
// is nil the method logs a warning and returns an empty IngestResult (fail-open).
func (e *WikiEngine) RunIngest(ctx context.Context) (*IngestResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cfg := e.config()
	if !cfg.Enabled {
		slog.Info("wiki_engine: ingest skipped — wiki disabled in config")
		return &IngestResult{}, nil
	}

	if e.llmClient == nil {
		slog.Warn("wiki_engine: ingest skipped — no LLM client configured")
		return &IngestResult{}, nil
	}

	result := &IngestResult{}

	// Check whether an index page already exists.
	indexPages, _, err := e.store.ListPages(db.WikiPageFilters{
		PageType: "index",
		Limit:    1,
	})
	if err != nil {
		return nil, fmt.Errorf("wiki engine: run ingest: list index pages: %w", err)
	}

	// Fetch all published non-index pages to build / refresh the index.
	allPages, _, err := e.store.ListPages(db.WikiPageFilters{
		Status: "published",
		Limit:  cfg.MaxPagesPerIngest,
	})
	if err != nil {
		return nil, fmt.Errorf("wiki engine: run ingest: list pages: %w", err)
	}

	// Filter out index pages from the listing — they should not reference themselves.
	var contentPages []db.WikiPage
	for _, p := range allPages {
		if p.PageType != "index" {
			contentPages = append(contentPages, p)
		}
	}

	if len(indexPages) == 0 {
		// No index page yet — only create one when there are pages to index.
		if len(contentPages) == 0 {
			e.lastIngest = time.Now()
			slog.Info("wiki_engine: ingest complete — no content pages to index")
			return result, nil
		}

		var sb strings.Builder
		sb.WriteString("# Knowledge Wiki Index\n\n")
		for _, p := range contentPages {
			sb.WriteString(fmt.Sprintf("- **%s** (%s) — %s\n", p.Title, p.PageType, p.Status))
		}

		indexPage := &db.WikiPage{
			ID:          uuid.NewString(),
			PageType:    "index",
			Title:       "Knowledge Wiki Index",
			Content:     sb.String(),
			Status:      "published",
			GeneratedBy: "ingest",
			Version:     1,
		}
		if err := e.store.SavePage(indexPage); err != nil {
			return nil, fmt.Errorf("wiki engine: run ingest: save index page: %w", err)
		}
		result.PagesCreated++
		slog.Info("wiki_engine: ingest created index page", "page_id", indexPage.ID)
	} else {
		// Update the existing index page to reflect current state.
		existing := indexPages[0]

		var sb strings.Builder
		sb.WriteString("# Knowledge Wiki Index\n\n")
		sb.WriteString(fmt.Sprintf("*Updated: %s*\n\n", time.Now().UTC().Format(time.RFC3339)))
		for _, p := range contentPages {
			sb.WriteString(fmt.Sprintf("- **%s** (%s) — staleness: %.1f%%\n",
				p.Title, p.PageType, p.StalenessScore*100))
		}

		existing.Content = sb.String()
		if err := e.store.UpdatePage(&existing); err != nil {
			return nil, fmt.Errorf("wiki engine: run ingest: update index page: %w", err)
		}
		result.PagesUpdated++
		slog.Info("wiki_engine: ingest updated index page", "page_id", existing.ID)
	}

	e.lastIngest = time.Now()
	slog.Info("wiki_engine: ingest complete",
		"pages_created", result.PagesCreated,
		"pages_updated", result.PagesUpdated,
	)
	return result, nil
}

// RunMaintenance computes a staleness score for every published page and
// persists it via the store. Pages that exceed the configured threshold are
// automatically marked stale by the store layer.
//
// Staleness formula:
//
//	0.3*(days_since_update/30) + 0.2*(version==1 ? 1 : 0) + 0.2*(no_incoming_links ? 1 : 0)
func (e *WikiEngine) RunMaintenance(ctx context.Context) (*MaintenanceResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cfg := e.config()

	pages, _, err := e.store.ListPages(db.WikiPageFilters{
		Status: "published",
		Limit:  1000,
	})
	if err != nil {
		return nil, fmt.Errorf("wiki engine: run maintenance: list pages: %w", err)
	}

	result := &MaintenanceResult{}

	for _, p := range pages {
		score, err := e.computeStaleness(p)
		if err != nil {
			slog.Warn("wiki_engine: maintenance: compute staleness failed",
				"page_id", p.ID, "error", err)
			continue
		}

		if err := e.store.UpdatePageStaleness(p.ID, score); err != nil {
			slog.Warn("wiki_engine: maintenance: update staleness failed",
				"page_id", p.ID, "error", err)
			continue
		}

		result.PagesScored++
		if score >= cfg.StalenessThreshold {
			result.PagesMarkedStale++
		}
	}

	slog.Info("wiki_engine: maintenance complete",
		"pages_scored", result.PagesScored,
		"pages_marked_stale", result.PagesMarkedStale,
	)
	return result, nil
}

// computeStaleness calculates the staleness score for a single page.
func (e *WikiEngine) computeStaleness(p db.WikiPage) (float64, error) {
	updatedAt, err := time.Parse(time.RFC3339Nano, p.UpdatedAt)
	if err != nil {
		// Try RFC3339 without nanoseconds.
		updatedAt, err = time.Parse(time.RFC3339, p.UpdatedAt)
		if err != nil {
			return 0, fmt.Errorf("wiki engine: parse updated_at %q: %w", p.UpdatedAt, err)
		}
	}

	daysSince := time.Since(updatedAt).Hours() / 24

	var ageFactor float64 = daysSince / 30
	if ageFactor > 1 {
		ageFactor = 1
	}

	var versionFactor float64
	if p.Version == 1 {
		versionFactor = 1
	}

	incomingLinks, err := e.store.ListLinksTo(p.ID)
	if err != nil {
		return 0, fmt.Errorf("wiki engine: list links to %q: %w", p.ID, err)
	}

	var linkFactor float64
	if len(incomingLinks) == 0 {
		linkFactor = 1
	}

	score := 0.3*ageFactor + 0.2*versionFactor + 0.2*linkFactor
	return score, nil
}

// GeneratePageFromData calls the LLM to produce wiki page content, saves the
// page to the store, and persists the provided source references. It returns
// the newly created WikiPage.
func (e *WikiEngine) GeneratePageFromData(
	ctx context.Context,
	title string,
	content string,
	pageType string,
	sourceRefs []db.WikiPageRef,
) (*db.WikiPage, error) {
	if e.llmClient == nil {
		return nil, fmt.Errorf("wiki engine: generate page: no LLM client configured")
	}

	systemPrompt := prompts.Compose(prompts.WikiPageGeneration, prompts.ObsidianMarkdown)
	userMessage := fmt.Sprintf("Title: %s\nType: %s\n\nSource material:\n%s", title, pageType, content)

	resp, err := e.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: userMessage},
		},
		MaxTokens:   8192,
		Temperature: 0.4,
	})
	if err != nil {
		return nil, fmt.Errorf("wiki engine: generate page: LLM complete: %w", err)
	}

	generated := resp.Content

	page := &db.WikiPage{
		ID:          uuid.NewString(),
		PageType:    pageType,
		Title:       title,
		Content:     generated,
		Status:      "published",
		GeneratedBy: "ingest",
		Version:     1,
	}

	if err := e.store.SavePage(page); err != nil {
		return nil, fmt.Errorf("wiki engine: generate page: save page: %w", err)
	}

	for i := range sourceRefs {
		ref := sourceRefs[i]
		ref.PageID = page.ID
		if err := e.store.SaveRef(&ref); err != nil {
			slog.Warn("wiki_engine: generate page: save ref failed",
				"page_id", page.ID, "source_id", ref.SourceID, "error", err)
		}
	}

	return page, nil
}
