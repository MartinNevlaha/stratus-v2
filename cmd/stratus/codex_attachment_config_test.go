package main

import (
	"bytes"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestCodexAttachmentConfigForwardsHostConnectionWithoutChangingUserSettings(t *testing.T) {
	for _, src := range []string{
		"# user's settings\nmodel='personal-model'\n",
		"[mcp_servers.stratus]\ncommand='stratus'\nargs=['mcp-serve','--runtime','codex']\nenabled=false # preserve\nenv_vars=['MY_CUSTOM_VALUE'] # preserve list comment\n[mcp_servers.other]\ncommand='other'\nenv_vars=['OTHER_VALUE']\n",
		"mcp_servers.stratus={command='stratus',args=['mcp-serve'],env_vars=[]} # inline\n",
		"mcp_servers.stratus={command='stratus',args=['mcp-serve']} # inline\n",
		"[mcp_servers.\"stratus\"]\r\ncommand='stratus'\r\nargs=['mcp-serve']\r\nenv_vars=[\r\n # retain array comment\r\n 'MY_CUSTOM_VALUE',\r\n]\r\n",
		"mcp_servers.stratus.command='stratus'\nmcp_servers.stratus.args=['mcp-serve']\n",
		"[mcp_servers]\nstratus.command='stratus'\nstratus.args=['mcp-serve']\n",
		"mcp_servers={stratus.command='stratus',stratus.args=['mcp-serve']}\n",
	} {
		t.Run(src, func(t *testing.T) {
			got, err := mergeCodexTOML([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			var parsed map[string]any
			if err := toml.Unmarshal(got, &parsed); err != nil {
				t.Fatal(err)
			}
			servers := parsed["mcp_servers"].(map[string]any)
			entry := servers["stratus"].(map[string]any)
			list, _ := entry["env_vars"].([]any)
			set := map[string]bool{}
			for _, item := range list {
				name := item.(string)
				if set[name] {
					t.Fatalf("duplicated variable %s", name)
				}
				set[name] = true
			}
			for _, name := range []string{"CODEX_APP_TOOLS_PIPE_PATH", "CODEX_ELECTRON_RESOURCES_PATH", "CODEX_MCP_NODE_PATH", "CODEX_BROWSER_USE_NODE_PATH", "CODEX_CLI_PATH", "CODEX_APP_TOOLS_CALLER_HOST_ID", "STRATUS_CODEX_APP_TOOLS_LAUNCHER", "STRATUS_CODEX_APP_TOOLS_SERVER"} {
				if !set[name] {
					t.Errorf("missing native connector variable %s", name)
				}
			}
			if set["CODEX_THREAD_ID"] || set["CODEX_TOOL_CALL_ID"] {
				t.Fatal("shared MCP must use per-call identity, not captured executor identity")
			}
			for _, fragment := range []string{"# user's settings", "model='personal-model'", "enabled=false # preserve", "# preserve list comment", "# retain array comment", "# inline", "env_vars=['OTHER_VALUE']"} {
				if bytes.Contains([]byte(src), []byte(fragment)) && !bytes.Contains(got, []byte(fragment)) {
					t.Errorf("user text changed: %s", fragment)
				}
			}
			if bytes.Contains([]byte(src), []byte("MY_CUSTOM_VALUE")) && !set["MY_CUSTOM_VALUE"] {
				t.Fatal("custom forwarded variable lost")
			}
			again, err := mergeCodexTOML(got)
			if err != nil || !bytes.Equal(got, again) {
				t.Fatalf("refresh not idempotent: %v", err)
			}
		})
	}
}
