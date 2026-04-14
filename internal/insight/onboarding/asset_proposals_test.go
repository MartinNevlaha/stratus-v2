package onboarding

import (
	"os"
	"path/filepath"
	"testing"
)

func makeProfile(langs []LanguageStat, patterns []string, ci string, framework string) *ProjectProfile {
	return &ProjectProfile{
		ProjectName:      "test-project",
		Languages:        langs,
		DetectedPatterns: patterns,
		CIProvider:       ci,
		TestStructure: TestStructure{
			Framework: framework,
		},
	}
}

func TestGenerateAssetProposals_GoProject(t *testing.T) {
	profile := makeProfile(
		[]LanguageStat{{Language: "Go", Percentage: 90}},
		nil, "", "",
	)

	proposals := GenerateAssetProposals(profile, t.TempDir(), nil)

	found := false
	for _, p := range proposals {
		if p.ProposedPath == ".claude/rules/go-conventions.md" {
			found = true
			if p.Type != "asset.rule" {
				t.Errorf("expected type asset.rule, got %s", p.Type)
			}
			break
		}
	}
	if !found {
		t.Error("expected go-conventions.md proposal for Go project")
	}
}

func TestGenerateAssetProposals_MultiLanguage(t *testing.T) {
	profile := makeProfile(
		[]LanguageStat{
			{Language: "Go", Percentage: 60},
			{Language: "TypeScript", Percentage: 40},
		},
		nil, "", "",
	)

	proposals := GenerateAssetProposals(profile, t.TempDir(), nil)

	paths := make(map[string]bool)
	for _, p := range proposals {
		paths[p.ProposedPath] = true
	}

	if !paths[".claude/rules/go-conventions.md"] {
		t.Error("expected go-conventions.md proposal")
	}
	if !paths[".claude/rules/ts-conventions.md"] {
		t.Error("expected ts-conventions.md proposal")
	}
}

func TestGenerateAssetProposals_WebAppPattern(t *testing.T) {
	profile := makeProfile(
		[]LanguageStat{{Language: "TypeScript", Percentage: 80}},
		[]string{"web-app"},
		"", "",
	)

	proposals := GenerateAssetProposals(profile, t.TempDir(), nil)

	ccFound := false
	ocFound := false
	for _, p := range proposals {
		if p.ProposedPath == ".claude/agents/delivery-frontend-specialist.md" {
			ccFound = true
			if p.Target != "claude-code" {
				t.Errorf("expected target claude-code, got %s", p.Target)
			}
		}
		if p.ProposedPath == ".opencode/agents/delivery-frontend-specialist.md" {
			ocFound = true
			if p.Target != "opencode" {
				t.Errorf("expected target opencode, got %s", p.Target)
			}
		}
	}
	if !ccFound {
		t.Error("expected CC frontend specialist agent proposal")
	}
	if !ocFound {
		t.Error("expected OC frontend specialist agent proposal")
	}
}

func TestGenerateAssetProposals_DockerAndCI(t *testing.T) {
	profile := makeProfile(
		[]LanguageStat{{Language: "Go", Percentage: 100}},
		[]string{"docker"},
		"github-actions", "",
	)

	proposals := GenerateAssetProposals(profile, t.TempDir(), nil)

	paths := make(map[string]bool)
	for _, p := range proposals {
		paths[p.ProposedPath] = true
	}

	if !paths[".claude/rules/docker-conventions.md"] {
		t.Error("expected docker-conventions.md proposal")
	}
	if !paths[".claude/rules/ci-conventions.md"] {
		t.Error("expected ci-conventions.md proposal")
	}
	if !paths[".claude/agents/delivery-devops-specialist.md"] {
		t.Error("expected CC devops specialist agent proposal")
	}
	if !paths[".opencode/agents/delivery-devops-specialist.md"] {
		t.Error("expected OC devops specialist agent proposal")
	}
}

func TestGenerateAssetProposals_TestFramework(t *testing.T) {
	profile := makeProfile(
		[]LanguageStat{{Language: "TypeScript", Percentage: 100}},
		nil, "", "jest",
	)

	proposals := GenerateAssetProposals(profile, t.TempDir(), nil)

	found := false
	for _, p := range proposals {
		if p.ProposedPath == ".claude/skills/run-tests/SKILL.md" {
			found = true
			if p.Type != "asset.skill.cc" {
				t.Errorf("expected type asset.skill.cc, got %s", p.Type)
			}
			break
		}
	}
	if !found {
		t.Error("expected run-tests skill proposal for jest framework")
	}
}

func TestGenerateAssetProposals_EmptyProfile(t *testing.T) {
	profile := makeProfile(nil, nil, "", "")

	proposals := GenerateAssetProposals(profile, t.TempDir(), nil)

	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals for empty profile, got %d", len(proposals))
	}
}

func TestGenerateAssetProposals_Dedup(t *testing.T) {
	dir := t.TempDir()

	// Create the go-conventions.md file on disk to trigger dedup
	ruleDir := filepath.Join(dir, ".claude", "rules")
	if err := os.MkdirAll(ruleDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ruleDir, "go-conventions.md"), []byte("existing"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	profile := makeProfile(
		[]LanguageStat{{Language: "Go", Percentage: 90}},
		nil, "", "",
	)

	proposals := GenerateAssetProposals(profile, dir, nil)

	for _, p := range proposals {
		if p.ProposedPath == ".claude/rules/go-conventions.md" {
			t.Error("go-conventions.md should have been deduplicated (file exists on disk)")
		}
	}
}

func TestGenerateAssetProposals_ConfidenceScaling(t *testing.T) {
	profile := makeProfile(
		[]LanguageStat{{Language: "Go", Percentage: 87}},
		nil, "", "",
	)

	proposals := GenerateAssetProposals(profile, t.TempDir(), nil)

	for _, p := range proposals {
		if p.ProposedPath == ".claude/rules/go-conventions.md" {
			const want = 0.87
			if p.Confidence != want {
				t.Errorf("expected confidence %.2f, got %.2f", want, p.Confidence)
			}
			return
		}
	}
	t.Error("go-conventions.md proposal not found")
}
