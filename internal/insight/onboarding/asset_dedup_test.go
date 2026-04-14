package onboarding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeduplicateProposals_FileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file on disk.
	existingFile := "rules/existing-rule.md"
	fullPath := filepath.Join(tmpDir, existingFile)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	proposals := []AssetProposal{
		{
			Type:         "asset.rule",
			Title:        "Existing Rule",
			ProposedPath: existingFile,
			Confidence:   0.9,
		},
		{
			Type:         "asset.rule",
			Title:        "New Rule",
			ProposedPath: "rules/new-rule.md",
			Confidence:   0.8,
		},
	}

	result := DeduplicateProposals(proposals, tmpDir, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(result))
	}
	if result[0].ProposedPath != "rules/new-rule.md" {
		t.Errorf("expected new-rule.md to be kept, got %q", result[0].ProposedPath)
	}
}

func TestDeduplicateProposals_DBExists(t *testing.T) {
	tmpDir := t.TempDir()

	existing := map[string]bool{
		"skills/my-skill.md": true,
	}

	proposals := []AssetProposal{
		{
			Type:         "asset.skill.cc",
			Title:        "My Skill",
			ProposedPath: "skills/my-skill.md",
			Confidence:   0.85,
		},
		{
			Type:         "asset.skill.cc",
			Title:        "Another Skill",
			ProposedPath: "skills/another-skill.md",
			Confidence:   0.75,
		},
	}

	result := DeduplicateProposals(proposals, tmpDir, existing)

	if len(result) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(result))
	}
	if result[0].ProposedPath != "skills/another-skill.md" {
		t.Errorf("expected another-skill.md to be kept, got %q", result[0].ProposedPath)
	}
}

func TestDeduplicateProposals_NoMatch(t *testing.T) {
	tmpDir := t.TempDir()

	proposals := []AssetProposal{
		{
			Type:         "asset.agent.cc",
			Title:        "Agent One",
			ProposedPath: "agents/agent-one.md",
			Confidence:   0.9,
		},
		{
			Type:         "asset.agent.cc",
			Title:        "Agent Two",
			ProposedPath: "agents/agent-two.md",
			Confidence:   0.8,
		},
		{
			Type:         "asset.command.oc",
			Title:        "My Command",
			ProposedPath: "commands/my-command.md",
			Confidence:   0.7,
		},
	}

	result := DeduplicateProposals(proposals, tmpDir, nil)

	if len(result) != 3 {
		t.Fatalf("expected 3 proposals, got %d", len(result))
	}
}

func TestDeduplicateProposals_MixedDedupSources(t *testing.T) {
	tmpDir := t.TempDir()

	// Create one file on disk.
	diskPath := "rules/disk-rule.md"
	fullDiskPath := filepath.Join(tmpDir, diskPath)
	if err := os.MkdirAll(filepath.Dir(fullDiskPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fullDiskPath, []byte("rule"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// One path only in DB.
	dbExisting := map[string]bool{
		"skills/db-skill.md": true,
	}

	proposals := []AssetProposal{
		{
			Type:         "asset.rule",
			Title:        "Disk Rule",
			ProposedPath: diskPath,
			Confidence:   0.9,
		},
		{
			Type:         "asset.skill.cc",
			Title:        "DB Skill",
			ProposedPath: "skills/db-skill.md",
			Confidence:   0.85,
		},
		{
			Type:         "asset.agent.oc",
			Title:        "New Agent",
			ProposedPath: "agents/new-agent.md",
			Confidence:   0.7,
		},
	}

	result := DeduplicateProposals(proposals, tmpDir, dbExisting)

	if len(result) != 1 {
		t.Fatalf("expected 1 proposal, got %d: %+v", len(result), result)
	}
	if result[0].ProposedPath != "agents/new-agent.md" {
		t.Errorf("expected new-agent.md to survive, got %q", result[0].ProposedPath)
	}
}
