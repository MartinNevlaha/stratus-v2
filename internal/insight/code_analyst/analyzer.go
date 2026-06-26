package code_analyst

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
	lang                string // UI language code ("en", "sk", …); defaults to "en"
	verify              bool   // when true, run an adversarial verification pass over findings
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

// WithVerify returns a shallow copy of the Analyzer with the adversarial
// verification pass enabled or disabled. When enabled, each surviving finding
// is re-checked against the source in a second LLM call and dropped unless the
// model confirms it (default-reject), which suppresses confident-but-wrong
// findings that the confidence threshold alone cannot catch.
func (a *Analyzer) WithVerify(verify bool) *Analyzer {
	cp := *a
	cp.verify = verify
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
	truncated := false
	if len(rawContent) > maxFileContentBytes {
		content = string(rawContent[:maxFileContentBytes])
		truncated = true
	}

	// Bug 1: prefix every line with its 1-based number so the model references
	// real line numbers instead of counting them itself.
	numberedContent, shownLines := numberSourceLines(content)

	// Bug 2: tell the model explicitly when it is only seeing part of the file,
	// so it does not infer control flow past the truncation point.
	truncationNotice := ""
	if truncated {
		truncationNotice = fmt.Sprintf(
			"NOTE: file truncated — showing lines 1-%d of %d total. Do NOT infer control flow, "+
				"error handling, or resource cleanup beyond line %d; that code is not shown.\n\n",
			shownLines, countContentLines(string(rawContent)), shownLines)
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
		truncationNotice,
		numberedContent,
	)

	systemPrompt := prompts.WithLanguage(codeAnalysisSystemPrompt, a.lang)

	resp, err := a.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt:   systemPrompt,
		Messages:       []llm.Message{{Role: "user", Content: userPrompt}},
		MaxTokens:      100000,
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
	totalTokens := resp.InputTokens + resp.OutputTokens

	// Bug 3/4: adversarial verification pass. Self-reported confidence is not a
	// reliable filter, so re-check each finding against the source and keep only
	// those the model confirms. A verify failure fails open (keep findings) so a
	// transient LLM/parse error never silently drops the whole run.
	if a.verify && len(filtered) > 0 {
		verified, verifyTokens, verr := a.verifyFindings(ctx, file.FilePath, filtered, numberedContent)
		totalTokens += verifyTokens
		if verr != nil {
			slog.Warn("code analyst: analyzer: verify pass failed, keeping unverified findings",
				"file", file.FilePath, "err", verr)
		} else {
			filtered = verified
		}
	}

	return &AnalysisResult{
		Findings:   filtered,
		TokensUsed: totalTokens,
	}, nil
}

// findingVerdict is the per-finding result of the adversarial verification pass.
type findingVerdict struct {
	Index   int    `json:"index"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// verifyFindings runs the adversarial verification pass: a single LLM call that
// tries to refute each finding against the (line-numbered) source. Only findings
// whose index is explicitly confirmed survive; any unconfirmed finding is
// dropped (default-reject). Returns the surviving findings and tokens consumed.
func (a *Analyzer) verifyFindings(ctx context.Context, filePath string, findings []Finding, numberedContent string) ([]Finding, int, error) {
	var fb strings.Builder
	for i, f := range findings {
		fmt.Fprintf(&fb, "[%d] (%s/%s, lines %d-%d) %s — %s\n",
			i, f.Category, f.Severity, f.LineStart, f.LineEnd, f.Title, f.Description)
	}

	userPrompt := fmt.Sprintf(codeAnalysisVerifyUserPromptTemplate, filePath, fb.String(), numberedContent)

	resp, err := a.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt:   codeAnalysisVerifySystemPrompt,
		Messages:       []llm.Message{{Role: "user", Content: userPrompt}},
		MaxTokens:      8000,
		Temperature:    0.0,
		ResponseFormat: "json",
	})
	if err != nil {
		return nil, 0, fmt.Errorf("verify completion: %w", err)
	}
	tokens := resp.InputTokens + resp.OutputTokens

	var verdicts []findingVerdict
	if err := llm.ParseJSONResponse(resp.Content, &verdicts); err != nil {
		return nil, tokens, fmt.Errorf("parse verify response: %w", err)
	}

	confirmed := make(map[int]bool, len(verdicts))
	for _, v := range verdicts {
		if strings.EqualFold(strings.TrimSpace(v.Verdict), "confirmed") {
			confirmed[v.Index] = true
		}
	}

	kept := make([]Finding, 0, len(findings))
	for i, f := range findings {
		if confirmed[i] {
			kept = append(kept, f)
		}
	}
	return kept, tokens, nil
}

// numberSourceLines prefixes each line of content with its 1-based line number
// and a tab, and returns the rendered text plus the number of lines emitted.
func numberSourceLines(content string) (string, int) {
	if content == "" {
		return "", 0
	}
	// A trailing newline would otherwise yield a spurious final empty line.
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	var b strings.Builder
	b.Grow(len(content) + len(lines)*6)
	for i, line := range lines {
		fmt.Fprintf(&b, "%d\t%s\n", i+1, line)
	}
	return b.String(), len(lines)
}

// countContentLines returns the number of lines in s (a final line without a
// trailing newline still counts).
func countContentLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
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
