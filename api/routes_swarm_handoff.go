package api

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/swarm"
)

const qualityGateAgent = "quality-gate"

const (
	dirKindIntegration = "integration"
	dirKindProjectRoot = "project_root"
)

// revisionPattern is a plausible git object name: 7 to 64 hex digits (SHA-1 or SHA-256).
var revisionPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// missionRevision describes the git state the swarm gates and reviews of a mission refer to.
type missionRevision struct {
	Dir      string
	Kind     string // dirKindIntegration, or dirKindProjectRoot when the worktree is missing
	SHA      string // full HEAD of Dir; empty when it could not be read
	Dirty    bool
	Err      error // why SHA is empty
	NoWT     bool  // the mission names an integration branch but its worktree is missing
	ShortSHA string
}

// resolveMissionRevision picks the directory the mission's work lives in (the integration
// worktree, else the project root) and reads its HEAD and cleanliness. It never mutates git.
func (s *Server) resolveMissionRevision(m *db.SwarmMission) missionRevision {
	rev := missionRevision{Dir: s.projectRoot, Kind: dirKindProjectRoot}
	if wt, ok := s.swarm.IntegrationWorktreePath(m); ok {
		rev.Dir, rev.Kind = wt, dirKindIntegration
	} else if wt != "" {
		rev.NoWT = true
	}
	sha, dirty, err := gitRevision(rev.Dir)
	if err != nil {
		rev.Err = err
		return rev
	}
	rev.SHA, rev.Dirty = sha, dirty
	rev.ShortSHA = shortSHA(sha)
	return rev
}

// where renders the location for gate evidence: "<dir> [<kind>] @ <short sha>[+dirty]".
func (r missionRevision) where() string {
	out := fmt.Sprintf("%s [%s]", r.Dir, r.Kind)
	if r.SHA == "" {
		return out
	}
	out += " @ " + r.ShortSHA
	if r.Dirty {
		out += "+dirty"
	}
	return out
}

