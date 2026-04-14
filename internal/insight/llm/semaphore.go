package llm

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/semaphore"
)

// providerSemaphores interns *semaphore.Weighted by "provider|baseURL" key so
// that all Client instances targeting the same endpoint share one gate.
var providerSemaphores sync.Map // map[string]*semaphore.Weighted

func providerKey(cfg Config) string {
	return fmt.Sprintf("%s|%s", cfg.Provider, cfg.EffectiveBaseURL())
}

// semaphoreClient wraps a Client with a per-provider concurrency gate.
// When Concurrency == 0 the wrapper is a no-op pass-through.
type semaphoreClient struct {
	inner Client
	sem   *semaphore.Weighted // nil when concurrency is unlimited
}

// newSemaphoreClient returns a semaphoreClient that shares the semaphore for
// cfg's (provider, baseURL) key across all callers in the same process.
func newSemaphoreClient(inner Client, cfg Config) *semaphoreClient {
	if cfg.Concurrency <= 0 {
		return &semaphoreClient{inner: inner}
	}

	key := providerKey(cfg)
	if v, ok := providerSemaphores.Load(key); ok {
		return &semaphoreClient{inner: inner, sem: v.(*semaphore.Weighted)}
	}
	sem := semaphore.NewWeighted(int64(cfg.Concurrency))
	actual, _ := providerSemaphores.LoadOrStore(key, sem)
	return &semaphoreClient{
		inner: inner,
		sem:   actual.(*semaphore.Weighted),
	}
}

func (c *semaphoreClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	if c.sem != nil {
		if err := c.sem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		defer c.sem.Release(1)
	}
	return c.inner.Complete(ctx, req)
}

func (c *semaphoreClient) Provider() string { return c.inner.Provider() }
func (c *semaphoreClient) Model() string    { return c.inner.Model() }
