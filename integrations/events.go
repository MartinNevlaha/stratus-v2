// Package integrations defines runtime-independent activity values.
package integrations

type Event struct {
	DeliveryRole    string   `json:"delivery_role,omitempty"`
	Kind            string   `json:"event"`
	NativeSessionID string   `json:"session_id"`
	WorkflowID      string   `json:"workflow_id,omitempty"`
	TurnID          string   `json:"turn_id,omitempty"`
	NativeAgentID   string   `json:"agent_id,omitempty"`
	AgentType       string   `json:"agent_type,omitempty"`
	ToolName        string   `json:"tool_name,omitempty"`
	ToolUseID       string   `json:"tool_use_id,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	Report          string   `json:"report,omitempty"`
	Files           []string `json:"files,omitempty"`
	MainAgent       bool     `json:"main_agent,omitempty"`
	Succeeded       *bool    `json:"succeeded,omitempty"`
}
