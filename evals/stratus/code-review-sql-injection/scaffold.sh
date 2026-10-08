#!/usr/bin/env bash
set -euo pipefail
echo '{"port": 1}' > .stratus.json
cat > store.go <<'GO'
package store

import (
	"database/sql"
	"fmt"
)

// FindUser returns the id of the user with the given name.
func FindUser(db *sql.DB, name string) (int64, error) {
	var id int64
	q := "SELECT id FROM users WHERE name = '" + name + "'"
	if err := db.QueryRow(q).Scan(&id); err != nil {
		return 0, fmt.Errorf("find user %q: %w", name, err)
	}
	return id, nil
}
GO
