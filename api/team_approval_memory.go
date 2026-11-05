package api

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

const (
	approvalDenialTitleRunes = 80
	approvalDenialTextRunes  = 300
	approvalRequestPrefix    = `Agent's request: "`
)

// approvalToolPrefix matches the "<Tool>: " or "<method>: " that runtimes put in
// front of a JSON payload ("Bash: {...}", "item/commandExecution/requestApproval: {...}").
var approvalToolPrefix = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_./-]{0,100}): `)

// approvalRequestFields are the payload fields that say what the agent wants
// to do, most specific first.
var approvalRequestFields = []string{"command", "file_path", "path", "reason"}

// pendingApproval finds the approval a user is answering in a team snapshot
// taken before the manager consumes it, with the workflow of the task the
// member is working on ("" if that task carries none).
func pendingApproval(t *teams.Team, memberID, approvalID string) (approval teams.Event, workflowID string, found bool) {
	for _, m := range t.Members {
		if m.ID != memberID || m.Approval == nil || m.Approval.ID != approvalID {
			continue
		}
		for _, task := range t.Tasks {
			if task.ID == m.ActiveTaskID {
				workflowID = task.WorkflowID
			}
		}
		return *m.Approval, workflowID, true
	}
	return teams.Event{}, "", false
}

// parseApprovalRequest splits an approval text into the tool and what the agent
// asked to do. Payloads it cannot read fall back to the raw text, so the
// request is never empty for a non-empty text. Nothing is redacted here.
func parseApprovalRequest(text string) (tool, request string) {
	text = strings.TrimSpace(text)
	tool = "approval request"
	m := approvalToolPrefix.FindStringSubmatch(text)
	if m == nil {
		return tool, text
	}
	tool = m[1]
	if i := strings.LastIndex(tool, "/"); i >= 0 && i < len(tool)-1 {
		tool = tool[i+1:]
	}
	var payload map[string]any
	if json.Unmarshal([]byte(text[len(m[0]):]), &payload) != nil {
		return tool, text
	}
	for _, field := range approvalRequestFields {
		switch v := payload[field].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return tool, v
			}
		case []any:
			var parts []string
			for _, p := range v {
				if str, ok := p.(string); ok {
					parts = append(parts, str)
				}
			}
			if joined := strings.Join(parts, " "); strings.TrimSpace(joined) != "" {
				return tool, joined
			}
		}
	}
	return tool, text
}

// recordApprovalDenial stores a user's refusal of a native approval request as
// a rejected decision, so the workflow brief tells later agents not to retry
// the refused action without any agent having to save it. What the agent asked
// for goes through the CI log sanitizer and redactor before it reaches memory
// and is quoted as the agent's words, not the user's. workflowID is the
// workflow of the task the member was working on, "" if none.
func (s *Server) recordApprovalDenial(teamID, memberID string, approval teams.Event, workflowID string) error {
	tool, request := parseApprovalRequest(approval.Text)
	clean := func(v string) string { return oneLine(redactCILog(sanitizeCIText(v))) }
	request = clipRunes(clean(request), approvalDenialTextRunes-len(approvalRequestPrefix)-1)
	refs := map[string]any{"status": "rejected", "member_id": memberID, "team_id": teamID}
	scope := "repo"
	if workflowID != "" {
		refs["workflow_id"] = workflowID
		scope = "workflow"
	}
	// Approval ids are small per-process integers for some runtimes, so they
	// only identify an approval within one team member.
	key := fmt.Sprintf("approval-denied:%s:%s:%s", teamID, memberID, approval.ID)
	if _, err := s.db.SaveEvent(db.SaveEventInput{
		Actor:     "user",
		Scope:     scope,
		Type:      "decision",
		Title:     "Rejected by user: " + clipRunes(clean(tool), approvalDenialTitleRunes),
		Text:      approvalRequestPrefix + request + `"`,
		Tags:      []string{"decision", "rejected", "approval_denied"},
		Refs:      refs,
		DedupeKey: &key,
	}); err != nil {
		return fmt.Errorf("save denied approval %s: %w", approval.ID, err)
	}
	return nil
}
