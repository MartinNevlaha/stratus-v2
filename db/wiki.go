package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AllowedWikiLinkTypes lists the valid edge types for wiki_links.link_type.
// This is the single source of truth — both the API handler and the wiki_engine
// suggester must consume this map via IsValidWikiLinkType.
var AllowedWikiLinkTypes = map[string]struct{}{
	"related":     {},
	"parent":      {},
	"child":       {},
	"contradicts": {},
	"supersedes":  {},
	"cites":       {},
}

// IsValidWikiLinkType returns true if t (after trim + lowercase) is in the allowlist.
func IsValidWikiLinkType(t string) bool {
	_, ok := AllowedWikiLinkTypes[strings.ToLower(strings.TrimSpace(t))]
	return ok
}

const wikiStalenessThreshold = 0.7

// Page type constants. page_type in DB is a free string — these are the
// canonical values used throughout the codebase.
const (
	PageTypeSummary = "summary"
	PageTypeEntity  = "entity"
	PageTypeConcept = "concept"
	PageTypeAnswer  = "answer"
	PageTypeIndex   = "index"
	PageTypeFeature = "feature"
	PageTypeRaw     = "raw"   // Karpathy raw layer: unprocessed ingested source
	PageTypeTopic   = "topic" // Clustered synthesis of ≥N raw pages
)

// GeneratedBy constants.
const (
	GeneratedByIngest        = "ingest"
	GeneratedByQuery         = "query"
	GeneratedByMaintenance   = "maintenance"
	GeneratedByEvolution     = "evolution"
	GeneratedByUserEdit      = "user_edit"
	GeneratedByLinkSuggester = "link_suggester"
	GeneratedByCluster       = "cluster"
	GeneratedByWorkflow      = "workflow"
)

// WikiPage is a LLM-generated knowledge page.
type WikiPage struct {
	ID             string         `json:"id"`
	PageType       string         `json:"page_type"`      // summary | entity | concept | answer | index
	Title          string         `json:"title"`
	Content        string         `json:"content"`
	Status         string         `json:"status"`         // draft | published | stale | archived
	StalenessScore float64        `json:"staleness_score"`
	SourceHashes   []string       `json:"source_hashes"`
	Tags           []string       `json:"tags"`
	Metadata       map[string]any `json:"metadata"`
	GeneratedBy    string         `json:"generated_by"`   // ingest | query | maintenance | evolution
	Version        int            `json:"version"`
	WorkflowID     string         `json:"workflow_id,omitempty"`
	FeatureSlug    string         `json:"feature_slug,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

// WikiLink is a directed relationship between two wiki pages.
type WikiLink struct {
	ID         string  `json:"id"`
	FromPageID string  `json:"from_page_id"`
	ToPageID   string  `json:"to_page_id"`
	LinkType   string  `json:"link_type"` // related | parent | child | contradicts | supersedes | cites
	Strength   float64 `json:"strength"`
	CreatedAt  string  `json:"created_at"`
}

// WikiPageRef is a citation from a wiki page to a raw source.
type WikiPageRef struct {
	ID         string `json:"id"`
	PageID     string `json:"page_id"`
	SourceType string `json:"source_type"` // event | trajectory | artifact | solution_pattern | problem_stats
	SourceID   string `json:"source_id"`
	Excerpt    string `json:"excerpt"`
	CreatedAt  string `json:"created_at"`
}

// WikiPageFilters controls ListWikiPages filtering.
type WikiPageFilters struct {
	PageType string
	Status   string
	Query    string
	Tag      string
	Limit    int
	Offset   int
}

// SaveWikiPage inserts a new wiki page. Generates a UUID if ID is empty.
func (d *DB) SaveWikiPage(p *WikiPage) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}

	hashesBytes, err := json.Marshal(p.SourceHashes)
	if err != nil {
		return fmt.Errorf("save wiki page: marshal source_hashes: %w", err)
	}
	if p.SourceHashes == nil {
		hashesBytes = []byte("[]")
	}

	tagsBytes, err := json.Marshal(p.Tags)
	if err != nil {
		return fmt.Errorf("save wiki page: marshal tags: %w", err)
	}
	if p.Tags == nil {
		tagsBytes = []byte("[]")
	}

	metaBytes, err := json.Marshal(p.Metadata)
	if err != nil {
		return fmt.Errorf("save wiki page: marshal metadata: %w", err)
	}
	if p.Metadata == nil {
		metaBytes = []byte("{}")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if p.Version == 0 {
		p.Version = 1
	}

	_, err = d.sql.Exec(`
		INSERT INTO wiki_pages
		(id, page_type, title, content, status, staleness_score,
		 source_hashes_json, tags_json, metadata_json, generated_by, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		p.ID, p.PageType, p.Title, p.Content, p.Status, p.StalenessScore,
		string(hashesBytes), string(tagsBytes), string(metaBytes),
		p.GeneratedBy, p.Version, now, now,
	)
	if err != nil {
		return fmt.Errorf("save wiki page: %w", err)
	}
	p.CreatedAt = now
	p.UpdatedAt = now
	return nil
}

