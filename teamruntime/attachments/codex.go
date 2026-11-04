// Package attachments connects to existing native chats without owning their
// application or opening another writer on their conversation history.
package attachments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CodexConfig is trusted host configuration, never model-supplied tool input.
// SourceThreadID must be the actual joining executor's native chat identity.
// Launcher and ServerPath name the official bundled Codex app-tools helper.
// When both are empty, they are discovered under CODEX_ELECTRON_RESOURCES_PATH.
// Env optionally overlays the inherited environment, primarily for fixtures.
type CodexConfig struct {
	SourceThreadID string
	Launcher       string
	ServerPath     string
	Env            []string
}

// Snapshot contains only native identity and the latest turn's status. Model
// and reasoning settings are not provided by this interface and remain unknown.
// Origin is user, team, or unknown; it does not establish task completion.
type Snapshot struct {
	NativeID      string        `json:"native_id"`
	ProjectRoot   string        `json:"project_root"`
	State         string        `json:"state"`
	ActiveFlags   []string      `json:"active_flags,omitempty"`
	TurnID        string        `json:"turn_id,omitempty"`
	TurnStatus    string        `json:"turn_status,omitempty"`
	Origin        string        `json:"origin"`
	PreviousTurns []TurnReceipt `json:"previous_turns,omitempty"`
}

// TurnReceipt proves the status of a bounded recent turn without retaining its
// items or text. It permits reconciliation when the next turn starts between polls.
type TurnReceipt struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// CodexClient owns only its standard MCP helper subprocess. Its native target
// is fixed for the lifetime of the connection and cannot be changed by Send.
type CodexClient struct {
	source string
	mcp    *mcpProcess
}

// OpenCodex starts the supplied official MCP helper and verifies the required
// native tools. It does not send a chat input or read conversation history.
// ctx bounds setup; the caller must Close the returned client on detach.
func OpenCodex(ctx context.Context, config CodexConfig) (*CodexClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.SourceThreadID == "" || strings.TrimSpace(config.SourceThreadID) != config.SourceThreadID || strings.ContainsAny(config.SourceThreadID, "\r\n\x00") {
		return nil, errors.New("Codex attachment requires the joining executor's native thread identity")
	}
	env := mergedEnvironment(config.Env)
	if environmentValue(env, "CODEX_APP_TOOLS_PIPE_PATH") == "" {
		return nil, errors.New("Codex attachment requires CODEX_APP_TOOLS_PIPE_PATH inherited from the native executor")
	}
	launcher, server := config.Launcher, config.ServerPath
	if launcher == "" && server == "" {
		resources := environmentValue(env, "CODEX_ELECTRON_RESOURCES_PATH")
		if resources == "" {
			return nil, errors.New("Codex app-tools helper unavailable: supply the official launcher and server paths, or inherit CODEX_ELECTRON_RESOURCES_PATH")
		}
		base := filepath.Join(resources, "plugins", "openai-bundled", "plugins", "codex-app-tools")
		launcher = filepath.Join(base, "scripts", "launch_codex_app_tools_mcp")
		server = filepath.Join(base, "server.mjs")
	}
	if err := validateHelperFile(launcher, true); err != nil {
		return nil, fmt.Errorf("Codex app-tools launcher: %w", err)
	}
	if err := validateHelperFile(server, false); err != nil {
		return nil, fmt.Errorf("Codex app-tools server: %w", err)
	}
	// No shell expansion, resume command, private IPC, or native-host ownership.
	cmd := exec.Command(launcher, server)
	cmd.Env = env
	process, err := startMCP(cmd)
	if err != nil {
		return nil, fmt.Errorf("start Codex app-tools helper: %w", err)
	}
	c := &CodexClient{source: config.SourceThreadID, mcp: process}
	ready := false
	defer func() {
		if !ready {
			_ = c.Close()
		}
	}()
	var initialized struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := process.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "stratus-chat-attachment", "version": "1"},
	}, &initialized); err != nil {
		return nil, fmt.Errorf("initialize Codex app-tools: %w", err)
	}
	if initialized.ProtocolVersion != "2025-06-18" {
		return nil, fmt.Errorf("Codex app-tools negotiated unsupported MCP protocol %q", initialized.ProtocolVersion)
	}
	if err := process.notify(ctx, "notifications/initialized"); err != nil {
		return nil, err
	}
	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := process.call(ctx, "tools/list", map[string]any{}, &catalog); err != nil {
		return nil, fmt.Errorf("list Codex app-tools: %w", err)
	}
	required := map[string]bool{"read_thread": false, "send_message_to_thread": false}
	for _, tool := range catalog.Tools {
		if _, ok := required[tool.Name]; ok {
			required[tool.Name] = true
		}
	}
	for name, present := range required {
		if !present {
			return nil, fmt.Errorf("Codex app-tools does not provide required native tool %s", name)
		}
	}
	ready = true
	return c, nil
}

