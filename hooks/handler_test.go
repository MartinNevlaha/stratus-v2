package hooks

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// Claude Code reads STDERR when a PreToolUse hook exits 2. Writing the reason only to
// stdout made every block arrive as "hook error: No stderr output" -- an error with no
// text, which the agent cannot act on and answers by ending its turn. A silent guard is
// indistinguishable from a crashed one, so the reason MUST reach stderr.
func TestWriteBlockPutsReasonOnStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	reason := "Write tools are not allowed during review phase. Use Read/Grep/Glob only."

	writeBlock(&stdout, &stderr, reason)

	if !strings.Contains(stderr.String(), reason) {
		t.Fatalf("reason missing from stderr; got %q", stderr.String())
	}

	// The stdout payload stays as it was: some runtimes read the JSON instead.
	var decision Decision
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &decision); err != nil {
		t.Fatalf("stdout is not the decision JSON: %v (%q)", err, stdout.String())
	}
	if decision.Continue {
		t.Fatalf("expected continue=false in the stdout payload")
	}
	if decision.Reason != reason {
		t.Fatalf("stdout reason = %q, want %q", decision.Reason, reason)
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
