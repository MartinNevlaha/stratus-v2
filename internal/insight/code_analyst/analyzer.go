package code_analyst

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/prompts"
)

const maxFileContentBytes = 32 * 1024 // 32KB cap on file content sent to LLM

// Analyzer performs LLM-powered code quality analysis on individual files.
type Analyzer struct {
	llmClient           llm.Client
	projRoot            string
	categories          []string // enabled categories (empty = all)
	confidenceThreshold float64
	lang                string   // UI language code ("en", "sk", …); defaults to "en"
}

// NewAnalyzer constructs an Analyzer.
// categories restricts which finding categories are returned (empty = all).
// confidenceThreshold filters out findings below the given confidence level.
func NewAnalyzer(llmClient llm.Client, projRoot string, categories []string, confidenceThreshold float64) *Analyzer {
	return &Analyzer{
		llmClient:           llmClient,
		projRoot:            projRoot,
		categories:          categories,
		confidenceThreshold: confidenceThreshold,
		lang:                "en",
	}
}

// WithLang returns a shallow copy of the Analyzer with the given language code
// set. Call this before AnalyzeFile to localise LLM prompts.
func (a *Analyzer) WithLang(lang string) *Analyzer {
	if lang == "" {
		lang = "en"
	}
	cp := *a
	cp.lang = lang
	return &cp
}

// AnalyzeFile reads a file, sends it to the LLM with the code analysis prompt,
// and returns structured findings.
func (a *Analyzer) AnalyzeFile(ctx context.Context, file FileScore, governanceRules string) (*AnalysisResult, error) {
	rawContent, err := os.ReadFile(filepath.Join(a.projRoot, file.FilePath))
	if err != nil {
		return nil, fmt.Errorf("code analyst: analyze file %q: %w", file.FilePath, err)
	}

	content := string(rawContent)
	if len(rawContent) > maxFileContentBytes {
		content = string(rawContent[:maxFileContentBytes])
	}

	language := detectLanguage(file.FilePath)

	userPrompt := fmt.Sprintf(
		codeAnalysisUserPromptTemplate,
		file.FilePath,
		language,
		file.CommitCount,
		file.LineCount,
		file.TechDebtMarkers,
		file.Coverage*100,
		governanceRulesSection(governanceRules),
		content,
	)

	systemPrompt := prompts.WithLanguage(codeAnalysisSystemPrompt, a.lang)

	resp, err := a.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt:   systemPrompt,
		Messages:       []llm.Message{{Role: "user", Content: userPrompt}},
		MaxTokens:      8192,
		Temperature:    0.2,
		ResponseFormat: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("code analyst: analyze file %q: llm completion: %w", file.FilePath, err)
	}

	var findings []Finding
	if err := llm.ParseJSONResponse(resp.Content, &findings); err != nil {
		return nil, fmt.Errorf("code analyst: analyze file %q: parse llm response: %w", file.FilePath, err)
	}

	filtered := a.filterFindings(findings)

	return &AnalysisResult{
		Findings:   filtered,
		TokensUsed: resp.InputTokens + resp.OutputTokens,
	}, nil
}

// filterFindings applies confidence threshold and category filters.
func (a *Analyzer) filterFindings(findings []Finding) []Finding {
	result := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if f.Confidence < a.confidenceThreshold {
			continue
		}
		if len(a.categories) > 0 && !containsCategory(a.categories, f.Category) {
			continue
		}
		result = append(result, f)
	}
	return result
}

// containsCategory reports whether cat is in the list.
func containsCategory(list []string, cat string) bool {
	for _, c := range list {
		if c == cat {
			return true
		}
	}
	return false
}

