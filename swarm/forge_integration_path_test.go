package swarm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func TestIntegrationWorktreePath_Exists(t *testing.T) {
	root := t.TempDir()
	s := NewStore(nil, root)
	m := &db.SwarmMission{MergeBranch: "swarm/m1/integration"}
	want := filepath.Join(root, ".stratus", "worktrees", "swarm-m1-integration")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := s.IntegrationWorktreePath(m)
	if !ok || got != want {
		t.Errorf("IntegrationWorktreePath = %q, %v; want %q, true", got, ok, want)
	}
}

func TestIntegrationWorktreePath_Missing(t *testing.T) {
	root := t.TempDir()
	s := NewStore(nil, root)
	m := &db.SwarmMission{MergeBranch: "swarm/m1/integration"}

	got, ok := s.IntegrationWorktreePath(m)
	if ok {
		t.Errorf("IntegrationWorktreePath ok = true for missing dir (%q)", got)
	}
	if want := filepath.Join(root, ".stratus", "worktrees", "swarm-m1-integration"); got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestIntegrationWorktreePath_EmptyMergeBranch(t *testing.T) {
	root := t.TempDir()
	s := NewStore(nil, root)
	// The worktree root exists, so an empty branch name would otherwise resolve to it.
	if err := os.MkdirAll(filepath.Join(root, ".stratus", "worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got, ok := s.IntegrationWorktreePath(&db.SwarmMission{}); ok {
		t.Errorf("IntegrationWorktreePath ok = true for empty merge branch (%q)", got)
	}
}
