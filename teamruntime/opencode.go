package teamruntime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
	"github.com/google/uuid"
)

type openCodeSession struct {
	cmd                      *exec.Cmd
	done                     chan struct{}
	closeOnce                sync.Once
	spec                     teams.SessionSpec
	base, password, nativeID string
	client                   *http.Client
	models                   []teams.Model
	emit                     func(teams.Event)
	submitMu                 sync.Mutex
	mu                       sync.Mutex
	permissions              map[string]string
	active                   bool
	events                   chan object
	streamErr                chan error
	streamCancel             context.CancelFunc
	stop                     chan struct{}
}

var openCodeURL = regexp.MustCompile(`http://127\.0\.0\.1:[0-9]+`)

func (a *nativeAdapter) openOpenCode(ctx context.Context, spec teams.SessionSpec, emit func(teams.Event), probe bool) (*openCodeSession, error) {
	password := uuid.NewString()
	cmd := a.command(spec.ProjectRoot, "serve", "--hostname", "127.0.0.1", "--port", "0")
	cmd.Env = replaceEnv(childEnv(a.runtime, spec.Token), map[string]string{"OPENCODE_SERVER_USERNAME": "opencode", "OPENCODE_SERVER_PASSWORD": password, "STRATUS_TEAM_API_BASE": spec.APIBase})
	if err := configureOpenCodeMCP(cmd, spec); err != nil {
		return nil, err
	}
	prepareProcess(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		out.Close()
		return nil, err
	}
	s := &openCodeSession{cmd: cmd, done: make(chan struct{}), spec: spec, password: password, emit: emit, permissions: map[string]string{}, events: make(chan object, 512), streamErr: make(chan error, 1), stop: make(chan struct{}), client: &http.Client{Timeout: 15 * time.Second}}
	ready := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(out)
		scan.Buffer(make([]byte, 4096), 1<<20)
		for scan.Scan() {
			line := strings.TrimSpace(scan.Text())
			if !strings.HasPrefix(line, "opencode server listening on ") {
				continue
			}
			if address := openCodeURL.FindString(line); address != "" {
				select {
				case ready <- address:
				default:
				}
			}
		}
		_ = cmd.Wait()
		close(s.done)
	}()
	ok := false
	defer func() {
		if !ok {
			s.Close()
		}
	}()
	select {
	case s.base = <-ready:
	case <-ctx.Done():
		return nil, fmt.Errorf("OpenCode startup: %w", ctx.Err())
	case <-s.done:
		return nil, errors.New("OpenCode exited before reporting its owned loopback address")
	}
	var health object
	if err = s.request(ctx, "GET", "/global/health", nil, &health); err != nil {
		return nil, err
	}
	if health["healthy"] != true {
		return nil, errors.New("OpenCode health check failed")
	}
	s.models, err = s.loadModels(ctx)
	if err != nil {
		return nil, err
	}
	if len(s.models) == 0 {
		return nil, errors.New("OpenCode has no connected provider advertising tool-capable models")
	}
	if probe {
		ok = true
		return s, nil
	}
	if err = validateModel(s.models, spec.Profile); err != nil {
		return nil, err
	}
	provider, model, valid := strings.Cut(spec.Profile.Model, "/")
	if !valid {
		return nil, errors.New("OpenCode model must be provider/model")
	}
	var session object
	if spec.NativeID != "" {
		if !strings.HasPrefix(spec.NativeID, "ses") {
			return nil, errors.New("invalid OpenCode session identity")
		}
		if err = s.request(ctx, "GET", "/session/"+url.PathEscape(spec.NativeID), nil, &session); err != nil {
			return nil, err
		}
		if filepath.Clean(str(session, "directory")) != filepath.Clean(spec.ProjectRoot) {
			return nil, errors.New("OpenCode session belongs to another working directory")
		}
	} else {
		modelSpec := object{"id": model, "providerID": provider}
		if spec.Profile.Effort != "" {
			modelSpec["variant"] = spec.Profile.Effort
		}
		if err = s.request(ctx, "POST", "/session", object{"title": "Stratus " + spec.TeamID + " / " + spec.MemberID, "model": modelSpec}, &session); err != nil {
			return nil, err
		}
	}
	s.nativeID = str(session, "id")
	if s.nativeID == "" || spec.NativeID != "" && s.nativeID != spec.NativeID {
		return nil, errors.New("OpenCode did not confirm the requested session identity")
	}
	if err = s.subscribe(ctx); err != nil {
		return nil, err
	}
	s.publish(teams.Event{Kind: "identity", NativeID: s.nativeID})
	ok = true
	return s, nil
}