// UpdateWikiPage updates mutable fields of an existing wiki page and increments the version.
func (d *DB) UpdateWikiPage(p *WikiPage) error {
	hashesBytes, err := json.Marshal(p.SourceHashes)
	if err != nil {
		return fmt.Errorf("update wiki page: marshal source_hashes: %w", err)
	}
	if p.SourceHashes == nil {
		hashesBytes = []byte("[]")
	}

	tagsBytes, err := json.Marshal(p.Tags)
	if err != nil {
		return fmt.Errorf("update wiki page: marshal tags: %w", err)
	}
	if p.Tags == nil {
		tagsBytes = []byte("[]")
	}

	metaBytes, err := json.Marshal(p.Metadata)
	if err != nil {
		return fmt.Errorf("update wiki page: marshal metadata: %w", err)
	}
	if p.Metadata == nil {
		metaBytes = []byte("{}")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = d.sql.Exec(`
		UPDATE wiki_pages
		SET content = ?, status = ?, staleness_score = ?,
		    source_hashes_json = ?, tags_json = ?, metadata_json = ?,
		    version = version + 1, updated_at = ?
		WHERE id = ?
	`,
		p.Content, p.Status, p.StalenessScore,
		string(hashesBytes), string(tagsBytes), string(metaBytes),
		now, p.ID,
	)
	if err != nil {
		return fmt.Errorf("update wiki page: %w", err)
	}
	p.UpdatedAt = now
	p.Version++
	return nil
}

// GetWikiPage returns a single wiki page by ID, or (nil, nil) if not found.
func (d *DB) GetWikiPage(id string) (*WikiPage, error) {
	row := d.sql.QueryRow(`
		SELECT id, page_type, title, content, status, staleness_score,
		       source_hashes_json, tags_json, metadata_json, generated_by, version, created_at, updated_at
		FROM wiki_pages
		WHERE id = ?
	`, id)
	return scanWikiPage(row)
}

// ListWikiPages returns a filtered page of wiki pages plus the total match count.
func (d *DB) ListWikiPages(f WikiPageFilters) ([]WikiPage, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}

	where := "WHERE 1=1"
	args := []any{}

	if f.PageType != "" {
		where += " AND page_type = ?"
		args = append(args, f.PageType)
	}
	if f.Status != "" {
		where += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.Tag != "" {
		// Tags are stored as a JSON array; use LIKE for a lightweight membership check.
		where += ` AND tags_json LIKE ?`
		args = append(args, "%"+f.Tag+"%")
	}

	var total int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM wiki_pages `+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("list wiki pages count: %w", err)
	}

	query := `
		SELECT id, page_type, title, content, status, staleness_score,
		       source_hashes_json, tags_json, metadata_json, generated_by, version, created_at, updated_at
		FROM wiki_pages ` + where + ` ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list wiki pages: %w", err)
	}
	defer rows.Close()

	pages, err := scanWikiPages(rows)
	if err != nil {
		return nil, 0, err
	}
	return pages, total, nil
}

// SearchWikiPages performs an FTS5 full-text search over wiki pages.
func (d *DB) SearchWikiPages(query string, pageType string, limit int) ([]WikiPage, error) {
	if limit <= 0 {
		limit = 20
	}

	ftsQuery := buildFTS5Query(query)
	if ftsQuery == "" {
		return []WikiPage{}, nil
	}

	rows, err := d.sql.Query(`
		SELECT p.id, p.page_type, p.title, p.content, p.status, p.staleness_score,
		       p.source_hashes_json, p.tags_json, p.metadata_json, p.generated_by, p.version,
		       p.created_at, p.updated_at
		FROM wiki_pages_fts f
		JOIN wiki_pages p ON p.rowid = f.rowid
		WHERE wiki_pages_fts MATCH ?
		  AND (? = '' OR p.page_type = ?)
		ORDER BY rank
		LIMIT ?
	`, ftsQuery, pageType, pageType, limit)
	if err != nil {
		return nil, fmt.Errorf("search wiki pages: %w", err)
	}
	defer rows.Close()

	return scanWikiPages(rows)
}

