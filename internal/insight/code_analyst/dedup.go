package code_analyst

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultMaxAge = 7 * 24 * time.Hour

// Deduplicator filters out files that haven't changed since the last analysis run.
type Deduplicator struct {
	store    CodeAnalystStore
	projRoot string
	maxAge   time.Duration // re-analyze even unchanged files after this duration (default: 7 days)
}

// NewDeduplicator constructs a Deduplicator. A zero maxAge defaults to 7 days.
func NewDeduplicator(store CodeAnalystStore, projRoot string, maxAge time.Duration) *Deduplicator {
	if maxAge <= 0 {
		maxAge = defaultMaxAge
	}
	return &Deduplicator{
		store:    store,
		projRoot: projRoot,
		maxAge:   maxAge,
	}
}

// FilterUnchanged removes files from the input that have the same git hash as
// when they were last analyzed and were analyzed within maxAge. Returns only
// files that need re-analysis.
//
// Individual cache lookup failures are fail-open: the file is included for
// analysis so that a transient DB error does not block progress. A failure in
// ComputeFileHashes is fail-closed and causes the function to return an error.
func (d *Deduplicator) FilterUnchanged(ctx context.Context, files []FileScore) ([]FileScore, error) {
	if len(files) == 0 {
		return []FileScore{}, nil
	}

	hashes, err := d.ComputeFileHashes(ctx, files)
	if err != nil {
		return nil, fmt.Errorf("code analyst: dedup: filter unchanged: %w", err)
	}

	now := time.Now().UTC()
	var result []FileScore

	for _, f := range files {
		hash := hashes[f.FilePath]

		// Populate the current git hash on the score regardless of cache outcome.
		if hash != "" {
			h := hash
			f.LastGitHash = &h
		}

		entry, err := d.store.GetFileCache(f.FilePath)
		if err != nil {
			// Fail-open for individual cache lookups: log and include the file.
			log.Printf("code analyst: dedup: get file cache %q: %v — including for analysis", f.FilePath, err)
			result = append(result, f)
			continue
		}

		if entry == nil {
			// Cache miss → needs analysis.
			result = append(result, f)
			continue
		}

		// Populate LastAnalyzedAt from the cache entry.
		at := entry.LastAnalyzedAt
		f.LastAnalyzedAt = &at

		// Hash changed → needs analysis.
		if entry.GitHash != hash {
			result = append(result, f)
			continue
		}

		// Hash matches — check staleness.
		analyzedAt, parseErr := time.Parse(time.RFC3339Nano, entry.LastAnalyzedAt)
		if parseErr != nil {
			// Unparseable timestamp: treat as stale, include for re-analysis.
			log.Printf("code analyst: dedup: parse last_analyzed_at for %q: %v — treating as stale", f.FilePath, parseErr)
			result = append(result, f)
			continue
		}

		if now.Sub(analyzedAt) > d.maxAge {
			// Stale cache → re-analyze.
			result = append(result, f)
			continue
		}

		// Hash matches and within maxAge → skip.
	}

	return result, nil
}

// ComputeFileHashes returns git object hashes for the given files using
// `git hash-object`. Files are batched into a single invocation for efficiency.
// Files that do not exist on disk are silently skipped (not included in the
// returned map).
func (d *Deduplicator) ComputeFileHashes(ctx context.Context, files []FileScore) (map[string]string, error) {
	if len(files) == 0 {
		return map[string]string{}, nil
	}

	// Separate files into those that exist and those that don't.
	// git hash-object errors on missing files, so we skip them up front.
	type entry struct {
		original string // as provided in FileScore.FilePath
		abs      string // absolute path for os.Stat
	}

	var present []entry
	for _, f := range files {
		abs := f.FilePath
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(d.projRoot, f.FilePath)
		}
		if _, err := os.Stat(abs); err != nil {
			// File missing — skip silently.
			continue
		}
		present = append(present, entry{original: f.FilePath, abs: abs})
	}

	if len(present) == 0 {
		return map[string]string{}, nil
	}

	// Build argument list: git hash-object <file1> <file2> ...
	args := make([]string, 0, len(present)+1)
	args = append(args, "hash-object")
	for _, e := range present {
		args = append(args, e.abs)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = d.projRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("code analyst: dedup: compute file hashes: git hash-object: %w", err)
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(present) {
		return nil, fmt.Errorf("code analyst: dedup: compute file hashes: expected %d hashes, got %d", len(present), len(lines))
	}

	result := make(map[string]string, len(present))
	for i, line := range lines {
		hash := strings.TrimSpace(line)
		if hash != "" {
			result[present[i].original] = hash
		}
	}

	return result, nil
}

// UpdateCache updates the file cache entry after a successful analysis.
func (d *Deduplicator) UpdateCache(path, gitHash, runID string, score float64, findingsCount int) error {
	if err := d.store.SetFileCache(path, gitHash, runID, score, findingsCount); err != nil {
		return fmt.Errorf("code analyst: dedup: update cache %q: %w", path, err)
	}
	return nil
}
