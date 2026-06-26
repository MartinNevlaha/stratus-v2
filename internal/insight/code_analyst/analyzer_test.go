package code_analyst

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

// mockLLMClient implements llm.Client for testing.
type mockLLMClient struct {
	response *llm.CompletionResponse
	err      error
	// queue, when non-empty, supplies one response per Complete call in order
	// (used to script the analyze call followed by the verify call). Falls back
	// to response once exhausted.
	queue []*llm.CompletionResponse
	// capturedRequest is the last request sent to Complete.
	capturedRequest llm.CompletionRequest
	// capturedRequests holds every request, in order.
	capturedRequests []llm.CompletionRequest
}

func (m *mockLLMClient) Complete(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.capturedRequest = req
	m.capturedRequests = append(m.capturedRequests, req)
	if m.err != nil {
		return nil, m.err
	}
	if len(m.queue) > 0 {
		r := m.queue[0]
		m.queue = m.queue[1:]
		return r, nil
	}
	return m.response, nil
}

func (m *mockLLMClient) Provider() string { return "mock" }
func (m *mockLLMClient) Model() string    { return "mock-model" }

// writeTemp creates a temporary file with given content and returns its path.
func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeTemp: %v", err)
	}
	return path
}

func TestAnalyzer_AnalyzeFile_Success(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	mockResp := `[{"category":"error_handling","severity":"warning","title":"Missing error check","description":"error return ignored","line_start":3,"line_end":3,"confidence":0.9,"suggestion":"check the error"}]`
	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: mockResp, InputTokens: 10, OutputTokens: 20}}

	a := NewAnalyzer(mock, dir, nil, 0.7)
	file := FileScore{FilePath: "main.go", CommitCount: 5, LineCount: 3, TechDebtMarkers: 0, Coverage: 0.8}

	result, err := a.AnalyzeFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Findings))
	}
	f := result.Findings[0]
	if f.Category != "error_handling" {
		t.Errorf("expected category error_handling, got %q", f.Category)
	}
	if f.Severity != "warning" {
		t.Errorf("expected severity warning, got %q", f.Severity)
	}
	if result.TokensUsed != 30 {
		t.Errorf("expected 30 tokens used, got %d", result.TokensUsed)
	}
}

func TestAnalyzer_AnalyzeFile_EmptyFindings(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "clean.go", "package clean\n")

	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: "[]", InputTokens: 5, OutputTokens: 5}}

	a := NewAnalyzer(mock, dir, nil, 0.7)
	file := FileScore{FilePath: "clean.go", CommitCount: 1, LineCount: 1}

	result, err := a.AnalyzeFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(result.Findings))
	}
}

func TestAnalyzer_AnalyzeFile_ConfidenceFilter(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "app.go", "package app\n")

	mockResp := `[
		{"category":"complexity","severity":"warning","title":"High complexity","description":"too complex","line_start":1,"line_end":10,"confidence":0.9,"suggestion":"refactor"},
		{"category":"dead_code","severity":"info","title":"Dead code","description":"unused var","line_start":5,"line_end":5,"confidence":0.5,"suggestion":"remove it"}
	]`
	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: mockResp}}

	a := NewAnalyzer(mock, dir, nil, 0.7)
	file := FileScore{FilePath: "app.go"}

	result, err := a.AnalyzeFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding after confidence filter, got %d", len(result.Findings))
	}
	if result.Findings[0].Category != "complexity" {
		t.Errorf("expected complexity finding to survive filter, got %q", result.Findings[0].Category)
	}
}

func TestAnalyzer_AnalyzeFile_CategoryFilter(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "svc.go", "package svc\n")

	mockResp := `[
		{"category":"security","severity":"critical","title":"SQL injection","description":"raw query","line_start":10,"line_end":10,"confidence":0.95,"suggestion":"use parameterized queries"},
		{"category":"complexity","severity":"warning","title":"High complexity","description":"nested loops","line_start":20,"line_end":40,"confidence":0.8,"suggestion":"refactor"}
	]`
	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: mockResp}}

	a := NewAnalyzer(mock, dir, []string{"security"}, 0.7)
	file := FileScore{FilePath: "svc.go"}

	result, err := a.AnalyzeFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding after category filter, got %d", len(result.Findings))
	}
	if result.Findings[0].Category != "security" {
		t.Errorf("expected security finding to survive filter, got %q", result.Findings[0].Category)
	}
}

