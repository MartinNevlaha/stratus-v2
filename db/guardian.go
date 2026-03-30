package db

import (
	"database/sql"
	"encoding/json"
)

// GuardianAlert represents a proactive codebase health alert.
type GuardianAlert struct {
	ID          int64                  `json:"id"`
	Type        string                 `json:"type"`
	Severity    string                 `json:"severity"`
	Message     string                 `json:"message"`
	Metadata    map[string]interface{} `json:"metadata"`
	DismissedAt *string                `json:"dismissed_at,omitempty"`
	CreatedAt   string                 `json:"created_at"`
}

// SaveGuardianAlert inserts a new guardian alert and returns its ID.
func (d *DB) SaveGuardianAlert(alertType, severity, message string, metadata map[string]interface{}) (int64, error) {
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	meta, err := json.Marshal(metadata)
	if err != nil {
		meta = []byte("{}")
	}
	res, err := d.sql.Exec(`
		INSERT INTO guardian_alerts (type, severity, message, metadata)
		VALUES (?, ?, ?, ?)`,
		alertType, severity, message, string(meta),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListGuardianAlerts returns non-dismissed alerts, newest first.
// If alertType is non-empty, filters by that type.
func (d *DB) ListGuardianAlerts(alertType string) ([]GuardianAlert, error) {
	q := `SELECT id, type, severity, message, metadata, dismissed_at, created_at
	      FROM guardian_alerts WHERE dismissed_at IS NULL`
	args := []interface{}{}
	if alertType != "" {
		q += " AND type = ?"
		args = append(args, alertType)
	}
	q += " ORDER BY created_at DESC"

	rows, err := d.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGuardianAlerts(rows)
}

// DismissGuardianAlert marks an alert as dismissed.
func (d *DB) DismissGuardianAlert(id int64) error {
	_, err := d.sql.Exec(`
		UPDATE guardian_alerts SET dismissed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`, id)
	return err
}

// DeleteGuardianAlert permanently removes an alert.
func (d *DB) DeleteGuardianAlert(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM guardian_alerts WHERE id = ?`, id)
	return err
}

// DismissAllGuardianAlerts marks all non-dismissed alerts as dismissed.
func (d *DB) DismissAllGuardianAlerts() (int64, error) {
	res, err := d.sql.Exec(`
		UPDATE guardian_alerts SET dismissed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE dismissed_at IS NULL`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// HasRecentAlert returns true if a non-dismissed alert of the given type with
// the given dedup key exists within the last 24 hours.
func (d *DB) HasRecentAlert(alertType, dedupKey string) (bool, error) {
	var count int
	err := d.sql.QueryRow(`
		SELECT COUNT(*) FROM guardian_alerts
		WHERE type = ?
		  AND dismissed_at IS NULL
		  AND json_extract(metadata, '$.dedup_key') = ?
		  AND created_at > datetime('now', '-24 hours')`,
		alertType, dedupKey,
	).Scan(&count)
	return count > 0, err
}

// GetGuardianBaseline returns a stored baseline value by key.
func (d *DB) GetGuardianBaseline(key string) (string, error) {
	var value string
	err := d.sql.QueryRow(`SELECT value FROM guardian_baselines WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetGuardianBaseline upserts a baseline value.
func (d *DB) SetGuardianBaseline(key, value string) error {
	_, err := d.sql.Exec(`
		INSERT INTO guardian_baselines (key, value, updated_at)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value,
	)
	return err
}

// CountEvents returns the total number of stored memory events.
func (d *DB) CountEvents() (int, error) {
	var count int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count)
	return count, err
}

func scanGuardianAlerts(rows *sql.Rows) ([]GuardianAlert, error) {
	var alerts []GuardianAlert
	for rows.Next() {
		var a GuardianAlert
		var metaStr string
		if err := rows.Scan(&a.ID, &a.Type, &a.Severity, &a.Message, &metaStr, &a.DismissedAt, &a.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(metaStr), &a.Metadata); err != nil {
			a.Metadata = map[string]interface{}{}
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}
