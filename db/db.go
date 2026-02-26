package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection with helper methods.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) the stratus SQLite database at the given path.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	conn, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// WAL mode supports concurrent readers; allow a small pool so long-running
	// operations (e.g. IndexGovernance on startup) don't starve HTTP handlers.
	// busy_timeout above ensures writers retry instead of returning SQLITE_BUSY.
	conn.SetMaxOpenConns(4)
	db := &DB{sql: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// Close closes the underlying SQL connection.
func (d *DB) Close() error { return d.sql.Close() }

// SQL returns the raw *sql.DB for advanced usage.
func (d *DB) SQL() *sql.DB { return d.sql }

func (d *DB) migrate() error {
	if _, err := d.sql.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	// Add columns that may be missing on existing databases.
	// SQLite doesn't support ADD COLUMN IF NOT EXISTS, so we ignore
	// "duplicate column" errors from ALTER TABLE.
	for _, stmt := range migrations {
		if _, err := d.sql.Exec(stmt); err != nil {
			if !isDuplicateColumn(err) {
				return fmt.Errorf("migration: %w", err)
			}
		}
	}
	return nil
}

// migrations contains ALTER TABLE statements for columns added after the initial schema.
// Each runs on every startup; duplicate-column errors are silently ignored.
var migrations = []string{
	`ALTER TABLE missions ADD COLUMN strategy TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE missions ADD COLUMN strategy_outcome TEXT NOT NULL DEFAULT '{}'`,
}

func isDuplicateColumn(err error) bool {
	// modernc.org/sqlite returns "duplicate column name: <col>" for ALTER TABLE ADD COLUMN
	// when the column already exists.
	msg := err.Error()
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists")
}
