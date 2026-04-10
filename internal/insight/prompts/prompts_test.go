package prompts

import (
	"strings"
	"testing"
)

func TestCompose_JoinsParts(t *testing.T) {
	result := Compose("part1", "part2", "part3")
	expected := "part1\n\npart2\n\npart3"
	if result != expected {
		t.Errorf("Compose() = %q, want %q", result, expected)
	}
}

func TestCompose_SinglePart(t *testing.T) {
	result := Compose("only")
	if result != "only" {
		t.Errorf("Compose() = %q, want %q", result, "only")
	}
}

func TestCompose_Empty(t *testing.T) {
	result := Compose()
	if result != "" {
		t.Errorf("Compose() = %q, want empty string", result)
	}
}

func TestObsidianMarkdown_Embedded(t *testing.T) {
	if ObsidianMarkdown == "" {
		t.Fatal("ObsidianMarkdown embed is empty")
	}
	if !strings.Contains(ObsidianMarkdown, "wikilink") && !strings.Contains(ObsidianMarkdown, "[[") {
		t.Error("ObsidianMarkdown does not contain 'wikilink' or '[['")
	}
}

func TestConstants_NonEmpty(t *testing.T) {
	constants := map[string]string{
		"WikiPageGeneration":   WikiPageGeneration,
		"WikiSynthesis":        WikiSynthesis,
		"HypothesisGeneration": HypothesisGeneration,
		"ExperimentEvaluation": ExperimentEvaluation,
	}
	for name, val := range constants {
		if val == "" {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestOnboardingPrompts_NonEmpty(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
	}{
		{"OnboardingArchitecture", OnboardingArchitecture},
		{"OnboardingModule", OnboardingModule},
		{"OnboardingConventions", OnboardingConventions},
		{"OnboardingBuildGuide", OnboardingBuildGuide},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.prompt == "" {
				t.Errorf("%s must be a non-empty string", tt.name)
			}
		})
	}
}

func TestOnboardingPrompts_ComposeWithObsidian(t *testing.T) {
	result := Compose(OnboardingArchitecture, ObsidianMarkdown)
	if result == "" {
		t.Fatal("Compose(OnboardingArchitecture, ObsidianMarkdown) must return a non-empty string")
	}
	if !strings.Contains(result, OnboardingArchitecture) {
		t.Error("result must contain OnboardingArchitecture")
	}
	if !strings.Contains(result, ObsidianMarkdown) {
		t.Error("result must contain ObsidianMarkdown")
	}
}
