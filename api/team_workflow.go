package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/projectidentity"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

// The native worker runtime is independent of the workflow owner runtime.
// Keep that metadata beside the immutable dispatch scope and original result;
// workflow_agents remains the existing readiness/review evidence projection.
const teamEvidenceSchema = `CREATE TABLE IF NOT EXISTS team_workflow_evidence (
 agent_id TEXT PRIMARY KEY, team_id TEXT NOT NULL, task_id TEXT NOT NULL,
 workflow_id TEXT NOT NULL, scope_json TEXT NOT NULL, runtime TEXT NOT NULL,
 native_session_id TEXT NOT NULL, model TEXT NOT NULL, effort TEXT NOT NULL,
 usage_known INTEGER NOT NULL DEFAULT 0, revision TEXT NOT NULL, receipt_hash TEXT NOT NULL,
 terminal INTEGER NOT NULL DEFAULT 0, stale INTEGER NOT NULL DEFAULT 0,
 state TEXT NOT NULL, result TEXT NOT NULL, error TEXT NOT NULL)`

var teamDeliveryRole = regexp.MustCompile(`^delivery-[a-z][a-z0-9-]{0,100}$`)
var teamVerdictLine = regexp.MustCompile(`(?i)^(?:review[ \t]+)?verdict:[ \t]*(PASS|FAIL)[.!]?$`)

func teamReviewVerdict(report string) db.ReviewVerdict {
	out := parseReviewVerdict(report)
	out.Verdict = ""
	fence := ""
	for _, line := range strings.Split(report, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			marker := line[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			line = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "**"), "**"))
		}
		if m := teamVerdictLine.FindStringSubmatch(line); len(m) == 2 {
			out.Verdict = strings.ToUpper(m[1])
		}
	}
	return out
}

// enrichTeamEvidenceAgents overrides owner-runtime inference for managed workers.
// Call after runtimeAgents; missing native usage remains explicitly unknown.
func (s *Server) enrichTeamEvidenceAgents(agents []db.WorkflowAgent) []db.WorkflowAgent {
	for i := range agents {
		if !strings.HasPrefix(agents[i].ID, "team:") {
			continue
		}
		known := false
		var runtime string
		if err := s.db.SQL().QueryRow(`SELECT runtime,usage_known FROM team_workflow_evidence WHERE agent_id=? AND workflow_id=?`, agents[i].ID, agents[i].WorkflowID).Scan(&runtime, &known); err != nil {
			continue
		}
		agents[i].Runtime = runtime
		agents[i].UsageAvailable = &known
	}
	return agents
}

func teamEvidenceRole(role string) string {
	if teamDeliveryRole.MatchString(role) {
		return role
	}
	switch teams.CanonicalRole(role) {
	case "review":
		return "delivery-code-reviewer"
	case "design":
		return "delivery-system-architect"
	case "diagnosis":
		return "delivery-debugger"
	case "implementation", "mechanical":
		return "delivery-implementation-expert"
	}
	return ""
}

