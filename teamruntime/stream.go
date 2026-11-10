package teamruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
	"github.com/google/uuid"
)

type permission struct {
	id     any
	method string
	params object
}
type streamSession struct {
	p              *lineProcess
	spec           teams.SessionSpec
	emit           func(teams.Event)
	models         []teams.Model
	resolved       string
	mu             sync.Mutex
	submitMu       sync.Mutex
	nativeID       string
	turnID         string
	observedModel  string
	observedEffort string
	usageBaseIn    *int64
	usageBaseOut   *int64
	permissions    map[string]permission
}

func (a *nativeAdapter) openStream(ctx context.Context, spec teams.SessionSpec, emit func(teams.Event), probe bool) (*streamSession, error) {
	args := []string{"app-server", "--stdio"}
	claude := a.runtime == "claude-code"
	if claude {
		if err := a.claudeAuthenticated(ctx, spec); err != nil {
			return nil, err
		}
		args = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--permission-prompt-tool", "stdio", "--setting-sources", "user,project,local"}
		if spec.Profile.Model != "" {
			args = append(args, "--model", spec.Profile.Model)
		}
		if spec.Profile.Effort != "" {
			args = append(args, "--effort", spec.Profile.Effort)
		}
		if spec.Instructions != "" {
			args = append(args, "--append-system-prompt", spec.Instructions)
		}
		if spec.NativeID != "" {
			args = append(args, "--resume", spec.NativeID)
		}
		if spec.MCPBinary != "" {
			config, _ := json.Marshal(object{"mcpServers": object{"stratus": object{"command": spec.MCPBinary, "args": []string{"mcp-serve"}}}})
			args = append(args, "--mcp-config", string(config))
		}
	}
	cmd := a.command(spec.ProjectRoot, args...)
	cmd.Env = childEnv(a.runtime, spec.Token)
	cmd.Env = replaceEnv(cmd.Env, map[string]string{"STRATUS_TEAM_API_BASE": spec.APIBase})
	p, err := startLines(cmd, claude)
	if err != nil {
		return nil, err
	}
	s := &streamSession{p: p, spec: spec, emit: emit, permissions: map[string]permission{}, nativeID: spec.NativeID}
	ok := false
	defer func() {
		if !ok {
			p.Close()
		}
	}()
	if claude {
		r, err := p.call(ctx, "initialize", object{"hooks": nil})
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, v := range arr(r, "models") {
			m, _ := v.(map[string]any)
			efforts := []string{}
			for _, e := range arr(m, "supportedEffortLevels") {
				if e, ok := e.(string); ok {
					efforts = append(efforts, e)
				}
			}
			for _, id := range []string{str(m, "value"), str(m, "resolvedModel")} {
				if id != "" && !seen[id] {
					seen[id] = true
					s.models = append(s.models, teams.Model{ID: id, Efforts: efforts})
				}
			}
			if str(m, "value") == spec.Profile.Model || str(m, "resolvedModel") == spec.Profile.Model {
				s.resolved = str(m, "resolvedModel")
				if s.resolved == "" {
					s.resolved = spec.Profile.Model
				}
			}
		}
		if !probe {
			if err := validateModel(s.models, spec.Profile); err != nil {
				return nil, err
			}
		}
	} else {
		if _, err = p.call(ctx, "initialize", object{"clientInfo": object{"name": "stratus-team", "title": "Stratus managed team", "version": "1"}}); err != nil {
			return nil, err
		}
		if err = p.send(object{"method": "initialized", "params": object{}}); err != nil {
			return nil, err
		}
		account, e := p.call(ctx, "account/read", object{})
		if e != nil {
			return nil, e
		}
		if need, _ := account["requiresOpenaiAuth"].(bool); need && account["account"] == nil {
			return nil, errors.New("Codex authentication is required")
		}
		s.models, err = codexModels(ctx, p)
		if err != nil {
			return nil, err
		}
		if !probe {
			if err = validateModel(s.models, spec.Profile); err != nil {
				return nil, err
			}
			params := object{"cwd": spec.ProjectRoot, "model": spec.Profile.Model, "developerInstructions": spec.Instructions, "approvalPolicy": "on-request", "sandbox": "workspace-write", "config": object{"model_reasoning_effort": spec.Profile.Effort}}
			if spec.MCPBinary != "" {
				config := obj(params, "config")
				config["mcp_servers.stratus.command"] = spec.MCPBinary
				config["mcp_servers.stratus.args"] = []string{"mcp-serve", "--runtime", "codex"}
				config["mcp_servers.stratus.env_vars"] = []string{"STRATUS_TEAM_TOKEN", "STRATUS_TEAM_API_BASE"}
				config["mcp_servers.stratus.enabled"] = true
			}
			method := "thread/start"
			if spec.NativeID != "" {
				method = "thread/resume"
				params["threadId"] = spec.NativeID
			}
			r, e := p.call(ctx, method, params)
			if e != nil {
				return nil, e
			}
			s.nativeID = str(obj(r, "thread"), "id")
			s.observedModel = str(r, "model")
			s.observedEffort = str(r, "reasoningEffort")
			if s.nativeID == "" || s.observedModel != spec.Profile.Model {
				return nil, fmt.Errorf("Codex did not confirm requested session/model (reported %q)", s.observedModel)
			}
			if spec.NativeID != "" && s.nativeID != spec.NativeID {
				return nil, errors.New("Codex resumed a different session")
			}
			if spec.Profile.Effort != "" && s.observedEffort != "" && s.observedEffort != spec.Profile.Effort {
				return nil, fmt.Errorf("Codex reported different effort %q", s.observedEffort)
			}
			s.publish(teams.Event{Kind: "identity", NativeID: s.nativeID, Model: s.observedModel, Effort: s.observedEffort})
		}
	}
	if len(s.models) == 0 {
		return nil, errors.New("runtime did not advertise any models")
	}
	sortModels(s.models)
	ok = true
	return s, nil
}

