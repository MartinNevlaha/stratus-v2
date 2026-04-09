package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var ErrBudgetExhausted = errors.New("llm: daily token budget exhausted")

// Priority levels for token budget allocation.
type Priority int

const (
	PriorityLow    Priority = iota // evolution loop
	PriorityMedium                 // wiki engine, product intel
	PriorityHigh                   // user queries, guardian
)

// AllowedSubsystems defines the valid subsystem names for budget tracking.
var AllowedSubsystems = map[string]bool{
	"wiki_engine":    true,
	"evolution_loop": true,
	"guardian":       true,
	"synthesizer":    true,
	"product_intel":  true,
	"unknown":        true,
}

// BudgetStore abstracts the DB operations needed by BudgetedClient.
type BudgetStore interface {
	GetDailyTokenUsageTotal(date string) (input, output int, err error)
	RecordTokenUsage(date, subsystem string, input, output int) error
}

// BudgetedClient wraps an llm.Client with daily token budget enforcement.
type BudgetedClient struct {
	inner      Client
	store      BudgetStore
	dailyLimit int // 0 = unlimited
	mu         sync.Mutex
}

// NewBudgetedClient wraps a Client with budget tracking.
func NewBudgetedClient(inner Client, store BudgetStore, dailyLimit int) *BudgetedClient {
	return &BudgetedClient{inner: inner, store: store, dailyLimit: dailyLimit}
}

// Complete implements Client. Uses default "unknown" subsystem and PriorityMedium.
func (b *BudgetedClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	return b.CompleteWithPriority(ctx, req, PriorityMedium, "unknown")
}

func (b *BudgetedClient) Provider() string { return b.inner.Provider() }
func (b *BudgetedClient) Model() string    { return b.inner.Model() }

// RemainingBudget returns tokens remaining for today. Returns -1 if unlimited.
func (b *BudgetedClient) RemainingBudget() (int, error) {
	if b.dailyLimit <= 0 {
		return -1, nil
	}
	today := time.Now().UTC().Format("2006-01-02")
	input, output, err := b.store.GetDailyTokenUsageTotal(today)
	if err != nil {
		return 0, fmt.Errorf("budgeted client: get remaining budget: %w", err)
	}
	used := input + output
	remaining := b.dailyLimit - used
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}

// DailyLimit returns the configured daily token limit.
func (b *BudgetedClient) DailyLimit() int { return b.dailyLimit }

// CompleteWithPriority checks budget, calls inner, records usage.
// High priority always proceeds. Low/Medium are blocked when budget is exhausted.
func (b *BudgetedClient) CompleteWithPriority(ctx context.Context, req CompletionRequest, priority Priority, subsystem string) (*CompletionResponse, error) {
	// Budget check (best-effort, soft limit)
	if b.dailyLimit > 0 && priority < PriorityHigh {
		today := time.Now().UTC().Format("2006-01-02")
		input, output, err := b.store.GetDailyTokenUsageTotal(today)
		if err != nil {
			slog.Warn("budgeted client: budget check failed, proceeding anyway", "err", err)
		} else if input+output >= b.dailyLimit {
			return nil, ErrBudgetExhausted
		}
	}

	resp, err := b.inner.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("budgeted client: %w", err)
	}

	// Record usage (best-effort)
	today := time.Now().UTC().Format("2006-01-02")
	if recordErr := b.store.RecordTokenUsage(today, subsystem, resp.InputTokens, resp.OutputTokens); recordErr != nil {
		slog.Warn("budgeted client: record token usage failed",
			"subsystem", subsystem,
			"date", today,
			"err", recordErr,
		)
	}

	return resp, nil
}
