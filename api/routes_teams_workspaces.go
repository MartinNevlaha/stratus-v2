package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
)

const (
	teamChecksBudget  = 5 * time.Minute
	teamGateTextLimit = 4000
)

// teamCheckResult is the outcome of the gates for one revision of a team's integration tree.
type teamCheckResult struct {
	Revision string // recorded revision: the SHA, "+dirty" or "+moved" suffixed when it never matches a HEAD
	SHA      string
	Gates    []handoffGate
	Expected []string // the gate names checks_ok needed
	// Artifacts is the warning about files the checks left in the working tree, if any.
	Artifacts string
	ChecksOK  bool
}

// teamChecksCache keeps the latest gate result per team and which teams are mid-run, in
// memory only: a restart simply re-runs the gates on request. Its zero value is ready.
type teamChecksCache struct {
	mu           sync.Mutex
	running      map[string]bool
	results      map[string]teamCheckResult
	integrations map[string]teams.IntegrationResult
}

func (c *teamChecksCache) begin(teamID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running[teamID] {
		return false
	}
	if c.running == nil {
		c.running = map[string]bool{}
	}
	c.running[teamID] = true
	return true
}

func (c *teamChecksCache) end(teamID string) {
	c.mu.Lock()
	delete(c.running, teamID)
	c.mu.Unlock()
}

func (c *teamChecksCache) result(teamID string) (teamCheckResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.results[teamID]
	return r, ok
}

func (c *teamChecksCache) store(teamID string, r teamCheckResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.results == nil {
		c.results = map[string]teamCheckResult{}
	}
	c.results[teamID] = r
}

func (c *teamChecksCache) integration(teamID string) (teams.IntegrationResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.integrations[teamID]
	return r, ok
}

func (c *teamChecksCache) storeIntegration(teamID string, r teams.IntegrationResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.integrations == nil {
		c.integrations = map[string]teams.IntegrationResult{}
	}
	c.integrations[teamID] = r
}

func (c *teamChecksCache) forget(teamID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.results, teamID)
	delete(c.integrations, teamID)
}

func (s *Server) broadcastTeamUpdated() {
	if s.hub != nil {
		s.hub.BroadcastJSON("team_updated", map[string]any{})
	}
}

// isolatedTeam loads the project's team and requires isolated workspaces.
func (s *Server) isolatedTeam(w http.ResponseWriter, r *http.Request) (*teams.Team, bool) {
	if !s.teamReady(w) {
		return nil, false
	}
	t, err := s.scopedTeam(r.PathValue("id"))
	if err != nil {
		s.teamError(w, err)
		return nil, false
	}
	if !t.IsolatedWorkspaces {
		jsonErr(w, http.StatusConflict, "this team does not use isolated workspaces")
		return nil, false
	}
	return t, true
}

// workspaceError maps team errors to their status codes and hides git details of
// anything else behind the server log.
func (s *Server) workspaceError(w http.ResponseWriter, op string, err error) {
	if errors.Is(err, teams.ErrNotFound) || errors.Is(err, teams.ErrConflict) || errors.Is(err, teams.ErrForbidden) || errors.Is(err, teams.ErrClosed) {
		s.teamError(w, err)
		return
	}
	log.Printf("team workspaces: %s: %v", op, err)
	jsonErr(w, http.StatusInternalServerError, op+" failed; see the server log")
}

// integrationRevision describes the integration tree the way the swarm handoff does.
func integrationRevision(t *teams.Team) missionRevision {
	rev := missionRevision{Dir: t.IntegrationDir, Kind: dirKindIntegration}
	sha, dirty, err := gitRevision(t.IntegrationDir)
	if err != nil {
		rev.Err = err
		return rev
	}
	rev.SHA, rev.Dirty, rev.ShortSHA = sha, dirty, shortSHA(sha)
	return rev
}

// POST /api/teams/{id}/integrate merges the members' committed work into the team's
// integration worktree and reports what merged, what conflicted and what was left out.
func (s *Server) handleTeamIntegrate(w http.ResponseWriter, r *http.Request) {
	t, ok := s.isolatedTeam(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), teamIntegrationBudget)
	defer cancel()
	res, err := s.teams.Integrate(ctx, t.ID)
	if err != nil {
		s.workspaceError(w, "integration", err)
		return
	}
	s.teamChecks.storeIntegration(t.ID, res)
	s.broadcastTeamUpdated()
	json200(w, res)
}

