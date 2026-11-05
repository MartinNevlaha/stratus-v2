package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSave_ConcurrentWritesStayValidAndLeaveNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".stratus.json")
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				cfg := Default()
				cfg.Language = "sk"
				cfg.Port = 40000 + g*100 + i
				if err := cfg.Save(path); err != nil {
					t.Errorf("save: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("file is corrupt after concurrent saves: %v\n%s", err, raw)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("stray files after saves: %v", names)
	}
}

func TestSave_KeepsExistingFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".stratus.json")
	// 0640 is neither the 0600 a new file gets nor the 0644 the old Save wrote,
	// so only real preservation keeps it.
	if err := os.WriteFile(path, []byte(`{}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640", info.Mode().Perm())
	}
}

func TestSave_NewFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".stratus.json")
	cfg := Default()
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestSave_FailureLeavesNoTempFileAndWrapsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing-dir", ".stratus.json")
	cfg := Default()
	err := cfg.Save(path)
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("unexpected files: %v", entries)
	}
}

func TestWithWriteLock_SerializesCallers(t *testing.T) {
	var inside, maxInside atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = WithWriteLock(func() error {
				n := inside.Add(1)
				for {
					m := maxInside.Load()
					if n <= m || maxInside.CompareAndSwap(m, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				inside.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()
	if got := maxInside.Load(); got != 1 {
		t.Fatalf("%d callers were inside the lock at once, want 1", got)
	}
}

func TestWithWriteLock_ExcludesSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".stratus.json")
	cfg := Default()
	saved := make(chan struct{})
	err := WithWriteLock(func() error {
		go func() {
			_ = cfg.Save(path)
			close(saved)
		}()
		select {
		case <-saved:
			t.Error("Save completed while the write lock was held")
		case <-time.After(50 * time.Millisecond):
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-saved:
	case <-time.After(2 * time.Second):
		t.Fatal("Save never completed after the lock was released")
	}
}

func TestWithWriteLock_PropagatesError(t *testing.T) {
	want := errors.New("boom")
	if got := WithWriteLock(func() error { return want }); !errors.Is(got, want) {
		t.Fatalf("error not propagated: %v", got)
	}
}

func TestWriteFileAtomic_WritesThroughSymlinkAndKeepsLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "stratus.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".stratus.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cfg := Default()
	cfg.Port = 41999
	if err := cfg.Save(link); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("Save replaced the symlink with a regular file")
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(raw, &back); err != nil || back.Port != 41999 {
		t.Fatalf("link target not updated: %v %s", err, raw)
	}
	if ti, _ := os.Stat(target); ti.Mode().Perm() != 0o640 {
		t.Fatalf("target mode = %v, want 0640", ti.Mode().Perm())
	}
	for _, d := range []string{dir, filepath.Dir(target)} {
		leftovers, _ := filepath.Glob(filepath.Join(d, "*.tmp"))
		if len(leftovers) != 0 {
			t.Fatalf("temp files left in %s: %v", d, leftovers)
		}
	}
}

func TestWriteFileAtomic_RenameFailureRemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".stratus.json")
	if err := os.Mkdir(path, 0o755); err != nil { // rename of a file onto a directory fails
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte(`{}`)); err == nil {
		t.Fatal("expected rename to fail")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != ".stratus.json" {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestWriteFileAtomic_ReadOnlyDirFailsCleanly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".stratus.json")
	if err := os.WriteFile(path, []byte(`{"port":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if err := WriteFileAtomic(path, []byte(`{}`)); err == nil {
		t.Fatal("expected error in a read-only directory")
	}
	os.Chmod(dir, 0o700)
	if raw, _ := os.ReadFile(path); string(raw) != `{"port":1}` {
		t.Fatalf("original file changed: %s", raw)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("stray files: %v", entries)
	}
}
