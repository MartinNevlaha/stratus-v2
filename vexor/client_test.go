package vexor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexUsesConfiguredTimeout(t *testing.T) {
	dir := t.TempDir()
	fakeVexor := filepath.Join(dir, "vexor")
	script := "#!/bin/sh\nif [ \"$1\" = \"index\" ]; then exec sleep 2; fi\n"
	if err := os.WriteFile(fakeVexor, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake vexor: %v", err)
	}

	client := New(fakeVexor, "", 15, 1)
	err := client.Index(nil)
	if err == nil {
		t.Fatal("expected index timeout")
	}
	if !strings.Contains(err.Error(), "vexor index timeout after 1s") {
		t.Fatalf("expected configured timeout in error, got %q", err.Error())
	}
}

func fakeVexorRecordingArgs(t *testing.T) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "vexor")
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake vexor: %v", err)
	}
	return bin, argsFile
}

func TestSearch_PassesProjectRoot(t *testing.T) {
	bin, argsFile := fakeVexorRecordingArgs(t)
	client := New(bin, "", 15, 15).WithRoot("/srv/project")
	if _, err := client.Search("query", 3, "auto"); err != nil {
		t.Fatalf("search: %v", err)
	}
	got, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(got), "--path /srv/project") {
		t.Fatalf("expected --path /srv/project in args, got %q", got)
	}
}

func TestIndex_PassesProjectRoot(t *testing.T) {
	bin, argsFile := fakeVexorRecordingArgs(t)
	client := New(bin, "", 15, 15).WithRoot("/srv/project")
	if err := client.Index(nil); err != nil {
		t.Fatalf("index: %v", err)
	}
	got, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(got), "index --path /srv/project") {
		t.Fatalf("expected index --path /srv/project, got %q", got)
	}
}

func TestSearch_NoRoot_OmitsPath(t *testing.T) {
	bin, argsFile := fakeVexorRecordingArgs(t)
	client := New(bin, "", 15, 15)
	if _, err := client.Search("query", 3, "auto"); err != nil {
		t.Fatalf("search: %v", err)
	}
	got, _ := os.ReadFile(argsFile)
	if strings.Contains(string(got), "--path") {
		t.Fatalf("expected no --path without root, got %q", got)
	}
}

func TestParsePorcelain_UnavailableLines(t *testing.T) {
	out := "1\t0.899\t./a.ts\t32\t162\t163\tfoo :: bar\n3\t0.896\t./B.svelte\t0\t-\t-\tB.svelte\n"
	got := parsePorcelain(out)
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0].LineStart != 162 || got[0].Heading != "foo" || got[0].Excerpt != "bar" {
		t.Fatalf("unexpected first result: %+v", got[0])
	}
	if got[1].LineStart != 0 || got[1].Heading != "B.svelte" {
		t.Fatalf("unexpected second result: %+v", got[1])
	}
}