// POST /api/teams/{id}/verify runs build, vet and the placeholder scan on the exact
// revision of the integration worktree. One run per team at a time; a clean revision
// that was already checked is answered from memory.
func (s *Server) handleTeamVerify(w http.ResponseWriter, r *http.Request) {
	t, ok := s.isolatedTeam(w, r)
	if !ok {
		return
	}
	if t.IntegrationDir == "" {
		jsonErr(w, http.StatusConflict, "integrate the members' work before verifying it")
		return
	}
	if !s.teamChecks.begin(t.ID) {
		jsonErr(w, http.StatusConflict, "checks are already running for this team")
		return
	}
	rev := integrationRevision(t)
	if rev.Err != nil {
		s.teamChecks.end(t.ID)
		log.Printf("team workspaces: verify %s: %v", t.ID, rev.Err)
		jsonErr(w, http.StatusInternalServerError, "cannot read the integration revision; see the server log")
		return
	}
	// Only a fully passing result is served again; anything else is re-run, so a
	// transient failure (network, flaky tool) cannot stick to a revision.
	if cached, ok := s.teamChecks.result(t.ID); ok && cached.ChecksOK && !rev.Dirty && cached.SHA == rev.SHA {
		s.teamChecks.end(t.ID)
		json200(w, map[string]any{"checks": s.currentChecks(cached, rev), "cached": true})
		return
	}
	done := make(chan teamCheckResult, 1)
	go func() {
		defer s.teamChecks.end(t.ID)
		gates, _ := resolveGateSet(s.cfg, rev.Dir)
		result := runTeamChecks(rev, t.IntegrationBase, gates)
		// Only a clean tree that stayed put is worth remembering: a dirty or moved
		// run says nothing about a later checkout of the same commit.
		if result.SHA != "" && result.Revision == result.SHA {
			s.teamChecks.store(t.ID, result)
		}
		s.broadcastTeamUpdated() // also when the request already gave up waiting
		done <- result
	}()
	select {
	case result := <-done:
		json200(w, map[string]any{"checks": s.currentChecks(result, integrationRevision(t)), "cached": false})
	case <-time.After(teamChecksBudget):
		jsonErr(w, http.StatusGatewayTimeout, "checks exceeded their time budget; they keep running and the result will be available from the handoff")
	case <-r.Context().Done():
	}
}

// runTeamChecks runs the code gates on rev and tags the result with the revision it
// describes. When the tree moved while the gates ran, the result is tagged "+moved".
func runTeamChecks(rev missionRevision, base string, gates []gateSpec) teamCheckResult {
	run := runObservedGates(rev, base, gates)
	recorded := run.Recorded
	if run.ReadErr != nil {
		recorded = rev.SHA + "+moved"
	}
	expected := expectedGateNames(gates)
	result := teamCheckResult{Revision: recorded, SHA: rev.SHA, Expected: expected, Artifacts: run.Artifacts}
	passed := map[string]bool{}
	for _, o := range run.Outcomes {
		passed[o.name] = o.verdict == "pass"
		result.Gates = append(result.Gates, handoffGate{Name: o.name, Verdict: o.verdict, Content: truncate(o.content, teamGateTextLimit), Revision: recorded})
	}
	result.ChecksOK = !rev.Dirty && !strings.HasSuffix(recorded, "+moved") && allGatesPassed(expected, func(name string) bool { return passed[name] })
	return result
}

// currentChecks marks which gate rows still describe the integration tree: only a clean
// tree at the very revision that was checked is current.
func (s *Server) currentChecks(r teamCheckResult, now missionRevision) map[string]any {
	current := now.Err == nil && !now.Dirty && r.SHA != "" && sameRevision(r.Revision, now.SHA)
	gates := make([]handoffGate, len(r.Gates))
	copy(gates, r.Gates)
	for i := range gates {
		gates[i].Current = current
	}
	return map[string]any{"gates": gates, "expected_gates": r.Expected, "checks_ok": r.ChecksOK && current, "revision": r.Revision, "current": current}
}

type handoffMember struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Branch      string `json:"branch"`
	Ahead       int    `json:"ahead"`
	Uncommitted int    `json:"uncommitted"`
}

