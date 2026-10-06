// Package mcp implements the Model Context Protocol over stdio.
// Spec: https://modelcontextprotocol.io/specification
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
)

// protocolVersions are the MCP revisions this server speaks, newest first. A client is
// answered with the revision it asked for when it is one of these, else the newest.
// 2025-03-26 is left out: it requires JSON-RPC batches, which this server does not handle.
var protocolVersions = []string{"2025-06-18", "2024-11-05"}

// instructions reach the model with the tool list (Claude Code shows them as server
// instructions), so they say when to use which tool, not how each one works.
const instructions = `Stratus tracks this project's spec/bug/e2e workflows, memory, governance docs, wiki and swarm missions.
- Register a workflow with register_workflow before delegating to any delivery-* agent, and pass its workflow_id with every delegation.
- Move between phases only with transition_phase; the server rejects transitions the workflow's state machine does not allow.
- Before planning, implementing or reviewing, use retrieve for code, governance and wiki context; search, timeline and get_observations recall past decisions.
- swarm_* tools are for swarm workers following the worker_instructions they were given.`

// Server handles MCP JSON-RPC communication over stdio.
type Server struct {
	tools  map[string]Tool
	reader *bufio.Reader
	writer io.Writer
}

// Tool represents a callable MCP tool.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	// ReadOnly marks a tool that only reads Stratus state (annotated readOnlyHint).
	ReadOnly bool
	Handler  func(args map[string]any) (any, error)
}

// New creates a new MCP server reading from stdin and writing to stdout.
func New() *Server {
	return &Server{
		tools:  make(map[string]Tool),
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
	}
}

// Register adds a tool to the server.
func (s *Server) Register(t Tool) {
	s.tools[t.Name] = t
}

// Serve runs the MCP request/response loop until EOF.
func (s *Server) Serve() error {
	for {
		line, err := s.reader.ReadString('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}

		s.handleLine([]byte(line))
	}
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	// ID stays raw to tell a notification (no id) from a request with a null id.
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handleLine handles one JSON-RPC message read from stdin.
func (s *Server) handleLine(line []byte) {
	var req jsonRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.sendError(nil, -32700, "parse error")
		return
	}
	if string(req.ID) == "null" {
		s.sendError(nil, -32600, "invalid request: id must not be null")
		return
	}
	s.handle(req)
}

func (s *Server) handle(req jsonRPCRequest) {
	// A message without an id is a notification: it never gets a response.
	if len(req.ID) == 0 {
		return
	}
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := protocolVersions[0]
		if slices.Contains(protocolVersions, params.ProtocolVersion) {
			version = params.ProtocolVersion
		}
		s.send(req.ID, map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "stratus", "version": "2.0.0"},
			"instructions":    instructions,
		})

	case "ping":
		s.send(req.ID, map[string]any{})

	case "tools/list":
		// Sorted, so every session sees the same tool list.
		list := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			entry := map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			}
			if t.ReadOnly {
				entry["annotations"] = map[string]any{"readOnlyHint": true}
			}
			list = append(list, entry)
		}
		sort.Slice(list, func(i, j int) bool { return list[i]["name"].(string) < list[j]["name"].(string) })
		s.send(req.ID, map[string]any{"tools": list})

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.sendError(req.ID, -32602, "invalid params")
			return
		}
		tool, ok := s.tools[params.Name]
		if !ok {
			s.sendError(req.ID, -32601, fmt.Sprintf("tool %q not found", params.Name))
			return
		}
		result, err := tool.Handler(params.Arguments)
		if err != nil {
			s.send(req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "error: " + err.Error()}},
				"isError": true,
			})
			return
		}
		text, _ := json.Marshal(result)
		s.send(req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(text)}},
		})

	default:
		s.sendError(req.ID, -32601, fmt.Sprintf("method %q not found", req.Method))
	}
}

func (s *Server) send(id any, result any) {
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
	s.write(resp)
}

func (s *Server) sendError(id any, code int, msg string) {
	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg},
	}
	s.write(resp)
}

func (s *Server) write(resp jsonRPCResponse) {
	data, _ := json.Marshal(resp)
	_, _ = fmt.Fprintf(s.writer, "%s\n", data)
}
