package llm

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNoAPIKey             = errors.New("llm: no api key provided")
	ErrProviderNotSupported = errors.New("llm: provider not supported")
	ErrRequestFailed        = errors.New("llm: request failed")
)

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

	if cfg.MaxRetries > 0 {
		return NewClientWithRetry(client, RetryConfig{
			MaxRetries:  cfg.MaxRetries,
			InitialWait: 1 * time.Second,
			MaxWait:     30 * time.Second,
		}), nil
	}

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
