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
