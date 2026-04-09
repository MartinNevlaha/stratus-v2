package guardian

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newChatSuccessServer(content string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []struct {
				Message chatMessage `json:"message"`
			}{
				{Message: chatMessage{Role: "assistant", Content: content}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestNewLLMClient_Configured(t *testing.T) {
	c := NewLLMClient("http://localhost:8080/v1", "key", "model", 0.7, 1024)
	if !c.Configured() {
		t.Error("expected Configured() = true when endpoint and model are set")
	}
}

func TestLLMClient_Configured_EmptyEndpoint(t *testing.T) {
	c := newLLMClient("", "key", "model", 0.7, 1024)
	if c.Configured() {
		t.Error("expected Configured() = false when endpoint is empty")
	}
}

func TestLLMClient_Configured_EmptyModel(t *testing.T) {
	c := newLLMClient("http://localhost:8080/v1", "key", "", 0.7, 1024)
	if c.Configured() {
		t.Error("expected Configured() = false when model is empty")
	}
}

func TestLLMClient_Complete_Success(t *testing.T) {
	srv := newChatSuccessServer("hello from llm")
	defer srv.Close()

	c := newLLMClient(srv.URL, "test-key", "test-model", 0.5, 512)
	result, err := c.Complete(context.Background(), "system", "user input")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello from llm" {
		t.Errorf("result = %q, want hello from llm", result)
	}
}

func TestLLMClient_Complete_NoSystemPrompt(t *testing.T) {
	srv := newChatSuccessServer("response")
	defer srv.Close()

	c := newLLMClient(srv.URL, "", "test-model", 0.5, 0)
	result, err := c.Complete(context.Background(), "", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "response" {
		t.Errorf("result = %q, want response", result)
	}
}

func TestLLMClient_Complete_URLTrailingSlash(t *testing.T) {
	// Endpoint with trailing slash should still work correctly.
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		resp := chatResponse{Choices: []struct {
			Message chatMessage `json:"message"`
		}{{Message: chatMessage{Role: "assistant", Content: "ok"}}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newLLMClient(srv.URL+"/", "", "m", 0.5, 0)
	_, err := c.Complete(context.Background(), "", "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
}

func TestLLMClient_Complete_NotConfigured(t *testing.T) {
	c := newLLMClient("", "", "model", 0.5, 512)
	_, err := c.Complete(context.Background(), "", "hello")
	if err == nil {
		t.Fatal("expected error when not configured")
	}
}

func TestLLMClient_Complete_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errMsg := "invalid api key"
		resp := chatResponse{Error: &struct {
			Message string `json:"message"`
		}{Message: errMsg}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newLLMClient(srv.URL, "bad-key", "model", 0.5, 0)
	_, err := c.Complete(context.Background(), "", "hello")
	if err == nil {
		t.Fatal("expected error from API error response")
	}
}

func TestLLMClient_Complete_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(chatResponse{}) // no choices, no error
	}))
	defer srv.Close()

	c := newLLMClient(srv.URL, "", "model", 0.5, 0)
	_, err := c.Complete(context.Background(), "", "hello")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestLLMClient_Complete_NetworkError(t *testing.T) {
	c := newLLMClient("http://127.0.0.1:0", "", "model", 0.5, 0)
	_, err := c.Complete(context.Background(), "", "hello")
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestLLMClient_Complete_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json at all"))
	}))
	defer srv.Close()

	c := newLLMClient(srv.URL, "", "model", 0.5, 0)
	_, err := c.Complete(context.Background(), "", "hello")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestLLMClient_TestConnection_Success(t *testing.T) {
	srv := newChatSuccessServer("ok")
	defer srv.Close()

	c := newLLMClient(srv.URL, "", "model", 0.5, 0)
	if err := c.TestConnection(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLLMClient_TestConnection_Failure(t *testing.T) {
	c := newLLMClient("http://127.0.0.1:0", "", "model", 0.5, 0)
	if err := c.TestConnection(context.Background()); err == nil {
		t.Fatal("expected error for unreachable endpoint")
	}
}

func TestTestLLMEndpoint_Success(t *testing.T) {
	srv := newChatSuccessServer("ok")
	defer srv.Close()

	err := TestLLMEndpoint(context.Background(), srv.URL, "", "model", 0.5, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTestLLMEndpoint_Failure(t *testing.T) {
	err := TestLLMEndpoint(context.Background(), "http://127.0.0.1:0", "", "model", 0.5, 0)
	if err == nil {
		t.Fatal("expected error for unreachable endpoint")
	}
}
