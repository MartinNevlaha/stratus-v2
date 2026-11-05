package teams

import "testing"

func TestResolveProfile(t *testing.T) {
	cases := []struct {
		name                          string
		runtime, role, wfType, cmplx  string
		model, effort, reasonContains string
	}{
		{"codex review", "codex", "review", "", "", "gpt-6-astra", "high", "rola review → gpt-6-astra"},
		{"codex review spec-complex", "codex", "delivery-code-reviewer", "spec-complex", "", "gpt-6-astra", "xhigh", "rola review → gpt-6-astra, náročná úloha → xhigh"},
		{"codex design high complexity", "codex", "design", "", "high", "gpt-6-astra", "xhigh", "rola design → gpt-6-astra, náročná úloha → xhigh"},
		{"codex implementation", "codex", "implementation", "spec-complex", "", "gpt-6.1-sol", "high", "predvolené pre rolu implementation"},
		{"codex mechanical", "codex", "mechanical", "", "", "gpt-6-luna", "medium", "rola mechanical → gpt-6-luna"},
		{"claude diagnosis", "claude-code", "diagnosis", "", "", "opus", "high", "rola diagnosis → opus"},
		{"claude mechanical", "claude-code", "mechanical", "", "", "haiku", "medium", "rola mechanical → haiku"},
		{"opencode has no built-in model", "opencode", "review", "", "", "", "", "opencode nemá vstavaný model, treba profil projektu"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, reason := ResolveProfile(c.runtime, c.role, c.wfType, c.cmplx)
			if p.Runtime != c.runtime || p.Model != c.model || p.Effort != c.effort {
				t.Fatalf("profile = %+v", p)
			}
			if reason != c.reasonContains {
				t.Fatalf("reason %q, want %q", reason, c.reasonContains)
			}
			if d := DefaultProfile(c.runtime, c.role, c.wfType, c.cmplx); d != p {
				t.Fatalf("DefaultProfile %+v differs from ResolveProfile %+v", d, p)
			}
		})
	}
}
