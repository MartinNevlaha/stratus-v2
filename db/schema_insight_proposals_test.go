package db

import (
	"database/sql"
	"strings"
	"testing"
)

// TestInsightProposals_FreshDBNewColumnsExist verifies that a freshly created
// database has all four new columns on insight_proposals.
func TestInsightProposals_FreshDBNewColumnsExist(t *testing.T) {
	db := openTestDB(t)

	rows, err := db.sql.Query(`PRAGMA table_info(insight_proposals)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	found := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt *string
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan column row: %v", err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	required := []string{"wiki_page_id", "idempotency_hash", "last_seen_at", "signal_refs"}
	for _, col := range required {
		if !found[col] {
			t.Errorf("column %q missing from insight_proposals", col)
		}
	}
}

// TestInsightProposals_FreshDBIndexesExist verifies that the partial unique index
// and the composite non-unique index are present after a fresh open.
func TestInsightProposals_FreshDBIndexesExist(t *testing.T) {
	db := openTestDB(t)

	rows, err := db.sql.Query(`PRAGMA index_list(insight_proposals)`)
	if err != nil {
		t.Fatalf("PRAGMA index_list: %v", err)
	}
	defer rows.Close()

	type indexRow struct {
		seq     int
		name    string
		unique  int
		origin  string
		partial int
	}
	indexes := map[string]indexRow{}
	for rows.Next() {
		var r indexRow
		if err := rows.Scan(&r.seq, &r.name, &r.unique, &r.origin, &r.partial); err != nil {
			t.Fatalf("scan index row: %v", err)
		}
		indexes[r.name] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	// Unique partial index on idempotency_hash
	idxIdem, ok := indexes["idx_insight_proposals_idempotency"]
	if !ok {
		t.Error("index idx_insight_proposals_idempotency not found")
	} else {
		if idxIdem.unique != 1 {
			t.Errorf("idx_insight_proposals_idempotency: expected unique=1, got %d", idxIdem.unique)
		}
		if idxIdem.partial != 1 {
			t.Errorf("idx_insight_proposals_idempotency: expected partial=1, got %d", idxIdem.partial)
		}
	}

	// Non-unique composite index on (type, last_seen_at)
	idxType, ok := indexes["idx_insight_proposals_type_last_seen"]
	if !ok {
		t.Error("index idx_insight_proposals_type_last_seen not found")
	} else {
		if idxType.unique != 0 {
			t.Errorf("idx_insight_proposals_type_last_seen: expected unique=0, got %d", idxType.unique)
		}
	}
}

// TestInsightProposals_IdempotencyHashUniqueConstraint verifies that two inserts
// sharing the same idempotency_hash fail with a UNIQUE constraint error.
func TestInsightProposals_IdempotencyHashUniqueConstraint(t *testing.T) {
	db := openTestDB(t)

	insert := func(id, hash string) error {
		_, err := db.sql.Exec(`
			INSERT INTO insight_proposals
				(id, type, status, title, description, confidence, risk_level,
				 source_pattern_id, idempotency_hash)
			VALUES (?, 'idea', 'detected', 'title', 'desc', 0.8, 'medium', 'pat-1', ?)`,
			id, hash)
		return err
	}

	if err := insert("prop-1", "abc123"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := insert("prop-2", "abc123")
	if err == nil {
		t.Fatal("expected UNIQUE constraint error on duplicate idempotency_hash, got nil")
	}
	if !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("expected UNIQUE constraint error, got: %v", err)
	}
}

// TestInsightProposals_NullHashAllowsMultipleRows verifies that the partial index
// does not prevent multiple rows with idempotency_hash = NULL.
func TestInsightProposals_NullHashAllowsMultipleRows(t *testing.T) {
	db := openTestDB(t)

	insert := func(id string) error {
		_, err := db.sql.Exec(`
			INSERT INTO insight_proposals
				(id, type, status, title, description, confidence, risk_level,
				 source_pattern_id)
			VALUES (?, 'idea', 'detected', 'title', 'desc', 0.8, 'medium', 'pat-1')`,
			id)
		return err
	}

	if err := insert("prop-null-1"); err != nil {
		t.Fatalf("first null-hash insert: %v", err)
	}
	if err := insert("prop-null-2"); err != nil {
		t.Fatalf("second null-hash insert: %v", err)
	}
}

// TestInsightProposals_RoundTrip inserts a row with all new fields populated and
// reads them back, asserting exact values.
func TestInsightProposals_RoundTrip(t *testing.T) {
	db := openTestDB(t)

	const (
		propID     = "prop-rt-1"
		idemHash   = "deadbeef01234567deadbeef01234567deadbeef01234567deadbeef01234567"
		lastSeenAt = int64(1744000000)
		signalRefs = `["pkg/foo.go","MyFunc","abc1234"]`
	)

	// Insert a wiki page so the FK reference is valid.
	wikiPage := &WikiPage{
		PageType: "concept",
		Title:    "Test Wiki Page",
		Content:  "content",
		Status:   "published",
	}
	if err := db.SaveWikiPage(wikiPage); err != nil {
		t.Fatalf("SaveWikiPage: %v", err)
	}
	wikiPageID := wikiPage.ID

	_, err := db.sql.Exec(`
		INSERT INTO insight_proposals
			(id, type, status, title, description, confidence, risk_level,
			 source_pattern_id, wiki_page_id, idempotency_hash, last_seen_at, signal_refs)
		VALUES (?, 'idea', 'detected', 'My Proposal', 'a description', 0.9, 'low',
		        'pat-2', ?, ?, ?, ?)`,
		propID, wikiPageID, idemHash, lastSeenAt, signalRefs)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var (
		gotWikiPageID  sql.NullString
		gotIdemHash    sql.NullString
		gotLastSeenAt  sql.NullInt64
		gotSignalRefs  string
	)
	err = db.sql.QueryRow(`
		SELECT wiki_page_id, idempotency_hash, last_seen_at, signal_refs
		FROM insight_proposals WHERE id = ?`, propID).
		Scan(&gotWikiPageID, &gotIdemHash, &gotLastSeenAt, &gotSignalRefs)
	if err != nil {
		t.Fatalf("select: %v", err)
	}

	if !gotWikiPageID.Valid || gotWikiPageID.String != wikiPageID {
		t.Errorf("wiki_page_id: got %v want %q", gotWikiPageID, wikiPageID)
	}
	if !gotIdemHash.Valid || gotIdemHash.String != idemHash {
		t.Errorf("idempotency_hash: got %v want %q", gotIdemHash, idemHash)
	}
	if !gotLastSeenAt.Valid || gotLastSeenAt.Int64 != lastSeenAt {
		t.Errorf("last_seen_at: got %v want %d", gotLastSeenAt, lastSeenAt)
	}
	if gotSignalRefs != signalRefs {
		t.Errorf("signal_refs: got %q want %q", gotSignalRefs, signalRefs)
	}
}
