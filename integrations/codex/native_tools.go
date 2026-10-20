package codex

import "strings"

func nativeTool(name string) string {
	switch name {
	case "collaborationspawn_agent", "collaboration.spawn_agent", "spawn_agent", "Agent", "Task":
		return "spawn_agent"
	case "collaborationfollowup_task", "collaboration.followup_task", "followup_task":
		return "followup_task"
	}
	return name
}

// An explicit failure takes precedence over human-readable output.
func toolOutcome(v any) *bool {
	result := func(v bool) *bool { return &v }
	switch x := v.(type) {
	case map[string]any:
		if b, ok := x["success"].(bool); ok && !b {
			return result(false)
		}
		if b, ok := x["isError"].(bool); ok && b {
			return result(false)
		}
		if n, ok := x["exit_code"].(float64); ok {
			return result(n == 0)
		}
		if m, ok := x["metadata"].(map[string]any); ok {
			if n, ok := m["exit_code"].(float64); ok {
				return result(n == 0)
			}
		}
		if b, ok := x["success"].(bool); ok {
			return result(b)
		}
		if out := toolOutcome(x["output"]); out != nil {
			return out
		}
		if b, ok := x["isError"].(bool); ok {
			return result(!b)
		}
	case string:
		if strings.HasPrefix(strings.TrimSpace(x), "Success.") {
			return result(true)
		}
	}
	return nil
}
