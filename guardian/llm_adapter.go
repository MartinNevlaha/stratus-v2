package guardian

import (
	"context"
	"fmt"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

// llmAdapter wraps an llm.Client to match the guardian's
// (systemPrompt, userPrompt) -> (string, error) calling convention.
type llmAdapter struct {
	client llm.Client
}

func newLLMAdapter(client llm.Client) *llmAdapter {
	return &llmAdapter{client: client}
}

func (a *llmAdapter) configured() bool {
	return a.client != nil
}

// Configured returns whether the underlying client is set.
func (a *llmAdapter) Configured() bool {
	return a.configured()
}

// Complete translates the (systemPrompt, userPrompt) calling convention used
// by guardian checks into the canonical llm.CompletionRequest.
func (a *llmAdapter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if !a.configured() {
		return "", fmt.Errorf("guardian llm adapter: not configured")
	}
	resp, err := a.client.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		Messages:     []llm.Message{{Role: "user", Content: userPrompt}},
	})
	if err != nil {
		return "", fmt.Errorf("guardian llm adapter: %w", err)
	}
	return resp.Content, nil
}

// TestConnection sends a minimal prompt to verify the underlying client works.
func (a *llmAdapter) TestConnection(ctx context.Context) error {
	_, err := a.Complete(ctx, "", "Reply with the single word: ok")
	if err != nil {
		return fmt.Errorf("guardian llm adapter: test connection: %w", err)
	}
	return nil
}
