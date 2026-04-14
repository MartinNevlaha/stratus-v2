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

func TestResolveLLMConfig_GuardianNested(t *testing.T) {
	t.Run("guardian only populated uses guardian", func(t *testing.T) {
		top := LLMConfig{}
		guardianLLM := LLMConfig{Provider: "openai", Model: "gpt-4o", APIKey: "sk-g", BaseURL: "https://api.openai.com/v1", Temperature: 0.5, MaxTokens: 2048}
		result := ResolveLLMConfig(top, guardianLLM)
		if result.Provider != "openai" {
			t.Errorf("provider = %s, want openai", result.Provider)
		}
		if result.Model != "gpt-4o" {
			t.Errorf("model = %s, want gpt-4o", result.Model)
		}
		if result.APIKey != "sk-g" {
			t.Errorf("api_key = %s, want sk-g", result.APIKey)
		}
		if result.Temperature != 0.5 {
			t.Errorf("temperature = %f, want 0.5", result.Temperature)
		}
	})

	t.Run("top level populated guardian empty uses top level", func(t *testing.T) {
		top := LLMConfig{Provider: "ollama", Model: "llama3.1", APIKey: "key1", Temperature: 0.7}
		guardianLLM := LLMConfig{}
		result := ResolveLLMConfig(top, guardianLLM)
		if result.Provider != "ollama" {
			t.Errorf("provider = %s, want ollama", result.Provider)
		}
		if result.Model != "llama3.1" {
			t.Errorf("model = %s, want llama3.1", result.Model)
		}
		if result.APIKey != "key1" {
			t.Errorf("api_key = %s, want key1", result.APIKey)
		}
	})

	t.Run("both populated guardian overrides top level field by field", func(t *testing.T) {
		top := LLMConfig{Provider: "ollama", Model: "llama3.1", APIKey: "top-key", Temperature: 0.7, Timeout: 120}
		guardianLLM := LLMConfig{Provider: "openai", Model: "gpt-4o", APIKey: "guardian-key", Temperature: 0.3}
		result := ResolveLLMConfig(top, guardianLLM)
		if result.Provider != "openai" {
			t.Errorf("provider = %s, want openai (guardian overrides)", result.Provider)
		}
		if result.Model != "gpt-4o" {
			t.Errorf("model = %s, want gpt-4o (guardian overrides)", result.Model)
		}
		if result.APIKey != "guardian-key" {
			t.Errorf("api_key = %s, want guardian-key (guardian overrides)", result.APIKey)
		}
		if result.Temperature != 0.3 {
			t.Errorf("temperature = %f, want 0.3 (guardian overrides)", result.Temperature)
		}
		if result.Timeout != 120 {
			t.Errorf("timeout = %d, want 120 (inherited from top-level)", result.Timeout)
		}
	})
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

func TestResolveLLMConfig_Concurrency_OverrideFullySpecified(t *testing.T) {
	// Override is fully specified (has provider+model); Concurrency must be propagated.
	top := LLMConfig{Provider: "openai", Model: "gpt-3.5", Concurrency: 3}
	sub := LLMConfig{Provider: "zai", Model: "glm-5", APIKey: "k"}
	result := ResolveLLMConfig(top, sub)
	if result.Concurrency != 3 {
		t.Errorf("Concurrency = %d, want 3 (inherited from top-level when override has none)", result.Concurrency)
	}

	// Override has its own Concurrency; must win.
	sub2 := LLMConfig{Provider: "zai", Model: "glm-5", APIKey: "k", Concurrency: 1}
	result2 := ResolveLLMConfig(top, sub2)
	if result2.Concurrency != 1 {
		t.Errorf("Concurrency = %d, want 1 (override wins)", result2.Concurrency)
	}
}

func TestResolveLLMConfig_Concurrency_TopLevelBase(t *testing.T) {
	// Override is NOT fully specified; top-level is used as base and Concurrency
	// must be carried through.
	top := LLMConfig{Provider: "openai", Model: "gpt-4", Concurrency: 2}
	sub := LLMConfig{Temperature: 0.5} // partial override, no provider/model
	result := ResolveLLMConfig(top, sub)
	if result.Concurrency != 2 {
		t.Errorf("Concurrency = %d, want 2 (inherited from top-level base)", result.Concurrency)
	}

	// Override sets its own Concurrency; must win.
	sub2 := LLMConfig{Temperature: 0.5, Concurrency: 4}
	result2 := ResolveLLMConfig(top, sub2)
	if result2.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want 4 (override wins)", result2.Concurrency)
	}
}
