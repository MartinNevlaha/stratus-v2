package db

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Doc represents a governance document chunk.
type Doc struct {
	ID         int64   `json:"id"`
	FilePath   string  `json:"file_path"`
	ChunkIndex int     `json:"chunk_index"`
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	DocType    string  `json:"doc_type"`
	FileHash   string  `json:"file_hash"`
	Project    string  `json:"project"`
	IndexedAt  string  `json:"indexed_at"`
	Score      float64 `json:"score,omitempty"`
}

// docGlobs maps glob patterns to doc types.
var docGlobs = []struct {
	pattern string
	docType string
}{
	{".claude/rules/*.md", "rule"},
	{"docs/decisions/*.md", "adr"},
	{".claude/templates/*.md", "template"},
	{".claude/skills/**/*.md", "skill"},
	{".claude/agents/*.md", "agent"},
	{"docs/architecture/*.md", "architecture"},
	{"**/CLAUDE.md", "project"},
	{"README.md", "project"},
}

// IndexGovernance indexes governance docs from the given project root.
func (d *DB) IndexGovernance(projectRoot string) error {
	for _, g := range docGlobs {
		matches, err := findFiles(projectRoot, g.pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			if err := d.indexFile(path, g.docType, projectRoot); err != nil {
				// Best-effort: log and continue
				_ = err
			}
		}
	}
	// Remove stale entries using SQLite datetime arithmetic (avoids format mismatch).
	_, _ = d.sql.Exec(`DELETE FROM docs WHERE project = ? AND indexed_at < datetime('now', '-24 hours')`,
		projectRoot)
	return nil
}

// findFiles returns files matching relPattern under root.
// Supports ** for recursive directory matching.
func findFiles(root, relPattern string) ([]string, error) {
	if !strings.Contains(relPattern, "**") {
		return filepath.Glob(filepath.Join(root, relPattern))
	}
	// Split on ** to get base dir and file suffix pattern.
	// e.g. ".claude/skills/**/*.md" → base=".claude/skills", suffix="*.md"
	parts := strings.SplitN(relPattern, "**", 2)
	baseDir := filepath.Join(root, filepath.Clean(parts[0]))
	suffix := strings.TrimPrefix(parts[1], string(filepath.Separator))

	var matches []string
	_ = filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		matched, _ := filepath.Match(suffix, d.Name())
		if matched {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, nil
}

func (d *DB) indexFile(path, docType, project string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(content))

	// Check if unchanged
	var existingHash string
	err = d.sql.QueryRow(`SELECT file_hash FROM docs WHERE file_path = ? AND chunk_index = 0 LIMIT 1`, path).Scan(&existingHash)
	if err == nil && existingHash == hash {
		return nil // unchanged
	}

	// Delete old chunks for this file
	_, _ = d.sql.Exec(`DELETE FROM docs WHERE file_path = ?`, path)

	// Chunk by ## headers
	chunks := chunkMarkdown(string(content))
	for i, chunk := range chunks {
		_, err := d.sql.Exec(`
			INSERT OR REPLACE INTO docs (file_path, chunk_index, title, content, doc_type, file_hash, project)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			path, i, chunk.title, chunk.content, docType, hash, project,
		)
		if err != nil {
			return fmt.Errorf("insert doc chunk: %w", err)
		}
	}
	return nil
}

type markdownChunk struct {
	title   string
	content string
}

func chunkMarkdown(content string) []markdownChunk {
	lines := strings.Split(content, "\n")
	var chunks []markdownChunk
	var currentTitle string
	var currentLines []string

	flush := func() {
		text := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if text != "" {
			chunks = append(chunks, markdownChunk{title: currentTitle, content: text})
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			currentTitle = strings.TrimPrefix(line, "## ")
			currentLines = []string{line}
		} else {
			currentLines = append(currentLines, line)
		}
	}
	flush()

	if len(chunks) == 0 {
		// No ## headers — treat entire file as one chunk
		text := strings.TrimSpace(content)
		if text != "" {
			// Extract title from first # header or empty
			title := ""
			for _, line := range lines {
				if strings.HasPrefix(line, "# ") {
					title = strings.TrimPrefix(line, "# ")
					break
				}
			}
			chunks = []markdownChunk{{title: title, content: text}}
		}
	}
	return chunks
}

// SearchDocs searches governance docs using FTS5.
func (d *DB) SearchDocs(query, docType, project string, limit int) ([]Doc, error) {
	if limit <= 0 {
		limit = 10
	}

	var rows *sql.Rows
	var err error

	if query != "" {
		rows, err = d.sql.Query(`
			SELECT d.id, d.file_path, d.chunk_index, d.title, d.content, d.doc_type, d.file_hash, d.project, d.indexed_at,
			       (-rank) AS score
			FROM docs_fts f
			JOIN docs d ON d.id = f.rowid
			WHERE docs_fts MATCH ?
			  AND (? = '' OR d.doc_type = ?)
			  AND (? = '' OR d.project = ?)
			ORDER BY rank
			LIMIT ?`,
			query,
			docType, docType,
			project, project,
			limit,
		)
	} else {
		rows, err = d.sql.Query(`
			SELECT id, file_path, chunk_index, title, content, doc_type, file_hash, project, indexed_at, 0.0
			FROM docs
			WHERE (? = '' OR doc_type = ?)
			  AND (? = '' OR project = ?)
			ORDER BY indexed_at DESC
			LIMIT ?`,
			docType, docType,
			project, project,
			limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("search docs: %w", err)
	}
	defer rows.Close()

	var docs []Doc
	for rows.Next() {
		var doc Doc
		if err := rows.Scan(&doc.ID, &doc.FilePath, &doc.ChunkIndex, &doc.Title,
			&doc.Content, &doc.DocType, &doc.FileHash, &doc.Project, &doc.IndexedAt, &doc.Score); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

// GovernanceStats returns indexing statistics.
func (d *DB) GovernanceStats() (map[string]any, error) {
	var total int
	_ = d.sql.QueryRow(`SELECT COUNT(*) FROM docs`).Scan(&total)

	var byType []map[string]any
	rows, err := d.sql.Query(`SELECT doc_type, COUNT(*) FROM docs GROUP BY doc_type ORDER BY doc_type`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t string
			var c int
			if err := rows.Scan(&t, &c); err == nil {
				byType = append(byType, map[string]any{"type": t, "count": c})
			}
		}
	}

	return map[string]any{
		"total_chunks": total,
		"by_type":      byType,
	}, nil
}
