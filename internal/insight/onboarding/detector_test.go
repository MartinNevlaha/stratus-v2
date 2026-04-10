package onboarding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsNonGreenfield_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	isNonGreenfield, score := IsNonGreenfield(dir)

	if isNonGreenfield {
		t.Errorf("expected false for empty dir, got true (score=%.3f)", score)
	}
	if score >= 0.1 {
		t.Errorf("expected score < 0.1 for empty dir, got %.3f", score)
	}
}

func TestIsNonGreenfield_WithProjectMarkers(t *testing.T) {
	dir := t.TempDir()

	// Create go.mod
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	// Create README.md
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test Project\n"), 0644); err != nil {
		t.Fatalf("failed to create README.md: %v", err)
	}

	// Create 15 .go files (scores 0.3 for source file count, since >10 but <=30)
	for i := 0; i < 15; i++ {
		name := filepath.Join(dir, filepath.FromSlash("file"+string(rune('a'+i))+".go"))
		if err := os.WriteFile(name, []byte("package main\n"), 0644); err != nil {
			t.Fatalf("failed to create go file: %v", err)
		}
	}

	// Expected score: git=0*0.30 + files=0.3*0.25 + markers=1.0*0.20 + readme=1.0*0.10 + ci=0*0.15
	// = 0 + 0.075 + 0.20 + 0.10 + 0 = 0.375
	isNonGreenfield, score := IsNonGreenfield(dir)

	if isNonGreenfield {
		t.Errorf("expected false for project with 15 files (score ~0.375 < 0.4), got true (score=%.3f)", score)
	}
	if score < 0.35 || score > 0.42 {
		t.Errorf("expected score ~0.375, got %.3f", score)
	}
}

func TestIsNonGreenfield_RichProject(t *testing.T) {
	dir := t.TempDir()

	// Create go.mod
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	// Create README.md
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test Project\n"), 0644); err != nil {
		t.Fatalf("failed to create README.md: %v", err)
	}

	// Create .github/workflows/ directory
	workflowsDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("failed to create workflows dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "ci.yml"), []byte("name: CI\n"), 0644); err != nil {
		t.Fatalf("failed to create ci.yml: %v", err)
	}

	// Create 35 .go files (scores 0.6 for source file count, since >30 but <=100)
	for i := 0; i < 35; i++ {
		name := filepath.Join(dir, "file"+string(rune('a'+i%26))+string(rune('a'+i/26))+".go")
		if err := os.WriteFile(name, []byte("package main\n"), 0644); err != nil {
			t.Fatalf("failed to create go file: %v", err)
		}
	}

	// Expected score: git=0*0.30 + files=0.6*0.25 + markers=1.0*0.20 + readme=1.0*0.10 + ci=1.0*0.15
	// = 0 + 0.15 + 0.20 + 0.10 + 0.15 = 0.60
	isNonGreenfield, score := IsNonGreenfield(dir)

	if !isNonGreenfield {
		t.Errorf("expected true for rich project (score ~0.60), got false (score=%.3f)", score)
	}
	if score < 0.55 || score > 0.65 {
		t.Errorf("expected score ~0.60, got %.3f", score)
	}
}

func TestIsNonGreenfield_Threshold(t *testing.T) {
	// Test just below threshold: markers only (0.20) + readme (0.10) = 0.30, below 0.40
	belowDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(belowDir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(belowDir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("failed to create README.md: %v", err)
	}

	isBelow, scoreBelow := IsNonGreenfield(belowDir)
	if isBelow {
		t.Errorf("expected false below threshold (score ~0.30), got true (score=%.3f)", scoreBelow)
	}
	if scoreBelow >= 0.4 {
		t.Errorf("expected score < 0.4, got %.3f", scoreBelow)
	}

	// Test just at/above threshold: markers (0.20) + readme (0.10) + ci (0.15) = 0.45, above 0.40
	aboveDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(aboveDir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(aboveDir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("failed to create README.md: %v", err)
	}
	workflowsDir := filepath.Join(aboveDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("failed to create workflows dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "ci.yml"), []byte("name: CI\n"), 0644); err != nil {
		t.Fatalf("failed to create ci.yml: %v", err)
	}

	isAbove, scoreAbove := IsNonGreenfield(aboveDir)
	if !isAbove {
		t.Errorf("expected true at/above threshold (score ~0.45), got false (score=%.3f)", scoreAbove)
	}
	if scoreAbove < 0.4 {
		t.Errorf("expected score >= 0.4, got %.3f", scoreAbove)
	}
}
