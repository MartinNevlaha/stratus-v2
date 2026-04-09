package llm

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// mockBudgetStore is a test double for BudgetStore.
type mockBudgetStore struct {
	totalInput  int
	totalOutput int
	getErr      error
	recordErr   error
	recorded    []struct {
		date, subsystem    string
		input, output int
	}
}

func (m *mockBudgetStore) GetDailyTokenUsageTotal(date string) (int, int, error) {
	return m.totalInput, m.totalOutput, m.getErr
}

func (m *mockBudgetStore) RecordTokenUsage(date, subsystem string, input, output int) error {
	m.recorded = append(m.recorded, struct {
		date, subsystem    string
		input, output int
	}{date, subsystem, input, output})
	return m.recordErr
}

// mockClient is a test double for Client.
type mockClient struct {
	resp    *CompletionResponse
	err     error
	calls   int
}

func (m *mockClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	m.calls++
	return m.resp, m.err
}

func (m *mockClient) Provider() string { return "mock" }
func (m *mockClient) Model() string    { return "mock-model" }

func TestBudgetedClient_UnderBudget(t *testing.T) {
	store := &mockBudgetStore{totalInput: 100, totalOutput: 50}
	inner := &mockClient{resp: &CompletionResponse{Content: "ok", InputTokens: 10, OutputTokens: 5}}

	client := NewBudgetedClient(inner, store, 1000)
	resp, err := client.CompleteWithPriority(context.Background(), CompletionRequest{}, PriorityMedium, "unknown")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("expected content 'ok', got %q", resp.Content)
	}
	if len(store.recorded) != 1 {
		t.Fatalf("expected RecordTokenUsage called once, got %d", len(store.recorded))
	}
	r := store.recorded[0]
	if r.subsystem != "unknown" {
		t.Errorf("expected subsystem 'unknown', got %q", r.subsystem)
	}
	if r.input != 10 || r.output != 5 {
		t.Errorf("expected input=10 output=5, got input=%d output=%d", r.input, r.output)
	}
}

func TestBudgetedClient_BudgetExhausted_LowPriority(t *testing.T) {
	store := &mockBudgetStore{totalInput: 600, totalOutput: 400} // 1000 used = limit
	inner := &mockClient{resp: &CompletionResponse{Content: "ok"}}

	client := NewBudgetedClient(inner, store, 1000)
	_, err := client.CompleteWithPriority(context.Background(), CompletionRequest{}, PriorityLow, "unknown")

	if !errors.Is(err, ErrBudgetExhausted) {
		t.Errorf("expected ErrBudgetExhausted, got: %v", err)
	}
	if inner.calls != 0 {
		t.Error("expected inner.Complete not called when budget exhausted")
	}
}

func TestBudgetedClient_BudgetExhausted_HighPriority(t *testing.T) {
	store := &mockBudgetStore{totalInput: 600, totalOutput: 400} // 1000 used = limit
	inner := &mockClient{resp: &CompletionResponse{Content: "high priority ok", InputTokens: 20, OutputTokens: 10}}

	client := NewBudgetedClient(inner, store, 1000)
	resp, err := client.CompleteWithPriority(context.Background(), CompletionRequest{}, PriorityHigh, "guardian")

	if err != nil {
		t.Fatalf("expected no error for high priority, got: %v", err)
	}
	if resp.Content != "high priority ok" {
		t.Errorf("expected content 'high priority ok', got %q", resp.Content)
	}
	if inner.calls != 1 {
		t.Error("expected inner.Complete called once for high priority")
	}
}

func TestBudgetedClient_Unlimited(t *testing.T) {
	// dailyLimit=0 means unlimited — proceeds regardless of usage
	store := &mockBudgetStore{totalInput: 9999999, totalOutput: 9999999}
	inner := &mockClient{resp: &CompletionResponse{Content: "unlimited ok", InputTokens: 5, OutputTokens: 5}}

	client := NewBudgetedClient(inner, store, 0)
	resp, err := client.CompleteWithPriority(context.Background(), CompletionRequest{}, PriorityLow, "unknown")

	if err != nil {
		t.Fatalf("expected no error when unlimited, got: %v", err)
	}
	if resp.Content != "unlimited ok" {
		t.Errorf("expected content 'unlimited ok', got %q", resp.Content)
	}
}

