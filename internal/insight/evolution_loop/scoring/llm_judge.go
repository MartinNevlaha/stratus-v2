package scoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

// Sentinel errors for the LLM judge. Callers may use errors.Is to distinguish them.
var (
	// ErrInvalidTokenCap is returned when perCallCap is not positive.
	ErrInvalidTokenCap = errors.New("llm_judge: perCallCap must be > 0")

	// ErrJudgeResponseParse is returned when the LLM response cannot be parsed as JSON.
	ErrJudgeResponseParse = errors.New("llm_judge: malformed JSON response")

	// ErrJudgeScoreOutOfRange is returned when any parsed score is outside [0, 1].
	ErrJudgeScoreOutOfRange = errors.New("llm_judge: score out of [0,1]")
)

// judgeSystemPrompt is the fixed system prompt sent on every judge call.
// It instructs the model to respond with a strict single-line JSON object.
const judgeSystemPrompt = `You are an evaluation judge for software-engineering proposals. Given a hypothesis
and project context, respond with STRICT JSON on a single line, matching this schema:
{"impact":0.0-1.0,"effort":0.0-1.0,"confidence":0.0-1.0,"novelty":0.0-1.0}
"impact"     = expected benefit if implemented (0 = none, 1 = transformative).
"effort"     = implementation difficulty (0 = trivial, 1 = very hard).
"confidence" = how confident you are the hypothesis is actionable/valid.
"novelty"    = how non-obvious this idea is given the current project state.
Respond with ONLY the JSON object. No prose, no code fences.`

// CallOption configures an LLM Complete call.
type CallOption struct {
	responseFormat string
}

// WithResponseFormat sets the response format hint for the provider.
func WithResponseFormat(format string) CallOption {
	return CallOption{responseFormat: format}
}

// ResponseFormat returns the configured response format, or "" if unset.
func (o CallOption) ResponseFormat() string { return o.responseFormat }

// LLMClient is the minimal interface consumed by the judge.
// Implementations must forward maxTokens as the hard token limit to the model.
type LLMClient interface {
	// Complete sends system + user prompts and returns the raw text response,
	// the actual tokens consumed, and any transport/API error.
	Complete(ctx context.Context, system, user string, maxTokens int, opts ...CallOption) (text string, tokensUsed int, err error)
}

// LLMJudge scores a single Hypothesis against a baseline Bundle using an LLM.
type LLMJudge interface {
	// Score sends the hypothesis + minimal bundle context to the LLM, parses
	// a deterministic JSON response, and returns scores + tokens actually used.
	// perCallCap: hard max_tokens forwarded to the client. If non-positive → ErrInvalidTokenCap.
	Score(ctx context.Context, h Hypothesis, b baseline.Bundle, perCallCap int) (LLMScores, int, error)
}

type llmJudge struct {
	client LLMClient
}

// NewLLMJudge returns a new LLMJudge backed by the provided LLMClient.
func NewLLMJudge(client LLMClient) LLMJudge {
	return &llmJudge{client: client}
}

// Score implements LLMJudge.
func (j *llmJudge) Score(ctx context.Context, h Hypothesis, b baseline.Bundle, perCallCap int) (LLMScores, int, error) {
	if perCallCap <= 0 {
		return LLMScores{}, 0, ErrInvalidTokenCap
	}

	system := judgeSystemPrompt
	user := buildUserPrompt(h, b)

	text, tokensUsed, err := j.client.Complete(ctx, system, user, perCallCap, WithResponseFormat("json"))
	if err != nil {
		return LLMScores{}, 0, fmt.Errorf("llm_judge: complete: %w", err)
	}

	scores, err := parseJudgeResponse(text)
	if err != nil {
		return LLMScores{}, 0, err
	}

	// If the client reports 0 tokens but produced a response, estimate using a
	// char/4 heuristic (rough approximation of token count). Documented: this is
	// a fallback estimate only; real token counts should come from the client.
	if tokensUsed == 0 && len(text) > 0 {
		tokensUsed = (len(system) + len(user) + len(text)) / 4
		if tokensUsed == 0 {
			tokensUsed = 1
		}
	}

	return scores, tokensUsed, nil
}

// judgeResponse is the expected JSON shape from the model.
type judgeResponse struct {
	Impact     float64 `json:"impact"`
	Effort     float64 `json:"effort"`
	Confidence float64 `json:"confidence"`
	Novelty    float64 `json:"novelty"`
}

// parseJudgeResponse strips optional markdown code fences, unmarshals JSON,
// and validates that every score is within [0, 1].
func parseJudgeResponse(raw string) (LLMScores, error) {
	cleaned := stripFences(strings.TrimSpace(raw))

	var r judgeResponse
	if err := json.Unmarshal([]byte(cleaned), &r); err != nil {
		return LLMScores{}, fmt.Errorf("%w: %v", ErrJudgeResponseParse, err)
	}

	if err := validateScoreRange("impact", r.Impact); err != nil {
		return LLMScores{}, err
	}
	if err := validateScoreRange("effort", r.Effort); err != nil {
		return LLMScores{}, err
	}
	if err := validateScoreRange("confidence", r.Confidence); err != nil {
		return LLMScores{}, err
	}
	if err := validateScoreRange("novelty", r.Novelty); err != nil {
		return LLMScores{}, err
	}

	return LLMScores{
		Impact:     r.Impact,
		Effort:     r.Effort,
		Confidence: r.Confidence,
		Novelty:    r.Novelty,
	}, nil
}