// GET /api/teams/{id}/handoff summarises what an isolated team has produced: the
// integration revision, member branches, gate results, review validity and diff size.
// It only reads; git is never mutated.
func (s *Server) handleTeamHandoff(w http.ResponseWriter, r *http.Request) {
	if !s.teamReady(w) {
		return
	}
	t, err := s.scopedTeam(r.PathValue("id"))
	if err != nil {
		s.teamError(w, err)
		return
	}
	if !t.IsolatedWorkspaces {
		json200(w, map[string]any{"isolated": false})
		return
	}
	warnings := []string{}
	resp := map[string]any{"isolated": true, "members": []handoffMember{}, "merged": []string{}, "conflicts": []teams.IntegrationConflict{}}
	if t.IntegrationDir == "" {
		warnings = append(warnings, "the members' work has not been integrated yet")
		resp["checks"] = map[string]any{"gates": []handoffGate{}, "expected_gates": []string{}, "checks_ok": false, "revision": "", "current": false}
		resp["members"] = s.handoffMembers(t, "", &warnings)
		resp["warnings"] = warnings
		json200(w, resp)
		return
	}

	rev := integrationRevision(t)
	if rev.Err != nil {
		warnings = append(warnings, "current revision unknown: "+rev.Err.Error())
	} else if rev.Dirty {
		warnings = append(warnings, "the integration worktree has uncommitted changes")
	}
	gates, gateWarnings := resolveGateSet(s.cfg, t.IntegrationDir)
	warnings = append(warnings, gateWarnings...)
	resp["integration"] = map[string]any{"dir_kind": rev.Kind, "branch": t.IntegrationBranch, "revision": rev.SHA, "dirty": rev.Dirty}

	members := s.handoffMembers(t, t.IntegrationBranch, &warnings)
	resp["members"] = members
	merged := []string{}
	conflicts := []teams.IntegrationConflict{}
	last, hasLast := s.teamChecks.integration(t.ID)
	for _, m := range members {
		if m.Ahead == 0 && s.hasOwnCommits(t, m.Branch) {
			merged = append(merged, m.ID)
		}
		if m.Ahead > 0 {
			warnings = append(warnings, fmt.Sprintf("member %s has %d commit(s) not yet integrated", m.ID, m.Ahead))
			if hasLast {
				for _, c := range last.Conflicts {
					if c.Member == m.ID {
						conflicts = append(conflicts, c)
					}
				}
			}
		}
	}
	resp["merged"], resp["conflicts"] = merged, conflicts
	if len(conflicts) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d member branch(es) conflict with the integration branch", len(conflicts)))
	}

	if cached, ok := s.teamChecks.result(t.ID); ok {
		resp["checks"] = s.currentChecks(cached, rev)
		if cached.Artifacts != "" && rev.Dirty && cached.SHA == rev.SHA {
			warnings = append(warnings, cached.Artifacts)
		}
	} else {
		resp["checks"] = map[string]any{"gates": []handoffGate{}, "expected_gates": expectedGateNames(gates), "checks_ok": false, "revision": "", "current": false}
	}
	resp["review"] = s.teamReview(t, rev)
	if stat, reason := computeDiffStat(rev, t.IntegrationBase); stat != nil {
		resp["diff_stat"] = stat
		if stat.FilesChanged == 0 {
			warnings = append(warnings, "no changes versus the base; the placeholder scan has nothing to scan")
		}
	} else {
		warnings = append(warnings, "diff stat unavailable: "+reason)
	}
	resp["warnings"] = warnings
	json200(w, resp)
}

// handoffMembers reads each workspace member's commits ahead of the integration branch
// (all commits ahead of the project's HEAD when there is no integration yet) and its
// uncommitted file count.
func (s *Server) handoffMembers(t *teams.Team, integrationBranch string, warnings *[]string) []handoffMember {
	out := []handoffMember{}
	for _, m := range t.Members {
		if m.WorkspaceBranch == "" {
			continue
		}
		hm := handoffMember{ID: m.ID, Role: m.Role, Branch: m.WorkspaceBranch}
		from := integrationBranch
		if from == "" {
			from = "HEAD"
		}
		root := t.ProjectRoot
		if count, err := runGit(root, "rev-list", "--count", "--end-of-options", from+".."+m.WorkspaceBranch); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("member %s: commits ahead unknown: %v", m.ID, err))
		} else {
			fmt.Sscanf(strings.TrimSpace(count), "%d", &hm.Ahead)
		}
		if status, err := runGit(m.WorkspaceDir, "--no-optional-locks", "status", "--porcelain"); err != nil {
			*warnings = append(*warnings, fmt.Sprintf("member %s: workspace status unknown: %v", m.ID, err))
		} else if trimmed := strings.TrimSpace(status); trimmed != "" {
			hm.Uncommitted = len(strings.Split(trimmed, "\n"))
			*warnings = append(*warnings, fmt.Sprintf("member %s has %d uncommitted change(s) that are not part of the handoff", m.ID, hm.Uncommitted))
		}
		out = append(out, hm)
	}
	return out
}

