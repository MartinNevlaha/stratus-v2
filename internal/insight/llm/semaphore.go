package llm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
)

// providerGate holds the shared concurrency semaphore and the last-call
// timestamp for a single (provider, baseURL) key.
type providerGate struct {
	sem      *semaphore.Weighted // nil when Concurrency <= 0
	lastCall *atomic.Int64       // unix nano of last request start; shared across clients
}

// providerGates interns *providerGate by "provider|baseURL" key so that all
// Client instances targeting the same endpoint share one gate.
var providerGates sync.Map // map[string]*providerGate

func providerKey(cfg Config) string {
	return fmt.Sprintf("%s|%s", cfg.Provider, cfg.EffectiveBaseURL())
}

// semaphoreClient wraps a Client with a per-provider concurrency gate and
// optional minimum-request-interval spacing.
// When both Concurrency == 0 and MinRequestIntervalMs == 0 the wrapper is a
// no-op pass-through.
type semaphoreClient struct {
	inner       Client
	sem         *semaphore.Weighted // nil when Concurrency <= 0
	minInterval time.Duration       // 0 = disabled
	lastCall    *atomic.Int64       // shared pointer from providerGate; nil when spacing disabled
}

// newSemaphoreClient returns a semaphoreClient that shares the gate for
// cfg's (provider, baseURL) key across all callers in the same process.
func newSemaphoreClient(inner Client, cfg Config) *semaphoreClient {
	if cfg.Concurrency <= 0 && cfg.MinRequestIntervalMs <= 0 {
		return &semaphoreClient{inner: inner}
	}

	key := providerKey(cfg)

	// Build a candidate gate; LoadOrStore ensures first-writer-wins.
	var sem *semaphore.Weighted
	if cfg.Concurrency > 0 {
		sem = semaphore.NewWeighted(int64(cfg.Concurrency))
	}
	lastCall := &atomic.Int64{}
	candidate := &providerGate{sem: sem, lastCall: lastCall}
	actual, _ := providerGates.LoadOrStore(key, candidate)
	gate := actual.(*providerGate)

	return &semaphoreClient{
		inner:       inner,
		sem:         gate.sem,
		minInterval: time.Duration(cfg.MinRequestIntervalMs) * time.Millisecond,
		lastCall:    gate.lastCall,
	}
}

func (c *semaphoreClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	if c.sem != nil {
		if err := c.sem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		defer c.sem.Release(1)
	}

	if c.minInterval > 0 && c.lastCall != nil {
		last := c.lastCall.Load()
		if last > 0 {
			elapsed := time.Duration(time.Now().UnixNano() - last)
			if elapsed < c.minInterval {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(c.minInterval - elapsed):
				}
			}
		}
		c.lastCall.Store(time.Now().UnixNano())
	}

	return c.inner.Complete(ctx, req)
}

func (c *semaphoreClient) Provider() string { return c.inner.Provider() }
func (c *semaphoreClient) Model() string    { return c.inner.Model() }