func (s *openCodeSession) endpoint(path string) string {
	return s.base + path + "?directory=" + url.QueryEscape(s.spec.ProjectRoot)
}
func (s *openCodeSession) request(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	r, err := http.NewRequestWithContext(ctx, method, s.endpoint(path), reader)
	if err != nil {
		return err
	}
	r.SetBasicAuth("opencode", s.password)
	r.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(r)
	if err != nil {
		return fmt.Errorf("OpenCode %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("OpenCode %s %s: HTTP %d: %s", method, path, resp.StatusCode, string(b))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err = json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out); err != nil {
			return fmt.Errorf("OpenCode response: %w", err)
		}
	}
	return nil
}

func (s *openCodeSession) loadModels(ctx context.Context) ([]teams.Model, error) {
	var catalog object
	if err := s.request(ctx, "GET", "/provider", nil, &catalog); err != nil {
		return nil, err
	}
	connected := map[string]bool{}
	for _, v := range arr(catalog, "connected") {
		if id, ok := v.(string); ok {
			connected[id] = true
		}
	}
	models := []teams.Model{}
	for _, v := range arr(catalog, "all") {
		provider, _ := v.(map[string]any)
		id := str(provider, "id")
		if !connected[id] {
			continue
		}
		for key, v := range obj(provider, "models") {
			m, _ := v.(map[string]any)
			if toolcall, ok := obj(m, "capabilities")["toolcall"].(bool); !ok || !toolcall {
				continue
			}
			efforts := []string{}
			for effort := range obj(m, "variants") {
				efforts = append(efforts, effort)
			}
			models = append(models, teams.Model{ID: id + "/" + key, Efforts: efforts})
		}
	}
	sortModels(models)
	return models, nil
}

func (s *openCodeSession) subscribe(openCtx context.Context) error {
	// The handshake is bounded by Open; the stream lifetime belongs to Close.
	ctx, cancel := context.WithCancel(context.Background())
	s.streamCancel = cancel
	r, err := http.NewRequestWithContext(ctx, "GET", s.endpoint("/event"), nil)
	if err != nil {
		return err
	}
	r.SetBasicAuth("opencode", s.password)
	r.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 15 * time.Second}}
	type response struct {
		r   *http.Response
		err error
	}
	ready := make(chan response, 1)
	go func() { r, err := client.Do(r); ready <- response{r, err} }()
	var res response
	select {
	case res = <-ready:
	case <-openCtx.Done():
		cancel()
		res = <-ready
		if res.r != nil {
			res.r.Body.Close()
		}
		return openCtx.Err()
	}
	if res.err != nil {
		cancel()
		return res.err
	}
	if res.r.StatusCode != 200 {
		res.r.Body.Close()
		cancel()
		return fmt.Errorf("OpenCode event stream HTTP %d", res.r.StatusCode)
	}
	go func() {
		defer res.r.Body.Close()
		defer client.CloseIdleConnections()
		scan := bufio.NewScanner(res.r.Body)
		scan.Buffer(make([]byte, 64<<10), 8<<20)
		var data strings.Builder
		push := func() bool {
			if data.Len() == 0 {
				return true
			}
			var event object
			err := json.Unmarshal([]byte(strings.TrimSuffix(data.String(), "\n")), &event)
			data.Reset()
			if err != nil {
				select {
				case s.streamErr <- fmt.Errorf("invalid OpenCode event: %w", err):
				default:
				}
				return false
			}
			if !s.relevantEvent(event) {
				return true
			}
			select {
			case s.events <- event:
				return true
			case <-ctx.Done():
				return false
			default:
				select {
				case s.streamErr <- errors.New("OpenCode event buffer overflow"):
				default:
				}
				return false
			}
		}
		for scan.Scan() {
			line := scan.Text()
			if line == "" {
				if !push() {
					return
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
				data.WriteByte('\n')
			}
		}
		err := scan.Err()
		if err == nil {
			err = io.EOF
		}
		select {
		case s.streamErr <- fmt.Errorf("OpenCode event stream ended: %w", err):
		default:
		}
	}()
	return nil
}

func (s *openCodeSession) publish(e teams.Event) {
	if s.emit != nil {
		s.emit(e)
	}
}

func (s *openCodeSession) relevantEvent(event object) bool {
	if str(obj(event, "properties"), "sessionID") != s.nativeID {
		return false
	}
	switch str(event, "type") {
	case "permission.asked", "permission.v2.asked", "permission.replied", "permission.v2.replied", "question.asked", "question.v2.asked", "message.part.delta", "session.error", "session.status", "session.idle":
		return true
	default:
		return false
	}
}