func TestAnalyzer_AnalyzeFile_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: "[]"}}

	a := NewAnalyzer(mock, dir, nil, 0.7)
	file := FileScore{FilePath: "nonexistent.go"}

	_, err := a.AnalyzeFile(context.Background(), file, "")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "nonexistent.go") {
		t.Errorf("expected error to mention file path, got: %v", err)
	}
}

func TestAnalyzer_AnalyzeFile_LLMError(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "err.go", "package err\n")

	llmErr := errors.New("provider unavailable")
	mock := &mockLLMClient{err: llmErr}

	a := NewAnalyzer(mock, dir, nil, 0.7)
	file := FileScore{FilePath: "err.go"}

	_, err := a.AnalyzeFile(context.Background(), file, "")
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
	if !strings.Contains(err.Error(), "err.go") {
		t.Errorf("expected error to contain file path, got: %v", err)
	}
	if !errors.Is(err, llmErr) {
		t.Errorf("expected wrapped llm error, got: %v", err)
	}
}

func TestAnalyzer_AnalyzeFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "bad.go", "package bad\n")

	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: "not json at all"}}

	a := NewAnalyzer(mock, dir, nil, 0.7)
	file := FileScore{FilePath: "bad.go"}

	_, err := a.AnalyzeFile(context.Background(), file, "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse llm response") {
		t.Errorf("expected parse error in message, got: %v", err)
	}
}

func TestAnalyzer_AnalyzeFile_LargeFile(t *testing.T) {
	dir := t.TempDir()
	// Create a file larger than 32KB.
	largeContent := strings.Repeat("// comment line\n", 3000) // ~48KB
	writeTemp(t, dir, "large.go", largeContent)

	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: "[]"}}

	a := NewAnalyzer(mock, dir, nil, 0.7)
	file := FileScore{FilePath: "large.go"}

	_, err := a.AnalyzeFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the content sent to LLM was truncated.
	if len(mock.capturedRequest.Messages) == 0 {
		t.Fatal("no messages captured")
	}
	userMsg := mock.capturedRequest.Messages[0].Content
	// The content is truncated at 32KB then line-numbered (which adds a prefix
	// per line), so the message is larger than the raw cap but still far below
	// the full ~48KB file rendered with numbers (~63KB).
	if len(userMsg) > 48*1024 {
		t.Errorf("expected truncated content in LLM message, but message length is %d bytes", len(userMsg))
	}
	// Bug 2: a truncated file must carry an explicit truncation notice.
	if !strings.Contains(userMsg, "file truncated") {
		t.Errorf("expected truncation notice in user message, got: %s", userMsg[:200])
	}
}

func TestAnalyzer_AnalyzeFile_NumbersSourceLines(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: "[]"}}
	a := NewAnalyzer(mock, dir, nil, 0.7)
	file := FileScore{FilePath: "main.go"}

	if _, err := a.AnalyzeFile(context.Background(), file, ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	userMsg := mock.capturedRequest.Messages[0].Content
	for _, want := range []string{"1\tpackage main", "3\tfunc main() {}"} {
		if !strings.Contains(userMsg, want) {
			t.Errorf("expected line-numbered content %q in user message, got: %s", want, userMsg)
		}
	}
}

func TestAnalyzer_AnalyzeFile_VerifyRejectsFinding(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "v.go", "package v\n\nfunc f() {}\n")

	analyzeResp := `[{"category":"security","severity":"critical","title":"X","description":"d","line_start":3,"line_end":3,"confidence":0.95,"suggestion":"s"}]`
	verifyResp := `[{"index":0,"verdict":"rejected","reason":"line does not contain the issue"}]`
	mock := &mockLLMClient{queue: []*llm.CompletionResponse{
		{Content: analyzeResp, InputTokens: 10, OutputTokens: 10},
		{Content: verifyResp, InputTokens: 5, OutputTokens: 5},
	}}

	a := NewAnalyzer(mock, dir, nil, 0.7).WithVerify(true)
	result, err := a.AnalyzeFile(context.Background(), FileScore{FilePath: "v.go"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected rejected finding to be dropped, got %d findings", len(result.Findings))
	}
	if result.TokensUsed != 30 {
		t.Errorf("expected 30 tokens (analyze + verify), got %d", result.TokensUsed)
	}
	if len(mock.capturedRequests) != 2 {
		t.Fatalf("expected 2 LLM calls (analyze + verify), got %d", len(mock.capturedRequests))
	}
}

