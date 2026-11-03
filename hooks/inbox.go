package hooks

// Inbox delivers messages from other agent sessions (Claude Code, OpenCode, Codex) into
// this Claude Code session. It runs on UserPromptSubmit and PostToolUse, so a message
// reaches the model on its next prompt or tool call. Fail-open: any error adds nothing.
func Inbox(event HookEvent) Decision {
	// Messages belong to the main session; a subagent must not consume them.
	if event.SessionID == "" || event.AgentID != "" {
		return Decision{Continue: true}
	}
	msgs, err := defaultCommsClient().ClaimInbox(event.SessionID)
	if err != nil {
		return Decision{Continue: true}
	}
	return Decision{Continue: true, Context: FormatInbox(msgs)}
}

// SessionEnd marks the session's peer as ended so others stop addressing it. Best-effort.
func SessionEnd(event HookEvent) Decision {
	if event.SessionID != "" {
		_ = defaultCommsClient().EndPeer(event.SessionID)
	}
	return Decision{Continue: true}
}