// The model catalog can exist without a usable login. Read the native CLI's
// status without exposing account identifiers or credentials through errors.
func (a *nativeAdapter) claudeAuthenticated(ctx context.Context, spec teams.SessionSpec) error {
	cmd := a.command(spec.ProjectRoot, "auth", "status", "--json")
	cmd.Env = childEnv(a.runtime, spec.Token)
	prepareProcess(cmd)
	var output strings.Builder
	cmd.Stdout = &output
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Claude authentication status could not start: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return errors.New("Claude authentication status failed; run claude auth status to inspect the login")
		}
	case <-ctx.Done():
		killProcess(cmd)
		<-done
		return fmt.Errorf("Claude authentication status: %w", ctx.Err())
	}
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err := json.Unmarshal([]byte(output.String()), &status); err != nil || !status.LoggedIn {
		return errors.New("Claude authentication is required; run claude auth status to inspect the login")
	}
	return nil
}

func codexModels(ctx context.Context, p *lineProcess) ([]teams.Model, error) {
	models := []teams.Model{}
	cursor := ""
	for page := 0; page < 30; page++ {
		params := object{"includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		r, err := p.call(ctx, "model/list", params)
		if err != nil {
			return nil, err
		}
		for _, v := range arr(r, "data") {
			m, _ := v.(map[string]any)
			id := str(m, "model")
			if id == "" {
				id = str(m, "id")
			}
			efforts := []string{}
			for _, e := range arr(m, "supportedReasoningEfforts") {
				e, _ := e.(map[string]any)
				if effort := str(e, "reasoningEffort"); effort != "" {
					efforts = append(efforts, effort)
				}
			}
			if id != "" {
				models = append(models, teams.Model{ID: id, Efforts: efforts})
			}
		}
		cursor = str(r, "nextCursor")
		if cursor == "" {
			return models, nil
		}
	}
	return nil, errors.New("native model catalog pagination exceeded limit")
}

func (s *streamSession) publish(e teams.Event) {
	if s.emit != nil {
		s.emit(e)
	}
}

func (s *streamSession) Submit(ctx context.Context, in teams.Input) (teams.Result, error) {
	if !s.submitMu.TryLock() {
		return teams.Result{}, errors.New("native session already has an active turn")
	}
	defer s.submitMu.Unlock()
	if in.ID == "" || strings.TrimSpace(in.Text) == "" {
		return teams.Result{}, errors.New("input id and text are required")
	}
	if err := validateModel(s.models, s.spec.Profile); err != nil {
		return teams.Result{}, err
	}
	select {
	case <-s.p.done:
		return teams.Result{}, s.p.failure()
	case <-s.p.stop:
		return teams.Result{}, errors.New("native session closed")
	default:
	}
	s.mu.Lock()
	nativeID := s.nativeID
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.turnID = ""; s.permissions = map[string]permission{}; s.mu.Unlock() }()
	inputUUID := uuid.NewString()
	s.usageBaseIn = nil
	s.usageBaseOut = nil
	turnID := ""
	if s.p.claude {
		if err := s.p.send(object{"type": "user", "session_id": nativeID, "uuid": inputUUID, "parent_tool_use_id": nil, "message": object{"role": "user", "content": in.Text}}); err != nil {
			return teams.Result{}, err
		}
		turnID = inputUUID
	} else {
		params := object{"threadId": nativeID, "model": s.spec.Profile.Model, "input": []any{object{"type": "text", "text": in.Text, "text_elements": []any{}}}, "clientUserMessageId": inputUUID}
		if s.spec.Profile.Effort != "" {
			params["effort"] = s.spec.Profile.Effort
		}
		r, err := s.p.call(ctx, "turn/start", params)
		if err != nil {
			s.Close()
			return teams.Result{}, fmt.Errorf("turn acceptance uncertain: %w", err)
		}
		turnID = str(obj(r, "turn"), "id")
		if turnID == "" {
			s.Close()
			return teams.Result{}, errors.New("Codex accepted no turn identity")
		}
	}
	s.mu.Lock()
	s.turnID = turnID
	s.mu.Unlock()
	s.publish(teams.Event{Kind: "status", ID: in.ID, NativeID: nativeID, Text: "accepted"})
	result := teams.Result{NativeID: nativeID, Model: s.observedModel, Effort: s.observedEffort}
	var text strings.Builder
	for {
		select {
		case m := <-s.p.events:
			var done bool
			var err error
			if s.p.claude {
				done, err = s.claudeEvent(m, inputUUID, &result, &text)
			} else {
				done, err = s.codexEvent(m, turnID, &result, &text)
			}
			if err != nil {
				s.Close()
				return result, err
			}
			if done {
				return result, nil
			}
		case <-ctx.Done():
			c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.Interrupt(c)
			cancel()
			s.Close()
			return result, fmt.Errorf("native turn cancelled; submission must not be retried automatically: %w", ctx.Err())
		case <-s.p.done:
			// Drain already-framed completion before treating EOF as failure.
			for {
				select {
				case m := <-s.p.events:
					var done bool
					var err error
					if s.p.claude {
						done, err = s.claudeEvent(m, inputUUID, &result, &text)
					} else {
						done, err = s.codexEvent(m, turnID, &result, &text)
					}
					if err != nil {
						return result, err
					}
					if done {
						return result, nil
					}
				default:
					return result, s.p.failure()
				}
			}
		case <-s.p.stop:
			return result, errors.New("native session closed during turn")
		}
	}
}

