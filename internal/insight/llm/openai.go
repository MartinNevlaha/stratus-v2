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

type OpenAIClient struct {
	config     Config
	httpClient *http.Client
}

func NewOpenAIClient(cfg Config) (*OpenAIClient, error) {
	return &OpenAIClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.TimeoutDuration(),
		},
	}, nil
}

func (c *OpenAIClient) Provider() string { return c.config.Provider }
func (c *OpenAIClient) Model() string    { return c.config.Model }

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIError struct {
	// Error is stored as raw JSON to tolerate both shapes:
	//   object: {"error": {"message": "...", "type": "...", "code": "..."}}
	//   string: {"error": "some message"}
	// Some OpenAI-compatible providers (e.g. local proxies) return the string shape.
	RawError json.RawMessage `json:"error,omitempty"`
}

// errorMessage extracts a human-readable error message from the raw JSON field,
// tolerating both the object shape {"message":"..."} and the bare string shape.
func (e openAIError) errorMessage() string {
	if len(e.RawError) == 0 || string(e.RawError) == "null" {
		return ""
	}
	// Try string shape first.
	var s string
	if err := json.Unmarshal(e.RawError, &s); err == nil {
		return s
	}
	// Try object shape.
	var obj struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(e.RawError, &obj); err == nil && obj.Message != "" {
		return obj.Message
	}
	// Fallback: return raw bytes.
	return string(e.RawError)
}

func (c *OpenAIClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)

	if req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}

	for _, m := range req.Messages {
		messages = append(messages, openAIMessage{
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

	body := openAIRequest{
		Model:       c.config.Model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to marshal request: %w", err)
	}

	url := strings.TrimRight(c.config.EffectiveBaseURL(), "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("llm: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

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
		var apiErr openAIError
		_ = json.Unmarshal(respBody, &apiErr)
		if msg := apiErr.errorMessage(); msg != "" {
			return nil, fmt.Errorf("llm: api error (status %d): %s", resp.StatusCode, msg)
		}
		snippet := string(respBody)
		if len(snippet) > 512 {
			snippet = snippet[:512] + "..."
		}
		return nil, fmt.Errorf("llm: api error (status %d): %s", resp.StatusCode, snippet)
	}

	var openAIResp openAIResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return nil, fmt.Errorf("llm: failed to parse response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("llm: no choices in response")
	}

	return &CompletionResponse{
		Content:      openAIResp.Choices[0].Message.Content,
		Model:        openAIResp.Model,
		InputTokens:  openAIResp.Usage.PromptTokens,
		OutputTokens: openAIResp.Usage.CompletionTokens,
		FinishReason: openAIResp.Choices[0].FinishReason,
	}, nil
}

func (c *OpenAIClient) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}