// syncTeamWorkflowEvidence projects genuine assignments and results. It never
// advances workflows, marks implementation tasks done, or adopts worker ownership.
func (s *Server) syncTeamWorkflowEvidence(team teams.Team) error {
	hasWorkflow := false
	for _, task := range team.Tasks {
		if task.WorkflowID != "" && task.Kind == "task" {
			hasWorkflow = true
			break
		}
	}
	if !hasWorkflow {
		return nil
	}
	if s.coordinator == nil || s.db == nil {
		return teams.ErrForbidden
	}
	project, _, err := projectidentity.Resolve(team.ProjectRoot)
	if err != nil {
		return err
	}
	serverProject, _, err := s.teamProject()
	if err != nil {
		return err
	}
	if team.ID == "" || project != team.ProjectID || project != serverProject {
		return teams.ErrForbidden
	}
	if _, err = s.db.SQL().Exec(teamEvidenceSchema); err != nil {
		return err
	}
	var errs []error
	for _, task := range team.Tasks {
		if task.WorkflowID == "" || task.Kind != "task" || teamEvidenceRole(task.Role) == "" {
			continue
		}
		if err := s.syncTeamTaskEvidence(team, task); err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", task.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Server) syncTeamTaskEvidence(team teams.Team, task teams.Task) error {
	if task.ID == "" || task.WorkflowPhase == "" || task.WorkflowSessionID == "" {
		return teams.ErrForbidden
	}
	wf, err := s.coordinator.Get(task.WorkflowID)
	if err != nil {
		return err
	}
	var owner, worker *teams.Member
	for i := range team.Members {
		m := &team.Members[i]
		if m.Role == "lead" && m.NativeID != "" && teamSessionKey(*m) == task.WorkflowSessionID {
			owner = m
		}
		if m.ID == task.To {
			worker = m
		}
	}
	if owner == nil || worker == nil || wf.SessionID != task.WorkflowSessionID || wf.Runtime == "codex" && owner.Profile.Runtime != "codex" {
		return teams.ErrForbidden
	}
	if task.State == "complete" && (worker.NativeID == "" || task.StartedAt == nil || task.FinishedAt == nil) {
		return fmt.Errorf("%w: completed task lacks a native run", teams.ErrForbidden)
	}
	if task.From != "user" && task.From != owner.ID {
		assigned := false
		for _, parent := range team.Tasks {
			sameTask := parent.WorkflowTaskIndex == nil && task.WorkflowTaskIndex == nil || parent.WorkflowTaskIndex != nil && task.WorkflowTaskIndex != nil && *parent.WorkflowTaskIndex == *task.WorkflowTaskIndex
			if parent.ID == task.ParentTaskID && parent.To == task.From && parent.WorkflowID == task.WorkflowID && parent.WorkflowSessionID == task.WorkflowSessionID && parent.WorkflowPhase == task.WorkflowPhase && parent.WorkflowGeneration == task.WorkflowGeneration && sameTask {
				assigned = true
			}
		}
		if !assigned {
			return teams.ErrForbidden
		}
	}
	role := teamEvidenceRole(task.Role)
	if strings.Contains(role, "reviewer") && worker.ID == owner.ID {
		return fmt.Errorf("%w: independent review requires another member", teams.ErrForbidden)
	}
	revisionStale := false
	reviewer := strings.Contains(role, "reviewer")
	if reviewer && task.State == "complete" {
		if task.Purpose == teams.PurposeDesignReview {
			// A design review describes its documents, not the whole tree.
			_, hash, e := designDocs(team, *wf, designDocKind(wf))
			revisionStale = e != nil || task.Revision == "" || hash != task.Revision
		} else {
			revision, e := teamCodeRevision(team.ReviewRoot())
			revisionStale = e != nil || task.Revision == "" || revision != task.Revision
		}
	}
	scope, _ := json.Marshal(struct {
		Workflow, Owner, Phase, Role, From, To, Revision string
		Task                                             *int
		Generation                                       int64
	}{task.WorkflowID, task.WorkflowSessionID, task.WorkflowPhase, role, task.From, task.To, task.Revision, task.WorkflowTaskIndex, task.WorkflowGeneration})
	sum := sha256.Sum256([]byte(team.ID + "\x00" + task.ID))
	agentID := fmt.Sprintf("team:%x", sum[:16])
	tx, err := s.db.SQL().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE team_workflow_evidence SET receipt_hash=receipt_hash WHERE 0`); err != nil {
		return err
	}
	// Recheck ownership under SQLite's write lock; a concurrent phase transition
	// cannot make this receipt evidence for its new generation.
	var stateJSON string
	if err = tx.QueryRow(`SELECT state_json FROM workflows WHERE id=?`, task.WorkflowID).Scan(&stateJSON); err != nil {
		return err
	}
	var latest orchestration.WorkflowState
	if err = json.Unmarshal([]byte(stateJSON), &latest); err != nil {
		return err
	}
	if latest.SessionID != task.WorkflowSessionID || latest.Runtime != wf.Runtime {
		return teams.ErrForbidden
	}
	sameTask := latest.CurrentTask == nil && task.WorkflowTaskIndex == nil || latest.CurrentTask != nil && task.WorkflowTaskIndex != nil && *latest.CurrentTask == *task.WorkflowTaskIndex
	current := string(latest.Phase) == task.WorkflowPhase && latest.ReviewGeneration == task.WorkflowGeneration && sameTask && !latest.Aborted
	recordDelegation := func() error {
		if !current {
			return nil
		}
		err := s.coordinator.RecordDelegationScoped(wf.ID, role, task.WorkflowSessionID, task.WorkflowPhase, task.WorkflowTaskIndex, task.WorkflowGeneration)
		if errors.Is(err, orchestration.ErrOwnershipConflict) {
			return nil
		}
		return err
	}
	var oldScope, oldReceipt, oldState, oldResult string
	var oldTerminal, oldStale bool
	err = tx.QueryRow(`SELECT scope_json,receipt_hash,state,result,terminal,stale FROM team_workflow_evidence WHERE agent_id=?`, agentID).Scan(&oldScope, &oldReceipt, &oldState, &oldResult, &oldTerminal, &oldStale)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if oldScope != "" && oldScope != string(scope) {
		return fmt.Errorf("%w: immutable assignment scope changed", teams.ErrConflict)
	}
	terminal := task.State == "complete" || task.State == "cancelled"
	if oldTerminal && !terminal {
		return nil
	}
	if oldTerminal && (task.State != oldState || task.Result != oldResult) {
		return fmt.Errorf("%w: completed result changed", teams.ErrConflict)
	}
	// A previously accepted result remains historical evidence after leaving its
	// phase. A late completion never gains that exception. Re-entering the review
	// phase with a new generation or changing the reviewed tree invalidates it.
	historical := oldTerminal && oldState == "complete" && task.State == "complete" && string(latest.Phase) != task.WorkflowPhase && !latest.Aborted
	stale := revisionStale || oldStale || !current && !historical
	usageKnown := task.InputTokens != nil && task.OutputTokens != nil
	receipt, _ := json.Marshal(struct {
		Task                           teams.Task
		Stale                          bool
		Runtime, Native, Model, Effort string
	}{task, stale, worker.Profile.Runtime, worker.NativeID, worker.ObservedModel, worker.ObservedEffort})
	hash := fmt.Sprintf("%x", sha256.Sum256(receipt))
	if hash == oldReceipt {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return recordDelegation()
	}
	status := "queued"
	switch task.State {
	case "running", "starting":
		status = "running"
	case "complete":
		status = "done"
	case "cancelled", "needs_user":
		status = "failed"
	}
	verdict := db.ReviewVerdict{}
	if task.State == "complete" && task.Error == "" && !stale && reviewer {
		verdict = teamReviewVerdict(task.Result)
	}
	if stale || task.Error != "" {
		status = "failed"
		verdict = db.ReviewVerdict{}
	}
	started := task.CreatedAt
	if task.StartedAt != nil {
		started = *task.StartedAt
	}
	if started.IsZero() {
		return fmt.Errorf("%w: task creation time missing", teams.ErrForbidden)
	}
	timestamp := func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
	last := started
	ended := ""
	if task.FinishedAt != nil {
		last = *task.FinishedAt
		ended = timestamp(last)
	}
	if task.State == "complete" && task.FinishedAt == nil {
		status = "failed"
		verdict = db.ReviewVerdict{}
	}
	in, out := int64(0), int64(0)
	if usageKnown {
		in, out = *task.InputTokens, *task.OutputTokens
	}
	_, err = tx.Exec(`INSERT INTO workflow_agents(id,workflow_id,session_id,agent_type,phase,task_index,status,started_at,last_activity,ended_at,verdict,must_fix,should_fix,suggestion,native_agent_id,turn_id,review_generation,tokens_in,tokens_out)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,started_at=excluded.started_at,last_activity=excluded.last_activity,ended_at=excluded.ended_at,verdict=excluded.verdict,must_fix=excluded.must_fix,should_fix=excluded.should_fix,suggestion=excluded.suggestion,native_agent_id=excluded.native_agent_id,tokens_in=excluded.tokens_in,tokens_out=excluded.tokens_out`, agentID, task.WorkflowID, task.WorkflowSessionID, role, task.WorkflowPhase, task.WorkflowTaskIndex, status, timestamp(started), timestamp(last), ended, verdict.Verdict, verdict.MustFix, verdict.ShouldFix, verdict.Suggestion, worker.NativeID, task.ID, task.WorkflowGeneration, in, out)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO team_workflow_evidence(agent_id,team_id,task_id,workflow_id,scope_json,runtime,native_session_id,model,effort,usage_known,revision,receipt_hash,terminal,stale,state,result,error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(agent_id) DO UPDATE SET native_session_id=excluded.native_session_id,model=excluded.model,effort=excluded.effort,usage_known=excluded.usage_known,receipt_hash=excluded.receipt_hash,terminal=excluded.terminal,stale=excluded.stale,state=excluded.state,result=excluded.result,error=excluded.error`, agentID, team.ID, task.ID, task.WorkflowID, string(scope), worker.Profile.Runtime, worker.NativeID, worker.ObservedModel, worker.ObservedEffort, usageKnown, task.Revision, hash, terminal, stale, task.State, task.Result, task.Error)
	if err != nil {
		return err
	}
	if terminal && !oldTerminal {
		summary := fmt.Sprintf("Team task %s (%s, %s): %s", task.ID, role, worker.Profile.Runtime, task.Result)
		if task.Error != "" {
			summary += "\nError: " + task.Error
		}
		if _, err = tx.Exec(`INSERT INTO workflow_logs(workflow_id,session_id,ts,tool_name,summary,created_ms) VALUES(?,?,?,?,?,?)`, task.WorkflowID, task.WorkflowSessionID, timestamp(last), "team_result", summary, last.UnixMilli()); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return recordDelegation()
}