// DeleteWikiPage deletes a wiki page by ID (cascades to links and refs).
func (d *DB) DeleteWikiPage(id string) error {
	_, err := d.sql.Exec(`DELETE FROM wiki_pages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete wiki page: %w", err)
	}
	return nil
}

// UpdateWikiPageStaleness updates the staleness score and sets status to "stale"
// when the score exceeds wikiStalenessThreshold.
func (d *DB) UpdateWikiPageStaleness(id string, score float64) error {
	status := "published"
	if score > wikiStalenessThreshold {
		status = "stale"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := d.sql.Exec(`
		UPDATE wiki_pages
		SET staleness_score = ?, status = ?, updated_at = ?
		WHERE id = ?
	`, score, status, now, id)
	if err != nil {
		return fmt.Errorf("update wiki page staleness: %w", err)
	}
	return nil
}

// CountOrphanWikiPages returns the number of wiki pages that have no links
// (neither as source nor as target).
func (d *DB) CountOrphanWikiPages() (int, error) {
	var count int
	err := d.sql.QueryRow(`
		SELECT COUNT(*) FROM wiki_pages p
		WHERE NOT EXISTS (
			SELECT 1 FROM wiki_links
			WHERE from_page_id = p.id OR to_page_id = p.id
		)
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count orphan wiki pages: %w", err)
	}
	return count, nil
}

// SaveWikiLink inserts or replaces a wiki link (upserts on the UNIQUE constraint,
// updating strength on conflict).
func (d *DB) SaveWikiLink(l *WikiLink) error {
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := d.sql.Exec(`
		INSERT INTO wiki_links (id, from_page_id, to_page_id, link_type, strength, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(from_page_id, to_page_id, link_type) DO UPDATE SET
			strength = excluded.strength
	`, l.ID, l.FromPageID, l.ToPageID, l.LinkType, l.Strength, now)
	if err != nil {
		return fmt.Errorf("save wiki link: %w", err)
	}
	l.CreatedAt = now
	return nil
}

// ListWikiLinksFrom returns all links originating from the given page.
func (d *DB) ListWikiLinksFrom(pageID string) ([]WikiLink, error) {
	rows, err := d.sql.Query(`
		SELECT id, from_page_id, to_page_id, link_type, strength, created_at
		FROM wiki_links
		WHERE from_page_id = ?
		ORDER BY strength DESC
	`, pageID)
	if err != nil {
		return nil, fmt.Errorf("list wiki links from: %w", err)
	}
	defer rows.Close()
	return scanWikiLinks(rows)
}

// ListWikiLinksTo returns all links pointing to the given page.
func (d *DB) ListWikiLinksTo(pageID string) ([]WikiLink, error) {
	rows, err := d.sql.Query(`
		SELECT id, from_page_id, to_page_id, link_type, strength, created_at
		FROM wiki_links
		WHERE to_page_id = ?
		ORDER BY strength DESC
	`, pageID)
	if err != nil {
		return nil, fmt.Errorf("list wiki links to: %w", err)
	}
	defer rows.Close()
	return scanWikiLinks(rows)
}

// GetWikiGraph returns pages (optionally filtered by type) and all links between them
// for graph visualization.
func (d *DB) GetWikiGraph(pageType string, limit int) ([]WikiPage, []WikiLink, error) {
	if limit <= 0 {
		limit = 100
	}

	pageWhere := "WHERE 1=1"
	pageArgs := []any{}
	if pageType != "" {
		pageWhere += " AND page_type = ?"
		pageArgs = append(pageArgs, pageType)
	}
	pageArgs = append(pageArgs, limit)

	rows, err := d.sql.Query(`
		SELECT id, page_type, title, content, status, staleness_score,
		       source_hashes_json, tags_json, metadata_json, generated_by, version, created_at, updated_at
		FROM wiki_pages `+pageWhere+` ORDER BY updated_at DESC LIMIT ?`, pageArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("get wiki graph pages: %w", err)
	}
	defer rows.Close()

	pages, err := scanWikiPages(rows)
	if err != nil {
		return nil, nil, err
	}

	// Collect page IDs to filter links.
	if len(pages) == 0 {
		return pages, []WikiLink{}, nil
	}

	ids := make([]any, len(pages))
	for i, p := range pages {
		ids[i] = p.ID
	}
	placeholders := ""
	for i := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}

	linkRows, err := d.sql.Query(`
		SELECT id, from_page_id, to_page_id, link_type, strength, created_at
		FROM wiki_links
		WHERE from_page_id IN (`+placeholders+`)
		   OR to_page_id   IN (`+placeholders+`)
	`, append(ids, ids...)...)
	if err != nil {
		return nil, nil, fmt.Errorf("get wiki graph links: %w", err)
	}
	defer linkRows.Close()

	links, err := scanWikiLinks(linkRows)
	if err != nil {
		return nil, nil, err
	}
	return pages, links, nil
}

