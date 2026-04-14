package wiki_engine

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeIngester struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeIngester) IngestFile(_ context.Context, path string, _ []string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, path)
	f.mu.Unlock()
	return "raw-id", nil
}

func (f *fakeIngester) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestInboxWatcher_IngestsAndArchives(t *testing.T) {
	vault := t.TempDir()
	fi := &fakeIngester{}
	w := NewInboxWatcher(InboxWatcherConfig{
		VaultPath: vault,
		Ingester:  fi,
		Debounce:  100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(done)
	}()

	// Wait for watcher to init.
	time.Sleep(200 * time.Millisecond)

	path := filepath.Join(vault, "00-Inbox", "note.md")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fi.count() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if fi.count() == 0 {
		t.Fatal("ingester never called")
	}

	// Give archive a moment.
	time.Sleep(200 * time.Millisecond)
	archived := filepath.Join(vault, "01-Raw-Sources", "note.md")
	if _, err := os.Stat(archived); err != nil {
		t.Errorf("archive not written: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("original should be moved, got err=%v", err)
	}

	cancel()
	<-done
}
