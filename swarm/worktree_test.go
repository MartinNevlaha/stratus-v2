package swarm

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// newPreviewRepo makes a repository whose main has f.txt; branches a and b change f.txt
// in conflicting ways and branch c adds c.txt.
func newPreviewRepo(t *testing.T) string {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	commit := func(file, content string) {
		if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, root, "add", file)
		runGit(t, root, "commit", "-q", "-m", file+"="+content)
	}
	runGit(t, root, "init", "-q", "-b", "main")
	commit("f.txt", "base\n")
	for _, b := range []struct{ name, file, content string }{
		{"a", "f.txt", "a\n"}, {"b", "f.txt", "b\n"}, {"c", "c.txt", "c\n"},
	} {
		runGit(t, root, "checkout", "-q", "-b", b.name, "main")
		commit(b.file, b.content)
	}
	runGit(t, root, "checkout", "-q", "main")
	return root
}

// The server's working directory is not the project, so every git call must name its directory.
func TestCreatePreviewMerge_AbortsConflictInsidePreview(t *testing.T) {
	root := newPreviewRepo(t)
	t.Chdir(t.TempDir())
	wm := NewWorktreeManager(root)

	path, failed, err := wm.CreatePreviewMerge("m1", "main", []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("CreatePreviewMerge: %v", err)
	}
	if !slices.Equal(failed, []string{"b"}) {
		t.Fatalf("failed merges = %v, want [b]", failed)
	}
	if _, err := os.Stat(filepath.Join(path, "c.txt")); err != nil {
		t.Fatalf("branch c was not merged after b's conflict was aborted: %v", err)
	}
}

func TestCreatePreviewMerge_ReplacesStalePreview(t *testing.T) {
	root := newPreviewRepo(t)
	t.Chdir(t.TempDir())
	wm := NewWorktreeManager(root)

	if _, _, err := wm.CreatePreviewMerge("m1", "main", []string{"a"}); err != nil {
		t.Fatalf("first preview: %v", err)
	}
	path, failed, err := wm.CreatePreviewMerge("m1", "main", []string{"c"})
	if err != nil {
		t.Fatalf("second preview over a stale one: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed merges = %v, want none", failed)
	}
	if _, err := os.Stat(filepath.Join(path, "c.txt")); err != nil {
		t.Fatalf("second preview is missing branch c: %v", err)
	}
}
