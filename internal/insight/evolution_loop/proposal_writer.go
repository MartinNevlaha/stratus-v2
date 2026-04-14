package evolution_loop

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/generators"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/evolution_loop/scoring"
	"github.com/google/uuid"
)

// sqlExecQuerier is the minimal SQL interface satisfied by both *sql.DB and
// *sql.Conn, allowing internal helpers to work with either.
type sqlExecQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ProposalInput carries all data needed to persist a scored hypothesis.
type ProposalInput struct {
	Hypothesis scoring.Hypothesis
	Final      float64            // scoring.Blended.Final
	Static     scoring.StaticScores
	LLM        scoring.LLMScores
	Breakdown  map[string]float64 // scoring.Blended.Breakdown
}

// ProposalResult is returned by ProposalWriter.Write.
type ProposalResult struct {
	ProposalID string
	WikiPageID string    // empty unless category=feature_idea
	Inserted   bool      // false when ON CONFLICT UPDATE path taken
	LastSeenAt time.Time
}

// ProposalWriter persists a scored hypothesis proposal atomically.
type ProposalWriter interface {
	Write(ctx context.Context, in ProposalInput) (ProposalResult, error)
}

// proposalWriter is the concrete implementation.
type proposalWriter struct {
	db *db.DB
}

// NewProposalWriter creates a ProposalWriter backed by the given database.
func NewProposalWriter(database *db.DB) ProposalWriter {
	return &proposalWriter{db: database}
}

// proposalDescription is serialised as the description JSON blob.
type proposalDescription struct {
	Rationale  string             `json:"rationale"`
	FileRefs   []string           `json:"file_refs"`
	SymbolRefs []string           `json:"symbol_refs"`
	Scores     proposalScores     `json:"scores"`
	Breakdown  map[string]float64 `json:"breakdown"`
}

type proposalScores struct {
	Final  float64             `json:"final"`
	Static scoring.StaticScores `json:"static"`
	LLM    scoring.LLMScores   `json:"llm"`
}

// Write atomically upserts the proposal (and optionally a wiki page for
// feature_idea hypotheses).  It uses BEGIN IMMEDIATE to serialise concurrent
// writes.
func (w *proposalWriter) Write(ctx context.Context, in ProposalInput) (ProposalResult, error) {
	// ------------------------------------------------------------------
	// 1. Compute idempotency hash — server-authoritative.
	// ------------------------------------------------------------------
	hashInput := generators.SignalHashInputs(
		in.Hypothesis.Category,
		in.Hypothesis.Title,
		in.Hypothesis.SignalRefs,
	)
	sum := sha256.Sum256([]byte(hashInput))
	idempotencyHash := fmt.Sprintf("%x", sum)

	// ------------------------------------------------------------------
	// 2. Prepare JSON blobs.
	// ------------------------------------------------------------------
	sortedSignals := sortedCopy(in.Hypothesis.SignalRefs)

	signalRefsJSON, err := json.Marshal(sortedSignals)
	if err != nil {
		return ProposalResult{}, fmt.Errorf("proposal_writer: marshal signal_refs: %w", err)
	}

	fileRefs := in.Hypothesis.FileRefs
	if fileRefs == nil {
		fileRefs = []string{}
	}
	symbolRefs := in.Hypothesis.SymbolRefs
	if symbolRefs == nil {
		symbolRefs = []string{}
	}

	desc := proposalDescription{
		Rationale:  in.Hypothesis.Rationale,
		FileRefs:   fileRefs,
		SymbolRefs: symbolRefs,
		Scores: proposalScores{
			Final:  in.Final,
			Static: in.Static,
			LLM:    in.LLM,
		},
		Breakdown: in.Breakdown,
	}
	descJSON, err := json.Marshal(desc)
	if err != nil {
		return ProposalResult{}, fmt.Errorf("proposal_writer: marshal description: %w", err)
	}

	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339Nano)
	nowUnix := now.Unix()

	// ------------------------------------------------------------------
	// 3. Open transaction — BEGIN IMMEDIATE serialises concurrent writers.
	//
	// We pin to a single *sql.Conn so that BEGIN IMMEDIATE, all queries, and
	// COMMIT/ROLLBACK all travel on the same underlying SQLite connection.
	// Using rawDB.ExecContext across pool connections would silently spread
	// the transaction across multiple connections.
	// ------------------------------------------------------------------
	rawDB := w.db.SQL()
	conn, err := rawDB.Conn(ctx)
	if err != nil {
		return ProposalResult{}, fmt.Errorf("proposal_writer: acquire conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return ProposalResult{}, fmt.Errorf("proposal_writer: begin immediate: %w", err)
	}

	var result ProposalResult
	if err := w.writeInTx(ctx, conn, in, idempotencyHash, string(signalRefsJSON), string(descJSON), nowISO, nowUnix, now, &result); err != nil {
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		return ProposalResult{}, err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return ProposalResult{}, fmt.Errorf("proposal_writer: commit: %w", err)
	}
	return result, nil
}

