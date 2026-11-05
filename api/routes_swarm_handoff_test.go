package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func gateHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// handoffFixture is gateFixture plus an integration worktree on branch "merge" (path returned).
func handoffFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	s, root := gateFixture(t)
	wt := filepath.Join(root, ".stratus", "worktrees", "merge")
	gateGit(t, root, "worktree", "add", "-q", "-b", "merge", wt, "main")
	return s, root, wt
}

func gateCommit(t *testing.T, dir, file string) {
	t.Helper()
	gateWrite(t, dir, file, "x\n")
	gateGit(t, dir, "add", ".")
	gateGit(t, dir, "commit", "-q", "-m", "add "+file)
}

func postEvidence(t *testing.T, s *Server, ticket, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/swarm/tickets/"+ticket+"/evidence", strings.NewReader(body))
	req.SetPathValue("id", ticket)
	w := httptest.NewRecorder()
	s.handleRecordEvidence(w, req)
	return w
}

type handoffResp struct {
	MissionID string `json:"mission_id"`
	Status    string `json:"status"`
	Revision  string `json:"revision"`
	Dirty     bool   `json:"dirty"`
	DirKind   string `json:"dir_kind"`
	Gates     []struct {
		Name     string `json:"name"`
		Verdict  string `json:"verdict"`
		Revision string `json:"revision"`
		Current  bool   `json:"current"`
	} `json:"gates"`
	ChecksOK bool `json:"checks_ok"`
	Review   *struct {
		Verdict  string `json:"verdict"`
		Revision string `json:"revision"`
		Current  bool   `json:"current"`
		Valid    bool   `json:"valid"`
		Reason   string `json:"reason"`
	} `json:"review"`
	DiffStat *struct {
		FilesChanged int `json:"files_changed"`
		LinesAdded   int `json:"lines_added"`
	} `json:"diff_stat"`
	Forge struct {
		Total     int `json:"total"`
		Merged    int `json:"merged"`
		Pending   int `json:"pending"`
		Conflicts int `json:"conflicts"`
	} `json:"forge"`
	Warnings []string `json:"warnings"`
}

func getHandoff(t *testing.T, s *Server, id string) (int, handoffResp) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/swarm/missions/"+id+"/handoff", nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.handleMissionHandoff(w, req)
	var out handoffResp
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode handoff: %v\n%s", err, w.Body.String())
		}
	}
	return w.Code, out
}

func (h handoffResp) gate(name string) (verdict string, current, found bool) {
	for _, g := range h.Gates {
		if g.Name == name {
			return g.Verdict, g.Current, true
		}
	}
	return "", false, false
}