func TestAnalyzer_AnalyzeFile_VerifyKeepsConfirmedFinding(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "v.go", "package v\n\nfunc f() {}\n")

	analyzeResp := `[{"category":"security","severity":"critical","title":"X","description":"d","line_start":3,"line_end":3,"confidence":0.95,"suggestion":"s"}]`
	verifyResp := `[{"index":0,"verdict":"confirmed","reason":"valid"}]`
	mock := &mockLLMClient{queue: []*llm.CompletionResponse{
		{Content: analyzeResp},
		{Content: verifyResp},
	}}

	a := NewAnalyzer(mock, dir, nil, 0.7).WithVerify(true)
	result, err := a.AnalyzeFile(context.Background(), FileScore{FilePath: "v.go"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected confirmed finding to survive, got %d findings", len(result.Findings))
	}
}

func TestAnalyzer_AnalyzeFile_VerifyFailsOpen(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "v.go", "package v\n\nfunc f() {}\n")

	analyzeResp := `[{"category":"security","severity":"critical","title":"X","description":"d","line_start":3,"line_end":3,"confidence":0.95,"suggestion":"s"}]`
	mock := &mockLLMClient{queue: []*llm.CompletionResponse{
		{Content: analyzeResp},
		{Content: "not json"}, // verify pass returns garbage
	}}

	a := NewAnalyzer(mock, dir, nil, 0.7).WithVerify(true)
	result, err := a.AnalyzeFile(context.Background(), FileScore{FilePath: "v.go"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A verify parse failure must fail open: the finding is kept, not dropped.
	if len(result.Findings) != 1 {
		t.Fatalf("expected finding kept on verify failure (fail-open), got %d", len(result.Findings))
	}
}

func TestAnalyzer_AnalyzeFile_VerifyDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "v.go", "package v\n\nfunc f() {}\n")

	analyzeResp := `[{"category":"security","severity":"critical","title":"X","description":"d","line_start":3,"line_end":3,"confidence":0.95,"suggestion":"s"}]`
	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: analyzeResp}}

	a := NewAnalyzer(mock, dir, nil, 0.7) // verify not enabled
	result, err := a.AnalyzeFile(context.Background(), FileScore{FilePath: "v.go"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding without verify, got %d", len(result.Findings))
	}
	if len(mock.capturedRequests) != 1 {
		t.Errorf("expected exactly 1 LLM call when verify disabled, got %d", len(mock.capturedRequests))
	}
}

func TestAnalyzer_AnalyzeFile_GovernanceRulesIncluded(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "rule.go", "package rule\n")

	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: "[]"}}

	a := NewAnalyzer(mock, dir, nil, 0.7)
	file := FileScore{FilePath: "rule.go"}

	_, err := a.AnalyzeFile(context.Background(), file, "no direct DB calls from handlers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.capturedRequest.Messages) == 0 {
		t.Fatal("no messages captured")
	}
	userMsg := mock.capturedRequest.Messages[0].Content
	if !strings.Contains(userMsg, "no direct DB calls from handlers") {
		t.Errorf("expected governance rules in user message, got: %s", userMsg)
	}
}

func TestAnalyzer_AnalyzeFile_SlovakLanguageSuffixPresent(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: "[]"}}
	a := NewAnalyzer(mock, dir, nil, 0.7).WithLang("sk")
	file := FileScore{FilePath: "main.go"}

	_, err := a.AnalyzeFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sysPrompt := mock.capturedRequest.SystemPrompt
	if !strings.Contains(sysPrompt, "Slovak") {
		t.Errorf("expected 'Slovak' in system prompt for lang=sk, got: %s", sysPrompt)
	}
}

func TestAnalyzer_AnalyzeFile_EnglishLanguageSuffixPresent(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "main.go", "package main\n")

	mock := &mockLLMClient{response: &llm.CompletionResponse{Content: "[]"}}
	a := NewAnalyzer(mock, dir, nil, 0.7).WithLang("en")
	file := FileScore{FilePath: "main.go"}

	_, err := a.AnalyzeFile(context.Background(), file, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sysPrompt := mock.capturedRequest.SystemPrompt
	if !strings.Contains(sysPrompt, "English") {
		t.Errorf("expected 'English' in system prompt for lang=en, got: %s", sysPrompt)
	}
}

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"app.ts", "typescript"},
		{"index.js", "javascript"},
		{"script.py", "python"},
		{"lib.rs", "rust"},
		{"Main.java", "java"},
		{"ui.svelte", "svelte"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"README.md", "markdown"},
		{"query.sql", "sql"},
		{"run.sh", "shell"},
		{"data.json", "json"},
		{"unknown.xyz", "unknown"},
	}

	for _, tc := range cases {
		got := detectLanguage(tc.path)
		if got != tc.want {
			t.Errorf("detectLanguage(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
