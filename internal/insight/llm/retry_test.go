package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClientWithRetry_RateLimitError_HonorsRetryAfter(t *testing.T) {
	retryAfter := 60 * time.Millisecond

	calls := 0
	inner := &countingClient{
		fn: func(i int) (*CompletionResponse, error) {
			calls++
			if i == 0 {
				return nil, &RateLimitedError{RetryAfter: retryAfter}
			}
			return &CompletionResponse{Content: "ok"}, nil
		},
	}

	retryClient := NewClientWithRetry(inner, RetryConfig{
		MaxRetries:  3,
		InitialWait: 5 * time.Millisecond, // much shorter than RetryAfter
		MaxWait:     500 * time.Millisecond,
		Multiplier:  2.0,
	})

	start := time.Now()
	resp, err := retryClient.Complete(context.Background(), CompletionRequest{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q, want ok", resp.Content)
	}
	// Must have waited at least RetryAfter (with a small tolerance).
	if elapsed < retryAfter-5*time.Millisecond {
		t.Errorf("elapsed = %v, expected at least %v (RetryAfter)", elapsed, retryAfter)
	}
}

func TestClientWithRetry_NonRateLimitError_UsesGenericBackoff(t *testing.T) {
	genericBackoff := 50 * time.Millisecond
	retryAfter := 500 * time.Millisecond // much longer, must NOT be used

	calls := 0
	inner := &countingClient{
		fn: func(i int) (*CompletionResponse, error) {
			calls++
			if i == 0 {
				// Return a regular error, not a RateLimitedError.
				return nil, errors.New("generic error")
			}
			return &CompletionResponse{Content: "recovered"}, nil
		},
	}
	_ = retryAfter

	retryClient := NewClientWithRetry(inner, RetryConfig{
		MaxRetries:  3,
		InitialWait: genericBackoff,
		MaxWait:     500 * time.Millisecond,
		Multiplier:  2.0,
	})

	start := time.Now()
	resp, err := retryClient.Complete(context.Background(), CompletionRequest{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "recovered" {
		t.Errorf("content = %q, want recovered", resp.Content)
	}
	// Should use genericBackoff, not the much-longer retryAfter.
	if elapsed >= retryAfter {
		t.Errorf("elapsed = %v, expected < %v (should use generic backoff, not RetryAfter)", elapsed, retryAfter)
	}
}

func TestClientWithRetry_RateLimitError_ZeroRetryAfter_FallsBackToGenericWait(t *testing.T) {
	genericBackoff := 40 * time.Millisecond

	inner := &countingClient{
		fn: func(i int) (*CompletionResponse, error) {
			if i == 0 {
				return nil, &RateLimitedError{RetryAfter: 0} // zero → fall back to generic
			}
			return &CompletionResponse{Content: "ok"}, nil
		},
	}

	retryClient := NewClientWithRetry(inner, RetryConfig{
		MaxRetries:  3,
		InitialWait: genericBackoff,
		MaxWait:     500 * time.Millisecond,
		Multiplier:  2.0,
	})

	start := time.Now()
	resp, err := retryClient.Complete(context.Background(), CompletionRequest{})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q, want ok", resp.Content)
	}
	// Should wait the generic backoff (40ms).
	if elapsed < genericBackoff-5*time.Millisecond {
		t.Errorf("elapsed = %v, expected at least %v (generic backoff)", elapsed, genericBackoff)
	}
}