func hasWarning(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func TestRunQualityGates_RecordsRevisionAndDirKind(t *testing.T) {
	s, root, wt := handoffFixture(t)
	gateCommit(t, wt, "a.txt")
	head := gateHead(t, wt)

	s.runQualityGates("m1")

	ev := gateEvidence(t, s)
	if len(ev) != 3 {
		t.Fatalf("expected 3 gate rows, got %+v", ev)
	}
	for _, e := range ev {
		if e.Revision != head {
			t.Errorf("gate %q revision = %q, want %q", e.Content, e.Revision, head)
		}
		if !strings.Contains(e.Content, "@ "+head[:7]) || strings.Contains(e.Content, "+dirty") {
			t.Errorf("gate content should end its location with '@ %s' and no +dirty:\n%s", head[:7], e.Content)
		}
		if !strings.Contains(e.Content, "integration") {
			t.Errorf("gate content should say it ran in the integration worktree:\n%s", e.Content)
		}
	}

	// A dirty tree is marked in the content and in the stored revision, so the row can
	// never equal a clean HEAD later.
	gateWrite(t, wt, "scratch.txt", "uncommitted\n")
	s.runQualityGates("m1")
	var dirtyBuild *db.SwarmEvidence
	for _, e := range gateEvidence(t, s) {
		if strings.Contains(e.Content, "+dirty") && strings.HasPrefix(e.Content, "go build") {
			e := e
			dirtyBuild = &e
		}
	}
	if dirtyBuild == nil || dirtyBuild.Revision != head+"+dirty" {
		t.Fatalf("expected a go build row marked +dirty with revision %q, got %+v", head+"+dirty", dirtyBuild)
	}

	// Fallback to the project root says so.
	s2, root2 := gateFixture(t)
	s2.runQualityGates("m1")
	build := findGate(gateEvidence(t, s2), "go build: OK")
	if build == nil || !strings.Contains(build.Content, "project_root") || build.Revision != gateHead(t, root2) {
		t.Fatalf("fallback gate should record project_root and its revision, got %+v", build)
	}
	_ = root
}

func TestHandleRecordEvidence_ReviewWithoutRevisionDefaultsToCurrent(t *testing.T) {
	s, _, wt := handoffFixture(t)
	gateCommit(t, wt, "a.txt")

	w := postEvidence(t, s, "t1", `{"type":"review","content":"ok","agent":"rev","verdict":"pass"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var got db.SwarmEvidence
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if want := gateHead(t, wt); got.Revision != want {
		t.Errorf("response revision = %q, want integration HEAD %q", got.Revision, want)
	}
	stored := gateEvidence(t, s)
	if len(stored) != 1 || stored[0].Revision != got.Revision {
		t.Errorf("stored evidence = %+v", stored)
	}
}

func TestHandleRecordEvidence_NonReviewWithoutRevisionStaysEmpty(t *testing.T) {
	s, _ := gateFixture(t)
	w := postEvidence(t, s, "t1", `{"type":"note","content":"hi","agent":"a","verdict":"info"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if ev := gateEvidence(t, s); len(ev) != 1 || ev[0].Revision != "" {
		t.Errorf("note should carry no revision, got %+v", ev)
	}
}

func TestHandleRecordEvidence_ExplicitRevisionIsStoredLowercased(t *testing.T) {
	s, _ := gateFixture(t)
	w := postEvidence(t, s, "t1", `{"type":"review","content":"ok","agent":"rev","verdict":"pass","revision":"ABCDEF1234"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if ev := gateEvidence(t, s); len(ev) != 1 || ev[0].Revision != "abcdef1234" {
		t.Errorf("explicit revision not stored, got %+v", ev)
	}
}

func TestHandleRecordEvidence_InvalidRevisionRejected(t *testing.T) {
	s, _ := gateFixture(t)
	for _, rev := range []string{"xyz1234", "abc12", strings.Repeat("a", 65), "HEAD", "abc1234; rm"} {
		w := postEvidence(t, s, "t1", `{"type":"review","content":"ok","verdict":"pass","revision":"`+rev+`"}`)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "revision must be") {
			t.Errorf("revision %q: status = %d body = %s, want 400 'revision must be ...'", rev, w.Code, w.Body.String())
		}
	}
	if ev := gateEvidence(t, s); len(ev) != 0 {
		t.Errorf("rejected requests must not record evidence, got %+v", ev)
	}
}

func TestHandoff_UnknownMissionIs404(t *testing.T) {
	s, _ := gateFixture(t)
	if code, _ := getHandoff(t, s, "nope"); code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

func TestHandoff_ReviewValidOnlyForCurrentRevision(t *testing.T) {
	s, _, wt := handoffFixture(t)
	gateCommit(t, wt, "a.txt")
	r1 := gateHead(t, wt)
	if w := postEvidence(t, s, "t1", `{"type":"review","content":"ok","agent":"rev","verdict":"pass"}`); w.Code != http.StatusOK {
		t.Fatalf("record review: %s", w.Body.String())
	}

	code, h := getHandoff(t, s, "m1")
	if code != http.StatusOK || h.Revision != r1 || h.DirKind != "integration" || h.Dirty {
		t.Fatalf("handoff = %d %+v", code, h)
	}
	if h.Review == nil || !h.Review.Valid || h.Review.Revision != r1 || h.Review.Verdict != "pass" {
		t.Fatalf("review for current revision should be valid: %+v", h.Review)
	}

	gateCommit(t, wt, "b.txt")
	_, h = getHandoff(t, s, "m1")
	if h.Review == nil || h.Review.Valid || !strings.Contains(h.Review.Reason, "older") {
		t.Fatalf("review for an older revision must be invalid after a new commit: %+v", h.Review)
	}

	if w := postEvidence(t, s, "t1", `{"type":"review","content":"again","agent":"rev","verdict":"pass"}`); w.Code != http.StatusOK {
		t.Fatalf("record review: %s", w.Body.String())
	}
	_, h = getHandoff(t, s, "m1")
	if h.Review == nil || !h.Review.Valid {
		t.Fatalf("latest review recorded at the new HEAD should be valid: %+v", h.Review)
	}

	// Uncommitted changes mean the reviewed commit is no longer what is on disk.
	gateWrite(t, wt, "scratch.txt", "dirty\n")
	_, h = getHandoff(t, s, "m1")
	if !h.Dirty || h.Review == nil || h.Review.Valid || !hasWarning(h.Warnings, "uncommitted") {
		t.Fatalf("dirty tree must invalidate the review and warn: %+v", h)
	}
}

func TestHandoff_LegacyReviewWithoutRevisionIsInvalid(t *testing.T) {
	s, _ := gateFixture(t)
	if _, err := s.swarm.RecordEvidence("t1", "m1", "review", "old", "rev", "pass"); err != nil {
		t.Fatal(err)
	}
	_, h := getHandoff(t, s, "m1")
	if h.Review == nil || h.Review.Valid || h.Review.Reason != "no revision recorded" {
		t.Fatalf("legacy review should be invalid with reason, got %+v", h.Review)
	}
}

func TestHandoff_NoReviewOmitsReview(t *testing.T) {
	s, _ := gateFixture(t)
	_, h := getHandoff(t, s, "m1")
	if h.Review != nil || h.ChecksOK || len(h.Gates) != 0 {
		t.Errorf("fresh mission should have no review, gates or ok checks: %+v", h)
	}
}

func TestHandoff_ChecksOKRequiresAllThreeCurrentAndPassing(t *testing.T) {
	s, _, wt := handoffFixture(t)
	gateCommit(t, wt, "a.txt")
	r1 := gateHead(t, wt)

	s.runQualityGates("m1")
	_, h := getHandoff(t, s, "m1")
	for _, name := range []string{"build", "vet", "placeholder_scan"} {
		if v, cur, ok := h.gate(name); !ok || v != "pass" || !cur {
			t.Errorf("gate %s = %q current=%v found=%v", name, v, cur, ok)
		}
	}
	if !h.ChecksOK {
		t.Fatalf("checks_ok should be true with all three current and passing: %+v", h)
	}
	if h.DiffStat == nil || h.DiffStat.FilesChanged != 1 || h.DiffStat.LinesAdded != 1 {
		t.Errorf("diff_stat = %+v, want 1 file, 1 line added", h.DiffStat)
	}

	// A new commit makes every recorded gate outdated.
	gateCommit(t, wt, "b.txt")
	_, h = getHandoff(t, s, "m1")
	if h.ChecksOK {
		t.Error("checks_ok must be false once the revision moved on")
	}
	if _, cur, _ := h.gate("build"); cur {
		t.Error("build gate recorded at the old revision must not be current")
	}

	// A later failing vet at the current revision wins over the earlier pass.
	r2 := gateHead(t, wt)
	s.runQualityGates("m1")
	if _, err := s.swarm.RecordEvidenceAt("t1", "m1", "gate", "go vet FAILED (in x)", "quality-gate", "fail", r2); err != nil {
		t.Fatal(err)
	}
	_, h = getHandoff(t, s, "m1")
	if v, _, _ := h.gate("vet"); v != "fail" || h.ChecksOK {
		t.Errorf("latest vet row should be fail and checks_ok false: %+v", h)
	}
	_ = r1
}

func TestHandoff_ChecksOKFalseWhenPlaceholderMissing(t *testing.T) {
	s, _ := gateFixture(t)
	head := gateHead(t, s.projectRoot)
	for _, c := range []string{"go build: OK (in x)", "go vet: OK (in x)"} {
		if _, err := s.swarm.RecordEvidenceAt("t1", "m1", "gate", c, "quality-gate", "pass", head); err != nil {
			t.Fatal(err)
		}
	}
	_, h := getHandoff(t, s, "m1")
	if h.ChecksOK {
		t.Errorf("checks_ok needs all three gates: %+v", h)
	}
}

func TestHandoff_WarnsAboutPendingForgeAndMissingWorktree(t *testing.T) {
	s, _ := gateFixture(t)
	if err := s.db.CreateWorker("w1", "m1", "delivery-backend-engineer", "/wt", "b1"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.CreateForgeEntry("f1", "m1", "w1", "b1"); err != nil {
		t.Fatal(err)
	}
	_, h := getHandoff(t, s, "m1")
	if h.Forge.Total != 1 || h.Forge.Pending != 1 || h.Forge.Merged != 0 {
		t.Errorf("forge = %+v", h.Forge)
	}
	if h.DirKind != "project_root" {
		t.Errorf("dir_kind = %q, want project_root", h.DirKind)
	}
	if !hasWarning(h.Warnings, "pending") || !hasWarning(h.Warnings, "integration worktree") {
		t.Errorf("warnings = %v", h.Warnings)
	}
}

func TestHandoff_DiffStatOmittedWithWarningForBadBaseRef(t *testing.T) {
	s, _ := gateFixtureWithBase(t, "no-such-branch")
	_, h := getHandoff(t, s, "m1")
	if h.DiffStat != nil || !hasWarning(h.Warnings, "diff stat") {
		t.Errorf("diff_stat should be omitted with a warning: %+v", h)
	}
}

func TestHandoff_ReviewVerdictDecidesValidity(t *testing.T) {
	for _, tc := range []struct {
		verdict string
		valid   bool
	}{{"pass", true}, {"fail", false}, {"info", false}, {"approved", false}, {"", false}} {
		s, _, wt := handoffFixture(t)
		gateCommit(t, wt, "a.txt")
		body := `{"type":"review","content":"c","agent":"rev","verdict":"` + tc.verdict + `"}`
		if w := postEvidence(t, s, "t1", body); w.Code != http.StatusOK {
			t.Fatalf("record review: %s", w.Body.String())
		}
		_, h := getHandoff(t, s, "m1")
		if h.Review == nil || !h.Review.Current || h.Review.Valid != tc.valid || h.Review.Verdict != tc.verdict {
			t.Errorf("verdict %q at HEAD: want current=true valid=%v, got %+v", tc.verdict, tc.valid, h.Review)
		}
		if !tc.valid && h.Review != nil && h.Review.Reason == "" {
			t.Errorf("verdict %q: a not-valid review needs a reason", tc.verdict)
		}
	}
}

func TestHandoff_GatesRunOnDirtyTreeNeverBecomeCurrent(t *testing.T) {
	s, _, wt := handoffFixture(t)
	gateCommit(t, wt, "a.txt")
	gateWrite(t, wt, "scratch.txt", "uncommitted\n")
	s.runQualityGates("m1")

	_, h := getHandoff(t, s, "m1")
	if h.ChecksOK || !h.Dirty {
		t.Fatalf("dirty tree: checks_ok must be false: %+v", h)
	}

	if err := os.Remove(filepath.Join(wt, "scratch.txt")); err != nil {
		t.Fatal(err)
	}
	_, h = getHandoff(t, s, "m1")
	if h.Dirty || h.ChecksOK {
		t.Fatalf("cleaned tree: gates that ran dirty must not be current: %+v", h)
	}
	for _, g := range h.Gates {
		if g.Current {
			t.Errorf("gate %s ran on a dirty tree but reads current", g.Name)
		}
	}
}

func TestHandoff_ChecksOKFalseWhenTreeDirtyNow(t *testing.T) {
	s, _, wt := handoffFixture(t)
	gateCommit(t, wt, "a.txt")
	s.runQualityGates("m1")
	if _, h := getHandoff(t, s, "m1"); !h.ChecksOK {
		t.Fatalf("precondition: clean tree should be ok: %+v", h)
	}
	gateWrite(t, wt, "scratch.txt", "x\n")
	if _, h := getHandoff(t, s, "m1"); h.ChecksOK {
		t.Errorf("checks_ok must be false while the tree is dirty: %+v", h)
	}
}

func TestHandoff_WorkerCannotForgeGateRow(t *testing.T) {
	s, _ := gateFixture(t)
	head := gateHead(t, s.projectRoot)
	for _, c := range []string{"go build: OK (in x)", "go vet: OK (in x)", "Placeholder scan: clean (in x)"} {
		body := `{"type":"gate","content":"` + c + `","agent":"worker-1","verdict":"pass","revision":"` + head + `"}`
		if w := postEvidence(t, s, "t1", body); w.Code != http.StatusOK {
			t.Fatalf("record: %s", w.Body.String())
		}
	}
	_, h := getHandoff(t, s, "m1")
	if len(h.Gates) != 0 || h.ChecksOK {
		t.Errorf("gate rows not written by quality-gate must be ignored: %+v", h)
	}
}

func TestHandoff_WarnsWhenDiffIsEmpty(t *testing.T) {
	s, _, _ := handoffFixture(t)
	_, h := getHandoff(t, s, "m1")
	if !hasWarning(h.Warnings, "no changes versus base") {
		t.Errorf("empty diff should warn, got %v", h.Warnings)
	}
}

func TestHandleCreateMission_RejectsUnsafeBaseBranch(t *testing.T) {
	s, _ := gateFixture(t)
	for _, base := range []string{"--output=/tmp/x", "-x", "has space", "a\u0001b", "a..b", "bad~name", "/lead"} {
		body, _ := json.Marshal(map[string]string{"workflow_id": "spec-agents", "title": "T " + base, "base_branch": base})
		req := httptest.NewRequest(http.MethodPost, "/api/swarm/missions", strings.NewReader(string(body)))
		w := httptest.NewRecorder()
		s.handleCreateMission(w, req)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "base_branch") {
			t.Errorf("base_branch %q: status = %d body = %s, want 400 naming base_branch", base, w.Code, w.Body.String())
		}
	}
}

func TestHandleCreateMission_AcceptsOrdinaryBaseBranch(t *testing.T) {
	s, _ := gateFixture(t)
	body := `{"workflow_id":"spec-agents","title":"Fine","base_branch":"feature/x-1"}`
	w := httptest.NewRecorder()
	s.handleCreateMission(w, httptest.NewRequest(http.MethodPost, "/api/swarm/missions", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
}

// A bad base_branch that reached the database (older rows, direct writes) must still
// never be parsed by git as an option.
func TestStoredOptionLikeBaseBranch_NeverWritesFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "pwned.txt")
	base := "--output=" + out

	s, root := gateFixtureWithBase(t, base)
	_, h := getHandoff(t, s, "m1")
	if h.DiffStat != nil || !hasWarning(h.Warnings, "diff stat unavailable") {
		t.Errorf("diff stat with an option-like base must be unavailable: %+v", h)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("computeDiffStat wrote the --output file")
	}

	s.runQualityGates("m1")
	if _, err := os.Stat(out); err == nil {
		t.Fatal("placeholder gate wrote the --output file")
	}
	if skipped := findGate(gateEvidence(t, s), "Placeholder scan skipped"); skipped == nil {
		t.Errorf("placeholder scan should be skipped for an option-like base, got %+v", gateEvidence(t, s))
	}
	_ = root
}

func TestRunQualityGates_RevisionMovingDuringGatesMarksRowsMoved(t *testing.T) {
	s, _, wt := handoffFixture(t)
	gateCommit(t, wt, "a.txt")
	before := gateHead(t, wt)

	orig := runGateCmd
	t.Cleanup(func() { runGateCmd = orig })
	moved := false
	runGateCmd = func(dir, name string, args ...string) (string, error) {
		out, err := orig(dir, name, args...)
		if name == "go" && len(args) > 0 && args[0] == "vet" && !moved {
			moved = true
			gateCommit(t, wt, "b.txt") // HEAD moves while the gates are running
		}
		return out, err
	}

	s.runQualityGates("m1")

	after := gateHead(t, wt)
	if before == after {
		t.Fatal("test setup: HEAD did not move")
	}
	var info bool
	for _, e := range gateEvidence(t, s) {
		if gateName(e) != "" && e.Revision != before+"+moved" {
			t.Errorf("gate row %q revision = %q, want %q", e.Content, e.Revision, before+"+moved")
		}
		if strings.Contains(e.Content, "revision changed while gates ran") {
			info = e.Verdict == "info"
		}
	}
	if !info {
		t.Errorf("expected an info row 'revision changed while gates ran', got %+v", gateEvidence(t, s))
	}
	_, h := getHandoff(t, s, "m1")
	if h.ChecksOK {
		t.Errorf("gates that straddled a commit must not be ok: %+v", h)
	}
}

func TestRunQualityGates_StableRevisionAddsNoMovedRow(t *testing.T) {
	s, _, wt := handoffFixture(t)
	gateCommit(t, wt, "a.txt")
	s.runQualityGates("m1")
	for _, e := range gateEvidence(t, s) {
		if strings.Contains(e.Content, "revision changed") {
			t.Errorf("unexpected moved row: %+v", e)
		}
	}
}