// SaveWikiPageRef inserts a page reference, ignoring duplicates on the UNIQUE constraint.
func (d *DB) SaveWikiPageRef(r *WikiPageRef) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := d.sql.Exec(`
		INSERT OR IGNORE INTO wiki_page_refs (id, page_id, source_type, source_id, excerpt, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, r.ID, r.PageID, r.SourceType, r.SourceID, r.Excerpt, now)
	if err != nil {
		return fmt.Errorf("save wiki page ref: %w", err)
	}
	r.CreatedAt = now
	return nil
}

// ListWikiPageRefs returns all source references for the given page.
func (d *DB) ListWikiPageRefs(pageID string) ([]WikiPageRef, error) {
	rows, err := d.sql.Query(`
		SELECT id, page_id, source_type, source_id, excerpt, created_at
		FROM wiki_page_refs
		WHERE page_id = ?
		ORDER BY created_at DESC
	`, pageID)
	if err != nil {
		return nil, fmt.Errorf("list wiki page refs: %w", err)
	}
	defer rows.Close()
	return scanWikiPageRefs(rows)
}

// DeleteWikiLinks removes all links where the given page is either source or target.
func (d *DB) DeleteWikiLinks(pageID string) error {
	_, err := d.sql.Exec(`
		DELETE FROM wiki_links WHERE from_page_id = ? OR to_page_id = ?
	`, pageID, pageID)
	if err != nil {
		return fmt.Errorf("delete wiki links: %w", err)
	}
	return nil
}

// DeleteWikiLinkByID removes a single link by its primary key.
// Returns (false, nil) if no row matched, (true, nil) on success.
func (d *DB) DeleteWikiLinkByID(id string) (bool, error) {
	result, err := d.sql.Exec(`DELETE FROM wiki_links WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete wiki link by id: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete wiki link by id: rows affected: %w", err)
	}
	return n > 0, nil
}

// DeleteWikiPageRefs removes all source references for the given page.
func (d *DB) DeleteWikiPageRefs(pageID string) error {
	_, err := d.sql.Exec(`DELETE FROM wiki_page_refs WHERE page_id = ?`, pageID)
	if err != nil {
		return fmt.Errorf("delete wiki page refs: %w", err)
	}
	return nil
}

// WikiPageCount returns the total number of wiki pages.
func (d *DB) WikiPageCount() (int, error) {
	var count int
	err := d.sql.QueryRow("SELECT COUNT(*) FROM wiki_pages").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("wiki page count: %w", err)
	}
	return count, nil
}

// FindWikiPageByTitleNewest returns the most recently updated wiki page whose
// title matches (case-insensitive). Returns (nil, nil) if not found.
func (d *DB) FindWikiPageByTitleNewest(title string) (*WikiPage, error) {
	row := d.sql.QueryRow(`
		SELECT id, page_type, title, content, status, staleness_score,
		       source_hashes_json, tags_json, metadata_json, generated_by, version, created_at, updated_at
		FROM wiki_pages
		WHERE LOWER(title) = LOWER(?)
		ORDER BY updated_at DESC
		LIMIT 1
	`, title)
	return scanWikiPage(row)
}

