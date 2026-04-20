package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaClient talks to Ollama's native /api/chat endpoint instead of the
// OpenAI-compatible /v1/chat/completions proxy. This is required because the
// OpenAI-compat layer silently drops `response_format` for several models
// (notably gemma4), whereas the native endpoint honours `format: "json"` and
// constrains output to valid JSON.
type OllamaClient struct {
	config     Config
	httpClient *http.Client
}

func NewOllamaClient(cfg Config) (*OllamaClient, error) {
	return &OllamaClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.TimeoutDuration(),
		},
	}, nil
}

func (c *OllamaClient) Provider() string { return c.config.Provider }
func (c *OllamaClient) Model() string    { return c.config.Model }

func (c *OllamaClient) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   string          `json:"format,omitempty"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

type ollamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	// Ollama also returns a string `error` field on failures instead of an HTTP error.
	Error string `json:"error,omitempty"`
}

func (c *OllamaClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	messages := make([]ollamaMessage, 0, len(req.Messages)+1)

	if req.SystemPrompt != "" {
		messages = append(messages, ollamaMessage{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}

	for _, m := range req.Messages {
		messages = append(messages, ollamaMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.config.MaxTokens
	}

	temperature := req.Temperature
	if temperature == 0 {
		temperature = c.config.Temperature
	}

	body := ollamaRequest{
		Model:    c.config.Model,
		Messages: messages,
		Stream:   false,
		Options: &ollamaOptions{
			Temperature: temperature,
			NumPredict:  maxTokens,
		},
	}

	if req.ResponseFormat == "json" {
		body.Format = "json"
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", ollamaChatURL(c.config.EffectiveBaseURL()), bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("llm: failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitedError{RetryAfter: parseRetryAfter(resp.Header)}
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(respBody)
		if len(snippet) > 512 {
			snippet = snippet[:512] + "..."
		}
		return nil, fmt.Errorf("llm: api error (status %d): %s", resp.StatusCode, snippet)
	}

	var parsed ollamaResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("llm: failed to parse response: %w", err)
	}

	if parsed.Error != "" {
		return nil, fmt.Errorf("llm: api error: %s", parsed.Error)
	}

	if parsed.Message.Content == "" {
		return nil, fmt.Errorf("llm: empty message content in response")
	}

	return &CompletionResponse{
		Content:      parsed.Message.Content,
		Model:        parsed.Model,
		InputTokens:  parsed.PromptEvalCount,
		OutputTokens: parsed.EvalCount,
		FinishReason: parsed.DoneReason,
	}, nil
}

// ollamaChatURL converts an EffectiveBaseURL (e.g. http://localhost:11434/v1)
// into the native chat endpoint (http://localhost:11434/api/chat). The default
// ollama BaseURL in Config points at the OpenAI-compat prefix `/v1`; stripping
// it keeps the single configured host working for both the openai and native
// paths without requiring a new config field.
func ollamaChatURL(base string) string {
	trimmed := strings.TrimRight(base, "/")
	trimmed = strings.TrimSuffix(trimmed, "/v1")
	return trimmed + "/api/chat"
}
