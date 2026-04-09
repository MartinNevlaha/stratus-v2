package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// TokenUsageEntry represents a row from the llm_token_usage table.
type TokenUsageEntry struct {
	ID           int    `json:"id"`
	Date         string `json:"date"`
	Subsystem    string `json:"subsystem"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Requests     int    `json:"requests"`
	CreatedAt    string `json:"created_at"`
}

// RecordTokenUsage upserts token usage for a given date and subsystem.
func (d *DB) RecordTokenUsage(date, subsystem string, inputTokens, outputTokens int) error {
	_, err := d.sql.Exec(`
		INSERT INTO llm_token_usage (date, subsystem, input_tokens, output_tokens, requests)
		VALUES (?, ?, ?, ?, 1)
		ON CONFLICT(date, subsystem) DO UPDATE SET
			input_tokens  = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			requests      = requests + 1`,
		date, subsystem, inputTokens, outputTokens)
	if err != nil {
		return fmt.Errorf("record token usage: %w", err)
	}
	return nil
}

// GetDailyTokenUsage returns token counts for a specific date and subsystem.
func (d *DB) GetDailyTokenUsage(date, subsystem string) (inputTokens, outputTokens int, err error) {
	err = d.sql.QueryRow(`
		SELECT COALESCE(input_tokens, 0), COALESCE(output_tokens, 0)
		FROM llm_token_usage WHERE date = ? AND subsystem = ?`,
		date, subsystem).Scan(&inputTokens, &outputTokens)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("get daily token usage: %w", err)
	}
	return inputTokens, outputTokens, nil
}

// GetDailyTokenUsageTotal returns total token counts across all subsystems for a date.
func (d *DB) GetDailyTokenUsageTotal(date string) (inputTokens, outputTokens int, err error) {
	err = d.sql.QueryRow(`
		SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0)
		FROM llm_token_usage WHERE date = ?`, date).Scan(&inputTokens, &outputTokens)
	if err != nil {
		return 0, 0, fmt.Errorf("get daily token usage total: %w", err)
	}
	return inputTokens, outputTokens, nil
}

// GetTokenUsageHistory returns token usage rows for the last N days.
func (d *DB) GetTokenUsageHistory(days int) ([]TokenUsageEntry, error) {
	rows, err := d.sql.Query(`
		SELECT id, date, subsystem, input_tokens, output_tokens, requests, created_at
		FROM llm_token_usage
		WHERE date >= date('now', '-' || ? || ' days')
		ORDER BY date DESC, subsystem ASC`, days)
	if err != nil {
		return nil, fmt.Errorf("get token usage history: %w", err)
	}
	defer rows.Close()
	var result []TokenUsageEntry
	for rows.Next() {
		var u TokenUsageEntry
		if err := rows.Scan(&u.ID, &u.Date, &u.Subsystem, &u.InputTokens, &u.OutputTokens, &u.Requests, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan token usage row: %w", err)
		}
		result = append(result, u)
	}
	return result, rows.Err()
}
