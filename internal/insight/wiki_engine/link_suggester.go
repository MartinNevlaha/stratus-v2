package wiki_engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/prompts"
	"github.com/google/uuid"
)

// StubSuggestion is a link target proposed by the LLM that doesn't yet exist
// as a wiki page. New fields (LinkType, Strength, ToTitle) extend the struct
// without breaking existing callers — they are optional with safe defaults.
type StubSuggestion struct {
	Title     string   `json:"title"`
	Rationale string   `json:"rationale"`
	PageType  string   `json:"page_type"`
	Tags      []string `json:"tags"`
	LinkType  string   `json:"link_type"` // related | parent | child | cites
	Strength  float64  `json:"strength"`  // 0..1; default 0.5 when zero
	ToTitle   string   `json:"to_title"`  // target title for existing-page mode
}

// LinkSuggester asks the LLM which concepts in a page deserve their own wiki
// page and creates draft stubs for those that don't exist yet.
type LinkSuggester struct {
	store     WikiStore
	llmClient LLMClient
}

// NewLinkSuggester returns a LinkSuggester.
func NewLinkSuggester(store WikiStore, llmClient LLMClient) *LinkSuggester {
	return &LinkSuggester{store: store, llmClient: llmClient}
}

// Suggest returns link candidates for a page without persisting anything.
func (s *LinkSuggester) Suggest(ctx context.Context, page *db.WikiPage) ([]StubSuggestion, error) {
	if s.llmClient == nil {
		return nil, fmt.Errorf("link suggester: no LLM client")
	}

	content := page.Content
	if len(content) > 8000 {
		content = content[:8000]
	}
	user := fmt.Sprintf("Title: %s\nPage type: %s\n\nContent:\n%s", page.Title, page.PageType, content)

	resp, err := s.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt:   prompts.WikiLinkSuggestion,
		Messages:       []llm.Message{{Role: "user", Content: user}},
		MaxTokens:      100000,
		Temperature:    0.2,
		ResponseFormat: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("link suggester: complete: %w", err)
	}

	var wrapper struct {
		Links []StubSuggestion `json:"links"`
	}
	if err := llm.ParseJSONResponse(resp.Content, &wrapper); err != nil {
		return nil, fmt.Errorf("link suggester: parse: %w", err)
	}
	return filterSuggestions(wrapper.Links), nil
}

// SuggestAndCreateStubs runs Suggest and persists the suggestions.
// For each suggestion with ToTitle set, it looks up the existing page by
// case-insensitive title and saves a typed link to it. If the page is not
// found it falls back to stub creation. For suggestions without ToTitle a
// new stub page is created. Returns the number of stubs actually created.
func (s *LinkSuggester) SuggestAndCreateStubs(ctx context.Context, page *db.WikiPage) (int, error) {
	suggestions, err := s.Suggest(ctx, page)
	if err != nil {
		return 0, err
	}

	created := 0
	for _, sug := range suggestions {
		linkType := normalizeLinkType(sug.LinkType)
		strength := defaultedStrength(sug.Strength)

		if sug.ToTitle != "" {
			// Existing-page link mode.
			target, err := s.store.FindWikiPageByTitleNewest(sug.ToTitle)
			if err != nil {
				slog.Warn("link suggester: lookup existing page failed", "title", sug.ToTitle, "err", err)
				// fall through to stub creation below
			}
			if target != nil {
				if err := s.store.SaveLink(&db.WikiLink{
					ID:         uuid.NewString(),
					FromPageID: page.ID,
					ToPageID:   target.ID,
					LinkType:   linkType,
					Strength:   strength,
				}); err != nil {
					slog.Warn("link suggester: save link to existing failed", "from", page.ID, "to", target.ID, "err", err)
				}
				continue
			}
			// Not found — fall back to stub creation using ToTitle as the stub title.
			sug.Title = sug.ToTitle
		}

		// Stub creation mode.
		if sug.Title == "" {
			continue
		}
		if s.pageTitleExists(sug.Title) {
			continue
		}
		pageType := sug.PageType
		if pageType != db.PageTypeConcept && pageType != db.PageTypeEntity {
			pageType = db.PageTypeConcept
		}
		stub := &db.WikiPage{
			ID:       uuid.NewString(),
			PageType: pageType,
			Title:    sug.Title,
			Content: "## Stub\n\n" + sug.Rationale +
				"\n\n*This page was auto-generated as a stub. TODO: expand with concrete detail.*",
			Status:      "draft",
			GeneratedBy: db.GeneratedByLinkSuggester,
			Tags:        sug.Tags,
			Version:     1,
		}
		if err := s.store.SavePage(stub); err != nil {
			slog.Warn("link suggester: save stub failed", "title", sug.Title, "err", err)
			continue
		}
		if err := s.store.SaveLink(&db.WikiLink{
			ID:         uuid.NewString(),
			FromPageID: page.ID,
			ToPageID:   stub.ID,
			LinkType:   linkType,
			Strength:   strength,
		}); err != nil {
			slog.Warn("link suggester: save link failed", "from", page.ID, "to", stub.ID, "err", err)
		}
		created++
	}
	return created, nil
}

// normalizeLinkType lowercases and trims t, returning it if valid or "related"
// with a warning if not.
func normalizeLinkType(t string) string {
	trimmed := strings.ToLower(strings.TrimSpace(t))
	if db.IsValidWikiLinkType(trimmed) {
		return trimmed
	}
	slog.Warn("link suggester: invalid link_type from LLM, falling back to related", "got", t)
	return "related"
}

// defaultedStrength returns s if s > 0, otherwise 0.5.
func defaultedStrength(s float64) float64 {
	if s > 0 {
		return s
	}
	return 0.5
}

// pageTitleExists performs a best-effort existence check via SearchPages.
// False negatives just mean we create a duplicate-titled stub — acceptable
// given draft status.
func (s *LinkSuggester) pageTitleExists(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return true // treat as existing → skip
	}
	results, err := s.store.SearchPages(title, "", 5)
	if err != nil {
		return false
	}
	for _, p := range results {
		if strings.EqualFold(p.Title, title) {
			return true
		}
	}
	return false
}

func filterSuggestions(in []StubSuggestion) []StubSuggestion {
	if len(in) == 0 {
		return nil
	}
	out := make([]StubSuggestion, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		// Deduplicate by effective title (ToTitle takes precedence, then Title).
		key := strings.ToLower(strings.TrimSpace(s.ToTitle))
		if key == "" {
			s.Title = strings.TrimSpace(s.Title)
			if s.Title == "" {
				continue
			}
			key = strings.ToLower(s.Title)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
		if len(out) >= 5 {
			break
		}
	}
	return out
}