func (s *openCodeSession) Submit(ctx context.Context, in teams.Input) (teams.Result, error) {
	result := teams.Result{NativeID: s.nativeID}
	if !s.submitMu.TryLock() {
		return result, errors.New("native session already has an active turn")
	}
	defer s.submitMu.Unlock()
	if in.ID == "" || strings.TrimSpace(in.Text) == "" {
		return result, errors.New("input id and text are required")
	}
	select {
	case <-s.stop:
		return result, errors.New("OpenCode session closed")
	case <-s.done:
		return result, errors.New("OpenCode process exited")
	case err := <-s.streamErr:
		s.Close()
		return result, err
	default:
	}
	models, err := s.loadModels(ctx)
	if err != nil {
		return result, err
	}
	if err = validateModel(models, s.spec.Profile); err != nil {
		return result, err
	}
	provider, model, _ := strings.Cut(s.spec.Profile.Model, "/")
	messageID := "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	body := object{"messageID": messageID, "model": object{"providerID": provider, "modelID": model}, "parts": []any{object{"type": "text", "text": in.Text}}, "system": s.spec.Instructions}
	if s.spec.Profile.Effort != "" {
		body["variant"] = s.spec.Profile.Effort
	}
	s.mu.Lock()
	s.active = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.active = false; s.permissions = map[string]string{}; s.mu.Unlock() }()
	if err = s.request(ctx, "POST", "/session/"+url.PathEscape(s.nativeID)+"/prompt_async", body, nil); err != nil {
		s.Close()
		return result, fmt.Errorf("OpenCode turn acceptance uncertain; not replayed: %w", err)
	}
	s.publish(teams.Event{Kind: "status", ID: in.ID, NativeID: s.nativeID, Text: "accepted"})
	for {
		select {
		case event := <-s.events:
			p := obj(event, "properties")
			sessionID := str(p, "sessionID")
			if sessionID == "" {
				sessionID = str(obj(p, "info"), "sessionID")
			}
			if sessionID != "" && sessionID != s.nativeID {
				continue
			}
			switch str(event, "type") {
			case "permission.asked", "permission.v2.asked":
				if sessionID != s.nativeID {
					continue
				}
				id := str(p, "id")
				if id == "" {
					continue
				}
				s.mu.Lock()
				s.permissions[id] = str(event, "type")
				s.mu.Unlock()
				s.publish(teams.Event{Kind: "approval", ID: id, NativeID: s.nativeID, Text: compact(p)})
			case "permission.replied", "permission.v2.replied":
				s.mu.Lock()
				delete(s.permissions, str(p, "requestID"))
				s.mu.Unlock()
			case "question.asked", "question.v2.asked":
				s.Close()
				return result, fmt.Errorf("OpenCode needs a written answer: %s", compact(p["questions"]))
			case "message.part.delta":
				if str(p, "field") == "text" {
					s.publish(teams.Event{Kind: "output", NativeID: s.nativeID, Text: str(p, "delta")})
				}
			case "session.error":
				s.Close()
				return result, fmt.Errorf("OpenCode session error: %s", compact(p["error"]))
			case "session.status", "session.idle":
				if sessionID != s.nativeID {
					continue
				}
				if str(event, "type") == "session.status" && str(obj(p, "status"), "type") != "idle" {
					continue
				}
				completed, r, err := s.completed(ctx, messageID)
				if err != nil {
					s.Close()
					return result, err
				}
				if completed {
					return r, nil
				}
			}
		case err := <-s.streamErr:
			s.Close()
			return result, err
		case <-s.done:
			return result, errors.New("OpenCode process exited during turn")
		case <-s.stop:
			return result, errors.New("OpenCode session closed during turn")
		case <-ctx.Done():
			c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.Interrupt(c)
			cancel()
			s.Close()
			return result, fmt.Errorf("OpenCode turn cancelled; not replayed: %w", ctx.Err())
		}
	}
}