// UpsertWikiPageByWorkflow inserts or updates a wiki page identified by the
// (workflow_id, feature_slug) pair.  When a row already exists the content,
// title, status, staleness_score, source_hashes, tags, metadata, and
// generated_by fields are updated, the version counter is incremented, and
// created_at is preserved.  When no row exists a new one is inserted with the
// supplied workflow_id and feature_slug populated.  The returned *WikiPage
// always reflects the final state in the database.
func (d *DB) UpsertWikiPageByWorkflow(ctx context.Context, workflowID, featureSlug string, page *WikiPage) (*WikiPage, error) {
	if workflowID == "" {
		return nil, fmt.Errorf("upsert wiki page by workflow: workflow_id is required")
	}
	if featureSlug == "" {
		return nil, fmt.Errorf("upsert wiki page by workflow: feature_slug is required")
	}

	hashesBytes, err := json.Marshal(page.SourceHashes)
	if err != nil {
		return nil, fmt.Errorf("upsert wiki page by workflow: marshal source_hashes: %w", err)
	}
	if page.SourceHashes == nil {
		hashesBytes = []byte("[]")
	}

	tagsBytes, err := json.Marshal(page.Tags)
	if err != nil {
		return nil, fmt.Errorf("upsert wiki page by workflow: marshal tags: %w", err)
	}
	if page.Tags == nil {
		tagsBytes = []byte("[]")
	}

	metaBytes, err := json.Marshal(page.Metadata)
	if err != nil {
		return nil, fmt.Errorf("upsert wiki page by workflow: marshal metadata: %w", err)
	}
	if page.Metadata == nil {
		metaBytes = []byte("{}")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Look up an existing row by (workflow_id, feature_slug).
	var existingID string
	err = d.sql.QueryRowContext(ctx,
		`SELECT id FROM wiki_pages WHERE workflow_id = ? AND feature_slug = ?`,
		workflowID, featureSlug,
	).Scan(&existingID)

	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("upsert wiki page by workflow: lookup: %w", err)
		}
		// sql.ErrNoRows — no existing row, fall through to insert.
	}

	if existingID != "" {
		// Update existing row; preserve created_at, bump version.
		_, err = d.sql.ExecContext(ctx, `
			UPDATE wiki_pages
			SET title = ?, content = ?, status = ?, staleness_score = ?,
			    source_hashes_json = ?, tags_json = ?, metadata_json = ?,
			    generated_by = ?, version = version + 1, updated_at = ?
			WHERE id = ?
		`,
			page.Title, page.Content, page.Status, page.StalenessScore,
			string(hashesBytes), string(tagsBytes), string(metaBytes),
			page.GeneratedBy, now, existingID,
		)
		if err != nil {
			return nil, fmt.Errorf("upsert wiki page by workflow: update: %w", err)
		}

		row := d.sql.QueryRowContext(ctx, `
			SELECT id, page_type, title, content, status, staleness_score,
			       source_hashes_json, tags_json, metadata_json, generated_by, version, created_at, updated_at
			FROM wiki_pages WHERE id = ?
		`, existingID)
		updated, err := scanWikiPage(row)
		if err != nil {
			return nil, fmt.Errorf("upsert wiki page by workflow: reload: %w", err)
		}
		updated.WorkflowID = workflowID
		updated.FeatureSlug = featureSlug
		return updated, nil
	}

	// Insert new row.
	newID := page.ID
	if newID == "" {
		newID = uuid.NewString()
	}
	pageType := page.PageType
	if pageType == "" {
		pageType = "summary"
	}
	version := page.Version
	if version <= 0 {
		version = 1
	}

	_, err = d.sql.ExecContext(ctx, `
		INSERT INTO wiki_pages
		(id, page_type, title, content, status, staleness_score,
		 source_hashes_json, tags_json, metadata_json, generated_by, version,
		 workflow_id, feature_slug, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		newID, pageType, page.Title, page.Content, page.Status, page.StalenessScore,
		string(hashesBytes), string(tagsBytes), string(metaBytes),
		page.GeneratedBy, version, workflowID, featureSlug, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert wiki page by workflow: insert: %w", err)
	}

	result := *page
	result.ID = newID
	result.PageType = pageType
	result.Version = version
	result.WorkflowID = workflowID
	result.FeatureSlug = featureSlug
	result.CreatedAt = now
	result.UpdatedAt = now
	return &result, nil
}

// FindPagesBySourceFiles returns wiki page IDs that reference any of the given
// file paths via wiki_page_refs with source_type='artifact'.
func (d *DB) FindPagesBySourceFiles(files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(files))
	args := make([]any, len(files))
	for i, f := range files {
		placeholders[i] = "?"
		args[i] = f
	}

	query := `SELECT DISTINCT page_id FROM wiki_page_refs
              WHERE source_type = 'artifact' AND source_id IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("find pages by source files: %w", err)
	}
	defer rows.Close()

	var pageIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("find pages by source files: scan: %w", err)
		}
		pageIDs = append(pageIDs, id)
	}
	return pageIDs, rows.Err()
}

