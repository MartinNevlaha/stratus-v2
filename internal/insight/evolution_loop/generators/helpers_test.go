package generators

import (
	"testing"
)

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lowercase and trim",
			input: "  Hello World  ",
			want:  "hello world",
		},
		{
			name:  "collapse whitespace",
			input: "foo   bar   baz",
			want:  "foo bar baz",
		},
		{
			name:  "strip punctuation except dash and underscore",
			input: "foo.bar! baz: qux?",
			want:  "foobar baz qux",
		},
		{
			name:  "preserve dash and underscore",
			input: "foo-bar_baz",
			want:  "foo-bar_baz",
		},
		{
			name:  "mixed case with tabs",
			input: "MixED\tCase\tTEXT",
			want:  "mixed case text",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "punctuation only",
			input: "!@#$%^&*()",
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeTitle(tc.input)
			if got != tc.want {
				t.Errorf("normalizeTitle(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSignalHashInputs_Determinism(t *testing.T) {
	// Same inputs in different order must produce the same output.
	signals1 := []string{"churn:foo.go", "todo_count:3", "adr:001"}
	signals2 := []string{"adr:001", "churn:foo.go", "todo_count:3"}

	out1 := signalHashInputs("refactor_opportunity", "Refactor hotspot: foo.go", signals1)
	out2 := signalHashInputs("refactor_opportunity", "Refactor hotspot: foo.go", signals2)

	if out1 != out2 {
		t.Errorf("signalHashInputs not deterministic: %q != %q", out1, out2)
	}
}

func TestSignalHashInputs_DifferentInputs(t *testing.T) {
	s1 := signalHashInputs("cat_a", "title_a", []string{"sig1"})
	s2 := signalHashInputs("cat_b", "title_a", []string{"sig1"})
	if s1 == s2 {
		t.Error("different categories should produce different hash inputs")
	}
}

func TestSignalHashInputs_EmptySignals(t *testing.T) {
	out := signalHashInputs("cat", "title", nil)
	if out == "" {
		t.Error("signalHashInputs should not return empty string for nil signals")
	}
}
