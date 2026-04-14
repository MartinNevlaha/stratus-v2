package scoring

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/baseline"
)

// mockLLMClient is an in-process stub for the LLMClient interface.
type mockLLMClient struct {
	NextResponse  string
	NextErr       error
	NextTokens    int
	LastMaxTokens int
	LastSystem    string
	LastUser      string
	// ctxErr captures any error observed on the context when Complete is called.
	ctxErr error
}

func (m *mockLLMClient) Complete(ctx context.Context, system, user string, maxTokens int) (string, int, error) {
	m.LastSystem = system
	m.LastUser = user
	m.LastMaxTokens = maxTokens
	m.ctxErr = ctx.Err()
	return m.NextResponse, m.NextTokens, m.NextErr
}

// minimalBundle returns a small but valid baseline.Bundle for use in tests.
func minimalBundle() baseline.Bundle {
	return baseline.Bundle{
		ProjectRoot: "/tmp/project",
		VexorHits: []baseline.VexorHit{
			{Path: "pkg/foo/foo.go", Snippet: "func Foo() {}", Score: 0.9},
			{Path: "pkg/bar/bar.go", Snippet: "func Bar() {}", Score: 0.8},
			{Path: "pkg/baz/baz.go", Snippet: "func Baz() {}", Score: 0.7},
			{Path: "pkg/qux/qux.go", Snippet: "func Qux() {}", Score: 0.6},
		},
		GitCommits: []baseline.GitCommit{
			{Hash: "abc1", Subject: "fix: update pkg/foo", Files: []string{"pkg/foo/foo.go"}, At: time.Now()},
			{Hash: "abc2", Subject: "feat: add baz support", Files: []string{"pkg/baz/baz.go"}, At: time.Now()},
		},
		TODOs: []baseline.TODOItem{
			{Path: "pkg/foo/foo.go", Line: 42, Text: "TODO: refactor this", Kind: "TODO"},
		},
		WikiTitles: []baseline.WikiTitle{
			{ID: "w1", Title: "Architecture Guide", Staleness: 0.8},
			{ID: "w2", Title: "API Reference", Staleness: 0.3},
			{ID: "w3", Title: "Testing Handbook", Staleness: 0.5},
		},
		GovernanceRefs: []baseline.GovernanceRef{
			{ID: "g1", Title: "Error Handling Policy", Kind: "rule"},
			{ID: "g2", Title: "Config Validation Standard", Kind: "adr"},
		},
		GeneratedAt: time.Now(),
	}
}

func minimalHypothesis() Hypothesis {
	return Hypothesis{
		Category:   "refactor_opportunity",
		Title:      "Extract common auth logic",
		Rationale:  "Auth logic is duplicated across multiple packages.",
		FileRefs:   []string{"pkg/foo/foo.go", "pkg/bar/bar.go"},
		SymbolRefs: []string{"AuthMiddleware"},
		SignalRefs: []string{"pkg/foo/foo.go:42"},
	}
}

// ─── Test cases ──────────────────────────────────────────────────────────────

// 1. Valid JSON response → all scores parsed, no error.
func TestLLMJudge_ValidResponse_ParsesAllScores(t *testing.T) {
	client := &mockLLMClient{
		NextResponse: `{"impact":0.8,"effort":0.3,"confidence":0.9,"novelty":0.6}`,
		NextTokens:   150,
	}
	judge := NewLLMJudge(client)
	scores, tokens, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scores.Impact != 0.8 {
		t.Errorf("Impact = %v, want 0.8", scores.Impact)
	}
	if scores.Effort != 0.3 {
		t.Errorf("Effort = %v, want 0.3", scores.Effort)
	}
	if scores.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", scores.Confidence)
	}
	if scores.Novelty != 0.6 {
		t.Errorf("Novelty = %v, want 0.6", scores.Novelty)
	}
	if tokens != 150 {
		t.Errorf("tokensUsed = %v, want 150", tokens)
	}
}

// 2. perCallCap=0 → ErrInvalidTokenCap, no client call.
func TestLLMJudge_ZeroTokenCap_ReturnsErrInvalidTokenCap(t *testing.T) {
	client := &mockLLMClient{}
	judge := NewLLMJudge(client)
	_, _, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), 0)
	if !errors.Is(err, ErrInvalidTokenCap) {
		t.Fatalf("expected ErrInvalidTokenCap, got: %v", err)
	}
	if client.LastSystem != "" {
		t.Error("client should not have been called when perCallCap=0")
	}
}

// 3. perCallCap=1500 → LastMaxTokens == 1500.
func TestLLMJudge_TokenCapPassedToClient(t *testing.T) {
	client := &mockLLMClient{
		NextResponse: `{"impact":0.5,"effort":0.5,"confidence":0.5,"novelty":0.5}`,
		NextTokens:   100,
	}
	judge := NewLLMJudge(client)
	_, _, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), 1500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.LastMaxTokens != 1500 {
		t.Errorf("LastMaxTokens = %v, want 1500", client.LastMaxTokens)
	}
}

// 4. Malformed JSON → errors.Is(err, ErrJudgeResponseParse).
func TestLLMJudge_MalformedJSON_ReturnsErrJudgeResponseParse(t *testing.T) {
	client := &mockLLMClient{
		NextResponse: `not json at all`,
		NextTokens:   50,
	}
	judge := NewLLMJudge(client)
	_, _, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), 500)
	if !errors.Is(err, ErrJudgeResponseParse) {
		t.Fatalf("expected ErrJudgeResponseParse, got: %v", err)
	}
}