// validateScoreRange returns an ErrJudgeScoreOutOfRange-wrapped error when
// the value is outside [0, 1].
func validateScoreRange(field string, v float64) error {
	if v < 0 || v > 1 {
		return fmt.Errorf("%w: field %q value %v", ErrJudgeScoreOutOfRange, field, v)
	}
	return nil
}

// stripFences removes optional leading/trailing ```json ... ``` or ``` ... ```
// markdown fencing from a response string.
func stripFences(s string) string {
	// Try ```json ... ``` first, then plain ``` ... ```.
	for _, prefix := range []string{"```json", "```"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			s = strings.TrimSuffix(s, "```")
			s = strings.TrimSpace(s)
			break
		}
	}
	return s
}

// ─── User prompt construction ─────────────────────────────────────────────────

const (
	maxFileRefsInPrompt   = 10
	maxSymbolRefsInPrompt = 10
	maxVexorHitsInPrompt  = 3
	maxTODOsInPrompt      = 3
	maxCommitsInPrompt    = 3
	maxWikiInPrompt       = 3
	maxGovInPrompt        = 2
	snippetMaxChars       = 50
)

// buildUserPrompt composes a compact user prompt from the hypothesis and bundle.
// Target: under ~1500 characters.
func buildUserPrompt(h Hypothesis, b baseline.Bundle) string {
	var sb strings.Builder

	// ── Hypothesis section ──
	sb.WriteString("## Hypothesis\n")
	sb.WriteString("Category: ")
	sb.WriteString(h.Category)
	sb.WriteByte('\n')
	sb.WriteString("Title: ")
	sb.WriteString(h.Title)
	sb.WriteByte('\n')
	sb.WriteString("Rationale: ")
	sb.WriteString(h.Rationale)
	sb.WriteByte('\n')

	fileRefs := h.FileRefs
	if len(fileRefs) > maxFileRefsInPrompt {
		fileRefs = fileRefs[:maxFileRefsInPrompt]
	}
	if len(fileRefs) > 0 {
		sb.WriteString("Files: ")
		sb.WriteString(strings.Join(fileRefs, ", "))
		sb.WriteByte('\n')
	}

	symRefs := h.SymbolRefs
	if len(symRefs) > maxSymbolRefsInPrompt {
		symRefs = symRefs[:maxSymbolRefsInPrompt]
	}
	if len(symRefs) > 0 {
		sb.WriteString("Symbols: ")
		sb.WriteString(strings.Join(symRefs, ", "))
		sb.WriteByte('\n')
	}

	// ── Baseline context section ──
	sb.WriteString("\n## Project Context\n")

	// Top 3 Vexor hits (snippet capped at 50 chars).
	vexorHits := b.VexorHits
	if len(vexorHits) > maxVexorHitsInPrompt {
		vexorHits = vexorHits[:maxVexorHitsInPrompt]
	}
	if len(vexorHits) > 0 {
		sb.WriteString("Code snippets:\n")
		for _, hit := range vexorHits {
			snippet := hit.Snippet
			if len(snippet) > snippetMaxChars {
				snippet = snippet[:snippetMaxChars]
			}
			sb.WriteString("  - ")
			sb.WriteString(hit.Path)
			sb.WriteString(": ")
			sb.WriteString(snippet)
			sb.WriteByte('\n')
		}
	}

	// Top 3 TODOs matching any FileRef.
	fileRefSet := make(map[string]struct{}, len(h.FileRefs))
	for _, f := range h.FileRefs {
		fileRefSet[strings.ToLower(f)] = struct{}{}
	}
	var matchedTODOs []baseline.TODOItem
	for _, t := range b.TODOs {
		if _, ok := fileRefSet[strings.ToLower(t.Path)]; ok {
			matchedTODOs = append(matchedTODOs, t)
			if len(matchedTODOs) >= maxTODOsInPrompt {
				break
			}
		}
	}
	if len(matchedTODOs) > 0 {
		sb.WriteString("Related TODOs:\n")
		for _, todo := range matchedTODOs {
			sb.WriteString("  - ")
			sb.WriteString(todo.Path)
			sb.WriteString(": ")
			sb.WriteString(todo.Text)
			sb.WriteByte('\n')
		}
	}

	// Top 3 recent commit subjects touching FileRefs.
	var matchedCommits []string
	for _, c := range b.GitCommits {
		if len(matchedCommits) >= maxCommitsInPrompt {
			break
		}
		for _, f := range c.Files {
			if _, ok := fileRefSet[strings.ToLower(f)]; ok {
				matchedCommits = append(matchedCommits, c.Subject)
				break
			}
		}
	}
	if len(matchedCommits) > 0 {
		sb.WriteString("Recent commits:\n")
		for _, s := range matchedCommits {
			sb.WriteString("  - ")
			sb.WriteString(s)
			sb.WriteByte('\n')
		}
	}

	// Up to 3 wiki titles with staleness.
	wikiTitles := b.WikiTitles
	if len(wikiTitles) > maxWikiInPrompt {
		wikiTitles = wikiTitles[:maxWikiInPrompt]
	}
	if len(wikiTitles) > 0 {
		sb.WriteString("Wiki (staleness):\n")
		for _, w := range wikiTitles {
			sb.WriteString(fmt.Sprintf("  - %s (%.2f)\n", w.Title, w.Staleness))
		}
	}

	// Up to 2 governance ref titles.
	govRefs := b.GovernanceRefs
	if len(govRefs) > maxGovInPrompt {
		govRefs = govRefs[:maxGovInPrompt]
	}
	if len(govRefs) > 0 {
		sb.WriteString("Governance:\n")
		for _, g := range govRefs {
			sb.WriteString("  - ")
			sb.WriteString(g.Title)
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}
