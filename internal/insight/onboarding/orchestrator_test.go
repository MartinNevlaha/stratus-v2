package onboarding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockStore struct {
	pages []db.WikiPage
	links []db.WikiLink
	refs  []db.WikiPageRef
}

func (m *mockStore) SavePage(p *db.WikiPage) error {
	m.pages = append(m.pages, *p)
	return nil
}

func (m *mockStore) UpdatePage(p *db.WikiPage) error {
	for i, pg := range m.pages {
		if pg.ID == p.ID {
			m.pages[i] = *p
			return nil
		}
	}
	return errors.New("page not found")
}

func (m *mockStore) GetPage(id string) (*db.WikiPage, error) {
	for _, pg := range m.pages {
		if pg.ID == id {
			cp := pg
			return &cp, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockStore) ListPages(f db.WikiPageFilters) ([]db.WikiPage, int, error) {
	var result []db.WikiPage
	for _, pg := range m.pages {
		if f.Tag != "" {
			found := false
			for _, t := range pg.Tags {
				if t == f.Tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if f.Status != "" && pg.Status != f.Status {
			continue
		}
		result = append(result, pg)
	}
	return result, len(result), nil
}

func (m *mockStore) SearchPages(query string, pageType string, limit int) ([]db.WikiPage, error) {
	return nil, nil
}

func (m *mockStore) DeletePage(id string) error {
	for i, pg := range m.pages {
		if pg.ID == id {
			m.pages = append(m.pages[:i], m.pages[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *mockStore) UpdatePageStaleness(id string, score float64) error { return nil }

func (m *mockStore) SaveLink(l *db.WikiLink) error {
	m.links = append(m.links, *l)
	return nil
}

func (m *mockStore) ListLinksFrom(pageID string) ([]db.WikiLink, error) { return nil, nil }
func (m *mockStore) ListLinksTo(pageID string) ([]db.WikiLink, error)   { return nil, nil }
func (m *mockStore) DeleteLinks(pageID string) error                    { return nil }

func (m *mockStore) GetGraph(pageType string, limit int) ([]db.WikiPage, []db.WikiLink, error) {
	return nil, nil, nil
}

func (m *mockStore) SaveRef(r *db.WikiPageRef) error {
	m.refs = append(m.refs, *r)
	return nil
}

func (m *mockStore) ListRefs(pageID string) ([]db.WikiPageRef, error) { return nil, nil }
func (m *mockStore) DeleteRefs(pageID string) error                   { return nil }
func (m *mockStore) FindWikiPageByTitleNewest(_ string) (*db.WikiPage, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------

type mockLLM struct {
	responses []*llm.CompletionResponse
	errors    []error
	callCount int
}

func (m *mockLLM) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	idx := m.callCount
	m.callCount++
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	// Default response for any additional calls
	return &llm.CompletionResponse{Content: "default content", InputTokens: 10, OutputTokens: 20}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeGoodResponse(n int) []*llm.CompletionResponse {
	out := make([]*llm.CompletionResponse, n)
	for i := range out {
		out[i] = &llm.CompletionResponse{
			Content:      "# Page content",
			InputTokens:  100,
			OutputTokens: 200,
		}
	}
	return out
}

func minimalProfile() *ProjectProfile {
	return &ProjectProfile{
		RootPath:    "/tmp/test-project",
		ProjectName: "test-project",
		Languages: []LanguageStat{
			{Language: "Go", Extension: ".go", FileCount: 10, LineCount: 500},
		},
		ConfigFiles: []ConfigFile{
			{Path: "go.mod", Type: "go-module", Content: "module test\n\ngo 1.21\n"},
		},
		DirectoryTree: "cmd/\n  main.go\napi/\n  routes.go\n",
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRunOnboarding_Standard(t *testing.T) {
	store := &mockStore{}
	client := &mockLLM{responses: makeGoodResponse(20)}
	linker := wiki_engine.NewLinker(store)
	profile := minimalProfile()

	opts := OnboardingOpts{
		Depth:    "standard",
		MaxPages: 10,
	}

	result, err := RunOnboarding(context.Background(), store, client, linker, nil, profile, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// standard depth: arch overview + conventions + build guide + module pages
	if result.PagesGenerated < 3 {
		t.Errorf("expected at least 3 pages generated, got %d", result.PagesGenerated)
	}

	// All pages should have GeneratedBy = "onboarding" and tag "onboarding"
	for _, pg := range store.pages {
		if pg.GeneratedBy != "onboarding" {
			t.Errorf("page %q: GeneratedBy = %q, want %q", pg.Title, pg.GeneratedBy, "onboarding")
		}
		hasTag := false
		for _, tag := range pg.Tags {
			if tag == "onboarding" {
				hasTag = true
				break
			}
		}
		if !hasTag {
			t.Errorf("page %q: missing 'onboarding' tag, got %v", pg.Title, pg.Tags)
		}
	}

	// Architecture overview should be first page
	if len(store.pages) == 0 {
		t.Fatal("no pages were saved")
	}
	if store.pages[0].Title != "Architecture Overview" {
		t.Errorf("first page title = %q, want %q", store.pages[0].Title, "Architecture Overview")
	}
}

func TestRunOnboarding_PageFailure(t *testing.T) {
	store := &mockStore{}
	errs := make([]error, 20)
	errs[1] = errors.New("LLM timeout")
	responses := makeGoodResponse(20)
	client := &mockLLM{responses: responses, errors: errs}
	linker := wiki_engine.NewLinker(store)
	profile := minimalProfile()

	opts := OnboardingOpts{Depth: "standard", MaxPages: 10}

	result, err := RunOnboarding(context.Background(), store, client, linker, nil, profile, opts)
	if err != nil {
		t.Fatalf("unexpected fatal error: %v", err)
	}

	if result.PagesFailed != 1 {
		t.Errorf("PagesFailed = %d, want 1", result.PagesFailed)
	}
	if result.PagesGenerated == 0 {
		t.Errorf("PagesGenerated = 0, expected > 0")
	}
	if len(result.Errors) != 1 {
		t.Errorf("len(Errors) = %d, want 1", len(result.Errors))
	}
}

func TestRunOnboarding_NilLLM(t *testing.T) {
	store := &mockStore{}
	linker := wiki_engine.NewLinker(store)
	profile := minimalProfile()

	opts := OnboardingOpts{Depth: "standard", MaxPages: 10}

	_, err := RunOnboarding(context.Background(), store, nil, linker, nil, profile, opts)
	if err == nil {
		t.Fatal("expected error for nil llmClient, got nil")
	}
}

func TestRunOnboarding_NilProfile(t *testing.T) {
	store := &mockStore{}
	client := &mockLLM{responses: makeGoodResponse(10)}
	linker := wiki_engine.NewLinker(store)

	opts := OnboardingOpts{Depth: "standard", MaxPages: 10}

	_, err := RunOnboarding(context.Background(), store, client, linker, nil, nil, opts)
	if err == nil {
		t.Fatal("expected error for nil profile, got nil")
	}
}

func TestRunOnboarding_Idempotent(t *testing.T) {
	store := &mockStore{
		pages: []db.WikiPage{
			{
				ID:          "existing-arch",
				Title:       "Architecture Overview",
				Status:      "published",
				GeneratedBy: "onboarding",
				Tags:        []string{"onboarding"},
			},
		},
	}
	client := &mockLLM{responses: makeGoodResponse(20)}
	linker := wiki_engine.NewLinker(store)
	profile := minimalProfile()

	opts := OnboardingOpts{Depth: "standard", MaxPages: 10}

	result, err := RunOnboarding(context.Background(), store, client, linker, nil, profile, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.PagesSkipped < 1 {
		t.Errorf("PagesSkipped = %d, want >= 1", result.PagesSkipped)
	}
}

func TestRunOnboarding_ProgressCallback(t *testing.T) {
	store := &mockStore{}
	client := &mockLLM{responses: makeGoodResponse(20)}
	linker := wiki_engine.NewLinker(store)
	profile := minimalProfile()

	var progressCalls []OnboardingProgress
	opts := OnboardingOpts{
		Depth:    "standard",
		MaxPages: 10,
		ProgressFn: func(p OnboardingProgress) {
			progressCalls = append(progressCalls, p)
		},
	}

	_, err := RunOnboarding(context.Background(), store, client, linker, nil, profile, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(progressCalls) == 0 {
		t.Fatal("ProgressFn was never called")
	}

	// Check for "generating" status calls
	hasGenerating := false
	hasComplete := false
	for _, p := range progressCalls {
		if p.Status == "generating" {
			hasGenerating = true
		}
		if p.Status == "complete" {
			hasComplete = true
		}
	}

	if !hasGenerating {
		t.Error("expected at least one progress call with status 'generating'")
	}
	if !hasComplete {
		t.Error("expected final progress call with status 'complete'")
	}
}

func TestRunOnboarding_ContextCancelled(t *testing.T) {
	store := &mockStore{}

	// Slow LLM that checks context
	slowResponses := makeGoodResponse(20)
	client := &mockLLM{responses: slowResponses}

	linker := wiki_engine.NewLinker(store)
	profile := minimalProfile()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Cancel immediately
	cancel()

	opts := OnboardingOpts{Depth: "standard", MaxPages: 10}

	result, err := RunOnboarding(ctx, store, client, linker, nil, profile, opts)
	// Should return partial result (may have 0 pages if cancelled early enough)
	// The function should not panic or hang; err may be nil (partial) or context error
	_ = err
	if result == nil {
		t.Fatal("expected non-nil partial result even on context cancel")
	}
}

func TestRunOnboarding_ShallowDepth(t *testing.T) {
	store := &mockStore{}
	client := &mockLLM{responses: makeGoodResponse(5)}
	linker := wiki_engine.NewLinker(store)
	profile := minimalProfile()

	opts := OnboardingOpts{Depth: "shallow", MaxPages: 10}

	result, err := RunOnboarding(context.Background(), store, client, linker, nil, profile, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// shallow = arch overview + conventions + build guide = 3 pages
	totalPlanned := result.PagesGenerated + result.PagesFailed + result.PagesSkipped
	if totalPlanned != 3 {
		t.Errorf("shallow depth: expected 3 pages planned, got %d (generated=%d failed=%d skipped=%d)",
			totalPlanned, result.PagesGenerated, result.PagesFailed, result.PagesSkipped)
	}
}

// TestRunOnboarding_TokenBudgetExceeded verifies that when cumulative LLM tokens
// exceed the configured budget after N pages, RunOnboarding returns a
// partial-success result (pages written so far) with a budget-exceeded warning,
// and no error.
func TestRunOnboarding_TokenBudgetExceeded(t *testing.T) {
	store := &mockStore{}

	// Each response costs 300 tokens (100 in + 200 out).
	// Set budget to 350 so only 1 page can be generated before the guard fires.
	client := &mockLLM{responses: makeGoodResponse(20)}
	linker := wiki_engine.NewLinker(store)
	profile := minimalProfile()

	// Budget=350 tokens. Each page costs 300 tokens (100 in + 200 out).
	// After page 1: total=300, not yet exceeded. Page 2 generated: total=600 > 350 → guard fires.
	// So we expect exactly 2 pages generated (the one that crossed the threshold stops further generation).
	opts := OnboardingOpts{
		Depth:             "standard",
		MaxPages:          10,
		IngestTokenBudget: 350,
	}

	result, err := RunOnboarding(context.Background(), store, client, linker, nil, profile, opts)
	if err != nil {
		t.Fatalf("expected no error on budget-exceeded partial result, got: %v", err)
	}

	// Budget is exceeded after the 2nd page (cumulative 600 > 350), so exactly 2 pages generated.
	// No further pages are processed after the guard fires.
	if result.PagesGenerated != 2 {
		t.Errorf("PagesGenerated = %d, want 2 (budget fires after page that crosses threshold)", result.PagesGenerated)
	}

	// Standard depth with a minimal profile produces at most a few pages total;
	// ensure we stopped before all pages (guard was effective).
	if result.PagesGenerated+result.PagesFailed+result.PagesSkipped >= 5 {
		t.Errorf("expected fewer than 5 total pages due to budget guard, got generated=%d failed=%d skipped=%d",
			result.PagesGenerated, result.PagesFailed, result.PagesSkipped)
	}

	// A budget-warning must be present in the errors slice.
	hasBudgetWarning := false
	for _, e := range result.Errors {
		if len(e) > 0 && containsSubstr(e, "token budget") {
			hasBudgetWarning = true
			break
		}
	}
	if !hasBudgetWarning {
		t.Errorf("expected a 'token budget' warning in result.Errors, got: %v", result.Errors)
	}
}

// TestRunOnboarding_TokenBudgetZero_Unlimited verifies that IngestTokenBudget=0
// means no budget guard — all pages are generated normally.
func TestRunOnboarding_TokenBudgetZero_Unlimited(t *testing.T) {
	store := &mockStore{}
	client := &mockLLM{responses: makeGoodResponse(20)}
	linker := wiki_engine.NewLinker(store)
	profile := minimalProfile()

	opts := OnboardingOpts{
		Depth:            "shallow",
		MaxPages:         10,
		IngestTokenBudget: 0, // unlimited
	}

	result, err := RunOnboarding(context.Background(), store, client, linker, nil, profile, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// shallow = 3 pages; all should be generated since budget is unlimited.
	if result.PagesGenerated < 3 {
		t.Errorf("PagesGenerated = %d, want >= 3 with unlimited budget", result.PagesGenerated)
	}

	// No budget warning expected.
	for _, e := range result.Errors {
		if containsSubstr(e, "token budget") {
			t.Errorf("unexpected budget warning with IngestTokenBudget=0: %v", result.Errors)
			break
		}
	}
}

// containsSubstr is a helper to avoid importing strings in the test file.
func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}
