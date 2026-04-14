package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNoAPIKey             = errors.New("llm: no api key provided")
	ErrProviderNotSupported = errors.New("llm: provider not supported")
	ErrRequestFailed        = errors.New("llm: request failed")
	ErrRateLimited          = errors.New("llm: rate limited")
)

// RateLimitedError is returned when the provider responds with HTTP 429.
// RetryAfter carries the duration parsed from the Retry-After header (zero if absent).
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("llm: rate limited, retry after %v", e.RetryAfter)
	}
	return "llm: rate limited"
}

func (e *RateLimitedError) Is(target error) bool {
	return target == ErrRateLimited
}

// parseRetryAfter parses the Retry-After header per RFC 7231:
// it accepts both an integer seconds form and an HTTP-date form.
// Returns 0 if the header is absent or unparseable.
func parseRetryAfter(h http.Header) time.Duration {
	val := h.Get("Retry-After")
	if val == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionRequest struct {
	SystemPrompt string
	Messages     []Message
	MaxTokens    int
	Temperature  float64
}

type CompletionResponse struct {
	Content      string
	Model        string
	InputTokens  int
	OutputTokens int
	FinishReason string
}

type Client interface {
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
	Provider() string
	Model() string
}

func NewClient(cfg Config) (Client, error) {
	cfg = cfg.WithEnv()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var client Client
	var err error

	switch cfg.Provider {
	case "zai":
		client, err = NewZAIClient(cfg)
	case "openai", "ollama":
		client, err = NewOpenAIClient(cfg)
	case "anthropic":
		client, err = NewAnthropicClient(cfg)
	default:
		return nil, ErrProviderNotSupported
	}

	if err != nil {
		return nil, err
	}

	// Wrap retry first (inner), then semaphore (outer) so that a single semaphore
	// slot is held across all retry attempts — retry loops while holding the slot.
	if cfg.MaxRetries > 0 {
		client = NewClientWithRetry(client, RetryConfig{
			MaxRetries:  cfg.MaxRetries,
			InitialWait: 1 * time.Second,
			MaxWait:     30 * time.Second,
			Multiplier:  2.0,
		})
	}
	client = newSemaphoreClient(client, cfg)

	return client, nil
}

func MustNewClient(cfg Config) Client {
	client, err := NewClient(cfg)
	if err != nil {
		panic(err)
	}
	return client
}

// ParseJSONResponse attempts to extract JSON from LLM responses that may contain
// additional text or markdown formatting. It handles simple JSON objects and arrays
// but may fail with deeply nested structures.
// For production use, consider using a more robust JSON extraction library.
func ParseJSONResponse(response string, target interface{}) error {
	start := -1
	end := -1

	for i, c := range response {
		if c == '{' && start == -1 {
			start = i
		}
		if c == '}' {
			end = i
		}
		if c == '[' && start == -1 {
			start = i
		}
		if c == ']' {
			end = i
		}
	}

	if start == -1 || end == -1 {
		return errors.New("llm: no JSON found in response")
	}

	jsonStr := response[start : end+1]
	return json.Unmarshal([]byte(jsonStr), target)
}
