package main

import "testing"

func TestMCPRuntimeFlag_LegacyDefault(t *testing.T) {
	o, e := parseMCPRuntime(nil)
	if e != nil || o.Runtime != "" {
		t.Fatal(o, e)
	}
	o, e = parseMCPRuntime([]string{"--runtime", "codex"})
	if e != nil || o.Runtime != "codex" {
		t.Fatal(o, e)
	}
	if _, e = parseMCPRuntime([]string{"--runtime", "unknown"}); e == nil {
		t.Fatal("unknown accepted")
	}
}
