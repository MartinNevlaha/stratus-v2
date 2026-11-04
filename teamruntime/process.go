package teamruntime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type object = map[string]any

// lineProcess keeps stdout framing and request correlation independent of model
// turns. A closed/failed transport is terminal; accepted work is never replayed.
type lineProcess struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	writeMu   sync.Mutex
	mu        sync.Mutex
	pending   map[string]chan object
	err       error
	events    chan object
	done      chan struct{}
	stop      chan struct{}
	closeOnce sync.Once
	seq       atomic.Uint64
	claude    bool
}

func childEnv(runtime, token string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if k == "STRATUS_TEAM_TOKEN" {
			continue
		}
		if runtime == "claude-code" && k == "CLAUDECODE" {
			continue
		}
		env = append(env, v)
	}
	if token != "" {
		env = append(env, "STRATUS_TEAM_TOKEN="+token)
	}
	return env
}

func startLines(cmd *exec.Cmd, claude bool) (*lineProcess, error) {
	prepareProcess(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	// Never forward inherited environment or provider credentials through logs.
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	p := &lineProcess{cmd: cmd, stdin: in, pending: map[string]chan object{}, events: make(chan object, 512), done: make(chan struct{}), stop: make(chan struct{}), claude: claude}
	go func() {
		scanner := bufio.NewScanner(out)
		scanner.Buffer(make([]byte, 64<<10), 8<<20)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			// Claude itself can print diagnostic, non-JSON lines to stdout.
			if claude && !strings.HasPrefix(line, "{") {
				continue
			}
			var m object
			if e := json.Unmarshal([]byte(line), &m); e != nil {
				p.setError(fmt.Errorf("invalid native protocol JSON: %w", e))
				killProcess(cmd)
				break
			}
			key := ""
			if claude && str(m, "type") == "control_response" {
				key = str(obj(m, "response"), "request_id")
			} else if !claude && m["method"] == nil && m["id"] != nil {
				key = fmt.Sprint(m["id"])
			}
			if key != "" {
				p.mu.Lock()
				ch := p.pending[key]
				p.mu.Unlock()
				if ch != nil {
					select {
					case ch <- m:
					default:
					}
				}
				continue
			}
			select {
			case p.events <- m:
			case <-p.stop:
				break
			default:
				p.setError(errors.New("native event buffer overflow; session stopped"))
				killProcess(cmd)
			}
		}
		if err := scanner.Err(); err != nil {
			p.setError(fmt.Errorf("read native stream: %w", err))
		}
		if err := cmd.Wait(); err != nil {
			p.setError(fmt.Errorf("native process exited: %w", err))
		}
		p.setError(errors.New("native process closed"))
		close(p.done)
	}()
	return p, nil
}

func (p *lineProcess) setError(err error) {
	p.mu.Lock()
	if p.err == nil {
		p.err = err
	}
	p.mu.Unlock()
}
func (p *lineProcess) failure() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	return errors.New("native process closed")
}
func (p *lineProcess) send(m object) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	select {
	case <-p.stop:
		return errors.New("native session closed")
	case <-p.done:
		return p.failure()
	default:
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

func (p *lineProcess) call(ctx context.Context, method string, params object) (object, error) {
	key := fmt.Sprintf("stratus-%d", p.seq.Add(1))
	ch := make(chan object, 1)
	p.mu.Lock()
	p.pending[key] = ch
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, key); p.mu.Unlock() }()
	m := object{"id": key, "method": method, "params": params}
	if p.claude {
		params["subtype"] = method
		m = object{"type": "control_request", "request_id": key, "request": params}
	}
	if err := p.send(m); err != nil {
		return nil, err
	}
	select {
	case m := <-ch:
		if p.claude {
			r := obj(m, "response")
			if str(r, "subtype") != "success" {
				return nil, fmt.Errorf("Claude control %s: %v", method, r["error"])
			}
			return obj(r, "response"), nil
		}
		if m["error"] != nil {
			return nil, fmt.Errorf("native %s: %s", method, compact(m["error"]))
		}
		return obj(m, "result"), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.done:
		// A response followed immediately by exit is still a response.
		select {
		case m := <-ch:
			if m["error"] == nil && !p.claude {
				return obj(m, "result"), nil
			}
		default:
		}
		return nil, p.failure()
	case <-p.stop:
		return nil, errors.New("native session closed")
	}
}

func (p *lineProcess) Close() error {
	p.closeOnce.Do(func() { close(p.stop); p.stdin.Close(); terminateProcess(p.cmd) })
	select {
	case <-p.done:
		return nil
	case <-time.After(2 * time.Second):
		killProcess(p.cmd)
	}
	select {
	case <-p.done:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("native process did not exit after termination")
	}
}

func obj(m object, k string) object { v, _ := m[k].(map[string]any); return v }
func str(m object, k string) string { v, _ := m[k].(string); return v }
func arr(m object, k string) []any  { v, _ := m[k].([]any); return v }
func compact(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 8192 {
		return string(b[:8192]) + " [truncated]"
	}
	return string(b)
}
func number(m object, k string) *int64 {
	v, ok := m[k].(float64)
	if !ok {
		return nil
	}
	n := int64(v)
	return &n
}
