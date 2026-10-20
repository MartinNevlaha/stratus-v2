package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
	"path/filepath"
	"regexp"
	"strings"
)

var managedHookCommand = regexp.MustCompile(`^(stratus|/[^\s"']*/stratus|"/[^"\n]*/stratus"|'[^'\n]*/stratus')\s+codex-hook\s+(context|activity|guard)$`)

func isCodexHook(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	s, _ := m["command"].(string)
	return managedHookCommand.MatchString(s)
}
func containsCodexHook(v any) bool {
	if isCodexHook(v) {
		return true
	}
	switch x := v.(type) {
	case map[string]any:
		for _, v := range x {
			if containsCodexHook(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if containsCodexHook(v) {
				return true
			}
		}
	}
	return false
}
func mergeCodexTOML(src []byte) ([]byte, error) {
	root := map[string]any{}
	if e := toml.Unmarshal(src, &root); e != nil {
		return nil, fmt.Errorf(".codex/config.toml: %w", e)
	}
	if containsCodexHook(root["hooks"]) {
		return nil, fmt.Errorf(".codex/config.toml: inline Stratus hooks conflict with .codex/hooks.json")
	}
	servers, _ := root["mcp_servers"].(map[string]any)
	entry, exists := servers["stratus"]
	e, _ := entry.(map[string]any)
	if exists {
		command, _ := e["command"].(string)
		args, _ := e["args"].([]any)
		if e["url"] != nil || filepath.Base(command) != "stratus" {
			return nil, fmt.Errorf(".codex/config.toml: mcp_servers.stratus belongs to a different server")
		}
		if len(args) == 3 && args[0] == "mcp-serve" && args[1] == "--runtime" && args[2] == "codex" {
			return src, nil
		}
		if len(args) != 1 || args[0] != "mcp-serve" {
			return nil, fmt.Errorf(".codex/config.toml: conflicting Stratus arguments")
		}
		var parser unstable.Parser
		parser.Reset(src)
		var table []string
		start, end := -1, -1
		var visit func(*unstable.Node, []string)
		keys := func(n *unstable.Node) []string {
			var out []string
			it := n.Key()
			for it.Next() {
				out = append(out, string(it.Node().Data))
			}
			return out
		}
		visit = func(n *unstable.Node, path []string) {
			if n == nil {
				return
			}
			switch n.Kind {
			case unstable.KeyValue:
				p := append(append([]string{}, path...), keys(n)...)
				v := n.Value()
				if strings.Join(p, ".") == "mcp_servers.stratus.args" && v.Kind == unstable.Array {
					for child := v.Child(); child != nil; child = child.Next() {
						if child.Kind == unstable.String {
							start = int(child.Raw.Offset)
							end = start + int(child.Raw.Length)
							break
						}
					}
				}
				if v.Kind == unstable.InlineTable {
					for child := v.Child(); child != nil; child = child.Next() {
						visit(child, p)
					}
				}
			}
		}
		for parser.NextExpression() {
			n := parser.Expression()
			if n.Kind == unstable.Table {
				table = keys(n)
			} else {
				visit(n, table)
			}
		}
		if parser.Error() != nil || start < 0 || end <= start {
			return nil, fmt.Errorf(".codex/config.toml: cannot safely update Stratus arguments")
		}
		result := append([]byte{}, src[:start]...)
		result = append(result, []byte(`"mcp-serve", "--runtime", "codex"`)...)
		result = append(result, src[end:]...)
		var check map[string]any
		if err := toml.Unmarshal(result, &check); err != nil {
			return nil, fmt.Errorf(".codex/config.toml: cannot safely extend existing entry: %w", err)
		}
		return result, nil
	}
	nl := "\n"
	if bytes.Contains(src, []byte("\r\n")) {
		nl = "\r\n"
	}
	result := append([]byte{}, src...)
	if len(result) > 0 && !bytes.HasSuffix(result, []byte("\n")) {
		result = append(result, []byte(nl)...)
	}
	block := nl + "[mcp_servers.stratus]" + nl + "command = \"stratus\"" + nl + "args = [\"mcp-serve\", \"--runtime\", \"codex\"]" + nl
	result = append(result, []byte(block)...)
	var check map[string]any
	if e := toml.Unmarshal(result, &check); e != nil {
		return nil, fmt.Errorf(".codex/config.toml: existing table cannot be safely extended: %w", e)
	}
	return result, nil
}
func mergeCodexHooks(src, generated []byte) ([]byte, error) {
	root := map[string]any{}
	if len(src) > 0 {
		if e := json.Unmarshal(src, &root); e != nil {
			return nil, fmt.Errorf(".codex/hooks.json: %w", e)
		}
	}
	var gen map[string]any
	if e := json.Unmarshal(generated, &gen); e != nil {
		return nil, e
	}
	if root == nil {
		return nil, fmt.Errorf(".codex/hooks.json: object required")
	}
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		if root["hooks"] != nil {
			return nil, fmt.Errorf(".codex/hooks.json: hooks must be an object")
		}
		hooks = map[string]any{}
	}
	// Remember explicit handler disablement while replacing only commands that we own.
	disabled := map[string]map[string]any{}
	for event, v := range hooks {
		groups, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf(".codex/hooks.json: %s must be an array", event)
		}
		kept := []any{}
		for _, raw := range groups {
			group, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf(".codex/hooks.json: invalid %s group", event)
			}
			handlers, ok := group["hooks"].([]any)
			if !ok {
				return nil, fmt.Errorf(".codex/hooks.json: invalid %s handlers", event)
			}
			user := []any{}
			for _, handler := range handlers {
				if isCodexHook(handler) {
					h := handler.(map[string]any)
					command, _ := h["command"].(string)
					parts := strings.Fields(command)
					key := event + ":" + parts[len(parts)-1]
					flags := map[string]any{}
					for _, k := range []string{"enabled", "disabled"} {
						if v, ok := h[k]; ok {
							flags[k] = v
						}
					}
					disabled[key] = flags
				} else {
					user = append(user, handler)
				}
			}
			if len(user) > 0 {
				group["hooks"] = user
				kept = append(kept, group)
			}
		}
		hooks[event] = kept
	}
	generatedHooks, _ := gen["hooks"].(map[string]any)
	for event, v := range generatedHooks {
		groups, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid generated hooks")
		}
		for _, raw := range groups {
			g := raw.(map[string]any)
			for _, h := range g["hooks"].([]any) {
				m := h.(map[string]any)
				command, _ := m["command"].(string)
				parts := strings.Fields(command)
				for k, v := range disabled[event+":"+parts[len(parts)-1]] {
					m[k] = v
				}
			}
		}
		old, _ := hooks[event].([]any)
		hooks[event] = append(old, groups...)
	}
	root["hooks"] = hooks
	b, e := json.MarshalIndent(root, "", "  ")
	return append(b, '\n'), e
}
