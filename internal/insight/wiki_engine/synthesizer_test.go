package wiki_engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
)

// ---------------------------------------------------------------------------
// synthLLM — a minimal LLMClient mock used only by synthesizer tests.
// ---------------------------------------------------------------------------

type synthLLM struct {
	response *llm.CompletionResponse
	err      error
}

func (m *synthLLM) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

// compile-time interface check
var _ wiki_engine.LLMClient = (*synthLLM)(nil)

// captureSynthLLM records the last CompletionRequest for prompt inspection.
type captureSynthLLM struct {
	lastReq  llm.CompletionRequest
	response *llm.CompletionResponse
	err      error
}

func (c *captureSynthLLM) Complete(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	c.lastReq = req
	if c.err != nil {
		return nil, c.err
	}
	return c.response, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func synthPages() []db.WikiPage {
	return []db.WikiPage{
		{
			ID:       "page-1",
			PageType: "concept",
			Title:    "Distributed Systems",
			Content:  "Consensus algorithms in distributed computing [event:evt-1]",
			Status:   "published",
		},
		{
			ID:       "page-2",
			PageType: "summary",
			Title:    "CAP Theorem",
			Content:  "Trade-offs in distributed systems [trajectory:traj-2]",
			Status:   "published",
		},
	}
}

func llmResponse(content string, in, out int) *llm.CompletionResponse {
	return &llm.CompletionResponse{
		Content:      content,
		InputTokens:  in,
		OutputTokens: out,
	}
}

// ---------------------------------------------------------------------------
// SynthesizeAnswer tests
// ---------------------------------------------------------------------------

func TestSynthesizeAnswer_Success(t *testing.T) {
	store := newMockStore()
	store.searchResult = synthPages()

	lm := &synthLLM{
		response: llmResponse(
			"Distributed systems trade availability for consistency [event:evt-1] [trajectory:traj-2].",
			100, 50,
		),
	}
	s := wiki_engine.NewSynthesizer(store, lm)

	result, err := s.SynthesizeAnswer(context.Background(), "distributed systems", 10, false)
	if err != nil {
		t.Fatalf("SynthesizeAnswer: %v", err)
	}
	if result.Answer == "" {
		t.Error("expected non-empty answer")
	}
	if result.TokensUsed != 150 {
		t.Errorf("tokens_used: want 150, got %d", result.TokensUsed)
	}
	if len(result.Citations) != 2 {
		t.Errorf("citations: want 2, got %d", len(result.Citations))
	}
	if result.WikiPageID != nil {
		t.Error("expected WikiPageID to be nil when persist=false")
	}
}

func TestSynthesizeAnswer_Persist(t *testing.T) {
	store := newMockStore()
	store.searchResult = synthPages()

	lm := &synthLLM{
		response: llmResponse("Answer with [event:evt-99].", 80, 20),
	}
	s := wiki_engine.NewSynthesizer(store, lm)

	result, err := s.SynthesizeAnswer(context.Background(), "what is consensus?", 10, true)
	if err != nil {
		t.Fatalf("SynthesizeAnswer: %v", err)
	}
	if result.WikiPageID == nil {
		t.Fatal("expected WikiPageID to be set when persist=true")
	}
	if *result.WikiPageID == "" {
		t.Error("expected non-empty WikiPageID")
	}

	// Exactly one page should have been persisted.
	if len(store.pages) != 1 {
		t.Fatalf("expected 1 saved page, got %d", len(store.pages))
	}
	var saved *db.WikiPage
	for _, p := range store.pages {
		saved = p
	}
	if saved.PageType != "answer" {
		t.Errorf("page_type: want %q, got %q", "answer", saved.PageType)
	}
	if saved.GeneratedBy != "query" {
		t.Errorf("generated_by: want %q, got %q", "query", saved.GeneratedBy)
	}
}

func TestSynthesizeAnswer_NoPersist(t *testing.T) {
	store := newMockStore()
	store.searchResult = synthPages()

	lm := &synthLLM{response: llmResponse("Simple answer.", 50, 10)}
	s := wiki_engine.NewSynthesizer(store, lm)

	result, err := s.SynthesizeAnswer(context.Background(), "something", 5, false)
	if err != nil {
		t.Fatalf("SynthesizeAnswer: %v", err)
	}
	if result.WikiPageID != nil {
		t.Error("expected WikiPageID to be nil when persist=false")
	}
	if len(store.pages) != 0 {
		t.Errorf("expected no saved pages, got %d", len(store.pages))
	}
}

func TestSynthesizeAnswer_LLMError(t *testing.T) {
	store := newMockStore()
	store.searchResult = synthPages()

	lm := &synthLLM{err: errors.New("llm: request failed")}
	s := wiki_engine.NewSynthesizer(store, lm)

	result, err := s.SynthesizeAnswer(context.Background(), "query", 10, false)
	if err == nil {
		t.Fatal("expected error from LLM failure, got nil")
	}
	if result != nil {
		t.Error("expected nil result on LLM error")
	}
	if !errContains(err, "synthesize answer") {
		t.Errorf("error should wrap synthesize context, got: %v", err)
	}
}

func TestSynthesizeAnswer_NoResults(t *testing.T) {
	// Empty search results — LLM should still be called.
	store := newMockStore()
	store.searchResult = []db.WikiPage{}

	lm := &synthLLM{
		response: llmResponse("No pages found, but here is a general answer.", 30, 15),
	}
	s := wiki_engine.NewSynthesizer(store, lm)

	result, err := s.SynthesizeAnswer(context.Background(), "obscure topic", 10, false)
	if err != nil {
		t.Fatalf("SynthesizeAnswer: %v", err)
	}
	if result.Answer == "" {
		t.Error("expected non-empty answer even with no source pages")
	}
	if len(result.Citations) != 0 {
		t.Errorf("expected 0 citations for citation-free answer, got %d", len(result.Citations))
	}
}

// ---------------------------------------------------------------------------
// ExtractCitations tests
// ---------------------------------------------------------------------------

func TestExtractCitations_ParsesPatterns(t *testing.T) {
	s := wiki_engine.NewSynthesizer(nil, nil)

	cases := []struct {
		name     string
		text     string
		wantLen  int
		wantType string
		wantID   string
	}{
		{
			name:     "single event citation",
			text:     "See [event:evt-123] for details.",
			wantLen:  1,
			wantType: "event",
			wantID:   "evt-123",
		},
		{
			name:    "multiple distinct citations",
			text:    "From [event:e1] and [trajectory:t2] and [artifact:a3].",
			wantLen: 3,
		},
		{
			name:    "duplicate citations are deduplicated",
			text:    "[event:e1] is mentioned and also [event:e1] again.",
			wantLen: 1,
		},
		{
			name:     "underscore in source type",
			text:     "Relevant: [solution_pattern:sp-99]",
			wantLen:  1,
			wantType: "solution_pattern",
			wantID:   "sp-99",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			citations := s.ExtractCitations(tc.text)
			if len(citations) != tc.wantLen {
				t.Errorf("len(citations): want %d, got %d", tc.wantLen, len(citations))
			}
			if tc.wantType != "" && len(citations) > 0 {
				if citations[0].SourceType != tc.wantType {
					t.Errorf("source_type: want %q, got %q", tc.wantType, citations[0].SourceType)
				}
			}
			if tc.wantID != "" && len(citations) > 0 {
				if citations[0].SourceID != tc.wantID {
					t.Errorf("source_id: want %q, got %q", tc.wantID, citations[0].SourceID)
				}
			}
		})
	}
}

