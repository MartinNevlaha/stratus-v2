package wiki_engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// VaultStatus holds the current state of the Obsidian vault sync.
type VaultStatus struct {
	LastSync  *string  `json:"last_sync"`
	FileCount int      `json:"file_count"`
	VaultPath string   `json:"vault_path"`
	Errors    []string `json:"errors"`
}

// writeCounter is a package-level atomic counter used to generate unique tmp
// filenames for concurrent atomic writes.
var writeCounter atomic.Uint64

// VaultSync writes wiki pages from the database to an Obsidian vault on disk.
type VaultSync struct {
	store     WikiStore
	vaultPath string
	lastSync  *time.Time
	mu        sync.Mutex
}

// NewVaultSync creates a VaultSync backed by the given store and vault path.
func NewVaultSync(store WikiStore, vaultPath string) *VaultSync {
	return &VaultSync{store: store, vaultPath: vaultPath}
}

// SyncAll exports all published wiki pages to the vault directory.
// Each page is written to a subdirectory based on its type and converted to
// Obsidian-compatible markdown with YAML frontmatter.
// Non-fatal per-page errors are collected in VaultStatus.Errors; only a failure
// to list pages from the store is returned as a fatal error.
func (v *VaultSync) SyncAll(_ context.Context) (*VaultStatus, error) {
	pages, _, err := v.store.ListPages(db.WikiPageFilters{
		Status: "published",
		Limit:  10000,
	})
	if err != nil {
		return nil, fmt.Errorf("vault sync: list pages: %w", err)
	}

	// Fetch all pages once for wikilink conversion (best-effort; ignore error).
	allPages, _, _ := v.store.ListPages(db.WikiPageFilters{Limit: 10000})

	var collectedErrs []string
	fileCount := 0

	for i := range pages {
		page := &pages[i]

		refs, err := v.store.ListRefs(page.ID)
		if err != nil {
			slog.Warn("vault sync: list refs", "page_id", page.ID, "err", err)
			refs = nil
		}

		if err := v.SyncPage(page, refs, allPages); err != nil {
			slog.Warn("vault sync: write page", "page_id", page.ID, "err", err)
			collectedErrs = append(collectedErrs, err.Error())
			continue
		}
		fileCount++
	}

	now := time.Now()
	v.mu.Lock()
	v.lastSync = &now
	v.mu.Unlock()

	status := &VaultStatus{
		FileCount: fileCount,
		VaultPath: v.vaultPath,
		Errors:    collectedErrs,
	}
	ts := now.UTC().Format(time.RFC3339)
	status.LastSync = &ts

	return status, nil
}

// SyncPage converts a single wiki page to Obsidian markdown and writes it to
// the vault atomically (write to a .tmp file then rename).
// It creates parent directories as needed.
func (v *VaultSync) SyncPage(page *db.WikiPage, refs []db.WikiPageRef, linkedPages []db.WikiPage) error {
	content := PageToObsidian(page, refs, linkedPages)

	relPath := PageToVaultPath(page)
	absPath := filepath.Join(v.vaultPath, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("vault sync: mkdir %s: %w", filepath.Dir(absPath), err)
	}

	// Use a unique tmp filename per write to prevent concurrent goroutines from
	// clobbering each other's in-progress temp file before the rename.
	seq := writeCounter.Add(1)
	tmpPath := fmt.Sprintf("%s.%d.tmp", absPath, seq)
	if err := os.WriteFile(tmpPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("vault sync: write tmp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		// Best-effort cleanup of the temp file; ignore cleanup error.
		_ = os.Remove(tmpPath)
		return fmt.Errorf("vault sync: rename %s: %w", absPath, err)
	}
	return nil
}

// GetStatus returns the current vault sync status without triggering a sync.
// It counts all .md files currently present in the vault directory.
func (v *VaultSync) GetStatus() (*VaultStatus, error) {
	v.mu.Lock()
	lastSync := v.lastSync
	v.mu.Unlock()

	status := &VaultStatus{
		VaultPath: v.vaultPath,
		Errors:    []string{},
	}

	if lastSync != nil {
		ts := lastSync.UTC().Format(time.RFC3339)
		status.LastSync = &ts
	}

	if v.vaultPath == "" {
		return status, nil
	}

	count := 0
	err := filepath.Walk(v.vaultPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries non-fatally
		}
		if !info.IsDir() && filepath.Ext(path) == ".md" {
			count++
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("vault sync: walk vault: %w", err)
	}
	status.FileCount = count
	return status, nil
}

// EnsureVaultDirs creates the vault root and its standard subdirectories:
// summaries, entities, concepts, and answers.
func (v *VaultSync) EnsureVaultDirs() error {
	dirs := []string{
		v.vaultPath,
		filepath.Join(v.vaultPath, "summaries"),
		filepath.Join(v.vaultPath, "entities"),
		filepath.Join(v.vaultPath, "concepts"),
		filepath.Join(v.vaultPath, "answers"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("vault sync: ensure dir %s: %w", dir, err)
		}
	}
	return nil
}
