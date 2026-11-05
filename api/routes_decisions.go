package api

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
)

// decisionLimit caps the queue; the newest items win.
const decisionLimit = 100

// decisionAlertTypes are the Guardian alerts that mean something is stuck or over budget
// (the same set the dashboard toasts); routine health alerts stay in Guardian.
var decisionAlertTypes = map[string]bool{
	"stale_agent": true, "token_budget": true, "stale_worker": true,
	"reviewer_timeout": true, "agent_failed": true, "review_failed": true,
}

// guardianWorkflowReason maps the Guardian alert types that restate a workflow attention row
// to that row's reason; the other actionable types report something the attention check does not.
var guardianWorkflowReason = map[string]string{"review_failed": "review_failed", "stale_agent": "stale_agents"}

// decisionAction describes a request the dashboard can send to resolve an item.
// Path is relative to /api.
type decisionAction struct {
	Name   string         `json:"name"`
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body,omitempty"`
}

type decisionItem struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	Severity  string           `json:"severity"`
	Title     string           `json:"title"`
	Detail    string           `json:"detail"`
	CreatedAt string           `json:"created_at"`
	Link      string           `json:"link,omitempty"`
	Actions   []decisionAction `json:"actions"`
	ts        time.Time
}

type decisionSummary struct {
	NewItemsSince int  `json:"new_items_since"`
	GuardianNew   int  `json:"guardian_new"`
	TeamsUpdated  bool `json:"teams_updated"`
}

// handleDecisions merges everything that waits for the user into one list: team approvals
// and recoveries, workflows needing attention and actionable Guardian alerts. It is
// stateless and read-only. `since` only shapes the summary: what got resolved meanwhile is
// not recorded, so it is not reported.
func (s *Server) handleDecisions(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	if raw := r.URL.Query().Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "since must be an RFC3339 timestamp, e.g. 2026-01-02T15:04:05Z")
			return
		}
		since = t
	}

	items := []decisionItem{}
	teamsUpdated := false
	// A managed team member (it presents its team token) must not read the other members'
	// approval text, so it gets the workflow and Guardian sections only.
	if s.teams != nil && r.Header.Get("X-Stratus-Team-Token") == "" {
		// Team data is scoped to this server's project; guardian alerts and workflows are
		// not, because one server serves one project.
		tm, updated, err := s.teamDecisions(since)
		if err != nil {
			log.Printf("warning: decisions: list teams: %v", err)
		}
		items = append(items, tm...)
		teamsUpdated = updated
	}
	if s.coordinator != nil {
		att, err := s.attentionItems()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "list workflows failed")
			return
		}
		for _, it := range att {
			items = append(items, workflowDecisions(it)...)
		}
	}
	emitted := map[string]bool{}
	for _, it := range items {
		emitted[it.ID] = true
	}
	alerts, err := s.db.ListGuardianAlerts("")
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list guardian alerts failed")
		return
	}
	for _, a := range alerts {
		if !decisionAlertTypes[a.Type] {
			continue
		}
		wf, _ := a.Metadata["workflow_id"].(string)
		// The same problem is already a workflow row; one row per problem.
		if reason := guardianWorkflowReason[a.Type]; reason != "" && wf != "" && emitted["workflow:"+wf+":"+reason] {
			continue
		}
		it := decisionItem{
			ID: "guardian:" + strconv.FormatInt(a.ID, 10), Kind: "guardian", Severity: a.Severity,
			Title: a.Message, Detail: a.Type, CreatedAt: a.CreatedAt, ts: parseDecisionTime(a.CreatedAt),
			Actions: []decisionAction{{Name: "dismiss", Method: "PUT", Path: "/guardian/alerts/" + strconv.FormatInt(a.ID, 10) + "/dismiss", Body: map[string]any{}}},
		}
		if wf == "" {
			if agentID, _ := a.Metadata["agent_id"].(string); agentID != "" {
				if ag, found, err := s.db.FindWorkflowAgent(agentID); err == nil && found {
					wf = ag.WorkflowID
				}
			}
		}
		if wf != "" {
			it.Link = "#/workflow/" + url.PathEscape(wf)
		}
		items = append(items, it)
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].ts.After(items[j].ts) })
	if len(items) > decisionLimit {
		items = items[:decisionLimit]
	}
	sum := decisionSummary{TeamsUpdated: teamsUpdated}
	if !since.IsZero() {
		for _, it := range items {
			if it.ts.After(since) {
				sum.NewItemsSince++
				if it.Kind == "guardian" {
					sum.GuardianNew++
				}
			}
		}
	}
	json200(w, map[string]any{"items": items, "summary": sum})
}