// hasOwnCommits reports whether the member branch has commits beyond the integration base.
func (s *Server) hasOwnCommits(t *teams.Team, branch string) bool {
	if t.IntegrationBase == "" {
		return false
	}
	count, err := runGit(t.ProjectRoot, "rev-list", "--count", "--end-of-options", t.IntegrationBase+".."+branch)
	var n int
	fmt.Sscanf(strings.TrimSpace(count), "%d", &n)
	return err == nil && n > 0
}

// teamReview reports the latest completed review task and whether it still describes
// the integration tree: valid only for a clean tree whose fingerprint is the one the
// reviewer was given, and only with a pass verdict.
func (s *Server) teamReview(t *teams.Team, rev missionRevision) any {
	var latest *teams.Task
	for i := range t.Tasks {
		task := &t.Tasks[i]
		if task.Kind == "task" && teams.CanonicalRole(task.Role) == "review" && task.State == "complete" && task.Error == "" && task.FinishedAt != nil &&
			(latest == nil || task.FinishedAt.After(*latest.FinishedAt)) {
			latest = task
		}
	}
	if latest == nil {
		return nil
	}
	h := handoffReview{Verdict: strings.ToLower(teamReviewVerdict(latest.Result).Verdict), Revision: latest.Revision}
	current, err := teamCodeRevision(t.IntegrationDir)
	switch {
	case latest.Revision == "":
		h.Reason = "no revision recorded"
	case err != nil || rev.Err != nil:
		h.Reason = "current revision unknown"
	case latest.Revision != current:
		h.Reason = "reviewed an older revision"
	case rev.Dirty:
		h.Reason = "uncommitted changes since the reviewed revision"
	default:
		h.Current = true
		switch h.Verdict {
		case "pass":
			h.Valid = true
		case "fail":
			h.Reason = "review verdict is fail"
		default:
			h.Reason = "review has no pass verdict"
		}
	}
	return h
}

// sameOrigin reports whether the request carries an Origin header naming this server.
// A missing Origin does not count: browsers always send one on cross-site writes, so
// only a deliberate same-origin call may do something destructive.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && u.Host == r.Host
}

// DELETE /api/teams/{id}/workspaces?force=true removes the member and integration
// worktrees and branches and ends isolation. Without force it refuses (409, with a
// "blockers" list) while any of them holds unmerged commits or uncommitted changes.
// Gitignored files are not counted as changes, so a non-forced removal can delete them.
// force=true destroys data and therefore needs a same-origin Origin header.
func (s *Server) handleTeamWorkspaceRemove(w http.ResponseWriter, r *http.Request) {
	t, ok := s.isolatedTeam(w, r)
	if !ok {
		return
	}
	force := false
	switch r.URL.Query().Get("force") {
	case "", "false":
	case "true":
		force = true
	default:
		jsonErr(w, http.StatusBadRequest, "force must be true or false")
		return
	}
	if force && !sameOrigin(r) {
		jsonErr(w, http.StatusForbidden, "discarding workspaces requires the local dashboard")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), teamIntegrationBudget)
	defer cancel()
	updated, err := s.teams.RemoveWorkspaces(ctx, t.ID, force)
	if err != nil {
		var blocked *teams.BlockedError
		if errors.As(err, &blocked) {
			jsonErrCode(w, http.StatusConflict, "workspaces_blocked", blocked.Error(), map[string]any{"blockers": blocked.Blockers})
			return
		}
		s.workspaceError(w, "workspace removal", err)
		return
	}
	s.teamChecks.forget(t.ID)
	s.broadcastTeamUpdated()
	json200(w, publicTeam(*updated))
}
