package llm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// semaphoreClient wrapper tests
// ---------------------------------------------------------------------------

// blockingClient blocks until unblockCh is closed, tracking in-flight count.
type blockingClient struct {
	unblockCh chan struct{}
	inFlight  atomic.Int32
	maxSeen   atomic.Int32
	callCount atomic.Int32
}

func (c *blockingClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	current := c.inFlight.Add(1)
	defer c.inFlight.Add(-1)
	c.callCount.Add(1)

	for {
		seen := c.maxSeen.Load()
		if current <= seen {
			break
		}
		if c.maxSeen.CompareAndSwap(seen, current) {
			break
		}
	}

	select {
	case <-c.unblockCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return &CompletionResponse{Content: "ok"}, nil
}

func (c *blockingClient) Provider() string { return "zai" }
func (c *blockingClient) Model() string    { return "glm-5" }

// instantClient returns immediately without blocking.
type instantClient struct {
	callCount atomic.Int32
}

func (c *instantClient) Complete(_ context.Context, _ CompletionRequest) (*CompletionResponse, error) {
	c.callCount.Add(1)
	return &CompletionResponse{Content: "ok"}, nil
}

func (c *instantClient) Provider() string { return "zai" }
func (c *instantClient) Model() string    { return "glm-5" }

// resetProviderGates clears the global gate map between tests.
func resetProviderGates() {
	providerGates.Range(func(k, v any) bool {
		providerGates.Delete(k)
		return true
	})
}

func TestSemaphoreClient_Concurrency1_Serializes(t *testing.T) {
	resetProviderGates()

	unblock := make(chan struct{})
	inner := &blockingClient{unblockCh: unblock}

	// Both clients share same provider+baseURL → same semaphore.
	cfg := Config{
		Provider:    "zai",
		Model:       "glm-5",
		APIKey:      "k",
		BaseURL:     "http://test.example.com",
		Concurrency: 1,
	}
	wrapped1 := newSemaphoreClient(inner, cfg)
	wrapped2 := newSemaphoreClient(inner, cfg)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		wrapped1.Complete(context.Background(), CompletionRequest{}) //nolint
	}()
	go func() {
		defer wg.Done()
		wrapped2.Complete(context.Background(), CompletionRequest{}) //nolint
	}()

	// Give goroutines time to start and attempt to acquire.
	time.Sleep(50 * time.Millisecond)

	// Max in-flight must be 1 — the semaphore serializes them.
	if got := inner.maxSeen.Load(); got > 1 {
		t.Errorf("max in-flight = %d, want <=1 (concurrency=1 must serialize)", got)
	}

	close(unblock)
	wg.Wait()

	if got := inner.callCount.Load(); got != 2 {
		t.Errorf("callCount = %d, want 2", got)
	}
}

func TestSemaphoreClient_DifferentBaseURL_RunsParallel(t *testing.T) {
	resetProviderGates()

	unblock := make(chan struct{})
	inner := &blockingClient{unblockCh: unblock}

	cfgA := Config{Provider: "zai", Model: "glm-5", APIKey: "k", BaseURL: "http://host-a.example.com", Concurrency: 1}
	cfgB := Config{Provider: "zai", Model: "glm-5", APIKey: "k", BaseURL: "http://host-b.example.com", Concurrency: 1}

	wrappedA := newSemaphoreClient(inner, cfgA)
	wrappedB := newSemaphoreClient(inner, cfgB)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		wrappedA.Complete(context.Background(), CompletionRequest{}) //nolint
	}()
	go func() {
		defer wg.Done()
		wrappedB.Complete(context.Background(), CompletionRequest{}) //nolint
	}()

	// Give both goroutines time to enter Complete concurrently.
	time.Sleep(50 * time.Millisecond)

	// Different keys → different semaphores → both run in parallel.
	if got := inner.maxSeen.Load(); got < 2 {
		t.Errorf("max in-flight = %d, want 2 (different baseURLs must not share semaphore)", got)
	}

	close(unblock)
	wg.Wait()
}

func TestSemaphoreClient_ContextCancelled_BeforeAcquire(t *testing.T) {
	resetProviderGates()

	unblock := make(chan struct{}) // never closed — first call blocks forever
	inner := &blockingClient{unblockCh: unblock}

	cfg := Config{
		Provider:    "zai",
		Model:       "glm-5",
		APIKey:      "k",
		BaseURL:     "http://cancel-test.example.com",
		Concurrency: 1,
	}

	wrapped := newSemaphoreClient(inner, cfg)

	// First call acquires the semaphore and blocks.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wrapped.Complete(context.Background(), CompletionRequest{}) //nolint
	}()

	// Give first goroutine time to acquire.
	time.Sleep(30 * time.Millisecond)

	// Second call is cancelled before it can acquire.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := wrapped.Complete(ctx, CompletionRequest{})
	if err == nil {
		t.Fatal("expected context error when cancelled before acquire")
	}
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("expected context error, got: %v", err)
	}

	// Unblock the first goroutine.
	close(unblock)
	wg.Wait()
}

