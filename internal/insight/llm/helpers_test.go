package llm

import (
	"testing"
)

// ---------------------------------------------------------------------------
// ParseJSONResponse tests
// ---------------------------------------------------------------------------

func TestParseJSONResponse_SimpleObject(t *testing.T) {
	var result map[string]any
	err := ParseJSONResponse(`{"score": 0.9, "reason": "good"}`, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["score"] != 0.9 {
		t.Errorf("score = %v, want 0.9", result["score"])
	}
	if result["reason"] != "good" {
		t.Errorf("reason = %v, want good", result["reason"])
	}
}

func TestParseJSONResponse_ObjectWithSurroundingText(t *testing.T) {
	var result map[string]any
	raw := `Here is the result: {"key": "value"} — end of response`
	err := ParseJSONResponse(raw, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("key = %v, want value", result["key"])
	}
}

func TestParseJSONResponse_Array(t *testing.T) {
	var result []map[string]any
	raw := `Some preamble [{"id": 1}, {"id": 2}] trailing text`
	err := ParseJSONResponse(raw, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
}

func TestParseJSONResponse_NoJSON(t *testing.T) {
	var result map[string]any
	err := ParseJSONResponse("no json here at all", &result)
	if err == nil {
		t.Fatal("expected error for response with no JSON")
	}
}

func TestParseJSONResponse_InvalidJSON(t *testing.T) {
	var result map[string]any
	err := ParseJSONResponse("{invalid json}", &result)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseJSONResponse_EmptyString(t *testing.T) {
	var result map[string]any
	err := ParseJSONResponse("", &result)
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

// ---------------------------------------------------------------------------
// Config helper tests
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Provider != "zai" {
		t.Errorf("Provider = %q, want zai", cfg.Provider)
	}
	if cfg.Model != "glm-5" {
		t.Errorf("Model = %q, want glm-5", cfg.Model)
	}
	if cfg.Timeout != 120 {
		t.Errorf("Timeout = %d, want 120", cfg.Timeout)
	}
	if cfg.MaxTokens != 16384 {
		t.Errorf("MaxTokens = %d, want 16384", cfg.MaxTokens)
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want 0.7", cfg.Temperature)
	}
}

func TestConfig_TimeoutDuration_Zero(t *testing.T) {
	cfg := Config{Timeout: 0}
	d := cfg.TimeoutDuration()
	if d != 60e9 { // 60 seconds in nanoseconds
		t.Errorf("TimeoutDuration(0) = %v, want 60s", d)
	}
}

func TestConfig_TimeoutDuration_Positive(t *testing.T) {
	cfg := Config{Timeout: 30}
	d := cfg.TimeoutDuration()
	if d.Seconds() != 30 {
		t.Errorf("TimeoutDuration(30) = %v, want 30s", d)
	}
}

func TestConfig_EffectiveBaseURL_ZAI(t *testing.T) {
	cfg := Config{Provider: "zai"}
	if cfg.EffectiveBaseURL() != "https://api.z.ai/api/paas/v4" {
		t.Errorf("ZAI base URL = %q", cfg.EffectiveBaseURL())
	}
}

func TestConfig_EffectiveBaseURL_OpenAI(t *testing.T) {
	cfg := Config{Provider: "openai"}
	if cfg.EffectiveBaseURL() != "https://api.openai.com/v1" {
		t.Errorf("OpenAI base URL = %q", cfg.EffectiveBaseURL())
	}
}

func TestConfig_EffectiveBaseURL_Ollama(t *testing.T) {
	cfg := Config{Provider: "ollama"}
	if cfg.EffectiveBaseURL() != "http://localhost:11434/v1" {
		t.Errorf("Ollama base URL = %q", cfg.EffectiveBaseURL())
	}
}

func TestConfig_EffectiveBaseURL_Anthropic(t *testing.T) {
	cfg := Config{Provider: "anthropic"}
	if cfg.EffectiveBaseURL() != "https://api.anthropic.com" {
		t.Errorf("Anthropic base URL = %q", cfg.EffectiveBaseURL())
	}
}

func TestConfig_EffectiveBaseURL_CustomOverrides(t *testing.T) {
	cfg := Config{Provider: "openai", BaseURL: "http://my-proxy/v1"}
	if cfg.EffectiveBaseURL() != "http://my-proxy/v1" {
		t.Errorf("custom base URL = %q, want http://my-proxy/v1", cfg.EffectiveBaseURL())
	}
}

func TestConfig_EffectiveBaseURL_UnknownProvider(t *testing.T) {
	cfg := Config{Provider: "other", BaseURL: "http://custom"}
	if cfg.EffectiveBaseURL() != "http://custom" {
		t.Errorf("unknown provider base URL = %q, want http://custom", cfg.EffectiveBaseURL())
	}
}

func TestConfig_WithEnv_PreservesExistingAPIKey(t *testing.T) {
	cfg := Config{APIKey: "already-set"}
	result := cfg.WithEnv()
	if result.APIKey != "already-set" {
		t.Errorf("APIKey = %q, should not be overwritten by env", result.APIKey)
	}
}

func TestConfig_Validate_MissingModel(t *testing.T) {
	cfg := Config{Provider: "openai", APIKey: "k"}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for missing model")
	}
}

func TestConfig_Validate_MissingAPIKey_NonOllama(t *testing.T) {
	cfg := Config{Provider: "openai", Model: "gpt-4"}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for missing api_key with non-ollama provider")
	}
}

func TestConfig_Validate_NegativeTimeout(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "m", Timeout: -1}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for negative timeout")
	}
}