// Snapshot uses a bounded native metadata read. The helper may return preview
// and item text; those fields are deliberately neither retained nor exposed.
func (c *CodexClient) Snapshot(ctx context.Context) (Snapshot, error) {
	var native struct {
		SchemaVersion int `json:"schemaVersion"`
		Thread        struct {
			ID     string `json:"id"`
			Cwd    string `json:"cwd"`
			Status struct {
				Type        string   `json:"type"`
				ActiveFlags []string `json:"activeFlags"`
			} `json:"status"`
		} `json:"thread"`
		Turns []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Items  []struct {
				Type      string `json:"type"`
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"items"`
		} `json:"turns"`
	}
	if err := c.tool(ctx, "read_thread", map[string]any{
		"threadId": c.source, "turnLimit": 3, "includeOutputs": false, "maxOutputCharsPerItem": 1000,
	}, &native); err != nil {
		return Snapshot{}, err
	}
	if native.Thread.ID != c.source {
		return Snapshot{}, errors.New("Codex native snapshot identity does not match the attached chat")
	}
	if native.SchemaVersion != 1 || native.Thread.Status.Type == "" {
		return Snapshot{}, errors.New("Codex native snapshot has an unsupported or missing status schema")
	}
	s := Snapshot{NativeID: native.Thread.ID, ProjectRoot: native.Thread.Cwd, State: native.Thread.Status.Type, ActiveFlags: native.Thread.Status.ActiveFlags, Origin: "unknown"}
	// Native read_thread returns turns newest first.
	if len(native.Turns) != 0 {
		turn := native.Turns[0]
		s.TurnID, s.TurnStatus = turn.ID, turn.Status
		for _, item := range turn.Items {
			if item.Type == "userMessage" {
				s.Origin = "user"
				break
			}
			if item.Type == "functionCallOutput" && item.Name == "send_message_to_thread" && item.Namespace == "codex_app" {
				s.Origin = "team"
			}
		}
	}
	for i := 1; i < len(native.Turns) && i < 3; i++ {
		turn := native.Turns[i]
		s.PreviousTurns = append(s.PreviousTurns, TurnReceipt{ID: turn.ID, Status: turn.Status})
	}
	return s, nil
}

// Send accepts input into the same native chat, preserving its current model
// and effort. Success means native acceptance only, never task completion. The
// host serializes delivery with regular turns and may deliver within a turn.
// Errors after submission may be ambiguous; this client never retries them.
func (c *CodexClient) Send(ctx context.Context, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("Codex team input must not be empty")
	}
	var receipt struct {
		ThreadID string `json:"threadId"`
	}
	if err := c.tool(ctx, "send_message_to_thread", map[string]any{"threadId": c.source, "prompt": text}, &receipt); err != nil {
		return err
	}
	if receipt.ThreadID != c.source {
		return errors.New("Codex native acceptance identity does not match the attached chat; delivery is uncertain")
	}
	return nil
}

// Close detaches by stopping only this client's helper. It never cancels a
// native turn, terminates the desktop host, or sends another chat input.
func (c *CodexClient) Close() error { return c.mcp.close() }

func (c *CodexClient) tool(ctx context.Context, name string, args map[string]any, dst any) error {
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := c.mcp.call(ctx, "tools/call", map[string]any{
		"name": name, "arguments": args,
		// Fresh default turn/call IDs are deliberately left to the official helper.
		"_meta": map[string]string{"openai/threadId": c.source},
	}, &result); err != nil {
		return fmt.Errorf("Codex %s: %w", name, err)
	}
	for _, part := range result.Content {
		if part.Type != "text" {
			continue
		}
		if result.IsError {
			message := part.Text
			if len(message) > 1000 {
				message = message[:1000] + "…"
			}
			return fmt.Errorf("Codex %s rejected: %s", name, message)
		}
		if err := json.Unmarshal([]byte(part.Text), dst); err != nil {
			return fmt.Errorf("Codex %s returned invalid result JSON: %w", name, err)
		}
		return nil
	}
	if result.IsError {
		return fmt.Errorf("Codex %s rejected without text details", name)
	}
	return fmt.Errorf("Codex %s returned no native result", name)
}

func validateHelperFile(path string, executable bool) error {
	if !filepath.IsAbs(path) {
		return errors.New("an absolute official helper path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("helper path is not a regular file")
	}
	if executable && info.Mode().Perm()&0111 == 0 {
		return errors.New("helper launcher is not executable")
	}
	return nil
}

func mergedEnvironment(overrides []string) []string {
	env := os.Environ()
	for _, value := range overrides {
		key, _, ok := strings.Cut(value, "=")
		if !ok || key == "" {
			continue
		}
		for i := len(env) - 1; i >= 0; i-- {
			oldKey, _, _ := strings.Cut(env[i], "=")
			if oldKey == key {
				env = append(env[:i], env[i+1:]...)
			}
		}
		env = append(env, value)
	}
	return env
}

func environmentValue(env []string, name string) string {
	for i := len(env) - 1; i >= 0; i-- {
		key, value, _ := strings.Cut(env[i], "=")
		if key == name {
			return value
		}
	}
	return ""
}
