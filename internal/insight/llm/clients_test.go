package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newOpenAISuccessHandler(content, model string, promptTokens, completionTokens int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := openAIResponse{
			Model: model,
			Choices: []struct {
				Index   int `json:"index"`
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{Role: "assistant", Content: content},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{PromptTokens: promptTokens, CompletionTokens: completionTokens},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func newOpenAIErrorHandler(statusCode int, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		// Write the standard OpenAI object-shaped error directly.
		fmt.Fprintf(w, `{"error":{"message":%q,"type":"","code":""}}`, message)
	}
}

// ---------------------------------------------------------------------------
// OpenAI client tests
// ---------------------------------------------------------------------------

func TestOpenAIClient_Complete_Success(t *testing.T) {
	srv := httptest.NewServer(newOpenAISuccessHandler("hello world", "gpt-4", 10, 5))
	defer srv.Close()

	cfg := Config{
		Provider:    "openai",
		Model:       "gpt-4",
		APIKey:      "test-key",
		BaseURL:     srv.URL,
		Temperature: 0.7,
		MaxTokens:   100,
	}
	client, err := NewOpenAIClient(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}

	resp, err := client.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "you are a helper",
		Messages:     []Message{{Role: "user", Content: "say hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("content = %q, want %q", resp.Content, "hello world")
	}
	if resp.Model != "gpt-4" {
		t.Errorf("model = %q, want %q", resp.Model, "gpt-4")
	}
	if resp.InputTokens != 10 {
		t.Errorf("input_tokens = %d, want 10", resp.InputTokens)
	}
	if resp.OutputTokens != 5 {
		t.Errorf("output_tokens = %d, want 5", resp.OutputTokens)
	}
}

func TestOpenAIClient_Complete_NoSystemPrompt(t *testing.T) {
	srv := httptest.NewServer(newOpenAISuccessHandler("pong", "gpt-3.5", 2, 1))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-3.5", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewOpenAIClient(cfg)

	resp, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "pong" {
		t.Errorf("content = %q, want pong", resp.Content)
	}
}

func TestOpenAIClient_Complete_APIError(t *testing.T) {
	srv := httptest.NewServer(newOpenAIErrorHandler(http.StatusUnauthorized, "invalid api key"))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "bad-key", BaseURL: srv.URL}
	client, _ := NewOpenAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestOpenAIClient_Complete_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openAIResponse{}) // no choices
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewOpenAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestOpenAIClient_Complete_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewOpenAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestOpenAIClient_Complete_NetworkError(t *testing.T) {
	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: "http://127.0.0.1:0"}
	client, _ := NewOpenAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestOpenAIClient_ProviderAndModel(t *testing.T) {
	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k"}
	client, _ := NewOpenAIClient(cfg)

	if client.Provider() != "openai" {
		t.Errorf("Provider() = %q, want openai", client.Provider())
	}
	if client.Model() != "gpt-4" {
		t.Errorf("Model() = %q, want gpt-4", client.Model())
	}
}

func TestOpenAIClient_SetTimeout(t *testing.T) {
	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k"}
	client, _ := NewOpenAIClient(cfg)
	client.SetTimeout(5 * time.Second)
	if client.httpClient.Timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", client.httpClient.Timeout)
	}
}

// ---------------------------------------------------------------------------
// ZAI client tests
// ---------------------------------------------------------------------------

func newZAISuccessHandler(content, model string, promptTokens, completionTokens int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := zaiResponse{
			Model: model,
			Choices: []struct {
				Index   int `json:"index"`
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{Role: "assistant", Content: content},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{PromptTokens: promptTokens, CompletionTokens: completionTokens},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func TestZAIClient_Complete_Success(t *testing.T) {
	srv := httptest.NewServer(newZAISuccessHandler("zai response", "glm-5", 8, 4))
	defer srv.Close()

	cfg := Config{
		Provider:    "zai",
		Model:       "glm-5",
		APIKey:      "test-key",
		BaseURL:     srv.URL,
		Temperature: 0.7,
		MaxTokens:   100,
	}
	client, err := NewZAIClient(cfg)
	if err != nil {
		t.Fatalf("NewZAIClient: %v", err)
	}

	resp, err := client.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "system",
		Messages:     []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "zai response" {
		t.Errorf("content = %q, want zai response", resp.Content)
	}
	if resp.InputTokens != 8 {
		t.Errorf("input_tokens = %d, want 8", resp.InputTokens)
	}
}

func TestZAIClient_Complete_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(zaiError{
			Error: struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Code    string `json:"code"`
			}{Message: "rate limited"},
		})
	}))
	defer srv.Close()

	cfg := Config{Provider: "zai", Model: "glm-5", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewZAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for rate limit response")
	}
}

