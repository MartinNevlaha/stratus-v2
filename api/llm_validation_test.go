package api

import (
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
)

func TestValidateLLMConfig_ZeroValueAllowEmpty(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{}, true)
	if err != nil {
		t.Errorf("expected nil for zero value with allowEmpty=true, got %v", err)
	}
}

func TestValidateLLMConfig_ZeroValueNotAllowEmpty(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{}, false)
	if err == nil {
		t.Error("expected error for zero value with allowEmpty=false, got nil")
	}
}

func TestValidateLLMConfig_InvalidProvider(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{
		Provider: "bogus",
		Model:    "some-model",
	}, false)
	if err == nil {
		t.Fatal("expected error for invalid provider, got nil")
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Errorf("expected error to contain 'provider', got: %v", err)
	}
}

func TestValidateLLMConfig_TemperatureTooLow(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{
		Provider:    "openai",
		Model:       "gpt-4",
		Temperature: -0.1,
	}, false)
	if err == nil {
		t.Error("expected error for temperature=-0.1, got nil")
	}
}

func TestValidateLLMConfig_TemperatureTooHigh(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{
		Provider:    "openai",
		Model:       "gpt-4",
		Temperature: 2.1,
	}, false)
	if err == nil {
		t.Error("expected error for temperature=2.1, got nil")
	}
}

func TestValidateLLMConfig_TemperatureValid(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{
		Provider:    "openai",
		Model:       "gpt-4",
		Temperature: 1.5,
	}, false)
	if err != nil {
		t.Errorf("expected nil for temperature=1.5, got %v", err)
	}
}

func TestValidateLLMConfig_MaxTokensNegative(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{
		Provider:  "openai",
		Model:     "gpt-4",
		MaxTokens: -1,
	}, false)
	if err == nil {
		t.Error("expected error for max_tokens=-1, got nil")
	}
}

func TestValidateLLMConfig_MaxTokensTooHigh(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{
		Provider:  "openai",
		Model:     "gpt-4",
		MaxTokens: 200001,
	}, false)
	if err == nil {
		t.Error("expected error for max_tokens=200001, got nil")
	}
}

func TestValidateLLMConfig_TimeoutTooHigh(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4",
		Timeout:  601,
	}, false)
	if err == nil {
		t.Error("expected error for timeout=601, got nil")
	}
}

func TestValidateLLMConfig_MaxRetriesTooHigh(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{
		Provider:   "openai",
		Model:      "gpt-4",
		MaxRetries: 11,
	}, false)
	if err == nil {
		t.Error("expected error for max_retries=11, got nil")
	}
}

func TestValidateLLMConfig_FullValidOpenAI(t *testing.T) {
	err := validateLLMConfig(config.LLMConfig{
		Provider:    "openai",
		Model:       "gpt-4",
		APIKey:      "sk-test",
		Temperature: 0.7,
		MaxTokens:   4096,
		Timeout:     60,
		MaxRetries:  3,
	}, false)
	if err != nil {
		t.Errorf("expected nil for valid openai config, got %v", err)
	}
}

// maskLLMConfig tests

func TestMaskLLMConfig_NonEmptyAPIKey(t *testing.T) {
	cfg := config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4",
		APIKey:   "sk-supersecret",
	}
	masked := maskLLMConfig(cfg)
	if masked.APIKey != "***" {
		t.Errorf("expected APIKey to be '***', got %q", masked.APIKey)
	}
}

func TestMaskLLMConfig_EmptyAPIKey(t *testing.T) {
	cfg := config.LLMConfig{
		Provider: "openai",
		Model:    "gpt-4",
		APIKey:   "",
	}
	masked := maskLLMConfig(cfg)
	if masked.APIKey != "" {
		t.Errorf("expected APIKey to stay empty, got %q", masked.APIKey)
	}
}

func TestMaskLLMConfig_OtherFieldsUntouched(t *testing.T) {
	cfg := config.LLMConfig{
		Provider:    "openai",
		Model:       "gpt-4",
		APIKey:      "sk-secret",
		Temperature: 0.8,
		MaxTokens:   2048,
	}
	masked := maskLLMConfig(cfg)
	if masked.Provider != cfg.Provider {
		t.Errorf("Provider changed: want %q, got %q", cfg.Provider, masked.Provider)
	}
	if masked.Model != cfg.Model {
		t.Errorf("Model changed: want %q, got %q", cfg.Model, masked.Model)
	}
	if masked.Temperature != cfg.Temperature {
		t.Errorf("Temperature changed: want %v, got %v", cfg.Temperature, masked.Temperature)
	}
	if masked.MaxTokens != cfg.MaxTokens {
		t.Errorf("MaxTokens changed: want %v, got %v", cfg.MaxTokens, masked.MaxTokens)
	}
}

// restoreLLMAPIKey tests

func TestRestoreLLMAPIKey_SentinelReplaced(t *testing.T) {
	incoming := config.LLMConfig{APIKey: "***"}
	stored := config.LLMConfig{APIKey: "stored-real-key"}
	restoreLLMAPIKey(&incoming, stored)
	if incoming.APIKey != "stored-real-key" {
		t.Errorf("expected stored key to be restored, got %q", incoming.APIKey)
	}
}

func TestRestoreLLMAPIKey_EmptyReplaced(t *testing.T) {
	incoming := config.LLMConfig{APIKey: ""}
	stored := config.LLMConfig{APIKey: "stored-real-key"}
	restoreLLMAPIKey(&incoming, stored)
	if incoming.APIKey != "stored-real-key" {
		t.Errorf("expected stored key to be restored from empty, got %q", incoming.APIKey)
	}
}

func TestRestoreLLMAPIKey_NewKeyPreserved(t *testing.T) {
	incoming := config.LLMConfig{APIKey: "new-key"}
	stored := config.LLMConfig{APIKey: "stored-real-key"}
	restoreLLMAPIKey(&incoming, stored)
	if incoming.APIKey != "new-key" {
		t.Errorf("expected new key to be preserved, got %q", incoming.APIKey)
	}
}
