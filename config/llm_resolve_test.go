package config

import "testing"

func TestResolveLLMConfig_OverrideWins(t *testing.T) {
	top := LLMConfig{Provider: "ollama", Model: "llama3.1", Temperature: 0.7, Timeout: 120}
	sub := LLMConfig{Provider: "openai", Model: "gpt-4", APIKey: "sk-123"}
	result := ResolveLLMConfig(top, sub)
	if result.Provider != "openai" {
		t.Errorf("provider = %s, want openai", result.Provider)
	}
	if result.Model != "gpt-4" {
		t.Errorf("model = %s, want gpt-4", result.Model)
	}
	if result.Timeout != 120 {
		t.Errorf("timeout = %d, want 120 (inherited)", result.Timeout)
	}
}

func TestResolveLLMConfig_TopLevelFallback(t *testing.T) {
	top := LLMConfig{Provider: "ollama", Model: "llama3.1", Temperature: 0.7, BaseURL: "http://localhost:11434/v1"}
	sub := LLMConfig{Temperature: 0.3} // only override temperature
	result := ResolveLLMConfig(top, sub)
	if result.Provider != "ollama" {
		t.Errorf("provider = %s, want ollama", result.Provider)
	}
	if result.Temperature != 0.3 {
		t.Errorf("temperature = %f, want 0.3", result.Temperature)
	}
}

func TestResolveLLMConfig_EmptyOverride(t *testing.T) {
	top := LLMConfig{Provider: "ollama", Model: "llama3.1"}
	result := ResolveLLMConfig(top, LLMConfig{})
	if result.Provider != "ollama" {
		t.Errorf("provider = %s, want ollama", result.Provider)
	}
}

func TestResolveGuardianLLMConfig(t *testing.T) {
	top := LLMConfig{Provider: "ollama", Model: "llama3.1", APIKey: "key1"}
	g := GuardianConfig{LLMEndpoint: "http://custom:8080/v1", LLMModel: "mistral"}
	result := ResolveGuardianLLMConfig(top, g)
	if result.Provider != "openai" {
		t.Errorf("provider = %s, want openai", result.Provider)
	}
	if result.Model != "mistral" {
		t.Errorf("model = %s, want mistral", result.Model)
	}
	if result.BaseURL != "http://custom:8080/v1" {
		t.Errorf("base_url = %s, want http://custom:8080/v1", result.BaseURL)
	}
	if result.APIKey != "key1" {
		t.Errorf("api_key = %s, want key1 (should inherit from top-level)", result.APIKey)
	}
}

func TestResolveGuardianLLMConfig_EmptyGuardian(t *testing.T) {
	top := LLMConfig{Provider: "ollama", Model: "llama3.1"}
	result := ResolveGuardianLLMConfig(top, GuardianConfig{})
	if result.Provider != "ollama" {
		t.Errorf("provider = %s, should fall through to top-level", result.Provider)
	}
}

func TestResolveLLMConfig_PartialOverride(t *testing.T) {
	top := LLMConfig{Provider: "ollama", Model: "llama3.1", Temperature: 0.7, MaxTokens: 4096, Timeout: 120}
	sub := LLMConfig{Temperature: 1.2} // only temperature override
	result := ResolveLLMConfig(top, sub)
	if result.Temperature != 1.2 {
		t.Errorf("temperature = %f, want 1.2 (override wins)", result.Temperature)
	}
	if result.Provider != "ollama" {
		t.Errorf("provider = %s, want ollama (inherited from top-level)", result.Provider)
	}
	if result.Model != "llama3.1" {
		t.Errorf("model = %s, want llama3.1 (inherited from top-level)", result.Model)
	}
	if result.MaxTokens != 4096 {
		t.Errorf("max_tokens = %d, want 4096 (inherited from top-level)", result.MaxTokens)
	}
}

func TestResolveLLMConfig_BothEmpty(t *testing.T) {
	result := ResolveLLMConfig(LLMConfig{}, LLMConfig{})
	if result.Provider != "" {
		t.Errorf("provider = %s, want empty (LLM disabled)", result.Provider)
	}
	if result.Model != "" {
		t.Errorf("model = %s, want empty (LLM disabled)", result.Model)
	}
}
