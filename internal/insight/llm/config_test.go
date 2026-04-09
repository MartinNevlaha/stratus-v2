package llm

import "testing"

func TestConfig_Validate_InvalidProvider(t *testing.T) {
	cfg := Config{Provider: "invalid", Model: "m", APIKey: "k"}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid provider")
	}
}

func TestConfig_Validate_TemperatureOutOfRange(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "m", Temperature: 2.5}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for temperature > 2")
	}
}

func TestConfig_Validate_MaxTokensBounds(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "m", MaxTokens: 200000}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for max_tokens > 131072")
	}
}

func TestConfig_Validate_TimeoutBounds(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "m", Timeout: 700}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for timeout > 600")
	}
}

func TestConfig_Validate_MaxRetriesBounds(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "m", MaxRetries: 15}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for max_retries > 10")
	}
}

func TestConfig_Validate_Valid(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "llama3.1", Temperature: 0.7, MaxTokens: 4096, Timeout: 60}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConfig_Validate_EmptyProvider_Valid(t *testing.T) {
	// empty provider means LLM is disabled — must be valid
	cfg := Config{}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected nil for empty provider (disabled), got: %v", err)
	}
}
