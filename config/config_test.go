package config

import (
	"os"
	"testing"
)

// TestConfig_LanguageDefault verifies that the default language is "en".
func TestConfig_LanguageDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := Default()
	if cfg.Language != "en" {
		t.Errorf("Default().Language = %q, want %q", cfg.Language, "en")
	}
}

// TestDefault_LLMMaxRetries verifies that the default LLM config has MaxRetries == 3.
func TestDefault_LLMMaxRetries(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := Default()
	if cfg.LLM.MaxRetries != 3 {
		t.Errorf("Default().LLM.MaxRetries = %d, want 3", cfg.LLM.MaxRetries)
	}
}

// TestValidLanguage_Enum verifies that only "sk" and "en" are accepted.
func TestValidLanguage_Enum(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"en", true},
		{"sk", true},
		{"de", false},
		{"", false},
		{"EN", false},
		{"SK", false},
		{"fr", false},
	}
	for _, tc := range cases {
		got := ValidLanguage(tc.input)
		if got != tc.want {
			t.Errorf("ValidLanguage(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestLoad_DevMode(t *testing.T) {
	os.Unsetenv("STRATUS_DEV")

	cases := []struct {
		name    string
		value   string
		set     bool
		wantDev bool
	}{
		{name: "unset", set: false, wantDev: false},
		{name: "empty string", value: "", set: true, wantDev: false},
		{name: "0", value: "0", set: true, wantDev: false},
		{name: "1", value: "1", set: true, wantDev: true},
		{name: "true", value: "true", set: true, wantDev: true},
		{name: "TRUE", value: "TRUE", set: true, wantDev: true},
		{name: "True", value: "True", set: true, wantDev: true},
		{name: "false", value: "false", set: true, wantDev: false},
		{name: "yes", value: "yes", set: true, wantDev: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.set {
				t.Setenv("STRATUS_DEV", tc.value)
			} else {
				os.Unsetenv("STRATUS_DEV")
			}

			cfg := Load()

			if cfg.DevMode != tc.wantDev {
				t.Errorf("DevMode = %v, want %v (STRATUS_DEV=%q set=%v)", cfg.DevMode, tc.wantDev, tc.value, tc.set)
			}
		})
	}
}
