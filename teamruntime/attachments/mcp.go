package attachments

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const mcpRequestTimeout = 25 * time.Second
const maxMCPMessage = 4 << 20

type mcpReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// mcpProcess speaks line-delimited standard MCP JSON-RPC. A terminal transport
// error resolves every waiter and never restarts or replays a request.
type mcpProcess struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	gate     chan struct{}
	done     chan struct{}
	exited   chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	err      error
	nextID   uint64
	pending  map[uint64]chan mcpReply
}

func startMCP(cmd *exec.Cmd) (*mcpProcess, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	// Provider credentials and unrelated helper diagnostics do not enter logs.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()
		return nil, err
	}
	p := &mcpProcess{cmd: cmd, stdin: in, stdout: out, gate: make(chan struct{}, 1), done: make(chan struct{}), exited: make(chan struct{}), pending: make(map[uint64]chan mcpReply)}
	go p.read()
	return p, nil
}

func (p *mcpProcess) read() {
	defer close(p.exited)
	scanner := bufio.NewScanner(p.stdout)
	scanner.Buffer(make([]byte, 64<<10), maxMCPMessage)
	for scanner.Scan() {
		var envelope struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			ID      json.RawMessage `json:"id"`
		}
		line := scanner.Bytes()
		if err := json.Unmarshal(line, &envelope); err != nil || envelope.JSONRPC != "2.0" {
			p.stop(errors.New("Codex helper returned invalid MCP JSON-RPC"))
			break
		}
		if len(envelope.ID) == 0 || string(envelope.ID) == "null" || envelope.Method != "" {
			continue // Native notifications are not task completion evidence.
		}
		var reply mcpReply
		if err := json.Unmarshal(line, &reply); err != nil {
			p.stop(fmt.Errorf("decode Codex MCP response: %w", err))
			break
		}
		p.mu.Lock()
		ch := p.pending[reply.ID]
		p.mu.Unlock()
		if ch != nil {
			select {
			case ch <- reply:
			default:
			}
		}
	}
	if err := scanner.Err(); err != nil {
		p.stop(fmt.Errorf("read Codex MCP response: %w", err))
	} else {
		p.stop(errors.New("Codex helper connection closed"))
	}
	_ = p.cmd.Wait()
}

func (p *mcpProcess) call(ctx context.Context, method string, params any, dst any) error {
	return p.exchange(ctx, method, params, dst, false)
}

func (p *mcpProcess) notify(ctx context.Context, method string) error {
	return p.exchange(ctx, method, nil, nil, true)
}

func (p *mcpProcess) exchange(ctx context.Context, method string, params any, dst any, notification bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, mcpRequestTimeout)
	defer cancel()
	select {
	case p.gate <- struct{}{}:
		defer func() { <-p.gate }()
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return p.failure()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-p.done:
		return p.failure()
	default:
	}
	request := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		request["params"] = params
	}
	var response chan mcpReply
	if !notification {
		p.mu.Lock()
		p.nextID++
		id := p.nextID
		response = make(chan mcpReply, 1)
		p.pending[id] = response
		p.mu.Unlock()
		defer func() { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }()
		request["id"] = id
	}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if len(data) >= maxMCPMessage {
		return errors.New("Codex MCP request exceeds the payload limit")
	}
	data = append(data, '\n')
	written := make(chan error, 1)
	go func() {
		_, err := p.stdin.Write(data)
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			p.stop(fmt.Errorf("write Codex MCP request: %w", err))
			return p.failure()
		}
	case <-ctx.Done():
		p.stop(ctx.Err())
		return ctx.Err()
	case <-p.done:
		return p.failure()
	}
	if notification {
		return nil
	}
	select {
	case reply := <-response:
		if reply.Error != nil {
			return fmt.Errorf("native MCP error %d: %s", reply.Error.Code, reply.Error.Message)
		}
		if len(reply.Result) == 0 || string(reply.Result) == "null" {
			return errors.New("Codex MCP response has no result")
		}
		if err := json.Unmarshal(reply.Result, dst); err != nil {
			return fmt.Errorf("decode Codex MCP result: %w", err)
		}
		return nil
	case <-ctx.Done():
		// A timed-out write or response is uncertain. Terminate the helper so a
		// late response cannot be mistaken for a subsequent request's receipt.
		p.stop(ctx.Err())
		return ctx.Err()
	case <-p.done:
		return p.failure()
	}
}

func (p *mcpProcess) failure() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *mcpProcess) stop(err error) {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.done)
		_ = p.stdin.Close()
		_ = p.stdout.Close()
		// This is the helper we started, never the desktop host or its group.
		_ = p.cmd.Process.Kill()
	})
}

func (p *mcpProcess) close() error {
	p.stop(errors.New("Codex attachment closed"))
	select {
	case <-p.exited:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("Codex helper did not exit after detach")
	}
}