func TestBudgetedClient_InnerError(t *testing.T) {
	store := &mockBudgetStore{totalInput: 0, totalOutput: 0}
	innerErr := errors.New("upstream failure")
	inner := &mockClient{err: innerErr}

	client := NewBudgetedClient(inner, store, 1000)
	_, err := client.CompleteWithPriority(context.Background(), CompletionRequest{}, PriorityMedium, "unknown")

	if err == nil {
		t.Fatal("expected error from inner, got nil")
	}
	if !errors.Is(err, innerErr) {
		t.Errorf("expected wrapped inner error, got: %v", err)
	}
	// Error message must contain context prefix
	expected := "budgeted client:"
	if len(err.Error()) < len(expected) || err.Error()[:len(expected)] != expected {
		t.Errorf("expected error prefixed with %q, got: %v", expected, err)
	}
}

func TestBudgetedClient_RecordUsageFailure(t *testing.T) {
	// RecordTokenUsage failure should NOT fail the LLM call (best-effort)
	store := &mockBudgetStore{
		totalInput:  0,
		totalOutput: 0,
		recordErr:   errors.New("db write failure"),
	}
	inner := &mockClient{resp: &CompletionResponse{Content: "still ok", InputTokens: 3, OutputTokens: 2}}

	client := NewBudgetedClient(inner, store, 1000)
	resp, err := client.CompleteWithPriority(context.Background(), CompletionRequest{}, PriorityMedium, "unknown")

	if err != nil {
		t.Fatalf("expected no error despite record failure, got: %v", err)
	}
	if resp.Content != "still ok" {
		t.Errorf("expected content 'still ok', got %q", resp.Content)
	}
}

func TestBudgetedClient_RemainingBudget(t *testing.T) {
	tests := []struct {
		name        string
		input       int
		output      int
		dailyLimit  int
		wantRemain  int
	}{
		{
			name:       "partial usage",
			input:      300,
			output:     200,
			dailyLimit: 1000,
			wantRemain: 500,
		},
		{
			name:       "fully exhausted",
			input:      600,
			output:     400,
			dailyLimit: 1000,
			wantRemain: 0,
		},
		{
			name:       "over limit clamped to zero",
			input:      700,
			output:     500,
			dailyLimit: 1000,
			wantRemain: 0,
		},
		{
			name:       "unlimited returns -1",
			input:      9999,
			output:     9999,
			dailyLimit: 0,
			wantRemain: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockBudgetStore{totalInput: tt.input, totalOutput: tt.output}
			inner := &mockClient{}
			client := NewBudgetedClient(inner, store, tt.dailyLimit)

			remaining, err := client.RemainingBudget()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if remaining != tt.wantRemain {
				t.Errorf("expected remaining=%d, got %d", tt.wantRemain, remaining)
			}
		})
	}
}

func TestBudgetedClient_RemainingBudget_StoreError(t *testing.T) {
	store := &mockBudgetStore{getErr: errors.New("db error")}
	inner := &mockClient{}
	client := NewBudgetedClient(inner, store, 1000)

	_, err := client.RemainingBudget()
	if err == nil {
		t.Fatal("expected error from store, got nil")
	}
	expected := fmt.Sprintf("budgeted client: get remaining budget: %s", store.getErr)
	if err.Error() != expected {
		t.Errorf("expected error %q, got %q", expected, err.Error())
	}
}

func TestBudgetedClient_Complete_UsesUnknownSubsystemAndMediumPriority(t *testing.T) {
	// Complete() must delegate to CompleteWithPriority with "unknown" + PriorityMedium
	// Verify by checking that usage is recorded under "unknown"
	store := &mockBudgetStore{}
	inner := &mockClient{resp: &CompletionResponse{Content: "ok", InputTokens: 1, OutputTokens: 1}}

	client := NewBudgetedClient(inner, store, 1000)
	_, err := client.Complete(context.Background(), CompletionRequest{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.recorded) != 1 {
		t.Fatalf("expected 1 recorded entry, got %d", len(store.recorded))
	}
	if store.recorded[0].subsystem != "unknown" {
		t.Errorf("expected subsystem 'unknown', got %q", store.recorded[0].subsystem)
	}
}

func TestBudgetedClient_ProviderAndModel(t *testing.T) {
	inner := &mockClient{}
	client := NewBudgetedClient(inner, &mockBudgetStore{}, 1000)

	if client.Provider() != "mock" {
		t.Errorf("expected Provider() 'mock', got %q", client.Provider())
	}
	if client.Model() != "mock-model" {
		t.Errorf("expected Model() 'mock-model', got %q", client.Model())
	}
}
