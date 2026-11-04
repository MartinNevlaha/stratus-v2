package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// Forward host connection paths, never their values or a captured chat/turn ID.
// Shared MCP processes must resolve native identity independently for every call.
var codexAttachmentEnvironment = []string{
	"CODEX_APP_TOOLS_PIPE_PATH", "CODEX_ELECTRON_RESOURCES_PATH",
	"CODEX_MCP_NODE_PATH", "CODEX_BROWSER_USE_NODE_PATH", "CODEX_CLI_PATH",
	"CODEX_APP_TOOLS_CALLER_HOST_ID", "STRATUS_CODEX_APP_TOOLS_LAUNCHER",
	"STRATUS_CODEX_APP_TOOLS_SERVER",
}

func mergeCodexAttachmentEnvironment(src []byte) ([]byte, error) {
	var config map[string]any
	if err := toml.Unmarshal(src, &config); err != nil {
		return nil, err
	}
	servers, _ := config["mcp_servers"].(map[string]any)
	entry, _ := servers["stratus"].(map[string]any)
	if entry == nil {
		return nil, fmt.Errorf(".codex/config.toml: Stratus MCP entry is missing")
	}
	existing := map[string]bool{}
	if value, present := entry["env_vars"]; present {
		list, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf(".codex/config.toml: Stratus env_vars must be an array of names")
		}
		for _, item := range list {
			name, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf(".codex/config.toml: Stratus env_vars must contain only names")
			}
			existing[name] = true
		}
	}
	missing := []string{}
	for _, name := range codexAttachmentEnvironment {
		if !existing[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return src, nil
	}
	encoded, _ := json.Marshal(missing)
	nl := "\n"
	if bytes.Contains(src, []byte("\r\n")) {
		nl = "\r\n"
	}
	// Insert into the existing syntax instead of reserializing user comments,
	// disabled flags, quoting style, unrelated servers and configuration.
	position, priority := 0, 0
	insertion := "mcp_servers.stratus.env_vars = " + string(encoded) + nl
	var parser unstable.Parser
	parser.Reset(src)
	keys := func(node *unstable.Node) (parts []string, end int) {
		it := node.Key()
		for it.Next() {
			key := it.Node()
			parts = append(parts, string(key.Data))
			end = int(key.Raw.Offset + key.Raw.Length)
		}
		return
	}
	var visit func(*unstable.Node, []string)
	visit = func(node *unstable.Node, parent []string) {
		if node == nil || node.Kind != unstable.KeyValue {
			return
		}
		key, keyEnd := keys(node)
		path := append(append([]string{}, parent...), key...)
		joined := strings.Join(path, ".")
		value := node.Value()
		if joined == "mcp_servers.stratus.env_vars" && value.Kind == unstable.Array {
			equal := bytes.IndexByte(src[keyEnd:], '=')
			if equal >= 0 {
				start := keyEnd + equal + 1
				for start < len(src) && (src[start] == ' ' || src[start] == '\t') {
					start++
				}
				if start < len(src) && src[start] == '[' {
					position, priority = start+1, 3
					insertion = string(encoded[1:len(encoded)-1]) + ", "
				}
			}
		}
		if value.Kind == unstable.InlineTable {
			if joined == "mcp_servers" && priority == 0 {
				position = int(value.Raw.Offset) + 1
				insertion = "stratus.env_vars = " + string(encoded) + ", "
			}
			if joined == "mcp_servers.stratus" && priority < 2 {
				position, priority = int(value.Raw.Offset)+1, 2
				insertion = "env_vars = " + string(encoded) + ", "
			}
			for child := value.Child(); child != nil; child = child.Next() {
				visit(child, path)
			}
		}
	}
	var table []string
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			var end int
			table, end = keys(node)
			if strings.Join(table, ".") == "mcp_servers" && priority == 0 {
				if newline := bytes.IndexByte(src[end:], '\n'); newline >= 0 {
					position = end + newline + 1
					insertion = "stratus.env_vars = " + string(encoded) + nl
				}
			}
			if strings.Join(table, ".") == "mcp_servers.stratus" && priority < 1 {
				if newline := bytes.IndexByte(src[end:], '\n'); newline >= 0 {
					position, priority = end+newline+1, 1
					insertion = "env_vars = " + string(encoded) + nl
				}
			}
		} else {
			visit(node, table)
		}
	}
	if parser.Error() != nil {
		return nil, parser.Error()
	}
	result := make([]byte, 0, len(src)+len(insertion))
	result = append(result, src[:position]...)
	result = append(result, insertion...)
	result = append(result, src[position:]...)
	var check map[string]any
	if err := toml.Unmarshal(result, &check); err != nil {
		return nil, fmt.Errorf(".codex/config.toml: cannot safely extend Stratus env_vars: %w", err)
	}
	return result, nil
}
