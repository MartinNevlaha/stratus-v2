package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func parseRequiredTimeRFC3339Nano(field, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, fmt.Errorf("%s is empty", field)
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s: %w", field, err)
	}
	return parsed, nil
}

func parseOptionalTimeRFC3339Nano(field string, raw sql.NullString) (*time.Time, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", field, err)
	}
	return &parsed, nil
}

func unmarshalJSONField(field, raw string, dest any) error {
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), dest); err != nil {
		return fmt.Errorf("unmarshal %s: %w", field, err)
	}
	return nil
}
