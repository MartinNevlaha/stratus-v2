package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Ollama native /api/chat client tests
// ---------------------------------------------------------------------------

func newOllamaSuccessHandler(content, model string, promptTokens, evalTokens int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaResponse{
			Model: model,
			Done:  true,
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{Role: "assistant", Content: content},
			DoneReason:      "stop",
			PromptEvalCount: promptTokens,
			EvalCount:       evalTokens,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func TestOllamaClient_Complete_Success(t *testing.T) {
	var gotPath string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		newOllamaSuccessHandler("hello gemma", "gemma4:e4b", 12, 6)(w, r)
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: srv.URL, Temperature: 0.2, MaxTokens: 256}
	client, err := NewOllamaClient(cfg)
	if err != nil {
		t.Fatalf("NewOllamaClient: %v", err)
	}

	resp, err := client.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "be terse",
		Messages:     []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotPath != "/api/chat" {
		t.Errorf("path = %q, want /api/chat", gotPath)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty (ollama requires no auth)", gotAuth)
	}
	if resp.Content != "hello gemma" {
		t.Errorf("content = %q, want hello gemma", resp.Content)
	}
	if resp.Model != "gemma4:e4b" {
		t.Errorf("model = %q, want gemma4:e4b", resp.Model)
	}
	if resp.InputTokens != 12 {
		t.Errorf("input_tokens = %d, want 12", resp.InputTokens)
	}
	if resp.OutputTokens != 6 {
		t.Errorf("output_tokens = %d, want 6", resp.OutputTokens)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", resp.FinishReason)
	}
}

func TestOllamaClient_Complete_ResponseFormatJSON_IncludesFormatInBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		newOllamaSuccessHandler(`{"ok":true}`, "gemma4:e4b", 1, 1)(w, r)
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: srv.URL}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages:       []Message{{Role: "user", Content: "give json"}},
		ResponseFormat: "json",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotBody["format"] != "json" {
		t.Errorf("request body format = %v, want \"json\"", gotBody["format"])
	}
	if gotBody["stream"] != false {
		t.Errorf("request body stream = %v, want false", gotBody["stream"])
	}
}

func TestOllamaClient_Complete_ResponseFormatJSON_DisablesThinking(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		newOllamaSuccessHandler(`{}`, "gemma4:e4b", 1, 1)(w, r)
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: srv.URL}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages:       []Message{{Role: "user", Content: "give json"}},
		ResponseFormat: "json",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	think, ok := gotBody["think"]
	if !ok {
		t.Fatal("request body must contain think:false when ResponseFormat is json (avoid thinking tokens truncating JSON)")
	}
	if think != false {
		t.Errorf("think = %v, want false", think)
	}
}

func TestOllamaClient_Complete_ResponseFormatEmpty_OmitsThinkAndFormat(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		newOllamaSuccessHandler("ok", "gemma4:e4b", 1, 1)(w, r)
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: srv.URL}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, ok := gotBody["think"]; ok {
		t.Errorf("request body must not contain think when ResponseFormat is empty; got %v", gotBody["think"])
	}
}

func TestOllamaClient_Complete_ResponseFormatEmpty_OmitsFormat(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		newOllamaSuccessHandler("free text", "gemma4:e4b", 1, 1)(w, r)
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: srv.URL}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "say hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, ok := gotBody["format"]; ok {
		t.Errorf("request body must not contain format when ResponseFormat is empty; got %v", gotBody["format"])
	}
}

func TestOllamaClient_Complete_OptionsMapped(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		newOllamaSuccessHandler("ok", "gemma4:e4b", 1, 1)(w, r)
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: srv.URL, Temperature: 0.9, MaxTokens: 2048}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages:    []Message{{Role: "user", Content: "hi"}},
		Temperature: 0.3,
		MaxTokens:   128,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	opts, ok := gotBody["options"].(map[string]any)
	if !ok {
		t.Fatalf("options field = %T, want map[string]any", gotBody["options"])
	}
	if opts["temperature"] != 0.3 {
		t.Errorf("temperature = %v, want 0.3 (request override of config)", opts["temperature"])
	}
	if opts["num_predict"] != float64(128) {
		t.Errorf("num_predict = %v, want 128 (request override of config)", opts["num_predict"])
	}
}

func TestOllamaClient_Complete_APIError_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"model 'llama99' not found"}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "llama99", BaseURL: srv.URL}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %q, want to contain status 404", err.Error())
	}
}

func TestOllamaClient_Complete_StringErrorIn200Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":"context too large"}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: srv.URL}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when body contains error field")
	}
	if !strings.Contains(err.Error(), "context too large") {
		t.Errorf("error = %q, want to contain 'context too large'", err.Error())
	}
}

func TestOllamaClient_Complete_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gemma4:e4b","done":true,"message":{"role":"assistant","content":""}}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: srv.URL}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestOllamaClient_Complete_NetworkError(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: "http://127.0.0.1:0"}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestOllamaClient_Complete_429_ReturnsRateLimitedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "ollama", Model: "gemma4:e4b", BaseURL: srv.URL}
	client, _ := NewOllamaClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
	var rle *RateLimitedError
	if errors.As(err, &rle) {
		if rle.RetryAfter != 7*time.Second {
			t.Errorf("RetryAfter = %v, want 7s", rle.RetryAfter)
		}
	}
}

func TestOllamaClient_ProviderAndModel(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "gemma4:e4b"}
	client, _ := NewOllamaClient(cfg)
	if client.Provider() != "ollama" {
		t.Errorf("Provider() = %q, want ollama", client.Provider())
	}
	if client.Model() != "gemma4:e4b" {
		t.Errorf("Model() = %q, want gemma4:e4b", client.Model())
	}
}

func TestOllamaChatURL_StripsV1Suffix(t *testing.T) {
	cases := map[string]string{
		"http://localhost:11434/v1":  "http://localhost:11434/api/chat",
		"http://localhost:11434/v1/": "http://localhost:11434/api/chat",
		"http://localhost:11434":     "http://localhost:11434/api/chat",
		"http://localhost:11434/":    "http://localhost:11434/api/chat",
	}
	for in, want := range cases {
		if got := ollamaChatURL(in); got != want {
			t.Errorf("ollamaChatURL(%q) = %q, want %q", in, got, want)
		}
	}
}
