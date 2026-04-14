package wiki_engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/google/uuid"
)

// PullStatus summarises a PullAll run.
type PullStatus struct {
	FilesScanned  int      `json:"files_scanned"`
	PagesUpdated  int      `json:"pages_updated"`
	PagesCreated  int      `json:"pages_created"`
	Conflicts     int      `json:"conflicts"`
	Errors        []string `json:"errors"`
	DurationMs    int64    `json:"duration_ms"`
}

// frontmatterRe matches the YAML frontmatter block at the very top of a file.
var frontmatterRe = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n(.*)`)

// PullAll walks the vault, parses frontmatter from every .md file and writes
// back changes to the DB. Conflicts (file version < DB version while content
// differs) are moved to <vault>/_conflicts/.
func (v *VaultSync) PullAll(ctx context.Context) (*PullStatus, error) {
	start := time.Now()
	st := &PullStatus{}

	if v.vaultPath == "" {
		return nil, errors.New("vault pull: vault_path not configured")
	}

	err := filepath.WalkDir(v.vaultPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			st.Errors = append(st.Errors, walkErr.Error())
			return nil
		}
		if d.IsDir() {
			// Skip the conflicts directory and the inbox (inbox is ingest's job).
			name := d.Name()
			if name == "_conflicts" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		st.FilesScanned++

		created, updated, conflict, err := v.PullPage(ctx, path)
		if err != nil {
			st.Errors = append(st.Errors, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if created {
			st.PagesCreated++
		} else if updated {
			st.PagesUpdated++
		}
		if conflict {
			st.Conflicts++
		}
		return nil
	})
	st.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		return st, err
	}
	return st, nil
}

// PullPage reads a single vault .md file and reconciles it with the DB.
// Return semantics: created/updated/conflict flags.
func (v *VaultSync) PullPage(ctx context.Context, path string) (created, updated, conflict bool, err error) {
	_ = ctx
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false, false, fmt.Errorf("vault pull: read: %w", err)
	}
	fm, body, ok := parseFrontmatter(string(data))
	if !ok {
		// No frontmatter — treat as a user-authored new page.
		return v.createFromBody(path, string(data))
	}

	id := fm["id"]
	if id == "" {
		return v.createFromBody(path, body)
	}

	existing, err := v.store.GetPage(id)
	if err != nil {
		return false, false, false, fmt.Errorf("vault pull: get page %s: %w", id, err)
	}
	if existing == nil {
		return v.createWithID(id, fm, body)
	}

	fileUpdated, _ := parseTimeLoose(fm["updated_at"])
	dbUpdated, _ := parseTimeLoose(existing.UpdatedAt)
	fileVersion, _ := strconv.Atoi(fm["version"])

	contentChanged := strings.TrimSpace(body) != strings.TrimSpace(existing.Content)
	if !contentChanged {
		return false, false, false, nil
	}

	// Conflict: file is older (or same) AND version mismatched.
	if !fileUpdated.IsZero() && !dbUpdated.IsZero() && !fileUpdated.After(dbUpdated) && fileVersion < existing.Version {
		if err := v.writeConflict(path, string(data)); err != nil {
			slog.Warn("vault pull: conflict write failed", "path", path, "err", err)
		}
		return false, false, true, nil
	}

	existing.Content = strings.TrimLeft(body, "\n\r\t ")
	existing.Version++
	existing.GeneratedBy = db.GeneratedByUserEdit
	if title := fm["title"]; title != "" {
		existing.Title = title
	}
	if pt := fm["page_type"]; pt != "" {
		existing.PageType = pt
	}
	if err := v.store.UpdatePage(existing); err != nil {
		return false, false, false, fmt.Errorf("vault pull: update: %w", err)
	}
	return false, true, false, nil
}

func (v *VaultSync) createFromBody(path, body string) (created, updated, conflict bool, err error) {
	title := strings.TrimSuffix(filepath.Base(path), ".md")
	page := &db.WikiPage{
		ID:          uuid.NewString(),
		PageType:    db.PageTypeConcept,
		Title:       title,
		Content:     body,
		Status:      "published",
		GeneratedBy: db.GeneratedByUserEdit,
		Version:     1,
	}
	if err := v.store.SavePage(page); err != nil {
		return false, false, false, fmt.Errorf("vault pull: save: %w", err)
	}
	return true, false, false, nil
}

func (v *VaultSync) createWithID(id string, fm map[string]string, body string) (created, updated, conflict bool, err error) {
	pt := fm["page_type"]
	if pt == "" {
		pt = db.PageTypeConcept
	}
	title := fm["title"]
	if title == "" {
		title = id
	}
	page := &db.WikiPage{
		ID:          id,
		PageType:    pt,
		Title:       title,
		Content:     body,
		Status:      "published",
		GeneratedBy: db.GeneratedByUserEdit,
		Version:     1,
	}
	if err := v.store.SavePage(page); err != nil {
		return false, false, false, fmt.Errorf("vault pull: save id %s: %w", id, err)
	}
	return true, false, false, nil
}

func (v *VaultSync) writeConflict(origPath, content string) error {
	dir := filepath.Join(v.vaultPath, "_conflicts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ts := time.Now().UTC().Format("20060102-150405")
	name := ts + "-" + filepath.Base(origPath)
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}

// parseFrontmatter extracts simple key: value lines from a YAML frontmatter
// block. It ignores nested structures and list items — we only need the scalar
// metadata the engine writes out.
func parseFrontmatter(s string) (map[string]string, string, bool) {
	m := frontmatterRe.FindStringSubmatch(s)
	if len(m) != 3 {
		return nil, "", false
	}
	out := make(map[string]string)
	for _, line := range strings.Split(m[1], "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "- ") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		val := strings.TrimSpace(line[colon+1:])
		val = strings.Trim(val, `"`)
		if key == "" || val == "" {
			continue
		}
		out[key] = val
	}
	return out, m[2], true
}

func parseTimeLoose(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable time %q", s)
}
