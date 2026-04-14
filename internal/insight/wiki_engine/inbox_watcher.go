package wiki_engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// InboxIngester is the subset of ingest.Ingester needed by InboxWatcher.
// Defined as an interface here so the watcher package doesn't import
// internal/insight/ingest (which would create an import cycle).
type InboxIngester interface {
	IngestFile(ctx context.Context, path string, tags []string) (rawPageID string, err error)
}

// InboxWatcher watches <vaultPath>/<inboxDir> for new files and auto-ingests
// them. Processed files are moved to <vaultPath>/01-Raw-Sources/ to prevent
// re-ingestion on restart.
type InboxWatcher struct {
	vaultPath string
	inboxDir  string
	archiveDir string
	ingester  InboxIngester
	debounce  time.Duration

	mu      sync.Mutex
	pending map[string]*time.Timer
}

// InboxWatcherConfig is the constructor parameter bundle.
type InboxWatcherConfig struct {
	VaultPath  string
	InboxDir   string        // default "00-Inbox"
	ArchiveDir string        // default "01-Raw-Sources"
	Debounce   time.Duration // default 2s
	Ingester   InboxIngester
}

// NewInboxWatcher constructs a watcher. It does not start the goroutine — call Run.
func NewInboxWatcher(cfg InboxWatcherConfig) *InboxWatcher {
	if cfg.InboxDir == "" {
		cfg.InboxDir = "00-Inbox"
	}
	if cfg.ArchiveDir == "" {
		cfg.ArchiveDir = "01-Raw-Sources"
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = 2 * time.Second
	}
	return &InboxWatcher{
		vaultPath:  cfg.VaultPath,
		inboxDir:   cfg.InboxDir,
		archiveDir: cfg.ArchiveDir,
		ingester:   cfg.Ingester,
		debounce:   cfg.Debounce,
		pending:    make(map[string]*time.Timer),
	}
}

// Run blocks until ctx is cancelled. It creates the inbox directory if missing.
func (w *InboxWatcher) Run(ctx context.Context) error {
	if w.vaultPath == "" {
		return fmt.Errorf("inbox watcher: vault path empty")
	}
	if w.ingester == nil {
		return fmt.Errorf("inbox watcher: no ingester")
	}

	absInbox := filepath.Join(w.vaultPath, w.inboxDir)
	if err := os.MkdirAll(absInbox, 0o755); err != nil {
		return fmt.Errorf("inbox watcher: mkdir: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("inbox watcher: new watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(absInbox); err != nil {
		return fmt.Errorf("inbox watcher: add path %s: %w", absInbox, err)
	}
	slog.Info("inbox watcher: running", "path", absInbox, "debounce", w.debounce)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if ev.Has(fsnotify.Create) || ev.Has(fsnotify.Write) {
				w.scheduleIngest(ctx, ev.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			slog.Warn("inbox watcher: error", "err", err)
		}
	}
}

func (w *InboxWatcher) scheduleIngest(ctx context.Context, path string) {
	if strings.HasPrefix(filepath.Base(path), ".") {
		return // ignore hidden/dotfiles (editor swap files)
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.pending[path]; ok {
		t.Reset(w.debounce)
		return
	}
	w.pending[path] = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()
		w.ingestOne(ctx, path)
	})
}

func (w *InboxWatcher) ingestOne(ctx context.Context, path string) {
	info, err := os.Stat(path)
	if err != nil {
		return // file may have been moved/deleted
	}
	if info.IsDir() {
		return
	}

	rawID, err := w.ingester.IngestFile(ctx, path, []string{"inbox"})
	if err != nil {
		slog.Warn("inbox watcher: ingest failed", "path", path, "err", err)
		return
	}
	slog.Info("inbox watcher: ingested", "path", path, "raw_page_id", rawID)

	if err := w.archive(path); err != nil {
		slog.Warn("inbox watcher: archive failed", "path", path, "err", err)
	}
}

func (w *InboxWatcher) archive(path string) error {
	dest := filepath.Join(w.vaultPath, w.archiveDir)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	target := filepath.Join(dest, filepath.Base(path))
	// Avoid overwriting existing archives.
	if _, err := os.Stat(target); err == nil {
		ext := filepath.Ext(target)
		base := strings.TrimSuffix(target, ext)
		target = fmt.Sprintf("%s-%d%s", base, time.Now().Unix(), ext)
	}
	return os.Rename(path, target)
}