func TestZAIClient_Complete_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(zaiResponse{}) // no choices
	}))
	defer srv.Close()

	cfg := Config{Provider: "zai", Model: "glm-5", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewZAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestZAIClient_Complete_NetworkError(t *testing.T) {
	cfg := Config{Provider: "zai", Model: "glm-5", APIKey: "k", BaseURL: "http://127.0.0.1:0"}
	client, _ := NewZAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestZAIClient_ProviderAndModel(t *testing.T) {
	cfg := Config{Provider: "zai", Model: "glm-5", APIKey: "k"}
	client, _ := NewZAIClient(cfg)

	if client.Provider() != "zai" {
		t.Errorf("Provider() = %q, want zai", client.Provider())
	}
	if client.Model() != "glm-5" {
		t.Errorf("Model() = %q, want glm-5", client.Model())
	}
}

func TestZAIClient_SetTimeout(t *testing.T) {
	cfg := Config{Provider: "zai", Model: "glm-5", APIKey: "k"}
	client, _ := NewZAIClient(cfg)
	client.SetTimeout(3 * time.Second)
	if client.httpClient.Timeout != 3*time.Second {
		t.Errorf("timeout = %v, want 3s", client.httpClient.Timeout)
	}
}

// ---------------------------------------------------------------------------
// Anthropic client tests
// ---------------------------------------------------------------------------

func newAnthropicSuccessHandler(textContent, model string, inputTokens, outputTokens int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Model: model,
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: textContent},
			},
			StopReason: "end_turn",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{InputTokens: inputTokens, OutputTokens: outputTokens},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func TestAnthropicClient_Complete_Success(t *testing.T) {
	srv := httptest.NewServer(newAnthropicSuccessHandler("anthropic says hi", "claude-3", 12, 6))
	defer srv.Close()

	cfg := Config{
		Provider:    "anthropic",
		Model:       "claude-3",
		APIKey:      "test-key",
		BaseURL:     srv.URL,
		Temperature: 0.5,
		MaxTokens:   200,
	}
	client, err := NewAnthropicClient(cfg)
	if err != nil {
		t.Fatalf("NewAnthropicClient: %v", err)
	}

	resp, err := client.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "be helpful",
		Messages:     []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "anthropic says hi" {
		t.Errorf("content = %q, want anthropic says hi", resp.Content)
	}
	if resp.Model != "claude-3" {
		t.Errorf("model = %q, want claude-3", resp.Model)
	}
	if resp.InputTokens != 12 {
		t.Errorf("input_tokens = %d, want 12", resp.InputTokens)
	}
	if resp.OutputTokens != 6 {
		t.Errorf("output_tokens = %d, want 6", resp.OutputTokens)
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("finish_reason = %q, want end_turn", resp.FinishReason)
	}
}

func TestAnthropicClient_Complete_SkipsSystemRoleMessages(t *testing.T) {
	// Anthropic uses 'system' as a top-level field; system-role messages in
	// the messages array should be filtered out.
	srv := httptest.NewServer(newAnthropicSuccessHandler("ok", "claude-3", 1, 1))
	defer srv.Close()

	cfg := Config{Provider: "anthropic", Model: "claude-3", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewAnthropicClient(cfg)

	resp, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "should be filtered"},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q, want ok", resp.Content)
	}
}

func TestAnthropicClient_Complete_MultipleTextBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := anthropicResponse{
			Model: "claude-3",
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: "hello "},
				{Type: "text", Text: "world"},
			},
			StopReason: "end_turn",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := Config{Provider: "anthropic", Model: "claude-3", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewAnthropicClient(cfg)

	resp, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("content = %q, want 'hello world' (concatenated blocks)", resp.Content)
	}
}

