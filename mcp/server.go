// Package mcp implements the Model Context Protocol over stdio.
// Spec: https://modelcontextprotocol.io/specification
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Server handles MCP JSON-RPC communication over stdio.
type Server struct {
	tools    map[string]Tool
	reader   *bufio.Reader
	writer   io.Writer
}

// Tool represents a callable MCP tool.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     func(args map[string]any) (any, error)
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

		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.sendError(nil, -32700, "parse error")
			continue
		}

		s.handle(req)
	}
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handle(req jsonRPCRequest) {
	switch req.Method {
	case "initialize":
		s.send(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "stratus", "version": "2.0.0"},
		})

	case "tools/list":
		list := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			list = append(list, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
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

	case "notifications/initialized":
		// No response needed for notifications

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
