package wiki_engine

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/prompts"
	"github.com/google/uuid"
)

type SynthesisResult struct {
	Answer     string     `json:"answer"`
	Citations  []Citation `json:"citations"`
	WikiPageID *string    `json:"wiki_page_id,omitempty"`
	TokensUsed int        `json:"tokens_used"`
}

type Citation struct {
	SourceType string  `json:"source_type"`
	SourceID   string  `json:"source_id"`
	Excerpt    string  `json:"excerpt"`
	Relevance  float64 `json:"relevance"`
}

var citationPattern = regexp.MustCompile(`\[(\w+):([^\]]+)\]`)
const maxContextChars = 12000
var pageTypePriority = map[string]int{
	"concept": 0, "entity": 1, "summary": 2, "topic": 3,
	"raw": 4, "answer": 5, "index": 6,
}
type Synthesizer struct {
	store     WikiStore
	llmClient LLMClient
}

func NewSynthesizer(store WikiStore, llm LLMClient) *Synthesizer {
	return &Synthesizer{store: store, llmClient: llm}
}
func (s *Synthesizer) SynthesizeAnswer(ctx context.Context, query string, maxSources int, persist bool) (*SynthesisResult, error) {
	if maxSources <= 0 || maxSources > 50 {
		maxSources = 50
	}
	pages, err := s.store.SearchPages(query, "", maxSources)
	if err != nil {
		return nil, fmt.Errorf("synthesize answer: search pages: %w", err)
	}
	ranked := rankPages(pages)
	sourceContext := s.buildSourceContext(ctx, ranked)
	if sourceContext == "" {
		return &SynthesisResult{
			Answer:     "No relevant wiki pages found for this query.",
			Citations:  []Citation{},
			TokensUsed: 0,
		}, nil
	}
	systemPrompt := prompts.Compose(prompts.WikiSynthesis, prompts.ObsidianMarkdown)
	userMessage := fmt.Sprintf("Query: %s\n\nSource pages:\n%s", query, sourceContext)
	resp, err := s.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: userMessage}},
		MaxTokens:    100000,
		Temperature:  0.3,
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

func (s *Synthesizer) GeneratePageContent(ctx context.Context, title string, sourceData string, pageType string) (string, error) {
	systemPrompt := prompts.Compose(prompts.WikiPageGeneration, prompts.ObsidianMarkdown)
	userMessage := fmt.Sprintf("Title: %s\n\nSource data:\n%s", title, sourceData)
	resp, err := s.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: userMessage}},
		MaxTokens:    100000,
		Temperature:  0.4,
	})
	if err != nil {
		return "", fmt.Errorf("generate page content: %w", err)
	}
	return resp.Content, nil
}
func (s *Synthesizer) ExtractCitations(text string) []Citation {
	matches := citationPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return []Citation{}
	}
	seen := make(map[string]bool)
	citations := make([]Citation, 0, len(matches))
	for _, m := range matches {
		key := m[1] + ":" + m[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		citations = append(citations, Citation{
			SourceType: m[1], SourceID: m[2], Excerpt: "", Relevance: 1.0,
		})
	}
	return citations
}

func rankPages(pages []db.WikiPage) []db.WikiPage {
	if len(pages) == 0 {
		return nil
	}
	ranked := make([]db.WikiPage, len(pages))
	copy(ranked, pages)
	sort.SliceStable(ranked, func(i, j int) bool {
		si := ranked[i].StalenessScore
		sj := ranked[j].StalenessScore
		if ranked[i].GeneratedBy == "evolution" {
			si -= 0.1
		}
		if ranked[j].GeneratedBy == "evolution" {
			sj -= 0.1
		}
		if si != sj {
			return si < sj
		}
		pi, oki := pageTypePriority[ranked[i].PageType]
		pj, okj := pageTypePriority[ranked[j].PageType]
		if !oki {
			pi = len(pageTypePriority)
		}
		if !okj {
			pj = len(pageTypePriority)
		}
		return pi < pj
	})
	return ranked
}

