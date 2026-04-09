package llm

import (
	"context"
	"errors"
	"testing"
)

// compile-time check: SubsystemClient must satisfy Client interface
var _ Client = (*SubsystemClient)(nil)

func TestSubsystemClient_SatisfiesClientInterface(t *testing.T) {
	// compile-time assertion above is sufficient; this test documents intent
	t.Log("SubsystemClient satisfies Client interface")
}

func TestSubsystemClient_DelegatesToBudgeted(t *testing.T) {
	store := &mockBudgetStore{}
	inner := &mockClient{resp: &CompletionResponse{Content: "wiki result", InputTokens: 8, OutputTokens: 4}}
	budgeted := NewBudgetedClient(inner, store, 1000)

	sc := NewSubsystemClient(budgeted, "wiki_engine", PriorityMedium)
	resp, err := sc.Complete(context.Background(), CompletionRequest{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "wiki result" {
		t.Errorf("expected 'wiki result', got %q", resp.Content)
	}
	// Verify subsystem was forwarded to store
	if len(store.recorded) != 1 {
		t.Fatalf("expected 1 recorded usage entry, got %d", len(store.recorded))
	}
	if store.recorded[0].subsystem != "wiki_engine" {
		t.Errorf("expected subsystem 'wiki_engine', got %q", store.recorded[0].subsystem)
	}
	if store.recorded[0].input != 8 || store.recorded[0].output != 4 {
		t.Errorf("expected input=8 output=4, got input=%d output=%d", store.recorded[0].input, store.recorded[0].output)
	}
}

func TestSubsystemClient_LowPriorityBlockedByBudget(t *testing.T) {
	// evolution_loop is PriorityLow; exhausted budget should block it
	store := &mockBudgetStore{totalInput: 500, totalOutput: 500} // 1000 used = limit
	inner := &mockClient{resp: &CompletionResponse{Content: "should not reach"}}
	budgeted := NewBudgetedClient(inner, store, 1000)

	sc := NewSubsystemClient(budgeted, "evolution_loop", PriorityLow)
	_, err := sc.Complete(context.Background(), CompletionRequest{})

	if !errors.Is(err, ErrBudgetExhausted) {
		t.Errorf("expected ErrBudgetExhausted for low priority with exhausted budget, got: %v", err)
	}
}

func TestSubsystemClient_HighPriorityBypassesBudget(t *testing.T) {
	// guardian is PriorityHigh; exhausted budget must NOT block it
	store := &mockBudgetStore{totalInput: 500, totalOutput: 500}
	inner := &mockClient{resp: &CompletionResponse{Content: "guardian ok", InputTokens: 5, OutputTokens: 3}}
	budgeted := NewBudgetedClient(inner, store, 1000)

	sc := NewSubsystemClient(budgeted, "guardian", PriorityHigh)
	resp, err := sc.Complete(context.Background(), CompletionRequest{})

	if err != nil {
		t.Fatalf("expected no error for high priority, got: %v", err)
	}
	if resp.Content != "guardian ok" {
		t.Errorf("expected 'guardian ok', got %q", resp.Content)
	}
}

func TestSubsystemClient_InvalidSubsystem(t *testing.T) {
	store := &mockBudgetStore{}
	inner := &mockClient{}
	budgeted := NewBudgetedClient(inner, store, 1000)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for invalid subsystem, got none")
		}
	}()

	NewSubsystemClient(budgeted, "not_a_real_subsystem", PriorityMedium)
}

func TestSubsystemClient_ProviderAndModel(t *testing.T) {
	store := &mockBudgetStore{}
	inner := &mockClient{}
	budgeted := NewBudgetedClient(inner, store, 1000)
	sc := NewSubsystemClient(budgeted, "synthesizer", PriorityMedium)

	if sc.Provider() != "mock" {
		t.Errorf("expected Provider() 'mock', got %q", sc.Provider())
	}
	if sc.Model() != "mock-model" {
		t.Errorf("expected Model() 'mock-model', got %q", sc.Model())
	}
}