func (s *streamSession) codexEvent(m object, turnID string, r *teams.Result, text *strings.Builder) (bool, error) {
	p := obj(m, "params")
	method := str(m, "method")
	if id := str(p, "threadId"); id != "" && id != r.NativeID {
		return false, nil
	}
	if id := str(p, "turnId"); id != "" && id != turnID {
		return false, nil
	}
	if m["id"] != nil {
		switch method {
		case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval":
			key := fmt.Sprint(m["id"])
			s.mu.Lock()
			s.permissions[key] = permission{id: m["id"], method: method, params: p}
			s.mu.Unlock()
			s.publish(teams.Event{Kind: "approval", ID: key, NativeID: r.NativeID, Text: method + ": " + compact(p)})
			return false, nil
		default:
			_ = s.p.send(object{"id": m["id"], "error": object{"code": -32601, "message": "This request needs an input type not supported by the managed team approval form"}})
			return false, fmt.Errorf("native request %s needs user input unsupported by this adapter", method)
		}
	}
	switch method {
	case "item/agentMessage/delta":
		s.publish(teams.Event{Kind: "output", NativeID: r.NativeID, Text: str(p, "delta")})
	case "item/completed":
		item := obj(p, "item")
		if str(item, "type") == "agentMessage" {
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(str(item, "text"))
		}
	case "thread/tokenUsage/updated":
		usage := obj(p, "tokenUsage")
		last, total := obj(usage, "last"), obj(usage, "total")
		li, lo, ti, to := number(last, "inputTokens"), number(last, "outputTokens"), number(total, "inputTokens"), number(total, "outputTokens")
		if li != nil && lo != nil && ti != nil && to != nil {
			if s.usageBaseIn == nil {
				a, b := *ti-*li, *to-*lo
				s.usageBaseIn = &a
				s.usageBaseOut = &b
			}
			a, b := *ti-*s.usageBaseIn, *to-*s.usageBaseOut
			if a >= 0 && b >= 0 {
				r.InputTokens = &a
				r.OutputTokens = &b
			}
		} else {
			r.InputTokens = nil
			r.OutputTokens = nil
		}
	case "model/rerouted":
		return false, fmt.Errorf("native runtime changed model to %q; requested %q", str(p, "toModel"), s.spec.Profile.Model)
	case "turn/completed":
		turn := obj(p, "turn")
		if str(turn, "id") != turnID {
			return false, nil
		}
		if str(turn, "status") != "completed" {
			return false, fmt.Errorf("Codex turn %s: %s", str(turn, "status"), compact(turn["error"]))
		}
		parts := []string{}
		for _, v := range arr(turn, "items") {
			item, _ := v.(map[string]any)
			if str(item, "type") == "agentMessage" {
				parts = append(parts, str(item, "text"))
			}
		}
		if len(parts) > 0 {
			r.Text = strings.Join(parts, "\n")
		} else {
			r.Text = text.String()
		}
		return true, nil
	}
	return false, nil
}