func TestExtractCitations_NoCitations(t *testing.T) {
	s := wiki_engine.NewSynthesizer(nil, nil)

	citations := s.ExtractCitations("This text has no citation markers at all.")
	if len(citations) != 0 {
		t.Errorf("expected empty slice, got %d citations", len(citations))
	}
}

func TestExtractCitations_EmptyString(t *testing.T) {
	s := wiki_engine.NewSynthesizer(nil, nil)

	citations := s.ExtractCitations("")
	if citations == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(citations) != 0 {
		t.Errorf("expected 0 citations, got %d", len(citations))
	}
}

// ---------------------------------------------------------------------------
// GeneratePageContent tests
// ---------------------------------------------------------------------------

func TestGeneratePageContent_Success(t *testing.T) {
	store := newMockStore()
	lm := &synthLLM{
		response: llmResponse("# My Page\n\nGenerated markdown content.", 60, 30),
	}
	s := wiki_engine.NewSynthesizer(store, lm)

	content, err := s.GeneratePageContent(context.Background(), "My Page", "raw data here", "concept")
	if err != nil {
		t.Fatalf("GeneratePageContent: %v", err)
	}
	if content == "" {
		t.Error("expected non-empty generated content")
	}
}

func TestGeneratePageContent_LLMError(t *testing.T) {
	store := newMockStore()
	lm := &synthLLM{err: errors.New("upstream timeout")}
	s := wiki_engine.NewSynthesizer(store, lm)

	content, err := s.GeneratePageContent(context.Background(), "Title", "data", "summary")
	if err == nil {
		t.Fatal("expected error from LLM failure, got nil")
	}
	if content != "" {
		t.Error("expected empty content on LLM error")
	}
	if !errContains(err, "generate page content") {
		t.Errorf("error should wrap generate context, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests: SynthesizeAnswer — system prompt uses obsidian prompt library
// ---------------------------------------------------------------------------

// TestSynthesizeAnswer_SystemPromptContainsObsidianMarkdown verifies that the
// system prompt sent to the LLM includes obsidian wikilink syntax.
func TestSynthesizeAnswer_SystemPromptContainsObsidianMarkdown(t *testing.T) {
	store := newMockStore()
	store.searchResult = synthPages()

	lm := &captureSynthLLM{
		response: llmResponse("answer text", 50, 20),
	}
	s := wiki_engine.NewSynthesizer(store, lm)

	_, err := s.SynthesizeAnswer(context.Background(), "distributed systems", 10, false)
	if err != nil {
		t.Fatalf("SynthesizeAnswer: %v", err)
	}

	prompt := lm.lastReq.SystemPrompt
	if !strings.Contains(prompt, "[[") {
		t.Errorf("SynthesizeAnswer system prompt should contain obsidian wikilink '[[', got: %q", prompt)
	}
	if !strings.Contains(prompt, "wikilink") {
		t.Errorf("SynthesizeAnswer system prompt should contain 'wikilink', got: %q", prompt)
	}
}

// TestSynthesizeAnswer_SystemPromptContainsWikiSynthesisInstruction verifies
// that the WikiSynthesis prompt fragment is included in the composed system prompt.
func TestSynthesizeAnswer_SystemPromptContainsWikiSynthesisInstruction(t *testing.T) {
	store := newMockStore()
	store.searchResult = synthPages()

	lm := &captureSynthLLM{
		response: llmResponse("answer text", 50, 20),
	}
	s := wiki_engine.NewSynthesizer(store, lm)

	_, err := s.SynthesizeAnswer(context.Background(), "query", 10, false)
	if err != nil {
		t.Fatalf("SynthesizeAnswer: %v", err)
	}

	prompt := lm.lastReq.SystemPrompt
	if !strings.Contains(prompt, "synthesizer") {
		t.Errorf("SynthesizeAnswer system prompt should contain 'synthesizer' from WikiSynthesis, got: %q", prompt)
	}
}

// ---------------------------------------------------------------------------
// Tests: GeneratePageContent — system prompt uses obsidian prompt library
// ---------------------------------------------------------------------------

// TestGeneratePageContent_SystemPromptContainsObsidianMarkdown verifies that
// the system prompt sent to the LLM includes obsidian wikilink syntax.
func TestGeneratePageContent_SystemPromptContainsObsidianMarkdown(t *testing.T) {
	store := newMockStore()
	lm := &captureSynthLLM{
		response: llmResponse("# Page\n\nContent here.", 60, 30),
	}
	s := wiki_engine.NewSynthesizer(store, lm)

	_, err := s.GeneratePageContent(context.Background(), "My Page", "raw data", "concept")
	if err != nil {
		t.Fatalf("GeneratePageContent: %v", err)
	}

	prompt := lm.lastReq.SystemPrompt
	if !strings.Contains(prompt, "[[") {
		t.Errorf("GeneratePageContent system prompt should contain obsidian wikilink '[[', got: %q", prompt)
	}
	if !strings.Contains(prompt, "wikilink") {
		t.Errorf("GeneratePageContent system prompt should contain 'wikilink', got: %q", prompt)
	}
}

// TestGeneratePageContent_SystemPromptContainsWikiPageGenerationInstruction
// verifies that the WikiPageGeneration prompt is included in the composed system prompt.
func TestGeneratePageContent_SystemPromptContainsWikiPageGenerationInstruction(t *testing.T) {
	store := newMockStore()
	lm := &captureSynthLLM{
		response: llmResponse("# Page\n\nContent here.", 60, 30),
	}
	s := wiki_engine.NewSynthesizer(store, lm)

	_, err := s.GeneratePageContent(context.Background(), "My Page", "raw data", "concept")
	if err != nil {
		t.Fatalf("GeneratePageContent: %v", err)
	}

	prompt := lm.lastReq.SystemPrompt
	if !strings.Contains(prompt, "wiki") {
		t.Errorf("GeneratePageContent system prompt should contain 'wiki' from WikiPageGeneration, got: %q", prompt)
	}
}

// ---------------------------------------------------------------------------
// test utility
// ---------------------------------------------------------------------------

func errContains(err error, sub string) bool {
	if err == nil || sub == "" {
		return sub == ""
	}
	s := err.Error()
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
