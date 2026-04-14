package config

// ResolveLLMConfig merges a top-level LLM config with a subsystem-specific override.
// Non-zero fields in the override take precedence. If the override has both provider
// and model set, it is treated as a fully-specified config and returned directly.
func ResolveLLMConfig(topLevel, override LLMConfig) LLMConfig {
	// If override has both provider + model, it's fully specified
	if override.Provider != "" && override.Model != "" {
		result := override
		// Still fill in missing fields from top-level
		if result.APIKey == "" {
			result.APIKey = topLevel.APIKey
		}
		if result.BaseURL == "" {
			result.BaseURL = topLevel.BaseURL
		}
		if result.Timeout == 0 {
			result.Timeout = topLevel.Timeout
		}
		if result.MaxTokens == 0 {
			result.MaxTokens = topLevel.MaxTokens
		}
		if result.Temperature == 0 {
			result.Temperature = topLevel.Temperature
		}
		if result.MaxRetries == 0 {
			result.MaxRetries = topLevel.MaxRetries
		}
		if result.Concurrency == 0 {
			result.Concurrency = topLevel.Concurrency
		}
		return result
	}
	// Otherwise use top-level as base, override individual fields
	result := topLevel
	if override.Provider != "" {
		result.Provider = override.Provider
	}
	if override.Model != "" {
		result.Model = override.Model
	}
	if override.APIKey != "" {
		result.APIKey = override.APIKey
	}
	if override.BaseURL != "" {
		result.BaseURL = override.BaseURL
	}
	if override.Timeout != 0 {
		result.Timeout = override.Timeout
	}
	if override.MaxTokens != 0 {
		result.MaxTokens = override.MaxTokens
	}
	if override.Temperature != 0 {
		result.Temperature = override.Temperature
	}
	if override.MaxRetries != 0 {
		result.MaxRetries = override.MaxRetries
	}
	if override.Concurrency != 0 {
		result.Concurrency = override.Concurrency
	}
	return result
}