func (w *proposalWriter) writeInTx(
	ctx context.Context,
	rawDB sqlExecQuerier,
	in ProposalInput,
	idempotencyHash, signalRefsJSON, descJSON, nowISO string,
	nowUnix int64,
	now time.Time,
	result *ProposalResult,
) error {
	// ------------------------------------------------------------------
	// 4. Check whether a row with this hash already exists.
	// ------------------------------------------------------------------
	// We cannot use ON CONFLICT(idempotency_hash) because that column has a
	// *partial* unique index (WHERE idempotency_hash IS NOT NULL), and SQLite
	// only recognises conflict targets that match a full PRIMARY KEY or UNIQUE
	// constraint.  Instead, inside the BEGIN IMMEDIATE transaction we do an
	// explicit SELECT-then-INSERT-or-UPDATE.

	var existingID string
	err := rawDB.QueryRowContext(ctx,
		`SELECT id FROM insight_proposals WHERE idempotency_hash = ? LIMIT 1`,
		idempotencyHash,
	).Scan(&existingID)

	switch {
	case err == nil:
		// Row exists — update last_seen_at and updated_at only.
		if _, err := rawDB.ExecContext(ctx,
			`UPDATE insight_proposals SET last_seen_at = ?, updated_at = ? WHERE id = ?`,
			nowUnix, nowISO, existingID,
		); err != nil {
			return fmt.Errorf("proposal_writer: update existing proposal: %w", err)
		}
		result.ProposalID = existingID
		result.Inserted = false
		result.LastSeenAt = now

	case err == sql.ErrNoRows:
		// No existing row — insert new proposal.
		proposalID := uuid.NewString()
		if _, err := rawDB.ExecContext(ctx, `
INSERT INTO insight_proposals (
  id, type, status, title, description,
  confidence, risk_level, source_pattern_id, evidence, recommendation,
  created_at, updated_at,
  wiki_page_id, idempotency_hash, last_seen_at, signal_refs
)
VALUES (?, ?, 'pending', ?, ?,
        ?, 'low', '', '{}', '{}',
        ?, ?,
        NULL, ?, ?, ?)`,
			proposalID,
			in.Hypothesis.Category,
			in.Hypothesis.Title,
			descJSON,
			in.Final,   // confidence
			nowISO,     // created_at
			nowISO,     // updated_at
			idempotencyHash,
			nowUnix,    // last_seen_at
			signalRefsJSON,
		); err != nil {
			return fmt.Errorf("proposal_writer: insert proposal: %w", err)
		}
		result.ProposalID = proposalID
		result.Inserted = true
		result.LastSeenAt = now

	default:
		return fmt.Errorf("proposal_writer: check existing proposal: %w", err)
	}

	// ------------------------------------------------------------------
	// 5. Wiki dual-write (feature_idea + insert path only).
	// ------------------------------------------------------------------
	if in.Hypothesis.Category == "feature_idea" {
		if result.Inserted {
			wikiID, err := w.insertWikiPage(ctx, rawDB, result.ProposalID, in, signalRefsJSON, nowISO, now)
			if err != nil {
				return fmt.Errorf("proposal_writer: wiki dual-write: %w", err)
			}
			// Back-fill wiki_page_id on the proposal row.
			if _, err := rawDB.ExecContext(ctx,
				`UPDATE insight_proposals SET wiki_page_id = ? WHERE id = ?`,
				wikiID, result.ProposalID,
			); err != nil {
				return fmt.Errorf("proposal_writer: update wiki_page_id: %w", err)
			}
			result.WikiPageID = wikiID
		} else {
			// Update path — fetch existing wiki_page_id.
			var wikiPageID sql.NullString
			if err := rawDB.QueryRowContext(ctx,
				`SELECT wiki_page_id FROM insight_proposals WHERE id = ?`, result.ProposalID,
			).Scan(&wikiPageID); err != nil && err != sql.ErrNoRows {
				return fmt.Errorf("proposal_writer: fetch wiki_page_id: %w", err)
			}
			if wikiPageID.Valid {
				result.WikiPageID = wikiPageID.String
			}
		}
	}

	return nil
}