// recorded is the revision stored with evidence produced now: the SHA, with "+dirty" when
// the tree had uncommitted changes, so such evidence never equals a clean HEAD later.
func (r missionRevision) recorded() string {
	if r.SHA != "" && r.Dirty {
		return r.SHA + "+dirty"
	}
	return r.SHA
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// gitRevision returns the HEAD commit of dir and whether its working tree has uncommitted
// or untracked changes. It is read-only: --no-optional-locks keeps git status from
// refreshing the index.
func gitRevision(dir string) (sha string, dirty bool, err error) {
	head, err := runGit(dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", false, fmt.Errorf("read HEAD: %w", err)
	}
	status, err := runGit(dir, "--no-optional-locks", "status", "--porcelain")
	if err != nil {
		return "", false, fmt.Errorf("read status: %w", err)
	}
	return strings.TrimSpace(head), strings.TrimSpace(status) != "", nil
}

// sameRevision reports whether recorded (full or abbreviated, at least 7 digits) names current.
func sameRevision(recorded, current string) bool {
	return recorded != "" && current != "" && strings.HasPrefix(current, strings.ToLower(recorded))
}

// gateName maps a gate evidence row written by the quality gates (a worker posting type=gate
// cannot forge one) to build, vet or placeholder_scan ("" otherwise).
func gateName(e db.SwarmEvidence) string {
	if e.Agent != qualityGateAgent {
		return ""
	}
	content := e.Content
	switch {
	case strings.HasPrefix(content, "go build"):
		return "build"
	case strings.HasPrefix(content, "go vet"):
		return "vet"
	case strings.HasPrefix(content, "Placeholder scan"),
		strings.HasPrefix(content, "Found ") && strings.Contains(content, "placeholder markers"):
		return "placeholder_scan"
	}
	return ""
}

type handoffGate struct {
	Name     string `json:"name"`
	Verdict  string `json:"verdict"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
	Current  bool   `json:"current"`
}

type handoffReview struct {
	Verdict  string `json:"verdict"`
	Revision string `json:"revision"`
	Current  bool   `json:"current"` // reviewed revision is HEAD and the tree is clean
	Valid    bool   `json:"valid"`   // current and the verdict is pass
	Reason   string `json:"reason,omitempty"`
}

type handoffDiffStat struct {
	FilesChanged int `json:"files_changed"`
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

type handoffForge struct {
	Total     int `json:"total"`
	Merged    int `json:"merged"`
	Pending   int `json:"pending"`
	Conflicts int `json:"conflicts"`
	Failed    int `json:"failed"`
}

// handleMissionHandoff summarises what the mission's evidence says about the current
// revision of its integration tree: gate results, review validity, diff size, forge state.
// It only reads.
func (s *Server) handleMissionHandoff(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mission, err := s.swarm.GetMission(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "mission not found: "+err.Error())
		return
	}
	evidence, err := s.swarm.ListMissionEvidence(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list evidence: "+err.Error())
		return
	}
	forge, err := s.swarm.ListForgeEntries(id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list forge: "+err.Error())
		return
	}

	rev := s.resolveMissionRevision(mission)
	warnings := []string{}
	if rev.NoWT {
		warnings = append(warnings, "integration worktree is missing; reporting the project root instead")
	}
	if rev.Err != nil {
		warnings = append(warnings, "current revision unknown: "+rev.Err.Error())
	} else if rev.Dirty {
		warnings = append(warnings, "working tree has uncommitted changes")
	}

	// Latest row per gate (evidence is ordered oldest first) and the latest review.
	latest := map[string]db.SwarmEvidence{}
	var review *db.SwarmEvidence
	for i := range evidence {
		e := evidence[i]
		switch e.Type {
		case swarm.EvidenceGate:
			if name := gateName(e); name != "" {
				latest[name] = e
			}
		case swarm.EvidenceReview:
			review = &evidence[i]
		}
	}

	gates := []handoffGate{}
	checksOK := true
	for _, name := range []string{"build", "vet", "placeholder_scan"} {
		e, ok := latest[name]
		if !ok {
			checksOK = false
			continue
		}
		current := sameRevision(e.Revision, rev.SHA)
		gates = append(gates, handoffGate{Name: name, Verdict: e.Verdict, Content: e.Content, Revision: e.Revision, Current: current})
		if !current || e.Verdict != "pass" {
			checksOK = false
		}
	}
	if rev.Dirty || rev.SHA == "" {
		checksOK = false
	}

	resp := map[string]any{
		"mission_id": mission.ID,
		"status":     mission.Status,
		"revision":   rev.SHA,
		"dirty":      rev.Dirty,
		"dir_kind":   rev.Kind,
		"gates":      gates,
		"checks_ok":  checksOK,
	}

	if review != nil {
		h := handoffReview{Verdict: review.Verdict, Revision: review.Revision}
		switch {
		case review.Revision == "":
			h.Reason = "no revision recorded"
		case rev.SHA == "":
			h.Reason = "current revision unknown"
		case !sameRevision(review.Revision, rev.SHA):
			h.Reason = "reviewed an older revision"
		case rev.Dirty:
			h.Reason = "uncommitted changes since the reviewed revision"
		default:
			h.Current = true
			switch review.Verdict {
			case "pass":
				h.Valid = true
			case "fail":
				h.Reason = "review verdict is fail"
			default:
				h.Reason = "review has no pass verdict"
			}
		}
		resp["review"] = h
	}

	if stat, reason := computeDiffStat(rev, mission.BaseBranch); stat != nil {
		resp["diff_stat"] = stat
		if stat.FilesChanged == 0 {
			warnings = append(warnings, "no changes versus base; the placeholder scan has nothing to scan")
		}
	} else {
		warnings = append(warnings, "diff stat unavailable: "+reason)
	}

	var fc handoffForge
	for _, f := range forge {
		fc.Total++
		switch f.Status {
		case swarm.ForgeMerged:
			fc.Merged++
		case swarm.ForgePending, swarm.ForgeMerging:
			fc.Pending++
		case swarm.ForgeConflict:
			fc.Conflicts++
		case swarm.ForgeFailed:
			fc.Failed++
		}
	}
	if fc.Pending > 0 {
		warnings = append(warnings, fmt.Sprintf("%d forge entries still pending", fc.Pending))
	}
	if fc.Conflicts > 0 {
		warnings = append(warnings, fmt.Sprintf("%d forge entries have merge conflicts", fc.Conflicts))
	}
	if fc.Failed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d forge entries failed", fc.Failed))
	}
	resp["forge"] = fc
	resp["warnings"] = warnings

	json200(w, resp)
}

// computeDiffStat runs `git diff --stat base...HEAD` in the revision's directory. When it
// cannot be computed it returns nil and a reason.
func computeDiffStat(rev missionRevision, baseBranch string) (*handoffDiffStat, string) {
	if baseBranch == "" {
		return nil, "mission has no base branch"
	}
	out, err := runGit(rev.Dir, "diff", "--stat", "--end-of-options", baseBranch+"...HEAD")
	if err != nil {
		return nil, err.Error()
	}
	var cs orchestration.ChangeSummary
	parseGitStat(out, &cs)
	stat := &handoffDiffStat{LinesAdded: cs.LinesAdded, LinesRemoved: cs.LinesRemoved}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) > 0 {
		for _, part := range strings.Split(lines[len(lines)-1], ",") {
			if fields := strings.Fields(part); len(fields) > 1 && strings.HasPrefix(fields[1], "file") {
				stat.FilesChanged, _ = strconv.Atoi(fields[0])
			}
		}
	}
	return stat, ""
}

// validateRevision checks an optional revision from a request body and returns it lowercased.
func validateRevision(revision string) (string, error) {
	if revision == "" {
		return "", nil
	}
	if !revisionPattern.MatchString(revision) {
		return "", fmt.Errorf("revision must be a hex commit SHA of 7 to 64 characters")
	}
	return strings.ToLower(revision), nil
}

// defaultReviewRevision returns the current HEAD of the mission's integration tree, or ""
// (logged) when it cannot be read.
func (s *Server) defaultReviewRevision(missionID string) string {
	mission, err := s.swarm.GetMission(missionID)
	if err != nil {
		log.Printf("warning: review revision for mission %s: load mission: %v", missionID, err)
		return ""
	}
	rev := s.resolveMissionRevision(mission)
	if rev.Err != nil {
		log.Printf("warning: review revision for mission %s: %v", missionID, rev.Err)
	}
	return rev.SHA
}

// validateBaseBranch rejects a base branch git would not accept as a branch name, and
// anything that could be read as a command-line option.
func (s *Server) validateBaseBranch(name string) error {
	if name == "" {
		return nil // the store defaults it
	}
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("base_branch must not start with '-'")
	}
	for _, r := range name {
		if r <= ' ' || r == 0x7f {
			return fmt.Errorf("base_branch must not contain spaces or control characters")
		}
	}
	if _, err := runGit(s.projectRoot, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("base_branch is not a valid branch name: %w", err)
	}
	return nil
}
