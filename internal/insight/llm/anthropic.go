package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type AnthropicClient struct {
	config     Config
	httpClient *http.Client
}

func NewAnthropicClient(cfg Config) (*AnthropicClient, error) {
	return &AnthropicClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.TimeoutDuration(),
		},
	}, nil
}

func (c *AnthropicClient) Provider() string { return "anthropic" }
func (c *AnthropicClient) Model() string    { return c.config.Model }

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason   string `json:"stop_reason"`
	StopSequence string `json:"stop_sequence,omitempty"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *AnthropicClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	messages := make([]anthropicMessage, 0, len(req.Messages))

	for _, m := range req.Messages {
		if m.Role == "system" {
			continue
		}
		messages = append(messages, anthropicMessage{
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

	body := anthropicRequest{
		Model:       c.config.Model,
		MaxTokens:   maxTokens,
		System:      req.SystemPrompt,
		Messages:    messages,
		Temperature: temperature,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to marshal request: %w", err)
	}

	url := c.config.EffectiveBaseURL() + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("llm: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.config.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr anthropicError
		_ = json.Unmarshal(respBody, &apiErr)
		return nil, fmt.Errorf("llm: anthropic api error (status %d): %s", resp.StatusCode, apiErr.Error.Message)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("llm: failed to parse response: %w", err)
	}

	if len(anthropicResp.Content) == 0 {
		return nil, fmt.Errorf("llm: no content in response")
	}

	content := ""
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &CompletionResponse{
		Content:      content,
		Model:        anthropicResp.Model,
		InputTokens:  anthropicResp.Usage.InputTokens,
		OutputTokens: anthropicResp.Usage.OutputTokens,
		FinishReason: anthropicResp.StopReason,
	}, nil
}

func (c *AnthropicClient) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}
