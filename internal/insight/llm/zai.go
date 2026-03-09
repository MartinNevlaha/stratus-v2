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

type ZAIClient struct {
	config     Config
	httpClient *http.Client
}

func NewZAIClient(cfg Config) (*ZAIClient, error) {
	return &ZAIClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.TimeoutDuration(),
		},
	}, nil
}

func (c *ZAIClient) Provider() string { return "zai" }
func (c *ZAIClient) Model() string    { return c.config.Model }

type zaiRequest struct {
	Model       string       `json:"model"`
	Messages    []zaiMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
	Stream      bool         `json:"stream"`
}

type zaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type zaiResponse struct {
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

type zaiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (c *ZAIClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	messages := make([]zaiMessage, 0, len(req.Messages)+1)

	if req.SystemPrompt != "" {
		messages = append(messages, zaiMessage{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}

	for _, m := range req.Messages {
		messages = append(messages, zaiMessage{
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

	body := zaiRequest{
		Model:       c.config.Model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      false,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: failed to marshal request: %w", err)
	}

	url := c.config.EffectiveBaseURL() + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("llm: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	httpReq.Header.Set("Accept-Language", "en-US,en")

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
		var apiErr zaiError
		_ = json.Unmarshal(respBody, &apiErr)
		return nil, fmt.Errorf("llm: z.ai api error (status %d): %s", resp.StatusCode, apiErr.Error.Message)
	}

	var zaiResp zaiResponse
	if err := json.Unmarshal(respBody, &zaiResp); err != nil {
		return nil, fmt.Errorf("llm: failed to parse response: %w", err)
	}

	if len(zaiResp.Choices) == 0 {
		return nil, fmt.Errorf("llm: no choices in response")
	}

	return &CompletionResponse{
		Content:      zaiResp.Choices[0].Message.Content,
		Model:        zaiResp.Model,
		InputTokens:  zaiResp.Usage.PromptTokens,
		OutputTokens: zaiResp.Usage.CompletionTokens,
		FinishReason: zaiResp.Choices[0].FinishReason,
	}, nil
}

func (c *ZAIClient) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}