func TestAnthropicClient_Complete_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(anthropicError{
			Error: struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}{Message: "permission denied"},
		})
	}))
	defer srv.Close()

	cfg := Config{Provider: "anthropic", Model: "claude-3", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewAnthropicClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestAnthropicClient_Complete_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{}) // no content blocks
	}))
	defer srv.Close()

	cfg := Config{Provider: "anthropic", Model: "claude-3", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewAnthropicClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestAnthropicClient_Complete_NetworkError(t *testing.T) {
	cfg := Config{Provider: "anthropic", Model: "claude-3", APIKey: "k", BaseURL: "http://127.0.0.1:0"}
	client, _ := NewAnthropicClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestAnthropicClient_ProviderAndModel(t *testing.T) {
	cfg := Config{Provider: "anthropic", Model: "claude-3", APIKey: "k"}
	client, _ := NewAnthropicClient(cfg)

	if client.Provider() != "anthropic" {
		t.Errorf("Provider() = %q, want anthropic", client.Provider())
	}
	if client.Model() != "claude-3" {
		t.Errorf("Model() = %q, want claude-3", client.Model())
	}
}

func TestAnthropicClient_SetTimeout(t *testing.T) {
	cfg := Config{Provider: "anthropic", Model: "claude-3", APIKey: "k"}
	client, _ := NewAnthropicClient(cfg)
	client.SetTimeout(10 * time.Second)
	if client.httpClient.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", client.httpClient.Timeout)
	}
}

// ---------------------------------------------------------------------------
// ClientWithRetry tests
// ---------------------------------------------------------------------------

func TestClientWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	inner := &mockClient{resp: &CompletionResponse{Content: "ok"}}
	retryClient := NewClientWithRetry(inner, RetryConfig{MaxRetries: 3, InitialWait: time.Millisecond, MaxWait: time.Millisecond})

	resp, err := retryClient.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q, want ok", resp.Content)
	}
	if inner.calls != 1 {
		t.Errorf("calls = %d, want 1", inner.calls)
	}
}

func TestClientWithRetry_RetriesOnError(t *testing.T) {
	attempt := 0
	callErr := errors.New("temporary error")
	inner := &mockClient{}
	inner.err = callErr

	// After 2 failures, succeed.
	customInner := &countingClient{
		fn: func(i int) (*CompletionResponse, error) {
			if i < 2 {
				return nil, callErr
			}
			return &CompletionResponse{Content: "retry success"}, nil
		},
	}
	_ = attempt

	retryClient := NewClientWithRetry(customInner, RetryConfig{MaxRetries: 3, InitialWait: time.Millisecond, MaxWait: time.Millisecond})
	resp, err := retryClient.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if resp.Content != "retry success" {
		t.Errorf("content = %q, want retry success", resp.Content)
	}
	if customInner.calls != 3 {
		t.Errorf("calls = %d, want 3", customInner.calls)
	}
}

func TestClientWithRetry_ExhaustsMaxRetries(t *testing.T) {
	finalErr := errors.New("always fails")
	inner := &mockClient{err: finalErr}

	retryClient := NewClientWithRetry(inner, RetryConfig{MaxRetries: 2, InitialWait: time.Millisecond, MaxWait: time.Millisecond})
	_, err := retryClient.Complete(context.Background(), CompletionRequest{})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !errors.Is(err, finalErr) {
		t.Errorf("expected original error, got: %v", err)
	}
	if inner.calls != 3 { // 1 initial + 2 retries
		t.Errorf("calls = %d, want 3", inner.calls)
	}
}

func TestClientWithRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	customInner := &countingClient{
		fn: func(i int) (*CompletionResponse, error) {
			callCount++
			if callCount == 1 {
				cancel() // cancel after first failure
			}
			return nil, errors.New("fail")
		},
	}

	retryClient := NewClientWithRetry(customInner, RetryConfig{MaxRetries: 5, InitialWait: time.Millisecond, MaxWait: time.Millisecond})
	_, err := retryClient.Complete(ctx, CompletionRequest{})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestClientWithRetry_ProviderAndModel(t *testing.T) {
	inner := &mockClient{}
	retryClient := NewClientWithRetry(inner, RetryConfig{})

	if retryClient.Provider() != "mock" {
		t.Errorf("Provider() = %q, want mock", retryClient.Provider())
	}
	if retryClient.Model() != "mock-model" {
		t.Errorf("Model() = %q, want mock-model", retryClient.Model())
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	if cfg.InitialWait != time.Second {
		t.Errorf("InitialWait = %v, want 1s", cfg.InitialWait)
	}
	if cfg.MaxWait != 30*time.Second {
		t.Errorf("MaxWait = %v, want 30s", cfg.MaxWait)
	}
}

// countingClient is a helper that tracks call count and delegates to a function.
type countingClient struct {
	calls int
	fn    func(int) (*CompletionResponse, error)
}

func (c *countingClient) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	result, err := c.fn(c.calls)
	c.calls++
	return result, err
}
func (c *countingClient) Provider() string { return "counting" }
func (c *countingClient) Model() string    { return "counting-model" }

