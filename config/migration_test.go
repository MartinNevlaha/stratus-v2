package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateGuardianLegacyLLM(t *testing.T) {
	cases := []struct {
		name string
		raw  string // JSON for GuardianConfig only
		want GuardianConfig
	}{
		{
			name: "legacy only",
			raw:  `{"enabled":true,"llm_endpoint":"https://api.openai.com/v1","llm_api_key":"sk-x","llm_model":"gpt-4o","llm_temperature":0.5,"llm_max_tokens":2048}`,
			want: GuardianConfig{
				Enabled: true,
				LLM: LLMConfig{
					Provider: "openai", BaseURL: "https://api.openai.com/v1",
					APIKey: "sk-x", Model: "gpt-4o",
					Temperature: 0.5, MaxTokens: 2048,
				},
			},
		},
		{
			name: "nested only",
			raw:  `{"llm":{"provider":"zai","model":"glm-4","base_url":"https://api.z.ai","api_key":"zai-k","temperature":0.2,"max_tokens":4096}}`,
			want: GuardianConfig{
				LLM: LLMConfig{
					Provider: "zai", Model: "glm-4",
					BaseURL: "https://api.z.ai", APIKey: "zai-k",
					Temperature: 0.2, MaxTokens: 4096,
				},
			},
		},
		{
			name: "both — nested wins",
			raw:  `{"llm":{"provider":"zai","model":"glm-4","base_url":"https://api.z.ai","api_key":"zai-k"},"llm_endpoint":"legacy","llm_model":"legacy-model"}`,
			want: GuardianConfig{
				LLM: LLMConfig{
					Provider: "zai", Model: "glm-4",
					BaseURL: "https://api.z.ai", APIKey: "zai-k",
				},
			},
		},
		{
			name: "neither",
			raw:  `{"enabled":true}`,
			want: GuardianConfig{Enabled: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var g GuardianConfig
			if err := json.Unmarshal([]byte(tc.raw), &g); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			migrateGuardianLegacyLLM(&g)
			if g.LLM != tc.want.LLM {
				t.Errorf("LLM mismatch\n got: %+v\nwant: %+v", g.LLM, tc.want.LLM)
			}
			if g.LegacyLLMEndpoint != "" || g.LegacyLLMAPIKey != "" ||
				g.LegacyLLMModel != "" || g.LegacyLLMTemperature != 0 || g.LegacyLLMMaxTokens != 0 {
				t.Errorf("legacy fields not cleared: %+v", g)
			}
		})
	}
}

func TestLoad_MigratesLegacyGuardianLLM(t *testing.T) {
	dir := t.TempDir()
	cfgJSON := `{
		"port": 41777,
		"guardian": {
			"enabled": true,
			"llm_endpoint": "https://api.openai.com/v1",
			"llm_api_key": "sk-legacy",
			"llm_model": "gpt-4o-mini",
			"llm_temperature": 0.4,
			"llm_max_tokens": 512
		}
	}`
	cfgPath := filepath.Join(dir, ".stratus.json")
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Change to temp dir so Load()'s findStratusJSON walks to find it.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	cfg := Load()

	if cfg.Guardian.LLM.Provider != "openai" {
		t.Errorf("provider = %q, want openai", cfg.Guardian.LLM.Provider)
	}
	if cfg.Guardian.LLM.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("base_url = %q, want https://api.openai.com/v1", cfg.Guardian.LLM.BaseURL)
	}
	if cfg.Guardian.LLM.APIKey != "sk-legacy" {
		t.Errorf("api_key = %q, want sk-legacy", cfg.Guardian.LLM.APIKey)
	}
	if cfg.Guardian.LLM.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini", cfg.Guardian.LLM.Model)
	}
	if cfg.Guardian.LLM.Temperature != 0.4 {
		t.Errorf("temperature = %f, want 0.4", cfg.Guardian.LLM.Temperature)
	}
	if cfg.Guardian.LLM.MaxTokens != 512 {
		t.Errorf("max_tokens = %d, want 512", cfg.Guardian.LLM.MaxTokens)
	}
	// Legacy fields should be cleared.
	if cfg.Guardian.LegacyLLMEndpoint != "" || cfg.Guardian.LegacyLLMAPIKey != "" ||
		cfg.Guardian.LegacyLLMModel != "" || cfg.Guardian.LegacyLLMTemperature != 0 ||
		cfg.Guardian.LegacyLLMMaxTokens != 0 {
		t.Errorf("legacy fields not cleared after Load(): %+v", cfg.Guardian)
	}
}
