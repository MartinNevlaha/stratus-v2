package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/hooks"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Run(kind string, input io.Reader, stdout, stderr io.Writer, client *http.Client) int {
	emit := func(v any) { json.NewEncoder(stdout).Encode(v) }
	h, e := Decode(input)
	if e != nil {
		fmt.Fprintln(stderr, "Stratus Codex: invalid hook payload:", e)
		emit(map[string]any{})
		return 0
	}
	deny := func(reason string) int {
		emit(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}})
		return 0
	}
	in, id, policyErr := policyInput(h, "")
	delivery := strings.HasPrefix(in.Role, "delivery-")
	classified := in
	classified.Role = "delivery-unknown"
	needsRole := !delivery && h.AgentID != "" && (hooks.DeliveryPolicyNeedsWorkflow(classified) || h.ToolName == "apply_patch")
	if kind == "guard" {
		if h.Name != "PreToolUse" {
			emit(map[string]any{})
			return 0
		}
		if !delivery && !needsRole {
			emit(map[string]any{})
			return 0
		}
		if delivery && policyErr != nil {
			return deny(policyErr.Error())
		}
		if delivery && !hooks.DeliveryPolicyNeedsWorkflow(in) {
			emit(map[string]any{})
			return 0
		}
	}
	cfg, e := ResolveProject(h.Cwd)
	if e != nil {
		fmt.Fprintln(stderr, "Stratus Codex:", e)
		if kind == "guard" && (delivery || needsRole) {
			return deny("Cannot verify Stratus workflow: " + e.Error())
		}
		emit(map[string]any{})
		return 0
	}
	if client == nil {
		client = &http.Client{Timeout: time.Second}
	} else {
		copy := *client
		client = &copy
		if client.Timeout == 0 || client.Timeout > time.Second {
			client.Timeout = time.Second
		}
	}
	base := fmt.Sprintf("http://localhost:%d", cfg.Port)
	post := func(v any) error {
		b, _ := json.Marshal(v)
		res, e := client.Post(base+"/api/integrations/codex/events", "application/json", bytes.NewReader(b))
		if e != nil {
			return e
		}
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return fmt.Errorf("Stratus API status %d", res.StatusCode)
		}
		return nil
	}
	state := func() ([]*orchestration.WorkflowState, error) {
		res, e := client.Get(base + "/api/integrations/codex/workflows")
		if e != nil {
			return nil, e
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("Stratus API status %d", res.StatusCode)
		}
		var v struct {
			Workflows []*orchestration.WorkflowState `json:"workflows"`
		}
		e = json.NewDecoder(res.Body).Decode(&v)
		return v.Workflows, e
	}
	switch kind {
	case "guard":
		if needsRole {
			q := url.Values{"session_id": {h.SessionID}, "agent_id": {h.AgentID}}
			res, err := client.Get(base + "/api/integrations/codex/agents/role?" + q.Encode())
			if err != nil {
				return deny("Cannot verify native worker role: " + err.Error())
			}
			var binding struct {
				Role     string `json:"delivery_role"`
				Workflow string `json:"workflow_id"`
			}
			err = json.NewDecoder(res.Body).Decode(&binding)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 {
				return deny("Cannot verify native worker role")
			}
			if binding.Role == "" {
				emit(map[string]any{})
				return 0
			}
			if !strings.HasPrefix(binding.Role, "delivery-") || binding.Workflow == "" || id != "" && id != binding.Workflow {
				return deny("Conflicting native worker binding")
			}
			h.AgentType = binding.Role
			input := make(map[string]any, len(h.ToolInput)+1)
			for k, v := range h.ToolInput {
				input[k] = v
			}
			input["workflow_id"] = binding.Workflow
			h.ToolInput = input
		}
		policyRoot := cfg.ProjectRoot
		if wt, ok := linkedWorktree(cfg.ProjectRoot, h.Cwd); ok {
			policyRoot = wt
		}
		in, id, policyErr = policyInput(h, policyRoot)
		if policyErr != nil {
			return deny(policyErr.Error())
		}
		states, e := state()
		if e != nil {
			return deny("Cannot verify Stratus workflow: " + e.Error())
		}
		wf, e := resolveHookWorkflow(states, id, h.SessionID)
		if e != nil {
			return deny(e.Error())
		}
		in.WorkflowResolved = wf != nil
		if wf != nil {
			in.Phase = string(wf.Phase)
		}
		d := hooks.EvaluateDeliveryPolicy(in)
		if !d.Continue {
			return deny(d.Reason)
		}
		emit(map[string]any{})
	case "context":
		if h.SessionID == "" {
			fmt.Fprintln(stderr, "Stratus Codex: session identity unavailable")
			emit(map[string]any{})
			return 0
		}
		notice := "Native Codex session ID: " + h.SessionID + "\nUse this exact native ID in Stratus register_workflow and bind_workflow_session.\n"
		if b, e := os.ReadFile(filepath.Join(cfg.ProjectRoot, ".codex/stratus/instructions.md")); e == nil {
			notice += clipBytes(string(b), 12<<10)
		}
		if states, e := state(); e == nil {
			for _, wf := range states {
				if wf.Runtime == "codex" && wf.SessionID == "codex:"+h.SessionID {
					notice += fmt.Sprintf("\nOwned workflow: %s (%s)", wf.ID, wf.Phase)
				}
			}
		} else {
			notice += "\nStratus API unavailable; workflow status is not verified."
		}
		if normalized, e := normalizeForProject(h, cfg.ProjectRoot); e == nil {
			if e = post(normalized); e != nil {
				fmt.Fprintln(stderr, "Stratus Codex telemetry:", e)
			}
		}
		emit(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "SessionStart", "additionalContext": notice}})
	case "activity":
		normalized, e := normalizeForProject(h, cfg.ProjectRoot)
		if e == nil {
			if normalized.WorkflowID == "" {
				normalized.WorkflowID = id
			}
			e = post(normalized)
		}
		if e != nil {
			fmt.Fprintln(stderr, "Stratus Codex telemetry:", e)
		}
		if h.Name == "SubagentStart" && h.AgentID != "" {
			notice := fmt.Sprintf("Native parent Codex session ID: %s\nNative worker ID: %s\nNative turn ID: %s\nRead Workflow ID and Delivery role from your brief. If both are explicit, call bind_agent_role with workflow_id from the brief, session_id set to this exact parent ID, agent_id set to this exact worker ID, and delivery_role from the brief. Do not guess a missing role or identity. Reviewers must end with Verdict: PASS or Verdict: FAIL and label findings [must_fix], [should_fix], or [suggestion].", h.SessionID, h.AgentID, h.TurnID)
			emit(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "SubagentStart", "additionalContext": notice}})
		} else {
			emit(map[string]any{})
		}
	default:
		fmt.Fprintln(stderr, "Stratus Codex: unsupported handler", kind)
		return 1
	}
	return 0
}
