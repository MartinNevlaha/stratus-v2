package main

import (
	"strings"
	"testing"
)

// The coordinator prompts must tell the lead how to behave around the design review
// gate: re-present a reworked plan, revise the document itself when the author is
// unknown, and ask the user when the review is stuck.
func TestWorkflowPromptsDescribeDesignReviewGate(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		for _, fsys := range []interface {
			ReadFile(string) ([]byte, error)
		}{skillsFS, commandsOpenCodeFS, codexAssetsFS} {
			if b, err := fsys.ReadFile(name); err == nil {
				return string(b)
			}
		}
		t.Fatalf("embedded file %s not found", name)
		return ""
	}
	cases := []struct {
		file string
		want []string
	}{
		{"skills/spec/SKILL.md", []string{"design_review_required", "re-present the revised plan to the user", "author is unknown", "ask the user", "plan` to `accept"}},
		{"skills/spec-complex/SKILL.md", []string{"design_review_required", "author is unknown", "ask the user", "re-present", "plan` to `implement"}},
		{"commands-opencode/spec.md", []string{"design_review_required", "re-present the revised plan to the user", "author is unknown", "ask the user"}},
		{"commands-opencode/spec-complex.md", []string{"design_review_required", "author is unknown", "ask the user", "re-present"}},
		{"codex-assets/workflows/spec.md", []string{"design_review_required", "author is unknown", "ask the user"}},
	}
	for _, c := range cases {
		text := read(c.file)
		for _, want := range c.want {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not mention %q", c.file, want)
			}
		}
	}
}
