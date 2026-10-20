package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestCodexConfig_PreservesSource(t *testing.T) {
	src := []byte("# user comment\r\nmodel = 'user-choice'\r\n")
	got, e := mergeCodexTOML(src)
	if e != nil || !bytes.HasPrefix(got, src) {
		t.Fatal(string(got), e)
	}
	again, e := mergeCodexTOML(got)
	if e != nil || !bytes.Equal(got, again) {
		t.Fatal(e, string(again))
	}
}
func TestCodexConfig_DisabledEntry(t *testing.T) {
	src := []byte("[mcp_servers.stratus]\ncommand='stratus'\nargs=['mcp-serve']\nenabled=false # keep disabled\n")
	got, e := mergeCodexTOML(src)
	if e != nil || !bytes.Contains(got, []byte("enabled=false # keep disabled")) || !bytes.Contains(got, []byte("codex")) {
		t.Fatal(string(got), e)
	}
}
func TestCodexConfig_AlternateTOML(t *testing.T) {
	for _, src := range []string{"[mcp_servers.\"stratus\"]\ncommand='stratus'\nargs=[\n 'mcp-serve',\n]\n", `mcp_servers.stratus = { command='stratus',args=['mcp-serve'] }`} {
		got, e := mergeCodexTOML([]byte(src))
		if e != nil || !bytes.Contains(got, []byte("codex")) {
			t.Fatal(string(got), e)
		}
	}
}
func TestCodexConfig_Conflict(t *testing.T) {
	for _, s := range []string{"[invalid", `[mcp_servers.stratus]
url='https://unrelated.test/mcp'`, `[hooks]
PreToolUse=[{hooks=[{command='stratus codex-hook guard'}]}]`} {
		if _, e := mergeCodexTOML([]byte(s)); e == nil {
			t.Fatal("conflict accepted", s)
		}
	}
}
func TestCodexHooks_IdempotentMerge(t *testing.T) {
	src := []byte(`{"user_meta":true,"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo stratus codex-hook guard"},{"type":"command","command":"stratus codex-hook guard"}]}]}}`)
	generated := []byte(`{"hooks":{"PreToolUse":[{"matcher":".*","hooks":[{"type":"command","command":"stratus codex-hook guard"}]}]}}`)
	got, e := mergeCodexHooks(src, generated)
	if e != nil {
		t.Fatal(e)
	}
	again, e := mergeCodexHooks(got, generated)
	var a, b map[string]any
	json.Unmarshal(got, &a)
	json.Unmarshal(again, &b)
	if e != nil || !reflect.DeepEqual(a, b) || a["user_meta"] != true || !bytes.Contains(got, []byte("echo stratus")) {
		t.Fatal(string(got), string(again), e)
	}
}