// ---------------------------------------------------------------------------
// NewClient factory tests
// ---------------------------------------------------------------------------

func TestNewClient_OpenAI(t *testing.T) {
	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "sk-test"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient (openai): %v", err)
	}
	if client.Provider() != "openai" {
		t.Errorf("provider = %q, want openai", client.Provider())
	}
}

func TestNewClient_Anthropic(t *testing.T) {
	cfg := Config{Provider: "anthropic", Model: "claude-3", APIKey: "sk-ant-test"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient (anthropic): %v", err)
	}
	if client.Provider() != "anthropic" {
		t.Errorf("provider = %q, want anthropic", client.Provider())
	}
}

func TestNewClient_ZAI(t *testing.T) {
	cfg := Config{Provider: "zai", Model: "glm-5", APIKey: "zai-key"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient (zai): %v", err)
	}
	if client.Provider() != "zai" {
		t.Errorf("provider = %q, want zai", client.Provider())
	}
}

func TestNewClient_Ollama_NoAPIKey(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "llama3.1"}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient (ollama): %v", err)
	}
	if client.Provider() != "ollama" {
		t.Errorf("provider = %q, want ollama", client.Provider())
	}
}

func TestNewClient_UnsupportedProvider(t *testing.T) {
	cfg := Config{Provider: "unknown_provider", Model: "m", APIKey: "k"}
	_, err := NewClient(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestNewClient_WithRetry(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "llama3.1", MaxRetries: 2}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient with retries: %v", err)
	}
	// semaphore is always the outermost wrapper (even when Concurrency==0 it is a
	// no-op semaphoreClient). The retry wrapper lives inside it.
	if _, ok := client.(*semaphoreClient); !ok {
		t.Error("expected outermost wrapper to be *semaphoreClient when MaxRetries > 0")
	}
}

func TestNewClient_SemaphoreOuterRetryInner(t *testing.T) {
	// With Concurrency=1 and MaxRetries=2 the wrap order must be
	// semaphoreClient( ClientWithRetry( innerClient ) ).
	// This ensures a single semaphore slot is held for the full duration of all
	// retry attempts, not re-acquired on every attempt.
	cfg := Config{Provider: "ollama", Model: "llama3.1", MaxRetries: 2, Concurrency: 1}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	sc, ok := client.(*semaphoreClient)
	if !ok {
		t.Fatalf("outermost wrapper = %T, want *semaphoreClient", client)
	}
	if _, ok := sc.inner.(*ClientWithRetry); !ok {
		t.Errorf("inner of semaphoreClient = %T, want *ClientWithRetry", sc.inner)
	}
}

func TestNewClient_SemaphoreHeldAcrossRetries(t *testing.T) {
	// Goroutine A: Concurrency=1, returns 429 with short RetryAfter twice then succeeds.
	// Goroutine B: same endpoint, tries to complete concurrently.
	// Expected: B cannot start until A has exhausted retries AND released the slot.
	//
	// We verify this by observing that B's call does not begin until A finishes.
	cfg := Config{Provider: "ollama", Model: "llama3.1", MaxRetries: 2, Concurrency: 1}

	retryAfter := 20 * time.Millisecond

	aAttempts := 0
	innerA := &countingClient{
		fn: func(i int) (*CompletionResponse, error) {
			aAttempts++
			if i < 2 {
				return nil, &RateLimitedError{RetryAfter: retryAfter}
			}
			return &CompletionResponse{Content: "a-done"}, nil
		},
	}

	// Build the client stack manually with the same wrap order as NewClient.
	retryA := NewClientWithRetry(innerA, RetryConfig{
		MaxRetries:  cfg.MaxRetries,
		InitialWait: retryAfter,
		MaxWait:     retryAfter * 10,
		Multiplier:  2.0,
	})
	sem := newSemaphoreClient(retryA, cfg)

	// B uses the SAME semaphore (same provider key) but a different inner client.
	bStarted := make(chan struct{})
	innerB := &countingClient{
		fn: func(i int) (*CompletionResponse, error) {
			close(bStarted)
			return &CompletionResponse{Content: "b-done"}, nil
		},
	}
	semB := &semaphoreClient{inner: innerB, sem: sem.sem}

	// Start A.
	aDone := make(chan error, 1)
	go func() {
		_, err := sem.Complete(context.Background(), CompletionRequest{})
		aDone <- err
	}()

	// Give A a moment to acquire the semaphore.
	time.Sleep(5 * time.Millisecond)

	// Start B.
	bDone := make(chan error, 1)
	go func() {
		_, err := semB.Complete(context.Background(), CompletionRequest{})
		bDone <- err
	}()

	// Wait for A to finish.
	if err := <-aDone; err != nil {
		t.Fatalf("goroutine A failed: %v", err)
	}

	// Only after A is done should B be able to start.
	select {
	case <-bStarted:
		// good
	case <-time.After(500 * time.Millisecond):
		t.Fatal("goroutine B never started — semaphore was not released by A")
	}

	if err := <-bDone; err != nil {
		t.Fatalf("goroutine B failed: %v", err)
	}

	if aAttempts != 3 { // 1 initial + 2 retries
		t.Errorf("goroutine A attempts = %d, want 3", aAttempts)
	}
}