func (s *openCodeSession) completed(ctx context.Context, parentID string) (bool, teams.Result, error) {
	r := teams.Result{NativeID: s.nativeID}
	var messages []object
	if err := s.request(ctx, "GET", "/session/"+url.PathEscape(s.nativeID)+"/message", nil, &messages); err != nil {
		return false, r, err
	}
	texts := []string{}
	complete := false
	known := true
	var in, out int64
	for _, message := range messages {
		info := obj(message, "info")
		if str(info, "role") != "assistant" || str(info, "parentID") != parentID {
			continue
		}
		if info["error"] != nil {
			return false, r, fmt.Errorf("OpenCode turn failed: %s", compact(info["error"]))
		}
		observed := str(info, "providerID") + "/" + str(info, "modelID")
		if observed != s.spec.Profile.Model {
			return false, r, fmt.Errorf("OpenCode used model %q instead of %q", observed, s.spec.Profile.Model)
		}
		variant := str(info, "variant")
		if variant != "" && s.spec.Profile.Effort != "" && variant != s.spec.Profile.Effort {
			return false, r, fmt.Errorf("OpenCode used variant %q instead of %q", variant, s.spec.Profile.Effort)
		}
		r.Model = observed
		r.Effort = variant
		if finish := str(info, "finish"); finish != "" && finish != "tool-calls" && obj(info, "time")["completed"] != nil {
			if finish != "stop" {
				return false, r, fmt.Errorf("OpenCode ended with finish reason %q", finish)
			}
			complete = true
		}
		usage := obj(info, "tokens")
		a, b := number(usage, "input"), number(usage, "output")
		if a == nil || b == nil {
			known = false
		} else {
			in += *a
			out += *b
			for _, key := range []string{"read", "write"} {
				if n := number(obj(usage, "cache"), key); n != nil {
					in += *n
				}
			}
		}
		for _, v := range arr(message, "parts") {
			part, _ := v.(map[string]any)
			if str(part, "type") == "text" {
				texts = append(texts, str(part, "text"))
			}
		}
	}
	if complete {
		r.Text = strings.Join(texts, "\n")
		if known {
			r.InputTokens = &in
			r.OutputTokens = &out
		}
		s.publish(teams.Event{Kind: "identity", NativeID: r.NativeID, Model: r.Model, Effort: r.Effort})
	}
	return complete, r, nil
}

func (s *openCodeSession) Approve(ctx context.Context, id string, allow bool) error {
	s.mu.Lock()
	kind, ok := s.permissions[id]
	if ok {
		delete(s.permissions, id)
	}
	s.mu.Unlock()
	if !ok {
		return errors.New("approval is no longer pending")
	}
	path := "/permission/" + url.PathEscape(id) + "/reply"
	if kind == "permission.v2.asked" {
		path = "/api/session/" + url.PathEscape(s.nativeID) + "/permission/" + url.PathEscape(id) + "/reply"
	}
	reply := "reject"
	if allow {
		reply = "once"
	}
	return s.request(ctx, "POST", path, object{"reply": reply}, nil)
}
func (s *openCodeSession) Interrupt(ctx context.Context) error {
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if !active {
		return nil
	}
	return s.request(ctx, "POST", "/session/"+url.PathEscape(s.nativeID)+"/abort", object{}, nil)
}
func (s *openCodeSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.stop)
		if s.streamCancel != nil {
			s.streamCancel()
		}
		s.client.CloseIdleConnections()
		terminateProcess(s.cmd)
	})
	select {
	case <-s.done:
		return nil
	case <-time.After(2 * time.Second):
		killProcess(s.cmd)
	}
	select {
	case <-s.done:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("OpenCode did not exit after termination")
	}
}

func replaceEnv(env []string, values map[string]string) []string {
	result := make([]string, 0, len(env)+len(values))
	for _, v := range env {
		k, _, _ := strings.Cut(v, "=")
		if _, ok := values[k]; !ok {
			result = append(result, v)
		}
	}
	for k, v := range values {
		result = append(result, k+"="+v)
	}
	return result
}

func configureOpenCodeMCP(cmd *exec.Cmd, spec teams.SessionSpec) error {
	if spec.MCPBinary == "" {
		return nil
	}
	config := object{}
	if raw := os.Getenv("OPENCODE_CONFIG_CONTENT"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return fmt.Errorf("existing OPENCODE_CONFIG_CONTENT is invalid: %w", err)
		}
	}
	mcp := obj(config, "mcp")
	if mcp == nil {
		mcp = object{}
	}
	mcp["stratus"] = object{"type": "local", "command": []string{spec.MCPBinary, "mcp-serve"}, "enabled": true}
	config["mcp"] = mcp
	b, err := json.Marshal(config)
	if err != nil {
		return err
	}
	cmd.Env = replaceEnv(cmd.Env, map[string]string{"OPENCODE_CONFIG_CONTENT": string(b)})
	return nil
}