// insertWikiPage inserts a new wiki_pages row for a feature_idea proposal and
// returns the new page id.
func (w *proposalWriter) insertWikiPage(
	ctx context.Context,
	rawDB sqlExecQuerier,
	proposalID string,
	in ProposalInput,
	signalRefsJSON string,
	nowISO string,
	now time.Time,
) (string, error) {
	slug := truncateRunes(toKebab(in.Hypothesis.Title), 80)
	wikiID := "ideas/" + slug

	// Collision probe: if id already taken, append short proposal id suffix.
	var existingCount int
	if err := rawDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM wiki_pages WHERE id = ?`, wikiID,
	).Scan(&existingCount); err != nil {
		return "", fmt.Errorf("probe wiki id collision: %w", err)
	}
	if existingCount > 0 {
		// Use last 8 chars of proposal UUID (no dashes section).
		short := strings.ReplaceAll(proposalID, "-", "")
		if len(short) > 8 {
			short = short[len(short)-8:]
		}
		wikiID = truncateRunes("ideas/"+slug, 80) + "-" + short
	}

	content := buildWikiContent(proposalID, in, signalRefsJSON, now)

	metaJSON, err := json.Marshal(map[string]string{
		"proposal_id": proposalID,
		"source":      "evolution",
	})
	if err != nil {
		return "", fmt.Errorf("marshal wiki metadata: %w", err)
	}

	const insertWikiSQL = `
INSERT INTO wiki_pages (id, page_type, title, content, status, generated_by, tags_json, metadata_json, created_at, updated_at)
VALUES (?, 'idea', ?, ?, 'draft', 'evolution', '["idea","evolution"]', ?, ?, ?)`

	if _, err := rawDB.ExecContext(ctx, insertWikiSQL,
		wikiID,
		in.Hypothesis.Title,
		content,
		string(metaJSON),
		nowISO,
		nowISO,
	); err != nil {
		return "", fmt.Errorf("insert wiki page: %w", err)
	}
	return wikiID, nil
}

// buildWikiContent constructs the markdown body (with YAML frontmatter) for a
// feature_idea wiki page.
func buildWikiContent(proposalID string, in ProposalInput, signalRefsJSON string, now time.Time) string {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("proposal_id: %s\n", proposalID))
	sb.WriteString("category: feature_idea\n")
	sb.WriteString(fmt.Sprintf("generated_at: %s\n", now.Format(time.RFC3339)))
	sb.WriteString("signal_refs:\n")
	for _, s := range sortedCopy(in.Hypothesis.SignalRefs) {
		sb.WriteString(fmt.Sprintf("  - %s\n", s))
	}
	sb.WriteString("---\n\n")

	sb.WriteString(fmt.Sprintf("# %s\n\n", in.Hypothesis.Title))
	sb.WriteString(in.Hypothesis.Rationale)
	sb.WriteString("\n\n")

	sb.WriteString("## File refs\n")
	for _, f := range in.Hypothesis.FileRefs {
		sb.WriteString(fmt.Sprintf("- %s\n", f))
	}
	sb.WriteString("\n")

	sb.WriteString("## Scores\n")
	sb.WriteString(fmt.Sprintf("- Final: %.2f\n", in.Final))
	sb.WriteString(fmt.Sprintf("- Static.Churn: %.2f\n", in.Static.Churn))
	sb.WriteString(fmt.Sprintf("- Static.TestGap: %.2f\n", in.Static.TestGap))
	sb.WriteString(fmt.Sprintf("- Static.TODO: %.2f\n", in.Static.TODO))
	sb.WriteString(fmt.Sprintf("- Static.Staleness: %.2f\n", in.Static.Staleness))
	sb.WriteString(fmt.Sprintf("- Static.ADRViolation: %.2f\n", in.Static.ADRViolation))
	sb.WriteString(fmt.Sprintf("- LLM.Impact: %.2f\n", in.LLM.Impact))
	sb.WriteString(fmt.Sprintf("- LLM.Effort: %.2f\n", in.LLM.Effort))
	sb.WriteString(fmt.Sprintf("- LLM.Confidence: %.2f\n", in.LLM.Confidence))
	sb.WriteString(fmt.Sprintf("- LLM.Novelty: %.2f\n", in.LLM.Novelty))

	return sb.String()
}

// sortedCopy returns a sorted copy of the given slice (nil-safe).
func sortedCopy(ss []string) []string {
	if len(ss) == 0 {
		return []string{}
	}
	out := make([]string, len(ss))
	copy(out, ss)
	sort.Strings(out)
	return out
}
