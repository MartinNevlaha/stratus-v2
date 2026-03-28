package llm

import (
	"context"
	"time"
)

type RetryConfig struct {
	MaxRetries  int           `json:"max_retries"`
	InitialWait time.Duration `json:"initial_wait"`
	MaxWait     time.Duration `json:"max_wait"`
	Multiplier  float64       `json:"multiplier"`
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:  3,
		InitialWait: 1 * time.Second,
		MaxWait:     30 * time.Second,
		Multiplier:  2.0,
	}
}

type ClientWithRetry struct {
	client Client
	config RetryConfig
}

func NewClientWithRetry(client Client, config RetryConfig) *ClientWithRetry {
	return &ClientWithRetry{
		client: client,
		config: config,
	}
}

func (c *ClientWithRetry) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	var lastErr error
	wait := c.config.InitialWait

	for i := 0; i <= c.config.MaxRetries; i++ {
		resp, err := c.client.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		if i < c.config.MaxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			wait = time.Duration(float64(wait) * c.config.Multiplier)
			if wait > c.config.MaxWait {
				wait = c.config.MaxWait
			}
		}
	}

	return nil, lastErr
}

func (c *ClientWithRetry) Provider() string { return c.client.Provider() }
func (c *ClientWithRetry) Model() string    { return c.client.Model() }
