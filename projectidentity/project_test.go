package projectidentity

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveRootAndSubdirectory(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".stratus.json"), []byte(`{}`), 0600)
	sub := filepath.Join(root, "src")
	os.Mkdir(sub, 0755)
	a, canonical, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	b, other, err := Resolve(sub)
	if err != nil || a != b || canonical != other || canonical != root {
		t.Fatalf("%q %q %q %q %v", a, b, canonical, other, err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	c, _, err := Resolve(alias)
	if err != nil || c != a {
		t.Fatalf("symlink identity %q %v", c, err)
	}
}

func TestResolveSameBasenameIsNotIdentity(t *testing.T) {
	var ids []string
	for range 2 {
		root := filepath.Join(t.TempDir(), "same")
		os.Mkdir(root, 0755)
		id, _, err := Resolve(root)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if ids[0] == ids[1] {
		t.Fatal("unrelated directories grouped")
	}
	if _, _, err := Resolve(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("missing root accepted")
	}
}

func TestResolveVerifiedWorktrees(t *testing.T) {
	root := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %s: %v", args, b, e)
		}
	}
	git(root, "init", "-q")
	git(root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "fixture")
	os.WriteFile(filepath.Join(root, ".stratus.json"), []byte(`{}`), 0600)
	wt := filepath.Join(t.TempDir(), "worktree")
	git(root, "worktree", "add", "--detach", wt)
	os.WriteFile(filepath.Join(wt, ".stratus.json"), []byte(`{}`), 0600)
	a, _, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	b, canonical, err := Resolve(wt)
	if err != nil || a != b || canonical != wt {
		t.Fatalf("worktree: %q %q %q %v", a, b, canonical, err)
	}
	clone := filepath.Join(t.TempDir(), "clone")
	git(root, "clone", "-q", root, clone)
	c, _, err := Resolve(clone)
	if err != nil || c == a {
		t.Fatalf("clone grouped: %q %v", c, err)
	}
	// A nested linked checkout must retain its own working root even when only
	// the main checkout has a .stratus.json ancestor.
	nested := filepath.Join(root, ".worktrees", "nested")
	git(root, "worktree", "add", "--detach", nested)
	n, actual, err := Resolve(nested)
	if err != nil || n != a || actual != nested {
		t.Fatalf("nested worktree: %q %q %v", n, actual, err)
	}
}