func TestSemaphoreClient_Unlimited_RunsParallel(t *testing.T) {
	resetProviderGates()

	unblock := make(chan struct{})
	inner := &blockingClient{unblockCh: unblock}

	// Concurrency=0 means unlimited.
	cfg := Config{Provider: "zai", Model: "glm-5", APIKey: "k", BaseURL: "http://unlimited.example.com", Concurrency: 0}
	wrapped1 := newSemaphoreClient(inner, cfg)
	wrapped2 := newSemaphoreClient(inner, cfg)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		wrapped1.Complete(context.Background(), CompletionRequest{}) //nolint
	}()
	go func() {
		defer wg.Done()
		wrapped2.Complete(context.Background(), CompletionRequest{}) //nolint
	}()

	time.Sleep(50 * time.Millisecond)

	if got := inner.maxSeen.Load(); got < 2 {
		t.Errorf("max in-flight = %d, want 2 (concurrency=0 is unlimited)", got)
	}

	close(unblock)
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Spacing (MinRequestIntervalMs) tests
// ---------------------------------------------------------------------------

func TestSpacing_Zero_NoDelay(t *testing.T) {
	resetProviderGates()

	inner := &instantClient{}
	cfg := Config{
		Provider:             "zai",
		Model:                "glm-5",
		APIKey:               "k",
		BaseURL:              "http://spacing-zero.example.com",
		MinRequestIntervalMs: 0,
	}
	wrapped := newSemaphoreClient(inner, cfg)

	start := time.Now()
	for i := 0; i < 5; i++ {
		if _, err := wrapped.Complete(context.Background(), CompletionRequest{}); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	if elapsed >= 100*time.Millisecond {
		t.Errorf("5 calls with MinRequestIntervalMs=0 took %v, want < 100ms", elapsed)
	}
	if got := inner.callCount.Load(); got != 5 {
		t.Errorf("callCount = %d, want 5", got)
	}
}

func TestSpacing_EnforcesMinInterval(t *testing.T) {
	resetProviderGates()

	inner := &instantClient{}
	cfg := Config{
		Provider:             "zai",
		Model:                "glm-5",
		APIKey:               "k",
		BaseURL:              "http://spacing-enforce.example.com",
		MinRequestIntervalMs: 200,
	}
	wrapped := newSemaphoreClient(inner, cfg)

	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := wrapped.Complete(context.Background(), CompletionRequest{}); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	// First call is immediate; calls 2 and 3 each wait ≥200ms → total ≥400ms.
	if elapsed < 400*time.Millisecond {
		t.Errorf("3 calls with MinRequestIntervalMs=200 took %v, want >= 400ms", elapsed)
	}
	if got := inner.callCount.Load(); got != 3 {
		t.Errorf("callCount = %d, want 3", got)
	}
}

func TestSpacing_SharedAcrossClients(t *testing.T) {
	resetProviderGates()

	inner := &instantClient{}
	cfg := Config{
		Provider:             "zai",
		Model:                "glm-5",
		APIKey:               "k",
		BaseURL:              "http://spacing-shared.example.com",
		MinRequestIntervalMs: 200,
	}
	// Two distinct wrappers sharing the same (provider, baseURL) key.
	wrapped1 := newSemaphoreClient(inner, cfg)
	wrapped2 := newSemaphoreClient(inner, cfg)

	start := time.Now()
	// Interleave: 3 calls on wrapped1, 2 calls on wrapped2.
	clients := []*semaphoreClient{wrapped1, wrapped2, wrapped1, wrapped2, wrapped1}
	for i, c := range clients {
		if _, err := c.Complete(context.Background(), CompletionRequest{}); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	// First call immediate; 4 subsequent each wait ≥200ms → total ≥800ms.
	if elapsed < 800*time.Millisecond {
		t.Errorf("5 interleaved calls with shared spacing took %v, want >= 800ms", elapsed)
	}
	if got := inner.callCount.Load(); got != 5 {
		t.Errorf("callCount = %d, want 5", got)
	}
}

func TestSpacing_ContextCancelled_DuringWait(t *testing.T) {
	resetProviderGates()

	inner := &instantClient{}
	cfg := Config{
		Provider:             "zai",
		Model:                "glm-5",
		APIKey:               "k",
		BaseURL:              "http://spacing-cancel.example.com",
		MinRequestIntervalMs: 1000,
	}
	wrapped := newSemaphoreClient(inner, cfg)

	// First call — sets lastCall, completes immediately.
	if _, err := wrapped.Complete(context.Background(), CompletionRequest{}); err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Second call — will need to wait ~1000ms; cancel after ~200ms.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := wrapped.Complete(ctx, CompletionRequest{})
	if err == nil {
		t.Fatal("expected context error during spacing wait, got nil")
	}
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("expected context error, got: %v", err)
	}

	// Inner must not have been called a second time.
	if got := inner.callCount.Load(); got != 1 {
		t.Errorf("callCount = %d, want 1 (inner must not be called when ctx cancelled during wait)", got)
	}
}
