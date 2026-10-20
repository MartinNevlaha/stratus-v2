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
	in := hooks.DeliveryPolicyInput{Role: h.AgentType, ProjectRoot: root, WorkingDir: h.Cwd}
	id, _ := h.ToolInput["workflow_id"].(string)
	in.Delegation = nativeTool(h.ToolName) == "spawn_agent"
	if in.Delegation {
		for _, k := range []string{"agent_type", "subagent_type", "type"} {
			if role, ok := h.ToolInput[k].(string); ok && strings.HasPrefix(role, "delivery-") {
				in.Role = role
				break
			}
		}
	}
	var labelErr error
	briefRole := ""
	for _, key := range []string{"message", "prompt"} {
		brief, _ := h.ToolInput[key].(string)
		for _, m := range workflowLabel.FindAllStringSubmatch(brief, -1) {
			if id != "" && id != m[1] {
				labelErr = fmt.Errorf("conflicting workflow IDs")
			}
			id = m[1]
		}
		if in.Delegation {
			for _, m := range roleLabel.FindAllStringSubmatch(brief, -1) {
				if briefRole != "" && briefRole != m[1] {
					labelErr = fmt.Errorf("conflicting delivery roles")
				}
				briefRole, in.Role = m[1], m[1]
			}
		}
	}
	if labelErr != nil {
		return in, id, labelErr
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
		for _, path := range paths {
			if root == "" {
				// Classify writes before discovery; validate against the resolved root later.
				in.WritePaths = append(in.WritePaths, path)
				continue
			}
			p, ok := operationProjectPath(root, h.Cwd, path)
			if !ok {
				return in, id, fmt.Errorf("patch path is outside the project")
			}
			in.WritePaths = append(in.WritePaths, p)
		}
	}
	return in, id, nil
}
func resolveHookWorkflow(states []*orchestration.WorkflowState, id, session string) (*orchestration.WorkflowState, error) {
	return orchestration.ResolveOwned(states, id, orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: session})
}