func (s *streamSession) claudeEvent(m object, inputID string, r *teams.Result, text *strings.Builder) (bool, error) {
	if str(m, "type") == "control_request" {
		req := obj(m, "request")
		id := str(m, "request_id")
		if str(req, "subtype") != "can_use_tool" {
			_ = s.p.send(object{"type": "control_response", "response": object{"subtype": "error", "request_id": id, "error": "Unsupported managed control request"}})
			return false, fmt.Errorf("unsupported Claude control request %q", str(req, "subtype"))
		}
		s.mu.Lock()
		s.permissions[id] = permission{id: id, method: "can_use_tool", params: req}
		s.mu.Unlock()
		s.publish(teams.Event{Kind: "approval", ID: id, NativeID: r.NativeID, Text: str(req, "tool_name") + ": " + compact(req["input"])})
		return false, nil
	}
	if str(m, "type") == "control_cancel_request" {
		s.mu.Lock()
		delete(s.permissions, str(m, "request_id"))
		s.mu.Unlock()
		return false, nil
	}
	// Worker output has its own context/model; only the root session determines
	// this member's observed profile and result.
	if m["parent_tool_use_id"] != nil {
		return false, nil
	}
	if id := str(m, "session_id"); id != "" {
		if r.NativeID != "" && r.NativeID != id {
			return false, errors.New("Claude changed native session identity")
		}
		r.NativeID = id
		s.mu.Lock()
		s.nativeID = id
		s.mu.Unlock()
	}
	observed := ""
	switch str(m, "type") {
	case "system":
		if str(m, "subtype") == "init" {
			observed = str(m, "model")
		}
	case "assistant":
		message := obj(m, "message")
		observed = str(message, "model")
		for _, v := range arr(message, "content") {
			part, _ := v.(map[string]any)
			if str(part, "type") == "text" {
				if text.Len() > 0 {
					text.WriteString("\n")
				}
				text.WriteString(str(part, "text"))
				s.publish(teams.Event{Kind: "output", NativeID: r.NativeID, Text: str(part, "text")})
			}
		}
	case "result":
		if id := str(m, "user_message_uuid"); id != "" && id != inputID {
			return false, nil
		}
		if isError, _ := m["is_error"].(bool); isError || str(m, "subtype") != "success" {
			return false, fmt.Errorf("Claude turn %s: %s", str(m, "subtype"), compact(m["errors"]))
		}
		if r.NativeID == "" || r.Model == "" {
			return false, errors.New("Claude completed without session/model confirmation")
		}
		if s.resolved != "" && r.Model == s.resolved {
			r.ModelBinding = &teams.ModelBinding{Requested: s.spec.Profile.Model, Resolved: s.resolved}
		}
		r.Text = str(m, "result")
		if r.Text == "" {
			r.Text = text.String()
		}
		usage := obj(m, "usage")
		r.InputTokens = number(usage, "input_tokens")
		r.OutputTokens = number(usage, "output_tokens")
		if r.InputTokens != nil {
			for _, key := range []string{"cache_read_input_tokens", "cache_creation_input_tokens"} {
				if n := number(usage, key); n != nil {
					*r.InputTokens += *n
				}
			}
		}
		return true, nil
	}
	if observed != "" {
		if observed != s.resolved && observed != s.spec.Profile.Model {
			return false, fmt.Errorf("Claude selected model %q instead of requested %q", observed, s.spec.Profile.Model)
		}
		r.Model = observed
		s.publish(teams.Event{Kind: "identity", NativeID: r.NativeID, Model: observed})
	}
	return false, nil
}

