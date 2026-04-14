package config

import (
	"errors"
	"testing"
)

// TestValidateWikiConfig_MaxPagesPerIngest covers the 0=unlimited sentinel,
// positive values, and negative rejection for MaxPagesPerIngest.
func TestValidateWikiConfig_MaxPagesPerIngest(t *testing.T) {
	cases := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"zero_unlimited_sentinel", 0, false},
		{"positive_allowed", 10, false},
		{"large_positive_allowed", 9999, false},
		{"negative_rejected", -1, true},
		{"large_negative_rejected", -100, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := WikiConfig{
				MaxPagesPerIngest:  tc.value,
				OnboardingMaxPages: 0, // valid sentinel
				StalenessThreshold: 0.5,
				MaxPageSizeTokens:  1000,
			}
			err := ValidateWikiConfig(&cfg)
			if tc.wantErr && err == nil {
				t.Errorf("MaxPagesPerIngest=%d: expected error, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("MaxPagesPerIngest=%d: unexpected error: %v", tc.value, err)
			}
		})
	}
}

// TestValidateWikiConfig_OnboardingMaxPages covers the 0=unlimited sentinel,
// positive values, and negative rejection for OnboardingMaxPages.
func TestValidateWikiConfig_OnboardingMaxPages(t *testing.T) {
	cases := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"zero_unlimited_sentinel", 0, false},
		{"positive_allowed", 50, false},
		{"large_positive_allowed", 9999, false},
		{"negative_rejected", -1, true},
		{"large_negative_rejected", -200, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := WikiConfig{
				MaxPagesPerIngest:  0, // valid sentinel
				OnboardingMaxPages: tc.value,
				StalenessThreshold: 0.5,
				MaxPageSizeTokens:  1000,
			}
			err := ValidateWikiConfig(&cfg)
			if tc.wantErr && err == nil {
				t.Errorf("OnboardingMaxPages=%d: expected error, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("OnboardingMaxPages=%d: unexpected error: %v", tc.value, err)
			}
		})
	}
}

// TestValidateWikiConfig_IngestTokenBudget covers 0=unlimited, positive, and negative rejection.
func TestValidateWikiConfig_IngestTokenBudget(t *testing.T) {
	cases := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"zero_unlimited_sentinel", 0, false},
		{"positive_allowed", 100000, false},
		{"negative_rejected", -1, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := WikiConfig{
				MaxPagesPerIngest:  0,
				OnboardingMaxPages: 0,
				IngestTokenBudget:  tc.value,
				StalenessThreshold: 0.5,
				MaxPageSizeTokens:  1000,
			}
			err := ValidateWikiConfig(&cfg)
			if tc.wantErr && err == nil {
				t.Errorf("IngestTokenBudget=%d: expected error, got nil", tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("IngestTokenBudget=%d: unexpected error: %v", tc.value, err)
			}
		})
	}
}

// TestValidateWikiConfig_SentinelComment verifies the sentinel is ErrInvalidWikiConfig.
func TestValidateWikiConfig_ErrorType(t *testing.T) {
	cfg := WikiConfig{
		MaxPagesPerIngest: -5,
	}
	err := ValidateWikiConfig(&cfg)
	if err == nil {
		t.Fatal("expected error for negative MaxPagesPerIngest")
	}
	if !errors.Is(err, ErrInvalidWikiConfig) {
		t.Errorf("error type: got %T (%v), want to wrap ErrInvalidWikiConfig", err, err)
	}
}
