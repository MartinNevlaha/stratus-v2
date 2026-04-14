package api

import (
	"fmt"

	"github.com/MartinNevlaha/stratus-v2/config"
)

// validateLLMConfig checks LLM config bounds. If allowEmpty is true, a fully
// zero-valued LLMConfig passes (used for subsystem overrides that defer to
// the global LLM). Otherwise Provider and Model must be non-empty.
func validateLLMConfig(c config.LLMConfig, allowEmpty bool) error {
	isZero := c.Provider == "" && c.Model == "" && c.BaseURL == "" &&
		c.APIKey == "" && c.Timeout == 0 && c.MaxTokens == 0 &&
		c.Temperature == 0 && c.MaxRetries == 0
	if isZero {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("llm config is empty")
	}

	// Provider enum (empty allowed for override partial shape)
	switch c.Provider {
	case "", "zai", "anthropic", "openai", "ollama":
	default:
		return fmt.Errorf("llm.provider: invalid value %q (allowed: zai, anthropic, openai, ollama)", c.Provider)
	}

	// Temperature ∈ [0, 2]
	if c.Temperature < 0 || c.Temperature > 2 {
		return fmt.Errorf("llm.temperature: %.2f out of range [0, 2]", c.Temperature)
	}

	// MaxTokens ∈ [0, 200000] (0 = inherit)
	if c.MaxTokens < 0 || c.MaxTokens > 200000 {
		return fmt.Errorf("llm.max_tokens: %d out of range [0, 200000]", c.MaxTokens)
	}

	// Timeout ∈ [0, 600] seconds (0 = inherit)
	if c.Timeout < 0 || c.Timeout > 600 {
		return fmt.Errorf("llm.timeout: %d out of range [0, 600]", c.Timeout)
	}

	// MaxRetries ∈ [0, 10]
	if c.MaxRetries < 0 || c.MaxRetries > 10 {
		return fmt.Errorf("llm.max_retries: %d out of range [0, 10]", c.MaxRetries)
	}

	return nil
}

// maskLLMConfig returns a copy of c with APIKey replaced by "***" if non-empty.
func maskLLMConfig(c config.LLMConfig) config.LLMConfig {
	if c.APIKey != "" {
		c.APIKey = "***"
	}
	return c
}

// restoreLLMAPIKey preserves the stored API key when the incoming value is the
// mask sentinel or an empty string. Only an explicit non-sentinel, non-empty
// value overwrites the stored key. Mutates `incoming` in place.
func restoreLLMAPIKey(incoming *config.LLMConfig, stored config.LLMConfig) {
	if incoming.APIKey == "" || incoming.APIKey == "***" {
		incoming.APIKey = stored.APIKey
	}
}