func (s *streamSession) Approve(ctx context.Context, id string, allow bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	req, ok := s.permissions[id]
	if ok {
		delete(s.permissions, id)
	}
	s.mu.Unlock()
	if !ok {
		return errors.New("approval is no longer pending")
	}
	if s.p.claude {
		answer := object{"behavior": "deny", "message": "Denied by the user"}
		if allow {
			answer = object{"behavior": "allow", "updatedInput": req.params["input"]}
		}
		return s.p.send(object{"type": "control_response", "response": object{"subtype": "success", "request_id": req.id, "response": answer}})
	}
	answer := object{"decision": "decline"}
	if allow {
		answer["decision"] = "accept"
	}
	if req.method == "item/permissions/requestApproval" {
		answer = object{"permissions": object{}, "scope": "turn"}
		if allow {
			answer["permissions"] = req.params["permissions"]
		}
	}
	return s.p.send(object{"id": req.id, "result": answer})
}

func (s *streamSession) Interrupt(ctx context.Context) error {
	s.mu.Lock()
	id, native := s.turnID, s.nativeID
	s.mu.Unlock()
	if id == "" {
		return nil
	}
	if s.p.claude {
		_, err := s.p.call(ctx, "interrupt", object{})
		return err
	}
	_, err := s.p.call(ctx, "turn/interrupt", object{"threadId": native, "turnId": id})
	return err
}
func (s *streamSession) Close() error { return s.p.Close() }