func extractSummary(content string) string {
	if content == "" {
		return ""
	}
	idx := strings.Index(content, "## ")
	if idx == -1 {
		return truncateRunes(content, 300)
	}
	end := strings.Index(content[idx+3:], "## ")
	if end == -1 {
		return truncateRunes(content[idx:], 300)
	}
	return truncateRunes(content[idx:idx+3+end], 300)
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

func (s *Synthesizer) expandLinkedPages(ctx context.Context, primaryPages []db.WikiPage, maxPages int) []db.WikiPage {
	primarySet := make(map[string]bool, len(primaryPages))
	for _, p := range primaryPages {
		primarySet[p.ID] = true
	}
	candidateIDs := make(map[string]bool)
	for _, p := range primaryPages {
		fromLinks, err := s.store.ListLinksFrom(p.ID)
		if err != nil {
			slog.Warn("expand linked pages: list links from", "page_id", p.ID, "err", err)
			continue
		}
		for _, l := range fromLinks {
			if !primarySet[l.ToPageID] {
				candidateIDs[l.ToPageID] = true
			}
		}
		toLinks, err := s.store.ListLinksTo(p.ID)
		if err != nil {
			slog.Warn("expand linked pages: list links to", "page_id", p.ID, "err", err)
			continue
		}
		for _, l := range toLinks {
			if !primarySet[l.FromPageID] {
				candidateIDs[l.FromPageID] = true
			}
		}
	}
	var expanded []db.WikiPage
	for id := range candidateIDs {
		if len(expanded) >= maxPages {
			break
		}
		page, err := s.store.GetPage(id)
		if err != nil {
			slog.Warn("expand linked pages: get page", "page_id", id, "err", err)
			continue
		}
		if page != nil {
			expanded = append(expanded, *page)
		}
	}
	return expanded
}

func (s *Synthesizer) buildSourceContext(ctx context.Context, pages []db.WikiPage) string {
	summaryBudget := maxContextChars * 30 / 100
	graphBudget := maxContextChars * 15 / 100
	detailBudget := maxContextChars - summaryBudget - graphBudget
	var sb strings.Builder
	writeSummaryLayer(&sb, pages, summaryBudget)
	expanded := s.expandLinkedPages(ctx, pages, 5)
	writeGraphLayer(&sb, expanded, graphBudget)
	writeDetailLayer(&sb, pages, detailBudget)
	return sb.String()
}
func writeSummaryLayer(sb *strings.Builder, pages []db.WikiPage, budget int) {
	remaining := budget
	for _, p := range pages {
		if remaining <= 0 {
			break
		}
		summary := extractSummary(p.Content)
		entry := fmt.Sprintf("### %s (id: %s) [%s]\n%s\n\n", p.Title, p.ID, p.PageType, summary)
		if len(entry) > remaining {
			entry = entry[:remaining]
		}
		sb.WriteString(entry)
		remaining -= len(entry)
	}
}
func writeGraphLayer(sb *strings.Builder, expanded []db.WikiPage, budget int) {
	if len(expanded) == 0 {
		return
	}
	header := "## Related Pages\n\n"
	if len(header) > budget {
		return
	}
	sb.WriteString(header)
	budget -= len(header)
	for _, p := range expanded {
		if budget <= 0 {
			break
		}
		summary := extractSummary(p.Content)
		entry := fmt.Sprintf("- %s (id: %s): %s\n", p.Title, p.ID, summary)
		if len(entry) > budget {
			entry = entry[:budget]
		}
		sb.WriteString(entry)
		budget -= len(entry)
	}
}
func writeDetailLayer(sb *strings.Builder, pages []db.WikiPage, budget int) {
	remaining := budget
	for _, p := range pages {
		if remaining <= 0 {
			break
		}
		title := fmt.Sprintf("### %s (id: %s)\n", p.Title, p.ID)
		content := p.Content
		if len(title)+len(content) > remaining {
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
}
func (s *Synthesizer) persistAnswerPage(ctx context.Context, query string, content string) (string, error) {
	_ = ctx
	page := &db.WikiPage{
		ID: uuid.NewString(), PageType: "answer", Title: query,
		Content: content, Status: "published", GeneratedBy: "query",
	}
	if err := s.store.SavePage(page); err != nil {
		return "", fmt.Errorf("persist answer page: %w", err)
	}
	return page.ID, nil
}