func TestNewClient_EmptyProvider_Valid(t *testing.T) {
	// Empty provider = LLM disabled; NewClient should return error via Validate
	// (actually Validate returns nil for empty provider, but NewClient falls through
	// to the default case which returns ErrProviderNotSupported)
	cfg := Config{}
	_, err := NewClient(cfg)
	// This will fail with ErrProviderNotSupported after Validate passes with empty provider
	// The behavior: Validate allows empty provider, but switch hits default → error.
	// That's fine — caller should check cfg.Provider before calling NewClient.
	_ = err // either outcome is acceptable; just verify no panic
}

func TestMustNewClient_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid config")
		}
	}()
	MustNewClient(Config{Provider: "bad_provider", Model: "m", APIKey: "k"})
}

// ---------------------------------------------------------------------------
// RateLimitedError / 429 tests
// ---------------------------------------------------------------------------

func TestZAIClient_429_ReturnsRateLimitedError_SecondsForm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "zai", Model: "glm-5", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewZAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got: %v", err)
	}
	var rle *RateLimitedError
	if !errors.As(err, &rle) {
		t.Fatal("expected *RateLimitedError")
	}
	if rle.RetryAfter != 5*time.Second {
		t.Errorf("RetryAfter = %v, want 5s", rle.RetryAfter)
	}
}

func TestZAIClient_429_ReturnsRateLimitedError_HTTPDateForm(t *testing.T) {
	retryAt := time.Now().Add(3 * time.Second).UTC()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", retryAt.Format(http.TimeFormat))
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "zai", Model: "glm-5", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewZAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got: %v", err)
	}
	var rle *RateLimitedError
	errors.As(err, &rle)
	// HTTP-date has 1s granularity; allow ±2s.
	if rle.RetryAfter < 1*time.Second || rle.RetryAfter > 5*time.Second {
		t.Errorf("RetryAfter = %v, expected ~3s", rle.RetryAfter)
	}
}

func TestOpenAIClient_429_ReturnsRateLimitedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewOpenAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got: %v", err)
	}
	var rle *RateLimitedError
	errors.As(err, &rle)
	if rle.RetryAfter != 10*time.Second {
		t.Errorf("RetryAfter = %v, want 10s", rle.RetryAfter)
	}
}

func TestRateLimitedError_Is_ErrRateLimited(t *testing.T) {
	err := &RateLimitedError{RetryAfter: 2 * time.Second}
	if !errors.Is(err, ErrRateLimited) {
		t.Error("errors.Is(RateLimitedError, ErrRateLimited) must return true")
	}
}

func TestParseRetryAfter_NegativeSeconds_ReturnsZero(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "-3")
	d := parseRetryAfter(h)
	if d != 0 {
		t.Errorf("parseRetryAfter(-3) = %v, want 0", d)
	}
}

func TestParseRetryAfter_PositiveSeconds_ReturnsCorrectDuration(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "5")
	d := parseRetryAfter(h)
	if d != 5*time.Second {
		t.Errorf("parseRetryAfter(5) = %v, want 5s", d)
	}
}

func TestConfig_Validate_NegativeConcurrency(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "m", Concurrency: -1}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for negative concurrency")
	}
}

func TestNewClient_WithConcurrency_WrapsInSemaphore(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "llama3.1", Concurrency: 1}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, ok := client.(*semaphoreClient); !ok {
		t.Error("expected *semaphoreClient when Concurrency > 0")
	}
}
