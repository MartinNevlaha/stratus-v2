package wiki_engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/prompts"
	"github.com/google/uuid"
)

// SynthesisResult holds an LLM-generated answer and its supporting metadata.
type SynthesisResult struct {
	Answer     string     `json:"answer"`
	Citations  []Citation `json:"citations"`
	WikiPageID *string    `json:"wiki_page_id,omitempty"`
	TokensUsed int        `json:"tokens_used"`
}

// Citation represents a single reference extracted from an LLM response.
type Citation struct {
	SourceType string  `json:"source_type"`
	SourceID   string  `json:"source_id"`
	Excerpt    string  `json:"excerpt"`
	Relevance  float64 `json:"relevance"`
}

// citationPattern matches [source_type:source_id] inline citation markers.
var citationPattern = regexp.MustCompile(`\[(\w+):([^\]]+)\]`)

// maxContextChars is the maximum number of characters sent to the LLM as source context.
const maxContextChars = 12000

// Synthesizer handles LLM-powered Q&A synthesis and page generation.
type Synthesizer struct {
	store     WikiStore
	llmClient LLMClient
}

// NewSynthesizer returns a Synthesizer backed by the given store and LLM client.
func NewSynthesizer(store WikiStore, llm LLMClient) *Synthesizer {
	return &Synthesizer{store: store, llmClient: llm}
}

// SynthesizeAnswer searches wiki pages relevant to query, calls the LLM to
// produce a markdown answer with inline citations, and optionally persists the
// answer as a new wiki page.
//
// maxSources is capped at 50 to avoid exceeding context limits.
func (s *Synthesizer) SynthesizeAnswer(ctx context.Context, query string, maxSources int, persist bool) (*SynthesisResult, error) {
	if maxSources <= 0 || maxSources > 50 {
		maxSources = 50
	}

	pages, err := s.store.SearchPages(query, "", maxSources)
	if err != nil {
		return nil, fmt.Errorf("synthesize answer: search pages: %w", err)
	}

	sourceContext := buildSourceContext(pages)

	systemPrompt := prompts.Compose(prompts.WikiSynthesis, prompts.ObsidianMarkdown)
	userMessage := fmt.Sprintf("Query: %s\n\nSource pages:\n%s", query, sourceContext)

	resp, err := s.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: userMessage},
		},
		MaxTokens:   2048,
		Temperature: 0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("synthesize answer: llm complete: %w", err)
	}

	citations := s.ExtractCitations(resp.Content)

	result := &SynthesisResult{
		Answer:     resp.Content,
		Citations:  citations,
		TokensUsed: resp.InputTokens + resp.OutputTokens,
	}

	if persist {
		pageID, saveErr := s.persistAnswerPage(ctx, query, resp.Content)
		if saveErr != nil {
			return nil, fmt.Errorf("synthesize answer: persist page: %w", saveErr)
		}
		result.WikiPageID = &pageID
	}

	return result, nil
}

// GeneratePageContent calls the LLM to produce markdown content for a wiki page
// of the given type, seeded with sourceData.
func (s *Synthesizer) GeneratePageContent(ctx context.Context, title string, sourceData string, pageType string) (string, error) {
	systemPrompt := prompts.Compose(prompts.WikiPageGeneration, prompts.ObsidianMarkdown)
	userMessage := fmt.Sprintf("Title: %s\n\nSource data:\n%s", title, sourceData)

	resp, err := s.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: userMessage},
		},
		MaxTokens:   2048,
		Temperature: 0.4,
	})
	if err != nil {
		return "", fmt.Errorf("generate page content: %w", err)
	}

	return resp.Content, nil
}

// ExtractCitations parses all [source_type:source_id] markers from text and
// returns a Citation slice. It is a pure function; no LLM call is made.
func (s *Synthesizer) ExtractCitations(text string) []Citation {
	matches := citationPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return []Citation{}
	}

	seen := make(map[string]bool)
	citations := make([]Citation, 0, len(matches))

	for _, m := range matches {
		sourceType := m[1]
		sourceID := m[2]
		key := sourceType + ":" + sourceID
		if seen[key] {
			continue
		}
		seen[key] = true

		citations = append(citations, Citation{
			SourceType: sourceType,
			SourceID:   sourceID,
			Excerpt:    "",
			Relevance:  1.0,
		})
	}

	return citations
}

// --- helpers ---

// buildSourceContext assembles a truncated text block from wiki pages to send
// to the LLM. It respects maxContextChars to avoid overrunning token budgets.
func buildSourceContext(pages []db.WikiPage) string {
	var sb strings.Builder
	remaining := maxContextChars

	for _, p := range pages {
		if remaining <= 0 {
			break
		}

		title := fmt.Sprintf("### %s (id: %s)\n", p.Title, p.ID)
		content := p.Content
		if len(title)+len(content) > remaining {
			// Truncate content to fit within the remaining budget.
			available := remaining - len(title)
			if available <= 0 {
				break
			}
			if available < len(content) {
				content = content[:available]
			}
		}

		sb.WriteString(title)
		sb.WriteString(content)
		sb.WriteString("\n\n")
		remaining -= len(title) + len(content) + 2
	}

	return sb.String()
}

// persistAnswerPage saves the synthesized answer as a new wiki page and returns
// the newly created page ID.
func (s *Synthesizer) persistAnswerPage(ctx context.Context, query string, content string) (string, error) {
	_ = ctx // reserved for future async use

	page := &db.WikiPage{
		ID:          uuid.NewString(),
		PageType:    "answer",
		Title:       query,
		Content:     content,
		Status:      "published",
		GeneratedBy: "query",
	}

	if err := s.store.SavePage(page); err != nil {
		return "", err
	}

	return page.ID, nil
}
