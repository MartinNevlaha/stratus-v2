package guardian

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// llmClient is a minimal OpenAI-compatible chat completions client.
type llmClient struct {
	endpoint    string
	apiKey      string
	model       string
	temperature float64
	maxTokens   int
	httpClient  *http.Client
}

func newLLMClient(endpoint, apiKey, model string, temperature float64, maxTokens int) *llmClient {
	return &llmClient{
		endpoint:    endpoint,
		apiKey:      apiKey,
		model:       model,
		temperature: temperature,
		maxTokens:   maxTokens,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *llmClient) configured() bool {
	return c.endpoint != "" && c.model != ""
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Complete sends a single user prompt and returns the assistant response text.
func (c *llmClient) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if !c.configured() {
		return "", fmt.Errorf("llm not configured")
	}

	messages := []chatMessage{}
	if systemPrompt != "" {
		messages = append(messages, chatMessage{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, chatMessage{Role: "user", Content: userPrompt})

	reqBody := chatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: c.temperature,
		MaxTokens:   c.maxTokens,
	}
	if reqBody.MaxTokens == 0 {
		reqBody.MaxTokens = 1024
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := c.endpoint
	if len(url) > 0 && url[len(url)-1] != '/' {
		url += "/"
	}
	url += "chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", err
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return "", fmt.Errorf("llm response parse: %w", err)
	}
	if chatResp.Error != nil {
		return "", fmt.Errorf("llm error: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices")
	}
	return chatResp.Choices[0].Message.Content, nil
}

// TestConnection sends a minimal prompt to verify the endpoint is reachable.
func (c *llmClient) TestConnection(ctx context.Context) error {
	_, err := c.Complete(ctx, "", "Reply with the single word: ok")
	return err
}

// TestLLMEndpoint tests an OpenAI-compatible LLM endpoint with a minimal prompt.
func TestLLMEndpoint(ctx context.Context, endpoint, apiKey, model string, temperature float64, maxTokens int) error {
	c := newLLMClient(endpoint, apiKey, model, temperature, maxTokens)
	return c.TestConnection(ctx)
}
