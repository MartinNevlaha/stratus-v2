package codex

import (
	"fmt"
	"github.com/MartinNevlaha/stratus-v2/hooks"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"regexp"
	"strings"
)

var workflowLabel = regexp.MustCompile(`(?im)^\s*(?:Workflow ID|workflow_id):\s*((?:spec|bug|e2e)-[a-z0-9-]+)\s*$`)
var roleLabel = regexp.MustCompile(`(?im)^\s*Delivery role:\s*(delivery-[a-z0-9-]+)\s*$`)

func policyInput(h HookEvent, root string) (hooks.DeliveryPolicyInput, string, error) {
	in := hooks.DeliveryPolicyInput{Role: h.AgentType, ProjectRoot: root}
	id, _ := h.ToolInput["workflow_id"].(string)
	prompt, _ := h.ToolInput["prompt"].(string)
	if m := workflowLabel.FindAllStringSubmatch(prompt, -1); len(m) == 1 {
		if id != "" && id != m[0][1] {
			return in, id, fmt.Errorf("conflicting workflow IDs")
		}
		id = m[0][1]
	} else if len(m) > 1 {
		return in, id, fmt.Errorf("ambiguous workflow labels")
	}
	in.Delegation = h.ToolName == "spawn_agent" || h.ToolName == "Agent" || h.ToolName == "Task"
	if in.Delegation {
		for _, k := range []string{"agent_type", "subagent_type", "type"} {
			if s, ok := h.ToolInput[k].(string); ok && strings.HasPrefix(s, "delivery-") {
				in.Role = s
				break
			}
		}
		if m := roleLabel.FindStringSubmatch(prompt); len(m) == 2 {
			in.Role = m[1]
		}
	}
	if h.ToolName == "Bash" || h.ToolName == "exec_command" {
		in.Command, _ = h.ToolInput["command"].(string)
		if in.Command == "" {
			in.Command, _ = h.ToolInput["cmd"].(string)
		}
	}
	if h.ToolName == "apply_patch" {
		command, _ := h.ToolInput["command"].(string)
		paths, e := PatchPaths(command)
		if e != nil {
			return in, id, e
		}
		in.WritePaths = paths
	}
	return in, id, nil
}
func resolveHookWorkflow(states []*orchestration.WorkflowState, id, session string) (*orchestration.WorkflowState, error) {
	return orchestration.ResolveOwned(states, id, orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: session})
}
