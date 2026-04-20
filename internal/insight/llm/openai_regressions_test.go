package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// OpenAI provider regression tests
//
// These cases guard against transport-level edge cases observed in the wild
// with OpenAI-compatible servers (local proxies, 3rd-party providers, etc.).
// ---------------------------------------------------------------------------

// Regression: some OpenAI-compatible servers return the `error` field as a
// bare string instead of an object. Previously the old openAIError struct
// would silently produce an empty message, discarding the real error text.
func TestOpenAIClient_Complete_StringShapedErrorField_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"bad gateway from proxy"}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL}
	client, err := NewOpenAIClient(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}

	_, err = client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 502 string-shaped error response")
	}
	if !strings.Contains(err.Error(), "bad gateway from proxy") {
		t.Errorf("error = %q, want to contain 'bad gateway from proxy'", err.Error())
	}
}

// Regression: string-shaped error on a 200 OK response (some providers embed
// errors in the body without changing the HTTP status code).
func TestOpenAIClient_Complete_StringShapedErrorField_200OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// 200 OK but body is not a valid completion response — invalid JSON
		// that happens to have an "error" string field would fail parse.
		// Instead test the valid-JSON-but-no-choices path that a provider might
		// return when returning a string error with status 200.
		w.Write([]byte(`{"error":"context length exceeded","choices":[]}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewOpenAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	// Empty choices should still return an error regardless of string error field.
	if err == nil {
		t.Fatal("expected error for empty choices in 200 response")
	}
}

// Regression: trailing slash on the BaseURL must not produce a double-slash
// path like //chat/completions. The client must normalise it.
func TestOpenAIClient_Complete_TrailingSlashBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"model":"gpt-4"}`))
	}))
	defer srv.Close()

	// Pass BaseURL with a trailing slash.
	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL + "/"}
	client, err := NewOpenAIClient(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}

	_, err = client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions (double-slash regression)", gotPath)
	}
}

// Regression: non-2xx response with a body that is not valid JSON should still
// return a meaningful error (falling back to the raw body snippet).
func TestOpenAIClient_Complete_BadGateway_NonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL}
	client, _ := NewOpenAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 502 non-JSON response")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error = %q, want to contain status code 502", err.Error())
	}
}

// Regression: non-2xx response with an object-shaped error (standard OpenAI
// format) must surface the message, not an empty string.
func TestOpenAIClient_Complete_ObjectShapedErrorField_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key","type":"auth_error","code":"401"}}`))
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "bad-key", BaseURL: srv.URL}
	client, _ := NewOpenAIClient(cfg)

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error = %q, want to contain 'invalid api key'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// ResponseFormat field tests — regression guards for the OpenAI client's
// response_format wire format. Ensures "json" maps to json_object, empty
// omits the key, and invalid values are silently dropped.
// ---------------------------------------------------------------------------

// TestOpenAIClient_Complete_ResponseFormatJSON_IncludesInBody verifies that
// when ResponseFormat is "json" the request body sent to the server contains
// response_format: {"type":"json_object"}.
func TestOpenAIClient_Complete_ResponseFormatJSON_IncludesInBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openAIResponse{
			Model: "gpt-4",
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
					}{Role: "assistant", Content: "{}"},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL}
	client, err := NewOpenAIClient(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}

	_, err = client.Complete(context.Background(), CompletionRequest{
		Messages:       []Message{{Role: "user", Content: "hi"}},
		MaxTokens:      16,
		ResponseFormat: "json",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	rfRaw, ok := gotBody["response_format"]
	if !ok {
		t.Fatal("request body missing response_format key")
	}
	rf, ok := rfRaw.(map[string]any)
	if !ok {
		t.Fatalf("response_format = %T, want map[string]any", rfRaw)
	}
	if rf["type"] != "json_object" {
		t.Errorf("response_format.type = %q, want json_object", rf["type"])
	}
}

// TestOpenAIClient_Complete_ResponseFormatEmpty_OmitsFromBody is a regression
// guard: callers that do not set ResponseFormat must not see the key injected
// into the request body (preserves existing behaviour for wiki, product_intelligence, etc.).
func TestOpenAIClient_Complete_ResponseFormatEmpty_OmitsFromBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openAIResponse{
			Model: "gpt-4",
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
					}{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL}
	client, err := NewOpenAIClient(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}

	_, err = client.Complete(context.Background(), CompletionRequest{
		Messages:       []Message{{Role: "user", Content: "hi"}},
		ResponseFormat: "",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if _, ok := gotBody["response_format"]; ok {
		t.Error("request body must not contain response_format when ResponseFormat is empty")
	}
}

// TestOpenAIClient_Complete_ResponseFormatInvalid_OmitsFromBody verifies that
// an unrecognised ResponseFormat value (e.g. "yaml") is silently dropped: the
// key must not appear in the request body and Complete must not return an error.
func TestOpenAIClient_Complete_ResponseFormatInvalid_OmitsFromBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(openAIResponse{
			Model: "gpt-4",
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
					}{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer srv.Close()

	cfg := Config{Provider: "openai", Model: "gpt-4", APIKey: "k", BaseURL: srv.URL}
	client, err := NewOpenAIClient(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}

	_, err = client.Complete(context.Background(), CompletionRequest{
		Messages:       []Message{{Role: "user", Content: "hi"}},
		ResponseFormat: "yaml",
	})
	if err != nil {
		t.Fatalf("Complete must not return error for unrecognised ResponseFormat: %v", err)
	}

	if _, ok := gotBody["response_format"]; ok {
		t.Error("request body must not contain response_format for unrecognised value")
	}
}