// 5. JSON with score=1.5 → errors.Is(err, ErrJudgeScoreOutOfRange).
func TestLLMJudge_ScoreOutOfRange_ReturnsErrJudgeScoreOutOfRange(t *testing.T) {
	client := &mockLLMClient{
		NextResponse: `{"impact":1.5,"effort":0.3,"confidence":0.9,"novelty":0.6}`,
		NextTokens:   50,
	}
	judge := NewLLMJudge(client)
	_, _, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), 500)
	if !errors.Is(err, ErrJudgeScoreOutOfRange) {
		t.Fatalf("expected ErrJudgeScoreOutOfRange, got: %v", err)
	}
}

// 6. Client returns error → returned error wraps client's error.
func TestLLMJudge_ClientError_WrapsClientErr(t *testing.T) {
	sentinel := errors.New("client-boom")
	client := &mockLLMClient{NextErr: sentinel}
	judge := NewLLMJudge(client)
	_, _, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), 500)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error should wrap client error; got: %v", err)
	}
}

// 7. Response wrapped in ```json fences → still parses.
func TestLLMJudge_MarkdownFences_StillParses(t *testing.T) {
	client := &mockLLMClient{
		NextResponse: "```json\n{\"impact\":0.7,\"effort\":0.4,\"confidence\":0.8,\"novelty\":0.5}\n```",
		NextTokens:   80,
	}
	judge := NewLLMJudge(client)
	scores, _, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scores.Impact != 0.7 {
		t.Errorf("Impact = %v, want 0.7", scores.Impact)
	}
}

// 8. tokensUsed passed through from client.
func TestLLMJudge_TokensUsed_PassedThrough(t *testing.T) {
	client := &mockLLMClient{
		NextResponse: `{"impact":0.5,"effort":0.5,"confidence":0.5,"novelty":0.5}`,
		NextTokens:   999,
	}
	judge := NewLLMJudge(client)
	_, tokensUsed, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokensUsed != 999 {
		t.Errorf("tokensUsed = %v, want 999", tokensUsed)
	}
}

// 9. tokensUsed=0 from client but response non-empty → estimated > 0.
func TestLLMJudge_ZeroTokensFromClient_Estimated(t *testing.T) {
	client := &mockLLMClient{
		NextResponse: `{"impact":0.5,"effort":0.5,"confidence":0.5,"novelty":0.5}`,
		NextTokens:   0, // client reports 0
	}
	judge := NewLLMJudge(client)
	_, tokensUsed, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokensUsed <= 0 {
		t.Errorf("expected estimated tokensUsed > 0, got %v", tokensUsed)
	}
}

// 10. User prompt includes hypothesis title (substring check on LastUser).
func TestLLMJudge_UserPromptContainsHypothesisTitle(t *testing.T) {
	client := &mockLLMClient{
		NextResponse: `{"impact":0.5,"effort":0.5,"confidence":0.5,"novelty":0.5}`,
		NextTokens:   100,
	}
	judge := NewLLMJudge(client)
	h := minimalHypothesis()
	h.Title = "UniqueHypothesisTitleXYZ"
	_, _, err := judge.Score(context.Background(), h, minimalBundle(), 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(client.LastUser, "UniqueHypothesisTitleXYZ") {
		t.Errorf("user prompt does not contain hypothesis title; got: %q", client.LastUser)
	}
}

// 11. System prompt is constant (stable across calls).
func TestLLMJudge_SystemPromptIsStable(t *testing.T) {
	client1 := &mockLLMClient{
		NextResponse: `{"impact":0.5,"effort":0.5,"confidence":0.5,"novelty":0.5}`,
		NextTokens:   100,
	}
	client2 := &mockLLMClient{
		NextResponse: `{"impact":0.5,"effort":0.5,"confidence":0.5,"novelty":0.5}`,
		NextTokens:   100,
	}
	judge1 := NewLLMJudge(client1)
	judge2 := NewLLMJudge(client2)

	h1 := minimalHypothesis()
	h2 := Hypothesis{
		Category:  "test_gap",
		Title:     "Different title entirely",
		Rationale: "Completely different rationale",
		FileRefs:  []string{"other/path.go"},
	}
	b := minimalBundle()

	_, _, _ = judge1.Score(context.Background(), h1, b, 500)
	_, _, _ = judge2.Score(context.Background(), h2, b, 500)

	if client1.LastSystem != client2.LastSystem {
		t.Errorf("system prompt is not stable across calls:\nfirst:  %q\nsecond: %q", client1.LastSystem, client2.LastSystem)
	}
}

// 12. Context cancellation propagated to client.
func TestLLMJudge_ContextCancellation_PropagatedToClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	client := &mockLLMClient{
		NextResponse: `{"impact":0.5,"effort":0.5,"confidence":0.5,"novelty":0.5}`,
		NextTokens:   100,
	}
	judge := NewLLMJudge(client)
	_, _, _ = judge.Score(ctx, minimalHypothesis(), minimalBundle(), 500)

	if client.ctxErr == nil {
		t.Error("expected context to be canceled when passed to client, but ctxErr is nil")
	}
	if !errors.Is(client.ctxErr, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", client.ctxErr)
	}
}

// ─── Negative perCallCap tests ────────────────────────────────────────────────

// Negative cap should also be rejected.
func TestLLMJudge_NegativeTokenCap_ReturnsErrInvalidTokenCap(t *testing.T) {
	client := &mockLLMClient{}
	judge := NewLLMJudge(client)
	_, _, err := judge.Score(context.Background(), minimalHypothesis(), minimalBundle(), -5)
	if !errors.Is(err, ErrInvalidTokenCap) {
		t.Fatalf("expected ErrInvalidTokenCap for negative cap, got: %v", err)
	}
}
