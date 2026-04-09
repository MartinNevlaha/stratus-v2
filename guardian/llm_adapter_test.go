package guardian

import (
	"context"
	"errors"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type mockLLMClient struct {
	response *llm.CompletionResponse
	err      error
	lastReq  llm.CompletionRequest
}

func (m *mockLLMClient) Complete(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.lastReq = req
	return m.response, m.err
}
func (m *mockLLMClient) Provider() string { return "mock" }
func (m *mockLLMClient) Model() string    { return "mock-model" }

func TestLLMAdapter_Complete(t *testing.T) {
	mock := &mockLLMClient{
		response: &llm.CompletionResponse{Content: "hello"},
	}
	adapter := newLLMAdapter(mock)

	result, err := adapter.Complete(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Errorf("got %q, want %q", result, "hello")
	}
	if mock.lastReq.SystemPrompt != "system prompt" {
		t.Error("system prompt not forwarded")
	}
	if len(mock.lastReq.Messages) != 1 || mock.lastReq.Messages[0].Content != "user prompt" {
		t.Error("user prompt not forwarded")
	}
}

func TestLLMAdapter_Error(t *testing.T) {
	underlying := errors.New("connection refused")
	mock := &mockLLMClient{err: underlying}
	adapter := newLLMAdapter(mock)

	_, err := adapter.Complete(context.Background(), "", "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, underlying) {
		t.Errorf("error not wrapped correctly: %v", err)
	}
}

func TestLLMAdapter_NotConfigured(t *testing.T) {
	adapter := newLLMAdapter(nil)
	_, err := adapter.Complete(context.Background(), "", "test")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestLLMAdapter_Configured(t *testing.T) {
	adapter := newLLMAdapter(&mockLLMClient{})
	if !adapter.Configured() {
		t.Error("expected Configured() = true")
	}

	nilAdapter := newLLMAdapter(nil)
	if nilAdapter.Configured() {
		t.Error("expected Configured() = false for nil")
	}
}

func TestLLMAdapter_TestConnection_Success(t *testing.T) {
	mock := &mockLLMClient{
		response: &llm.CompletionResponse{Content: "ok"},
	}
	adapter := newLLMAdapter(mock)
	if err := adapter.TestConnection(context.Background()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestLLMAdapter_TestConnection_Error(t *testing.T) {
	mock := &mockLLMClient{err: errors.New("unreachable")}
	adapter := newLLMAdapter(mock)
	err := adapter.TestConnection(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, mock.err) {
		t.Errorf("error not wrapped correctly: %v", err)
	}
}
