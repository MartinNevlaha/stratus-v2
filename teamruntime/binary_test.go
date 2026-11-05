package teamruntime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBinary writes an executable file and returns its path.
func fakeBinary(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// isolate replaces the PATH lookup and the home directory so a test never sees the
// developer's real installation.
func isolate(t *testing.T, home string) {
	t.Helper()
	oldLook, oldHome, oldFallback := lookPath, userHomeDir, systemFallbacks
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	userHomeDir = func() (string, error) { return home, nil }
	systemFallbacks = map[string][]string{}
	t.Cleanup(func() { lookPath, userHomeDir, systemFallbacks = oldLook, oldHome, oldFallback })
	t.Setenv("STRATUS_CODEX_BIN", "")
}

func TestResolveBinary_PathWinsOverFallbacks(t *testing.T) {
	dir := t.TempDir()
	isolate(t, dir)
	onPath := fakeBinary(t, dir, "codex-on-path")
	lookPath = func(string) (string, error) { return onPath, nil }
	fakeBinary(t, filepath.Join(mkdir(t, dir, ".local/bin")), "codex")

	got, err := resolveBinary("codex", "codex")
	if err != nil || got != onPath {
		t.Fatalf("got %q, %v; want the PATH entry %q", got, err, onPath)
	}
}

func TestResolveBinary_FindsKnownLocationWhenPathLacksIt(t *testing.T) {
	home := t.TempDir()
	isolate(t, home)
	want := fakeBinary(t, mkdir(t, home, ".local/bin"), "codex")

	got, err := resolveBinary("codex", "codex")
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestResolveBinary_FindsSystemInstalledBundle(t *testing.T) {
	isolate(t, t.TempDir())
	bundle := fakeBinary(t, t.TempDir(), "codex")
	systemFallbacks = map[string][]string{"codex": {filepath.Join(t.TempDir(), "missing"), bundle}}

	got, err := resolveBinary("codex", "codex")
	if err != nil || got != bundle {
		t.Fatalf("got %q, %v; want %q", got, err, bundle)
	}
}

func TestResolveBinary_FindsNodeVersionManagerInstall(t *testing.T) {
	home := t.TempDir()
	isolate(t, home)
	want := fakeBinary(t, mkdir(t, home, ".nvm/versions/node/v22.1.0/bin"), "codex")

	got, err := resolveBinary("codex", "codex")
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}

func TestResolveBinary_EnvOverrideWinsEverywhere(t *testing.T) {
	home := t.TempDir()
	isolate(t, home)
	onPath := fakeBinary(t, home, "codex-on-path")
	lookPath = func(string) (string, error) { return onPath, nil }
	override := fakeBinary(t, t.TempDir(), "my-codex")
	t.Setenv("STRATUS_CODEX_BIN", override)

	got, err := resolveBinary("codex", "codex")
	if err != nil || got != override {
		t.Fatalf("got %q, %v; want the override %q", got, err, override)
	}
}

func TestResolveBinary_InvalidEnvOverrideIsAnErrorNotASilentFallback(t *testing.T) {
	home := t.TempDir()
	isolate(t, home)
	onPath := fakeBinary(t, home, "codex-on-path")
	lookPath = func(string) (string, error) { return onPath, nil }

	for name, value := range map[string]string{
		"relative":    "codex",
		"missing":     filepath.Join(home, "nope"),
		"directory":   home,
		"not-exec":    writeNonExecutable(t, home),
		"has-newline": "/usr/bin/codex\n--evil",
	} {
		t.Setenv("STRATUS_CODEX_BIN", value)
		got, err := resolveBinary("codex", "codex")
		if err == nil || got != "" {
			t.Errorf("%s: got %q, %v; want an error and no path", name, got, err)
			continue
		}
		if !strings.Contains(err.Error(), "STRATUS_CODEX_BIN") {
			t.Errorf("%s: error should name the variable: %v", name, err)
		}
	}
}

func TestResolveBinary_NotFoundExplainsWhereItLookedAndHowToFixIt(t *testing.T) {
	home := t.TempDir()
	isolate(t, home)
	systemFallbacks = map[string][]string{"codex": {"/nonexistent/bundle/codex"}}

	_, err := resolveBinary("codex", "codex")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"codex executable is unavailable", "PATH", filepath.Join(home, ".local/bin/codex"), "/nonexistent/bundle/codex", "STRATUS_CODEX_BIN"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
}

func TestResolveBinary_SkipsDirectoriesAndNonExecutableFiles(t *testing.T) {
	home := t.TempDir()
	isolate(t, home)
	mkdir(t, home, ".local/bin/codex") // a directory with the right name
	writeNonExecutableAt(t, mkdir(t, home, ".npm-global/bin"), "codex")

	if got, err := resolveBinary("codex", "codex"); err == nil {
		t.Fatalf("directories and non-executable files must not match, got %q", got)
	}
}

func TestEnvNameFor(t *testing.T) {
	for runtime, want := range map[string]string{"codex": "STRATUS_CODEX_BIN", "claude-code": "STRATUS_CLAUDE_CODE_BIN", "opencode": "STRATUS_OPENCODE_BIN"} {
		if got := binaryEnv(runtime); got != want {
			t.Errorf("binaryEnv(%q) = %q, want %q", runtime, got, want)
		}
	}
}

func TestCommandUsesTheResolvedBinary(t *testing.T) {
	home := t.TempDir()
	isolate(t, home)
	want := fakeBinary(t, mkdir(t, home, ".local/bin"), "codex")
	a := &nativeAdapter{runtime: "codex", binary: "codex"}

	cmd := a.command(home, "app-server")
	if cmd.Path != want {
		t.Fatalf("command path = %q, want %q", cmd.Path, want)
	}
	if got := cmd.Args; len(got) != 2 || got[1] != "app-server" {
		t.Fatalf("args = %v", got)
	}
}

func mkdir(t *testing.T, base, rel string) string {
	t.Helper()
	p := filepath.Join(base, rel)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeNonExecutable(t *testing.T, dir string) string {
	t.Helper()
	return writeNonExecutableAt(t, dir, "plain-file")
}

func writeNonExecutableAt(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
