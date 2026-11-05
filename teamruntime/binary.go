package teamruntime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Seams for tests: the real PATH, home directory and system install locations differ
// per machine.
var (
	lookPath    = exec.LookPath
	userHomeDir = os.UserHomeDir
)

// systemFallbacks are install locations that are often missing from the PATH of a server
// started from a desktop launcher, a service or an IDE. Codex ships as a bundled binary
// of the ChatGPT/Codex desktop app and is not on the PATH at all.
var systemFallbacks = map[string][]string{
	"codex": {
		"/usr/lib/chatgpt/resources/codex",
		"/usr/local/bin/codex",
		"/opt/homebrew/bin/codex",
		"/Applications/ChatGPT.app/Contents/Resources/codex",
		"/Applications/Codex.app/Contents/Resources/codex",
	},
	"claude-code": {"/usr/local/bin/claude", "/opt/homebrew/bin/claude"},
	"opencode":    {"/usr/local/bin/opencode", "/opt/homebrew/bin/opencode"},
}

// homeFallbacks are per-user install locations, relative to the home directory.
var homeFallbacks = map[string][]string{
	"codex":       {".local/bin/codex", ".npm-global/bin/codex", ".bun/bin/codex", ".volta/bin/codex", ".cargo/bin/codex"},
	"claude-code": {".claude/local/claude", ".local/bin/claude", ".npm-global/bin/claude", ".bun/bin/claude", ".volta/bin/claude"},
	"opencode":    {".opencode/bin/opencode", ".local/bin/opencode", ".npm-global/bin/opencode", ".bun/bin/opencode"},
}

// binaryEnv names the variable that points a runtime at an explicit executable,
// e.g. STRATUS_CODEX_BIN or STRATUS_CLAUDE_CODE_BIN.
func binaryEnv(runtime string) string {
	return "STRATUS_" + strings.ToUpper(strings.ReplaceAll(runtime, "-", "_")) + "_BIN"
}

// resolveBinary finds the executable of a native runtime. Order: the explicit
// STRATUS_<RUNTIME>_BIN override (an invalid value is an error, never a silent
// fallback), the PATH, per-user install locations, system install locations.
func resolveBinary(runtime, name string) (string, error) {
	envName := binaryEnv(runtime)
	if v := os.Getenv(envName); v != "" {
		if strings.ContainsAny(v, "\x00\r\n") || !filepath.IsAbs(v) || !isExecutableFile(v) {
			return "", fmt.Errorf("%s executable is unavailable: %s must be the absolute path of an executable file", runtime, envName)
		}
		return v, nil
	}
	if p, err := lookPath(name); err == nil {
		return p, nil
	}
	searched := []string{}
	if home, err := userHomeDir(); err == nil && home != "" {
		for _, rel := range homeFallbacks[runtime] {
			searched = append(searched, filepath.Join(home, rel))
		}
		for _, p := range nodeVersionManagerBinaries(home, name) {
			searched = append(searched, p)
		}
	}
	searched = append(searched, systemFallbacks[runtime]...)
	for _, p := range searched {
		if isExecutableFile(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s executable is unavailable: not found in PATH or in %s; install it, add it to the PATH of the server, or set %s to its absolute path",
		runtime, strings.Join(searched, ", "), envName)
}

// nodeVersionManagerBinaries lists <home>/.nvm/versions/node/*/bin/<name>, newest
// version first.
func nodeVersionManagerBinaries(home, name string) []string {
	matches, err := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", name))
	if err != nil {
		return nil
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	return matches
}

// isExecutableFile reports whether path is a regular file with an execute bit.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
