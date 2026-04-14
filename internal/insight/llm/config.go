package llm

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	APIKey      string  `json:"-"`
	BaseURL     string  `json:"base_url,omitempty"`
	Timeout     int     `json:"timeout,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxRetries  int     `json:"max_retries,omitempty"`
	// Concurrency limits simultaneous in-flight requests to this provider.
	// 0 = unlimited; 1 = serialized (required for z.ai free tier); >1 = bounded.
	Concurrency int `json:"concurrency,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Provider:    "zai",
		Model:       "glm-5",
		Timeout:     120,
		MaxTokens:   16384,
		Temperature: 0.7,
	}
}

func (c Config) WithEnv() Config {
	if c.APIKey == "" {
		c.APIKey = os.Getenv("LLM_API_KEY")
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv("OPENAI_API_KEY")
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	return c
}

func (c Config) TimeoutDuration() time.Duration {
	if c.Timeout <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.Timeout) * time.Second
}

func (c Config) Validate() error {
	// Empty provider means LLM is disabled — valid for top-level config.
	if c.Provider == "" {
		return nil
	}
	validProviders := map[string]bool{"zai": true, "openai": true, "ollama": true, "anthropic": true}
	if !validProviders[c.Provider] {
		return fmt.Errorf("llm: unsupported provider %q, must be one of: zai, openai, ollama, anthropic", c.Provider)
	}
	if c.Model == "" {
		return fmt.Errorf("llm: model is required")
	}
	if c.APIKey == "" && c.Provider != "ollama" {
		return fmt.Errorf("llm: api_key is required for provider %s", c.Provider)
	}
	if c.MaxTokens < 0 || c.MaxTokens > 131072 {
		return fmt.Errorf("llm: max_tokens must be between 0 and 131072, got %d", c.MaxTokens)
	}
	if c.Temperature < 0 || c.Temperature > 2 {
		return fmt.Errorf("llm: temperature must be between 0 and 2, got %f", c.Temperature)
	}
	if c.Timeout < 0 || c.Timeout > 600 {
		return fmt.Errorf("llm: timeout must be between 0 and 600 seconds, got %d", c.Timeout)
	}
	if c.MaxRetries < 0 || c.MaxRetries > 10 {
		return fmt.Errorf("llm: max_retries must be between 0 and 10, got %d", c.MaxRetries)
	}
	if c.Concurrency < 0 {
		return fmt.Errorf("llm: concurrency must be >= 0, got %d", c.Concurrency)
	}
	return nil
}

func (c Config) EffectiveBaseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	switch c.Provider {
	case "zai":
		return "https://api.z.ai/api/paas/v4"
	case "openai":
		return "https://api.openai.com/v1"
	case "ollama":
		return "http://localhost:11434/v1"
	case "anthropic":
		return "https://api.anthropic.com"
	default:
		return c.BaseURL
	}
}
