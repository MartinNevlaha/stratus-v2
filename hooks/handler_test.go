package hooks

import (
	"bytes"
	"strings"
	"testing"
)

// Claude Code reads STDERR when a PreToolUse hook exits 2. Writing the reason only to
// stdout made every block arrive as "hook error: No stderr output" -- an error with no
// text, which the agent cannot act on and answers by ending its turn. A silent guard is
// indistinguishable from a crashed one, so the reason MUST reach stderr.
//
// Stdout must stay free of {"continue": false}: Claude Code reads stdout JSON even on
// exit 2, and continue=false stops the agent outright instead of blocking the one tool
// call, so it never sees the reason or gets to adapt.
func TestWriteBlockPutsReasonOnStderrOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	reason := "Write tools are not allowed during review phase. Use Read/Grep/Glob only."

	writeBlock(&stdout, &stderr, reason)

	if !strings.Contains(stderr.String(), reason) {
		t.Fatalf("reason missing from stderr; got %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %q", stdout.String())
	}
}

// An empty reason must not emit a blank line that reads as "no explanation given".
func TestWriteBlockWithoutReasonWritesNothingToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer

	writeBlock(&stdout, &stderr, "")

	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr for an empty reason, got %q", stderr.String())
	}
}
