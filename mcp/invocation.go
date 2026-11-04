package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// InvocationContext comes from executor-owned MCP metadata, never arguments.
type InvocationContext struct{ NativeID, TurnID string }

func parseInvocationContext(raw json.RawMessage) (InvocationContext, error) {
	var out InvocationContext
	if len(raw) == 0 {
		return out, nil
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(raw, &meta); err != nil || meta == nil {
		return out, fmt.Errorf("invalid executor metadata")
	}
	merge := func(value json.RawMessage, target *string) error {
		var text string
		if err := json.Unmarshal(value, &text); err != nil || text == "" {
			return fmt.Errorf("invalid executor identity metadata")
		}
		if err := validateNativeMetadata(text); err != nil {
			return err
		}
		if *target != "" && *target != text {
			return fmt.Errorf("conflicting executor identity metadata")
		}
		*target = text
		return nil
	}
	if value, ok := meta["x-codex-turn-metadata"]; ok {
		var embedded string
		if json.Unmarshal(value, &embedded) == nil {
			value = json.RawMessage(embedded)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(value, &fields); err != nil || fields == nil {
			return out, fmt.Errorf("invalid Codex turn metadata")
		}
		for _, field := range []string{"thread_id", "threadId"} {
			if value, ok := fields[field]; ok {
				if err := merge(value, &out.NativeID); err != nil {
					return out, err
				}
			}
		}
		for _, field := range []string{"turn_id", "turnId"} {
			if value, ok := fields[field]; ok {
				if err := merge(value, &out.TurnID); err != nil {
					return out, err
				}
			}
		}
	}
	for _, field := range []string{"openai/threadId", "codex/threadId"} {
		if value, ok := meta[field]; ok {
			if err := merge(value, &out.NativeID); err != nil {
				return out, err
			}
		}
	}
	for _, field := range []string{"openai/turnId", "codex/turnId"} {
		if value, ok := meta[field]; ok {
			if err := merge(value, &out.TurnID); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

func validateNativeMetadata(value string) error {
	if len(value) > 512 || value != strings.TrimSpace(value) || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("invalid executor identity metadata")
	}
	return nil
}
