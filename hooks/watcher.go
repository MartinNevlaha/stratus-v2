package hooks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

// Watcher extracts modified file paths from Write/Edit/MultiEdit/NotebookEdit
// tool inputs and queues them for vexor reindexing via the Stratus API.
// It always allows the tool call — this is a best-effort side effect.
func Watcher(event HookEvent) Decision {
	paths := watcherExtractPaths(event)
	if len(paths) > 0 {
		port := getPort()
		body, _ := json.Marshal(map[string]any{"paths": paths})
		client := &http.Client{Timeout: 1 * time.Second}
		req, err := http.NewRequest("POST",
			"http://localhost:"+port+"/api/retrieve/dirty",
			bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
			}
		}
	}
	return Decision{Continue: true}
}

// watcherExtractPaths returns the file paths that will be modified by the tool.
func watcherExtractPaths(event HookEvent) []string {
	switch event.ToolName {
	case "Write", "Edit":
		if p, _ := event.ToolInput["file_path"].(string); p != "" {
			return []string{p}
		}
	case "NotebookEdit":
		if p, _ := event.ToolInput["notebook_path"].(string); p != "" {
			return []string{p}
		}
	case "MultiEdit":
		edits, _ := event.ToolInput["edits"].([]any)
		seen := map[string]bool{}
		var paths []string
		for _, e := range edits {
			if edit, ok := e.(map[string]any); ok {
				if p, _ := edit["file_path"].(string); p != "" && !seen[p] {
					paths = append(paths, p)
					seen[p] = true
				}
			}
		}
		return paths
	}
	return nil
}
