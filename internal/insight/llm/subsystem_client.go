package llm

import (
	"context"
	"fmt"
)

// SubsystemClient tags all LLM calls with a subsystem name and priority for budget tracking.
type SubsystemClient struct {
	inner     *BudgetedClient
	subsystem string
	priority  Priority
}

// NewSubsystemClient creates a client that tags calls for budget tracking.
// Panics if subsystem is not in AllowedSubsystems.
func NewSubsystemClient(inner *BudgetedClient, subsystem string, priority Priority) *SubsystemClient {
	if !AllowedSubsystems[subsystem] {
		panic(fmt.Sprintf("llm: unknown subsystem %q", subsystem))
	}
	return &SubsystemClient{inner: inner, subsystem: subsystem, priority: priority}
}

func (s *SubsystemClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	return s.inner.CompleteWithPriority(ctx, req, s.priority, s.subsystem)
}

func (s *SubsystemClient) Provider() string { return s.inner.Provider() }
func (s *SubsystemClient) Model() string    { return s.inner.Model() }