func TestConfig_Validate_NegativeMaxRetries(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "m", MaxRetries: -1}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for negative max_retries")
	}
}

func TestConfig_Validate_NegativeTemperature(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "m", Temperature: -0.1}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for negative temperature")
	}
}

func TestConfig_Validate_NegativeMaxTokens(t *testing.T) {
	cfg := Config{Provider: "ollama", Model: "m", MaxTokens: -1}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for negative max_tokens")
	}
}

// ---------------------------------------------------------------------------
// Message helper tests
// ---------------------------------------------------------------------------

func TestSystemPrompt(t *testing.T) {
	m := SystemPrompt("be helpful")
	if m.Role != "system" {
		t.Errorf("role = %q, want system", m.Role)
	}
	if m.Content != "be helpful" {
		t.Errorf("content = %q, want be helpful", m.Content)
	}
}

func TestUserMessage(t *testing.T) {
	m := UserMessage("hello")
	if m.Role != "user" {
		t.Errorf("role = %q, want user", m.Role)
	}
	if m.Content != "hello" {
		t.Errorf("content = %q, want hello", m.Content)
	}
}

func TestAssistantMessage(t *testing.T) {
	m := AssistantMessage("hi there")
	if m.Role != "assistant" {
		t.Errorf("role = %q, want assistant", m.Role)
	}
	if m.Content != "hi there" {
		t.Errorf("content = %q, want hi there", m.Content)
	}
}

func TestBuildMessages_WithSystem(t *testing.T) {
	msgs := BuildMessages("system prompt", "user1", "user2")
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("msgs[0].role = %q, want system", msgs[0].Role)
	}
	if msgs[1].Role != "user" || msgs[1].Content != "user1" {
		t.Errorf("msgs[1] = %+v, want user/user1", msgs[1])
	}
	if msgs[2].Role != "user" || msgs[2].Content != "user2" {
		t.Errorf("msgs[2] = %+v, want user/user2", msgs[2])
	}
}

func TestBuildMessages_NoSystem(t *testing.T) {
	msgs := BuildMessages("", "only user")
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("msgs[0].role = %q, want user", msgs[0].Role)
	}
}

func TestBuildMessages_Empty(t *testing.T) {
	msgs := BuildMessages("")
	if len(msgs) != 0 {
		t.Errorf("len = %d, want 0", len(msgs))
	}
}

func TestBuildConversation_WithSystem(t *testing.T) {
	turns := []Message{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
	}
	msgs := BuildConversation("system", turns...)
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("first message should be system, got %q", msgs[0].Role)
	}
}

func TestBuildConversation_NoSystem(t *testing.T) {
	turns := []Message{
		{Role: "user", Content: "q1"},
	}
	msgs := BuildConversation("", turns...)
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("first message role = %q, want user", msgs[0].Role)
	}
}