// parseDecisionTime returns the zero time for an unparseable stamp, which sorts last.
func parseDecisionTime(raw string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return t
}

func workflowDecisions(it attentionItem) []decisionItem {
	ts := parseDecisionTime(it.UpdatedAt)
	link := "#/workflow/" + url.PathEscape(it.WorkflowID)
	title := it.Title
	if title == "" {
		title = it.WorkflowID
	}
	mk := func(reason, severity, detail string) decisionItem {
		return decisionItem{ID: "workflow:" + it.WorkflowID + ":" + reason, Kind: "workflow", Severity: severity,
			Title: title, Detail: detail, CreatedAt: it.UpdatedAt, ts: ts, Link: link, Actions: []decisionAction{}}
	}
	var out []decisionItem
	if it.ReviewFailed {
		out = append(out, mk("review_failed", "warning", "Posledná kontrola zlyhala ("+it.Phase+")"))
	}
	if it.StaleAgents > 0 {
		out = append(out, mk("stale_agents", "warning", fmt.Sprintf("Nečinní agenti: %d", it.StaleAgents)))
	}
	if len(it.Conflicts) > 0 {
		out = append(out, mk("conflicts", "info", fmt.Sprintf("Súbory upravuje viac agentov: %d", len(it.Conflicts))))
	}
	return out
}

// teamDecisions lists approvals and recoveries of this project's running teams and whether
// any team changed after since.
func (s *Server) teamDecisions(since time.Time) ([]decisionItem, bool, error) {
	project, _, err := s.teamProject()
	if err != nil {
		return nil, false, fmt.Errorf("resolve project: %w", err)
	}
	list, err := s.teams.List(project)
	if err != nil {
		return nil, false, fmt.Errorf("list teams: %w", err)
	}
	items, updated := decisionsFromTeams(list, since)
	return items, updated, nil
}

// decisionsFromTeams mirrors what Team.svelte offers as actions; stopped teams offer none.
func decisionsFromTeams(list []teams.Team, since time.Time) ([]decisionItem, bool) {
	var out []decisionItem
	updated := false
	for _, t := range list {
		if !since.IsZero() && t.UpdatedAt.After(since) {
			updated = true
		}
		if t.State == "stopped" {
			continue
		}
		for _, m := range t.Members {
			out = append(out, memberDecisions(t, m)...)
		}
	}
	return out, updated
}

func memberDecisions(t teams.Team, m teams.Member) []decisionItem {
	base := "team:" + t.ID + ":" + m.ID
	mk := func(id, kind, title, detail string, actions []decisionAction) decisionItem {
		return decisionItem{ID: id, Kind: kind, Severity: "warning", Title: title, Detail: detail,
			CreatedAt: t.UpdatedAt.UTC().Format(time.RFC3339Nano), ts: t.UpdatedAt, Actions: actions}
	}
	var out []decisionItem
	if m.Approval != nil && m.ApprovalResponseID == "" {
		path := "/teams/" + t.ID + "/approvals"
		body := func(allow bool) map[string]any {
			return map[string]any{"member_id": m.ID, "approval_id": m.Approval.ID, "allow": allow}
		}
		out = append(out, mk(base+":"+m.Approval.ID, "team_approval", "Tím "+t.ID+" ("+m.Role+"): nástroj potrebuje povolenie", m.Approval.Text, []decisionAction{
			{Name: "approve", Method: "POST", Path: path, Body: body(true)},
			{Name: "deny", Method: "POST", Path: path, Body: body(false)},
		}))
	}
	if m.Attachment == nil && m.State == "needs_user" && (m.Approval == nil || m.ApprovalResponseID != "") {
		out = append(out, mk(base+":recover", "team_recover", "Tím "+t.ID+" ("+m.Role+"): skontroluj výsledok a obnov", m.Error, []decisionAction{
			{Name: "recover", Method: "POST", Path: "/teams/" + t.ID + "/recover", Body: map[string]any{"member_id": m.ID}},
		}))
	}
	return out
}
