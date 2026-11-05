package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

// ---------- pure state machine ----------

func drTask(id, purpose, state, revision, result string) teams.Task {
	return teams.Task{ID: id, Kind: "task", Purpose: purpose, State: state, Revision: revision, Result: result, WorkflowID: "wf", WorkflowPhase: "design"}
}

func TestEvaluateDesignReview_States(t *testing.T) {
	rev, rw := teams.PurposeDesignReview, teams.PurposeDesignRework
	pass, fail := "Looks fine.\nVerdict: PASS", "[must_fix] gap — docs/a.md:3\nVerdict: FAIL"
	cases := []struct {
		name  string
		tasks []teams.Task
		w     []teams.DesignReviewWaiver
		want  string
	}{
		{"no review", nil, nil, drNone},
		{"review running", []teams.Task{drTask("r1", rev, "running", "H", "")}, nil, drReviewing},
		{"review queued", []teams.Task{drTask("r1", rev, "queued", "H", "")}, nil, drReviewing},
		{"pass for current hash", []teams.Task{drTask("r1", rev, "complete", "H", pass)}, nil, drPass},
		{"pass for an old hash", []teams.Task{drTask("r1", rev, "complete", "OLD", pass)}, nil, drNone},
		{"fail", []teams.Task{drTask("r1", rev, "complete", "H", fail)}, nil, drFail},
		{"fail with rework running", []teams.Task{drTask("r1", rev, "complete", "H", fail), drTask("dr-rework:r1", rw, "running", "H", "")}, nil, drReworking},
		{"rework done, documents changed", []teams.Task{drTask("r1", rev, "complete", "OLD", fail), drTask("dr-rework:r1", rw, "complete", "OLD", "done")}, nil, drNone},
		{"fail in the last round", []teams.Task{drTask("a", rev, "complete", "O1", fail), drTask("b", rev, "complete", "O2", fail), drTask("r1", rev, "complete", "H", fail)}, nil, drExhausted},
		{"pass wins in the last round", []teams.Task{drTask("a", rev, "complete", "O1", fail), drTask("b", rev, "complete", "O2", fail), drTask("r1", rev, "complete", "H", pass)}, nil, drPass},
		{"all rounds used, documents edited", []teams.Task{drTask("a", rev, "complete", "O1", fail), drTask("b", rev, "complete", "O2", fail), drTask("c", rev, "complete", "O3", fail)}, nil, drExhausted},
		{"result without a verdict line is not a pass", []teams.Task{drTask("r1", rev, "complete", "H", "All good, PASS from me")}, nil, drReviewFailed},
		{"verdict inside a code fence is not a pass", []teams.Task{drTask("r1", rev, "complete", "H", "```\nVerdict: PASS\n```")}, nil, drReviewFailed},
		{"verdict only as a sentence is not a pass", []teams.Task{drTask("r1", rev, "complete", "H", "My verdict: PASS because it is fine")}, nil, drReviewFailed},
		{"review ended in error", []teams.Task{drTask("r1", rev, "needs_user", "H", "Verdict: PASS")}, nil, drReviewFailed},
		{"waived for current hash", []teams.Task{drTask("r1", rev, "complete", "H", fail)}, []teams.DesignReviewWaiver{{WorkflowID: "wf", DocHash: "H"}}, drWaived},
		{"waiver of an old hash does not count", []teams.Task{drTask("r1", rev, "complete", "H", fail)}, []teams.DesignReviewWaiver{{WorkflowID: "wf", DocHash: "OLD"}}, drFail},
		{"waiver of another workflow does not count", nil, []teams.DesignReviewWaiver{{WorkflowID: "other", DocHash: "H"}}, drNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			team := teams.Team{DesignReview: &teams.DesignReviewConfig{Enabled: true, ReviewerMemberID: "critic", MaxRounds: 3}, Tasks: c.tasks, DesignReviewWaivers: c.w}
			if got := evaluateDesignReview(team, "wf", "H").State; got != c.want {
				t.Fatalf("state=%s, want %s", got, c.want)
			}
		})
	}
	t.Run("an edit after a pass starts a fresh round", func(t *testing.T) {
		team := teams.Team{DesignReview: &teams.DesignReviewConfig{Enabled: true, MaxRounds: 1}, Tasks: []teams.Task{drTask("r1", rev, "complete", "OLD", pass)}}
		if got := evaluateDesignReview(team, "wf", "H"); got.State != drNone || got.Round != 0 {
			t.Fatalf("eval=%+v", got)
		}
	})
	t.Run("an edit after a waiver starts a fresh round", func(t *testing.T) {
		failed := drTask("r1", rev, "complete", "OLD", fail)
		finished := time.Now().Add(-time.Hour)
		failed.FinishedAt = &finished
		team := teams.Team{DesignReview: &teams.DesignReviewConfig{Enabled: true, MaxRounds: 1}, Tasks: []teams.Task{failed},
			DesignReviewWaivers: []teams.DesignReviewWaiver{{WorkflowID: "wf", DocHash: "OLD", At: time.Now()}}}
		if got := evaluateDesignReview(team, "wf", "H"); got.State != drNone || got.Round != 0 {
			t.Fatalf("eval=%+v", got)
		}
	})
	t.Run("a failed round before the last pass is not counted", func(t *testing.T) {
		team := teams.Team{DesignReview: &teams.DesignReviewConfig{Enabled: true, MaxRounds: 2}, Tasks: []teams.Task{
			drTask("a", rev, "complete", "O1", fail), drTask("b", rev, "complete", "O2", pass), drTask("c", rev, "complete", "O3", fail)}}
		if got := evaluateDesignReview(team, "wf", "H"); got.State != drNone || got.Round != 1 {
			t.Fatalf("eval=%+v", got)
		}
	})
	t.Run("only a rework task counts as the rework of a review", func(t *testing.T) {
		squat := drTask("dr-rework:r1", "", "complete", "H", "squatted")
		team := teams.Team{DesignReview: &teams.DesignReviewConfig{Enabled: true, MaxRounds: 3}, Tasks: []teams.Task{drTask("r1", rev, "complete", "H", fail), squat}}
		if got := evaluateDesignReview(team, "wf", "H"); got.State != drFail || got.ReworkOfLast != nil {
			t.Fatalf("eval=%+v", got)
		}
	})
	t.Run("other workflows are ignored", func(t *testing.T) {
		other := drTask("r1", rev, "complete", "H", pass)
		other.WorkflowID = "elsewhere"
		team := teams.Team{DesignReview: &teams.DesignReviewConfig{Enabled: true, MaxRounds: 3}, Tasks: []teams.Task{other}}
		if got := evaluateDesignReview(team, "wf", "H"); got.State != drNone || got.Round != 0 {
			t.Fatalf("eval=%+v", got)
		}
	})
	t.Run("round counts finished reviews", func(t *testing.T) {
		team := teams.Team{DesignReview: &teams.DesignReviewConfig{Enabled: true, MaxRounds: 5}, Tasks: []teams.Task{drTask("a", rev, "complete", "O1", fail), drTask("b", rev, "running", "H", "")}}
		if got := evaluateDesignReview(team, "wf", "H"); got.Round != 1 || got.State != drReviewing {
			t.Fatalf("eval=%+v", got)
		}
	})
}

// ---------- documents ----------

func drRepo(t *testing.T) string {
	t.Helper()
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com"} {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	gateGit(t, root, "init", "-q", "-b", "main")
	for name, content := range map[string]string{"README.md": "x\n", "main.go": "package main\n", "docs/adr/0001-old.md": "old adr\n", "docs/adr/0002-edited.md": "v1\n", "docs/notes.md": "notes\n"} {
		drWrite(t, root, name, content)
	}
	gateGit(t, root, "add", ".")
	gateGit(t, root, "commit", "-q", "-m", "base")
	return root
}

func drWrite(t *testing.T, root, name, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func drHead(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func drPaths(docs []designDoc) []string {
	var out []string
	for _, d := range docs {
		out = append(out, d.Path)
	}
	return out
}

func TestDesignDocs_CoversDesignDocAndChangedADRsOnly(t *testing.T) {
	root := drRepo(t)
	wf := orchestration.WorkflowState{ID: "wf-a", BaseCommit: drHead(t, root), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	team := teams.Team{ProjectRoot: root}
	drWrite(t, root, "docs/plans/wf-a-design.md", "design v1\n")
	drWrite(t, root, "docs/plans/wf-a-plan.md", "plan\n")
	drWrite(t, root, "docs/adr/0002-edited.md", "v2\n")         // changed since the base commit
	drWrite(t, root, "docs/adr/0003-new.md", "brand new adr\n") // untracked
	docs, hash, err := designDocs(team, wf, "design")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docs/adr/0002-edited.md", "docs/adr/0003-new.md", "docs/plans/wf-a-design.md"}
	if got := drPaths(docs); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("docs=%v, want %v", got, want)
	}
	// Unrelated files, other docs and code do not change the hash.
	drWrite(t, root, "docs/notes.md", "other notes\n")
	drWrite(t, root, "docs/plans/wf-a-plan.md", "plan changed\n")
	drWrite(t, root, "main.go", "package main\n// edit\n")
	if _, same, err := designDocs(team, wf, "design"); err != nil || same != hash {
		t.Fatalf("unrelated edit changed the hash: %v", err)
	}
	for _, edit := range []string{"docs/plans/wf-a-design.md", "docs/adr/0003-new.md", "docs/adr/0002-edited.md"} {
		drWrite(t, root, edit, "edited\n")
		_, changed, err := designDocs(team, wf, "design")
		if err != nil || changed == hash {
			t.Fatalf("editing %s did not change the hash: %v", edit, err)
		}
		hash = changed
	}
}

func TestDesignDocs_PlanPhaseCoversPlanDocOnly(t *testing.T) {
	root := drRepo(t)
	wf := orchestration.WorkflowState{ID: "wf-a", BaseCommit: drHead(t, root)}
	drWrite(t, root, "docs/plans/wf-a-plan.md", "plan\n")
	drWrite(t, root, "docs/plans/wf-a-design.md", "design\n")
	drWrite(t, root, "docs/adr/0003-new.md", "new\n")
	docs, _, err := designDocs(teams.Team{ProjectRoot: root}, wf, "plan")
	if err != nil || len(docs) != 1 || docs[0].Path != "docs/plans/wf-a-plan.md" {
		t.Fatalf("docs=%v err=%v", drPaths(docs), err)
	}
}

func TestDesignDocs_MissingDesignDoc(t *testing.T) {
	root := drRepo(t)
	_, _, err := designDocs(teams.Team{ProjectRoot: root}, orchestration.WorkflowState{ID: "wf-a", BaseCommit: drHead(t, root)}, "design")
	var missing *designDocsMissingError
	if !errors.As(err, &missing) || len(missing.Missing) != 1 || missing.Missing[0] != "docs/plans/wf-a-design.md" {
		t.Fatalf("err=%v", err)
	}
}

func TestDesignDocs_RejectsUnsafeFiles(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
		want  string
	}{
		{"symlink out of the docs directory", func(t *testing.T, root string) {
			outside := filepath.Join(t.TempDir(), "secret.md")
			os.WriteFile(outside, []byte("secret"), 0o644)
			if err := os.Symlink(outside, filepath.Join(root, "docs/plans/wf-a-design.md")); err != nil {
				t.Fatal(err)
			}
		}, "outside the docs directory"},
		{"symlink to a project file outside docs", func(t *testing.T, root string) {
			if err := os.Symlink(filepath.Join(root, "main.go"), filepath.Join(root, "docs/plans/wf-a-design.md")); err != nil {
				t.Fatal(err)
			}
		}, "outside the docs directory"},
		{"ADR symlink out of the docs directory", func(t *testing.T, root string) {
			drWrite(t, root, "docs/plans/wf-a-design.md", "d\n")
			if err := os.Symlink("/etc/hostname", filepath.Join(root, "docs/adr/0009-link.md")); err != nil {
				t.Fatal(err)
			}
		}, "outside the docs directory"},
		{"design document is a directory", func(t *testing.T, root string) {
			os.MkdirAll(filepath.Join(root, "docs/plans/wf-a-design.md"), 0o755)
		}, "not a regular file"},
		{"oversized document", func(t *testing.T, root string) {
			drWrite(t, root, "docs/plans/wf-a-design.md", strings.Repeat("x", designReviewMaxDocBytes+1))
		}, "larger than"},
		{"too many ADR files", func(t *testing.T, root string) {
			drWrite(t, root, "docs/plans/wf-a-design.md", "d\n")
			for i := 0; i <= designReviewMaxADRs; i++ {
				drWrite(t, root, "docs/adr/new-"+strings.Repeat("a", i+1)+".md", "adr\n")
			}
		}, "exceed the limit"},
		{"total size cap", func(t *testing.T, root string) {
			big := strings.Repeat("x", designReviewMaxDocBytes)
			drWrite(t, root, "docs/plans/wf-a-design.md", big)
			for i := 0; i < 4; i++ {
				drWrite(t, root, "docs/adr/big-"+strings.Repeat("b", i+1)+".md", big)
			}
		}, "in total"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := drRepo(t)
			os.MkdirAll(filepath.Join(root, "docs/plans"), 0o755)
			c.setup(t, root)
			_, _, err := designDocs(teams.Team{ProjectRoot: root}, orchestration.WorkflowState{ID: "wf-a", BaseCommit: drHead(t, root)}, "design")
			if err == nil || !errors.Is(err, errDesignDocsInvalid) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err=%v, want invalid documents mentioning %q", err, c.want)
			}
		})
	}
}

func TestDesignDocs_RejectsWorkflowIDWithPath(t *testing.T) {
	root := drRepo(t)
	if _, _, err := designDocs(teams.Team{ProjectRoot: root}, orchestration.WorkflowState{ID: "../evil"}, "design"); !errors.Is(err, errDesignDocsInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestDesignDocs_FallbackWithoutBaseCommitUsesModificationTime(t *testing.T) {
	root := t.TempDir() // not a git repository
	created := time.Now().UTC()
	wf := orchestration.WorkflowState{ID: "wf-a", CreatedAt: created.Format(time.RFC3339Nano)}
	drWrite(t, root, "docs/plans/wf-a-design.md", "d\n")
	drWrite(t, root, "docs/adr/0001-old.md", "old\n")
	drWrite(t, root, "docs/adr/0002-recent.md", "recent\n")
	old := created.Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(root, "docs/adr/0001-old.md"), old, old); err != nil {
		t.Fatal(err)
	}
	later := created.Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "docs/adr/0002-recent.md"), later, later); err != nil {
		t.Fatal(err)
	}
	docs, _, err := designDocs(teams.Team{ProjectRoot: root}, wf, "design")
	if err != nil || strings.Join(drPaths(docs), ",") != "docs/adr/0002-recent.md,docs/plans/wf-a-design.md" {
		t.Fatalf("docs=%v err=%v", drPaths(docs), err)
	}
	// An unknown base commit takes the same fallback.
	wf.BaseCommit = strings.Repeat("a", 40)
	if docs, _, err = designDocs(teams.Team{ProjectRoot: root}, wf, "design"); err != nil || len(docs) != 2 {
		t.Fatalf("docs=%v err=%v", drPaths(docs), err)
	}
}

// ---------- gate fixture ----------

const drProfile = `{"runtime":"codex","model":"gpt-6.1-sol","effort":"high"}`

// drAdapter answers every turn at once. Design review turns reply with the scripted
// verdicts in order (PASS when the script ran out), rework turns run onRework, and
// a held review blocks until release.
type drAdapter struct {
	mu       sync.Mutex
	verdicts []string
	onRework func(text string)
	hold     chan struct{}
	inputs   []string
	events   map[string]func(teams.Event)
	approval bool // a design review turn first asks the user for a permission
}

func (a *drAdapter) Preflight(context.Context, string) teams.Availability {
	return teamTestAdapter{}.Preflight(context.Background(), "")
}
func (a *drAdapter) Open(_ context.Context, spec teams.SessionSpec, event func(teams.Event)) (teams.Session, error) {
	a.mu.Lock()
	if a.events == nil {
		a.events = map[string]func(teams.Event){}
	}
	a.events[spec.MemberID] = event
	a.mu.Unlock()
	return &drSession{a: a, member: spec.MemberID}, nil
}

type drSession struct {
	a      *drAdapter
	member string
}

func (s *drSession) Submit(ctx context.Context, in teams.Input) (teams.Result, error) {
	a := s.a
	a.mu.Lock()
	a.inputs = append(a.inputs, in.Text)
	hold, onRework := a.hold, a.onRework
	emit := a.events[s.member]
	askApproval := a.approval && strings.Contains(in.Text, "DESIGN REVIEW - workflow")
	reply := "ok"
	switch {
	case strings.Contains(in.Text, "DESIGN REVIEW - workflow"):
		reply = "Verdict: PASS"
		if len(a.verdicts) > 0 {
			reply, a.verdicts = a.verdicts[0], a.verdicts[1:]
		}
	case strings.Contains(in.Text, "DESIGN REWORK - workflow"):
		reply = "revised the documents"
	}
	a.mu.Unlock()
	if askApproval && emit != nil {
		emit(teams.Event{Kind: "approval", ID: "perm-1", Text: "May I read the documents?"})
	}
	if strings.Contains(in.Text, "DESIGN REVIEW - workflow") && hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return teams.Result{}, ctx.Err()
		}
	}
	if strings.Contains(in.Text, "DESIGN REWORK - workflow") && onRework != nil {
		onRework(in.Text)
	}
	return teams.Result{Text: reply, NativeID: "native-" + s.member, Model: "gpt-6.1-sol", Effort: "high"}, nil
}
func (*drSession) Approve(context.Context, string, bool) error { return nil }
func (*drSession) Interrupt(context.Context) error             { return nil }
func (*drSession) Close() error                                { return nil }

type drOpts struct {
	complex   bool
	noOption  bool
	reviewer  string
	maxRounds int
}

type drFixture struct {
	t     *testing.T
	s     *Server
	root  string
	team  teams.Team
	ad    *drAdapter
	wfID  string
	hits  int
	guard http.Handler
	real  http.Handler // the managed guard in front of the real phase handler
	next  string       // last request reaching the wrapped handler
}

func newDRFixture(t *testing.T, o drOpts) *drFixture {
	t.Helper()
	root := drRepo(t)
	d, err := db.Open(filepath.Join(t.TempDir(), "design.db"))
	if err != nil {
		t.Fatal(err)
	}
	ad := &drAdapter{}
	adapters := map[string]teams.Adapter{"codex": ad}
	m, err := teams.NewManager(d.SQL(), adapters, func() {})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: d, coordinator: orchestration.NewCoordinator(d), projectRoot: root, hub: NewHub(), teams: m, teamAdapters: adapters}
	s.setupDesignReview()
	t.Cleanup(func() { m.Close(); d.Close() })
	reviewer := o.reviewer
	if reviewer == "" {
		reviewer = "critic"
	}
	option := `,"design_review":{"enabled":true,"reviewer_member_id":"` + reviewer + `","max_rounds":` + strconv.Itoa(o.maxRounds) + `}`
	if o.noOption {
		option = ""
	}
	body := `{"members":[` +
		`{"id":"lead","role":"lead","profile":` + drProfile + `},` +
		`{"id":"architect","role":"delivery-system-architect","profile":` + drProfile + `},` +
		`{"id":"critic","role":"delivery-code-reviewer","profile":` + drProfile + `},` +
		`{"id":"dev","role":"implementation","profile":` + drProfile + `}]` + option + `}`
	w := commsDo(t, s.handleCreateTeam, "POST", "/api/teams", body, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("create team: %d %s", w.Code, w.Body)
	}
	var team teams.Team
	if err := json.Unmarshal(w.Body.Bytes(), &team); err != nil {
		t.Fatal(err)
	}
	f := &drFixture{t: t, s: s, root: root, team: team, ad: ad, wfID: "spec-x"}
	// The lead needs a native session before it can own a workflow.
	if _, err = m.Enqueue(context.Background(), team.ID, teams.TaskRequest{ID: "boot", From: "user", To: "lead", Text: "start"}); err != nil {
		t.Fatal(err)
	}
	f.wait(func(tm teams.Team) bool {
		return memberByID(tm, "lead").NativeID != "" && taskByID(tm, "boot").State == "complete"
	})
	complexity := orchestration.ComplexitySimple
	if o.complex {
		complexity = orchestration.ComplexityComplex
	}
	if _, err = s.coordinator.StartOwned(f.wfID, orchestration.WorkflowSpec, complexity, "Design gate", orchestration.WorkflowOwner{Runtime: "codex", NativeSessionID: "native-lead"}); err != nil {
		t.Fatal(err)
	}
	if err = s.coordinator.SetBaseCommit(f.wfID, drHead(t, root)); err != nil {
		t.Fatal(err)
	}
	if o.complex {
		for _, phase := range []orchestration.Phase{orchestration.PhaseDiscovery, orchestration.PhaseDesign} {
			if _, err = s.coordinator.Transition(f.wfID, phase); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.guard = s.managedTeamGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits++
		f.next = r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/workflows/{id}/phase", s.handleTransitionPhase)
	f.real = s.managedTeamGuard(mux)
	return f
}

func memberByID(tm teams.Team, id string) teams.Member {
	for _, m := range tm.Members {
		if m.ID == id {
			return m
		}
	}
	return teams.Member{}
}

func taskByID(tm teams.Team, id string) teams.Task {
	for _, task := range tm.Tasks {
		if task.ID == id {
			return task
		}
	}
	return teams.Task{}
}

func (f *drFixture) current() teams.Team {
	f.t.Helper()
	tm, err := f.s.teams.Get(f.team.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return *tm
}

func (f *drFixture) wait(cond func(teams.Team) bool) teams.Team {
	f.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if tm := f.current(); cond(tm) {
			return tm
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.t.Fatalf("condition not reached; tasks: %s", f.dump())
	return teams.Team{}
}

func (f *drFixture) dump() string {
	var b strings.Builder
	for _, task := range f.current().Tasks {
		b.WriteString(task.ID + "[" + task.Purpose + "/" + task.State + "] ")
	}
	return b.String()
}

func (f *drFixture) tasks(purpose string) []teams.Task {
	var out []teams.Task
	for _, task := range f.current().Tasks {
		if task.Purpose == purpose {
			out = append(out, task)
		}
	}
	return out
}

func (f *drFixture) waitState(want string) designSnapshot {
	f.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		wf, _ := f.s.coordinator.Get(f.wfID)
		if snap := designState(f.current(), *wf); snap.Status.State == want {
			return snap
		}
		time.Sleep(5 * time.Millisecond)
	}
	wf, _ := f.s.coordinator.Get(f.wfID)
	f.t.Fatalf("state %s not reached (is %s); tasks: %s", want, designState(f.current(), *wf).Status.State, f.dump())
	return designSnapshot{}
}

// delegate runs a workflow task for a member as if the user delegated it, and waits for it.
func (f *drFixture) delegate(id, to, role, text string) {
	f.t.Helper()
	team := f.current()
	req := teams.TaskRequest{ID: id, From: "user", To: to, Role: role, Kind: "task", Text: text, WorkflowID: f.wfID}
	if err := f.s.validateTeamWorkflow(&team, nil, &req); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.s.teams.Enqueue(context.Background(), team.ID, req); err != nil {
		f.t.Fatal(err)
	}
	f.wait(func(tm teams.Team) bool { return taskByID(tm, id).State == "complete" })
}

// authorDesign has the architect "write" the design document.
func (f *drFixture) authorDesign(content string) {
	f.t.Helper()
	f.delegate("author-1", "architect", "delivery-system-architect", "write the design")
	drWrite(f.t, f.root, "docs/plans/"+f.wfID+"-design.md", content)
}

func (f *drFixture) token(member string) string {
	return memberByID(f.current(), member).Token
}

// put sends the lead's phase transition through the managed guard.
func (f *drFixture) put(phase, token string) *httptest.ResponseRecorder {
	f.t.Helper()
	r := httptest.NewRequest("PUT", "/api/workflows/"+f.wfID+"/phase", strings.NewReader(`{"phase":"`+phase+`"}`))
	if token != "" {
		r.Header.Set("X-Stratus-Team-Token", token)
	}
	w := httptest.NewRecorder()
	f.guard.ServeHTTP(w, r)
	return w
}

func (f *drFixture) leadPut(phase string) *httptest.ResponseRecorder {
	return f.put(phase, f.token("lead"))
}

type gateBody struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	State     string `json:"state"`
	Round     int    `json:"round"`
	MaxRounds int    `json:"max_rounds"`
	Reviewer  *struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	} `json:"reviewer"`
	Docs []designDocRef `json:"docs"`
}

func (f *drFixture) expect409(w *httptest.ResponseRecorder, code, state string) gateBody {
	f.t.Helper()
	var b gateBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		f.t.Fatalf("body %q: %v", w.Body, err)
	}
	if w.Code != http.StatusConflict || b.Code != code || (state != "" && b.State != state) {
		f.t.Fatalf("status=%d code=%q state=%q body=%s; want 409 %s %s", w.Code, b.Code, b.State, w.Body, code, state)
	}
	return b
}

// ---------- gate behaviour ----------

func TestDesignReviewGate_BypassedWithoutTeamToken(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.authorDesign("design\n")
	if w := f.put("governance", ""); w.Code != 200 || f.hits != 1 {
		t.Fatalf("dashboard transition was gated: %d hits=%d", w.Code, f.hits)
	}
	if n := len(f.tasks(teams.PurposeDesignReview)); n != 0 {
		t.Fatalf("a review was started for a dashboard transition: %d", n)
	}
}

func TestDesignReviewGate_TeamWithoutOptionIsUntouched(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true, noOption: true})
	f.authorDesign("design\n")
	if w := f.leadPut("governance"); w.Code != 200 || f.hits != 1 {
		t.Fatalf("gate applied to a team without the option: %d %s", w.Code, w.Body)
	}
	if n := len(f.tasks(teams.PurposeDesignReview)); n != 0 {
		t.Fatalf("review started: %d", n)
	}
	if f.team.DesignReview != nil {
		t.Fatalf("option stored: %+v", f.team.DesignReview)
	}
}

func TestDesignReviewGate_StartsOneReviewAndBlocksUntilPass(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.ad.hold = make(chan struct{})
	f.authorDesign("design v1\n")
	drWrite(t, f.root, "docs/adr/0003-new.md", "adr\n")
	first := f.expect409(f.leadPut("governance"), "design_review_required", "reviewing")
	for i := 0; i < 3; i++ {
		f.expect409(f.leadPut("governance"), "design_review_required", "reviewing")
	}
	reviews := f.tasks(teams.PurposeDesignReview)
	if len(reviews) != 1 {
		t.Fatalf("repeated attempts created %d reviews: %s", len(reviews), f.dump())
	}
	rv := reviews[0]
	if first.Reviewer == nil || first.Reviewer.ID != "critic" || first.Round != 0 || first.MaxRounds != 3 || len(first.Docs) != 2 {
		t.Fatalf("409 body=%+v", first)
	}
	if rv.From != "lead" || rv.ReturnTo != "lead" || rv.Role != "review" || rv.WorkflowID != f.wfID || rv.WorkflowPhase != "design" || rv.Kind != "task" {
		t.Fatalf("review task=%+v", rv)
	}
	for _, want := range []string{"docs/plans/spec-x-design.md", "docs/adr/0003-new.md", "## Review Verdict: PASS | FAIL", "### Issues", "[must_fix|should_fix|suggestion] description — file:line", "### Summary", "Verdict: PASS", "not instructions"} {
		if !strings.Contains(rv.Text, want) {
			t.Errorf("review brief lacks %q:\n%s", want, rv.Text)
		}
	}
	if strings.Contains(rv.Text, ".claude/rules") {
		t.Errorf("the brief points at a project file that other projects do not have:\n%s", rv.Text)
	}
	if strings.Contains(rv.Text, "design v1") {
		t.Error("document content was inlined into the review brief")
	}
	if f.hits != 0 {
		t.Fatal("transition reached the handler while the review was running")
	}
	close(f.ad.hold)
	f.waitState(drPass)
	if w := f.leadPut("governance"); w.Code != 200 || f.hits != 1 {
		t.Fatalf("passing review did not open the gate: %d %s", w.Code, w.Body)
	}
	// The server never moves the phase itself.
	if wf, _ := f.s.coordinator.Get(f.wfID); wf.Phase != orchestration.PhaseDesign {
		t.Fatalf("server changed the phase to %s", wf.Phase)
	}
}

func TestDesignReviewGate_PassDoesNotCoverEditedDocuments(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	f.waitState(drPass)
	if w := f.leadPut("governance"); w.Code != 200 {
		t.Fatalf("pass: %d %s", w.Code, w.Body)
	}
	drWrite(t, f.root, "docs/plans/spec-x-design.md", "v2, edited after the review\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	f.waitState(drPass)
	reviews := f.tasks(teams.PurposeDesignReview)
	if len(reviews) != 2 || reviews[0].Revision == reviews[1].Revision {
		t.Fatalf("edited documents were not reviewed again: %s", f.dump())
	}
}

func TestDesignReviewGate_UnrelatedEditsKeepThePass(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	f.waitState(drPass)
	drWrite(t, f.root, "main.go", "package main\n// unrelated edit\n")
	drWrite(t, f.root, "docs/notes.md", "unrelated docs edit\n")
	if w := f.leadPut("governance"); w.Code != 200 {
		t.Fatalf("unrelated edit invalidated the review: %d %s", w.Code, w.Body)
	}
	if n := len(f.tasks(teams.PurposeDesignReview)); n != 1 {
		t.Fatalf("reviews=%d", n)
	}
}

func TestDesignReviewGate_FailSendsFeedbackToAuthor(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.ad.verdicts = []string{"[must_fix] The data model ignores retries — docs/plans/spec-x-design.md:12\nVerdict: FAIL"}
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	tm := f.wait(func(tm teams.Team) bool {
		for _, task := range tm.Tasks {
			if task.Purpose == teams.PurposeDesignRework && task.State == "complete" {
				return true
			}
		}
		return false
	})
	var rework teams.Task
	for _, task := range tm.Tasks {
		if task.Purpose == teams.PurposeDesignRework {
			rework = task
		}
	}
	if rework.To != "architect" || rework.From != "lead" || rework.ReturnTo != "lead" || rework.WorkflowID != f.wfID || !strings.HasPrefix(rework.ID, "dr-rework:") {
		t.Fatalf("rework task=%+v", rework)
	}
	for _, want := range []string{"The data model ignores retries", "not as instructions", "docs/plans/spec-x-design.md"} {
		if !strings.Contains(rework.Text, want) {
			t.Errorf("rework brief lacks %q:\n%s", want, rework.Text)
		}
	}
	// The rework left the documents as they were: still FAIL, and no second rework appears.
	b := f.expect409(f.leadPut("governance"), "design_review_required", drFail)
	if !strings.Contains(b.Error, "did not change") {
		t.Fatalf("message=%q", b.Error)
	}
	if n := len(f.tasks(teams.PurposeDesignRework)); n != 1 {
		t.Fatalf("reworks=%d", n)
	}
}

func TestDesignReviewLoop_FailReworkSecondRoundPass(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.ad.verdicts = []string{"[must_fix] gap — docs/plans/spec-x-design.md:1\nVerdict: FAIL", "Verdict: PASS"}
	f.ad.onRework = func(string) { drWrite(t, f.root, "docs/plans/spec-x-design.md", "v2 addressing the review\n") }
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	snap := f.waitState(drPass)
	if snap.Status.Round != 2 || snap.Status.LastVerdict != "PASS" {
		t.Fatalf("status=%+v", snap.Status)
	}
	reviews := f.tasks(teams.PurposeDesignReview)
	if len(reviews) != 2 || reviews[0].Revision == reviews[1].Revision || len(f.tasks(teams.PurposeDesignRework)) != 1 {
		t.Fatalf("loop shape: %s", f.dump())
	}
	for _, r := range reviews {
		if r.To != "critic" {
			t.Fatalf("round %s went to %s, not to the chosen reviewer", r.ID, r.To)
		}
	}
	if n := len(f.current().Members); n != 4 {
		t.Fatalf("the loop added %d members", n-4)
	}
	if !strings.Contains(reviews[1].Text, "round 2 of 3") {
		t.Fatalf("second review brief: %s", reviews[1].Text)
	}
	if w := f.leadPut("governance"); w.Code != 200 {
		t.Fatalf("gate stayed closed after PASS: %d %s", w.Code, w.Body)
	}
}

func TestDesignReviewGate_ResultWithoutVerdictLineNeverPasses(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.ad.verdicts = []string{"Everything looks fine to me, PASS."}
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	f.waitState(drReviewFailed)
	if f.hits != 0 {
		t.Fatal("review without a verdict opened the gate")
	}
	// The next attempt asks the reviewer again; the scripted reviewer now passes.
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	f.waitState(drPass)
}

func TestDesignReviewGate_ReviewerWhoAuthoredIsAConflict(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true, reviewer: "architect"})
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_conflict", "")
	if n := len(f.tasks(teams.PurposeDesignReview)); n != 0 {
		t.Fatalf("review started by its own author: %d", n)
	}
}

func TestDesignReviewGate_ExhaustedThenUserOverride(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true, maxRounds: 1})
	f.ad.verdicts = []string{"[must_fix] x — docs/plans/spec-x-design.md:1\nVerdict: FAIL"}
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	f.waitState(drExhausted)
	b := f.expect409(f.leadPut("governance"), "design_review_exhausted", drExhausted)
	if b.Round != 1 || b.MaxRounds != 1 {
		t.Fatalf("body=%+v", b)
	}
	if n := len(f.tasks(teams.PurposeDesignRework)); n != 0 {
		t.Fatalf("an exhausted review still sent rework: %d", n)
	}
	path := "/api/teams/" + f.team.ID + "/design-review/override"
	body := `{"workflow_id":"` + f.wfID + `","reason":"Schválené používateľom"}`
	guarded := func(origin, token string) int {
		r := httptest.NewRequest("POST", "http://localhost:41777"+path, strings.NewReader(body))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if token != "" {
			r.Header.Set("X-Stratus-Team-Token", token)
		}
		w := httptest.NewRecorder()
		f.guard.ServeHTTP(w, r)
		return w.Code
	}
	hitsBefore := f.hits
	if code := guarded("https://evil.example", ""); code != 403 {
		t.Fatalf("cross-origin override: %d", code)
	}
	if code := guarded("", f.token("lead")); code != 403 {
		t.Fatalf("a team token must not override: %d", code)
	}
	if f.hits != hitsBefore {
		t.Fatal("refused overrides reached the handler")
	}
	if code := guarded("http://localhost:41777", ""); code != 200 {
		t.Fatalf("same-origin override: %d", code)
	}
	override := func(body string) *httptest.ResponseRecorder {
		return commsDo(t, f.s.handleDesignReviewOverride, "POST", path, body, map[string]string{"id": f.team.ID})
	}
	for _, bad := range []string{`{"workflow_id":"spec-x"}`, `{"workflow_id":"spec-x","reason":"   "}`, `{"workflow_id":"spec-x","reason":"` + strings.Repeat("x", 301) + `"}`, `{"workflow_id":"ghost","reason":"ok"}`} {
		if w := override(bad); w.Code != 400 && w.Code != 404 {
			t.Fatalf("bad override accepted: %d %s", w.Code, w.Body)
		}
	}
	w := override(body)
	if w.Code != 200 {
		t.Fatalf("override: %d %s", w.Code, w.Body)
	}
	var st designStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || st.State != drWaived || !st.Waived {
		t.Fatalf("status=%+v err=%v", st, err)
	}
	if w = f.leadPut("governance"); w.Code != 200 {
		t.Fatalf("override did not open the gate: %d %s", w.Code, w.Body)
	}
	if got := f.current().DesignReviewWaivers; len(got) != 1 || got[0].WorkflowID != f.wfID || got[0].Reason != "Schválené používateľom" {
		t.Fatalf("waivers=%+v", got)
	}
	// The waiver belongs to these exact documents: an edit starts a fresh review round.
	drWrite(t, f.root, "docs/plans/spec-x-design.md", "v2 after the override\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	snap := f.waitState(drPass)
	if snap.Status.Round != 1 {
		t.Fatalf("round after the waiver=%d, want a fresh round 1", snap.Status.Round)
	}
}

func TestDesignReviewOverride_OnlyAfterFailedReview(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.authorDesign("v1\n")
	body := `{"workflow_id":"spec-x","reason":"just let me"}`
	w := commsDo(t, f.s.handleDesignReviewOverride, "POST", "/x", body, map[string]string{"id": f.team.ID})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "design_review_override_not_allowed") {
		t.Fatalf("override before any review: %d %s", w.Code, w.Body)
	}
}

func TestDesignReviewGate_BackwardAndAbortAreNeverBlocked(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.authorDesign("v1\n")
	if w := f.leadPut("plan"); w.Code != 200 || f.hits != 1 {
		t.Fatalf("design -> plan was blocked: %d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("POST", "/api/workflows/"+f.wfID+"/abort", strings.NewReader(`{}`))
	r.Header.Set("X-Stratus-Team-Token", f.token("lead"))
	w := httptest.NewRecorder()
	f.guard.ServeHTTP(w, r)
	if w.Code != 200 || f.hits != 2 {
		t.Fatalf("abort was blocked: %d %s", w.Code, w.Body)
	}
	if n := len(f.tasks(teams.PurposeDesignReview)); n != 0 {
		t.Fatalf("review started by an ungated transition: %d", n)
	}
}

func TestDesignReviewGate_DocumentsMissing(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	b := f.expect409(f.leadPut("governance"), "design_review_documents_missing", drDocsMissing)
	if !strings.Contains(b.Error, "docs/plans/spec-x-design.md") {
		t.Fatalf("message=%q", b.Error)
	}
	if n := len(f.tasks(teams.PurposeDesignReview)); n != 0 {
		t.Fatalf("review started without documents: %d", n)
	}
}

func TestDesignReviewGate_SymlinkedDocumentIsRejected(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	os.MkdirAll(filepath.Join(f.root, "docs/plans"), 0o755)
	if err := os.Symlink(filepath.Join(f.root, "main.go"), filepath.Join(f.root, "docs/plans/spec-x-design.md")); err != nil {
		t.Fatal(err)
	}
	f.expect409(f.leadPut("governance"), "design_review_documents_invalid", drDocsInvalid)
}

func TestDesignReviewGate_SimpleSpecPlanToImplement(t *testing.T) {
	f := newDRFixture(t, drOpts{})
	f.ad.verdicts = []string{"[must_fix] no rollback — docs/plans/spec-x-plan.md:4\nVerdict: FAIL", "Verdict: PASS"}
	f.ad.onRework = func(string) { drWrite(t, f.root, "docs/plans/spec-x-plan.md", "plan v2\n") }
	f.delegate("plan-1", "architect", "delivery-system-architect", "write the plan")
	drWrite(t, f.root, "docs/plans/spec-x-plan.md", "plan v1\n")
	drWrite(t, f.root, "docs/adr/0003-new.md", "adr that the plan review must not include\n")
	// Not gated: simple spec has no governance step, and discovery is not a plan exit here.
	b := f.expect409(f.leadPut("implement"), "design_review_required", "")
	if len(b.Docs) != 1 || b.Docs[0].Path != "docs/plans/spec-x-plan.md" {
		t.Fatalf("plan review docs=%+v", b.Docs)
	}
	f.waitState(drPass)
	if w := f.leadPut("implement"); w.Code != 200 {
		t.Fatalf("plan -> implement stayed closed: %d %s", w.Code, w.Body)
	}
	if n := len(f.tasks(teams.PurposeDesignRework)); n != 1 {
		t.Fatalf("reworks=%d", n)
	}
}

// ---------- status endpoint, decisions, evidence ----------

func TestDesignReviewStatus_Shapes(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	get := func(id string) *httptest.ResponseRecorder {
		return commsDo(t, f.s.handleDesignReviewStatus, "GET", "/api/workflows/"+id+"/design-review", "", map[string]string{"id": id})
	}
	if w := get("ghost"); w.Code != 404 {
		t.Fatalf("unknown workflow: %d", w.Code)
	}
	var st map[string]any
	w := get(f.wfID)
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if st["enabled"] != true || st["gated_transition"] != "design->governance" || st["state"] != drDocsMissing {
		t.Fatalf("before documents: %v", st)
	}
	f.ad.verdicts = []string{"[must_fix] a — docs/plans/spec-x-design.md:1\nVerdict: FAIL"}
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	// The automation sends the rework at once; the settled state is fail again.
	f.wait(func(tm teams.Team) bool {
		return taskByID(tm, "dr-rework:dr-review:"+f.wfID+":"+designHash(t, f)+":1").State == "complete"
	})
	f.waitState(drFail)
	st = map[string]any{}
	if err := json.Unmarshal(get(f.wfID).Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	reviewer, _ := st["reviewer"].(map[string]any)
	author, _ := st["author"].(map[string]any)
	docs, _ := st["docs"].([]any)
	if st["state"] != drFail || st["round"] != float64(1) || st["max_rounds"] != float64(3) || reviewer["id"] != "critic" || reviewer["role"] != "delivery-code-reviewer" ||
		author["id"] != "architect" || len(docs) != 1 || len(st["doc_hash"].(string)) != 12 || st["last_verdict"] != "FAIL" || st["last_review_task_id"] == "" ||
		!strings.Contains(st["issues_excerpt"].(string), "[must_fix] a") || st["waived"] != false {
		t.Fatalf("status=%v", st)
	}
	// A team without the option, and a workflow outside the gated phase.
	g := newDRFixture(t, drOpts{complex: true, noOption: true})
	w = commsDo(t, g.s.handleDesignReviewStatus, "GET", "/x", "", map[string]string{"id": g.wfID})
	if strings.TrimSpace(w.Body.String()) != `{"enabled":false}` {
		t.Fatalf("no option: %s", w.Body)
	}
	if _, err := f.s.coordinator.Transition(f.wfID, orchestration.PhaseGovernance); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(get(f.wfID).Body.Bytes(), &st); err != nil || st["state"] != drInactive || st["gated_transition"] != "" {
		t.Fatalf("outside the gated phase: %v %v", st, err)
	}
}

func TestDesignReviewDecisions_ExhaustedItem(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true, maxRounds: 1})
	f.authorDesign("v1\n")
	if items := f.s.designReviewDecisions([]teams.Team{f.current()}); len(items) != 0 {
		t.Fatalf("item before any review: %+v", items)
	}
	f.ad.verdicts = []string{"[must_fix] x — docs/plans/spec-x-design.md:1\nVerdict: FAIL"}
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	f.waitState(drExhausted)
	items := f.s.designReviewDecisions([]teams.Team{f.current()})
	if len(items) != 1 {
		t.Fatalf("items=%+v", items)
	}
	it := items[0]
	if it.ID != "design_review:"+f.team.ID+":spec-x" || it.Kind != "design_review" || it.Link != "#/workflow/spec-x" || len(it.Actions) != 1 {
		t.Fatalf("item=%+v", it)
	}
	a := it.Actions[0]
	if a.Name != "override" || a.Method != "POST" || a.Path != "/teams/"+f.team.ID+"/design-review/override" || a.Body["workflow_id"] != "spec-x" || a.Body["reason"] != "Schválené používateľom" {
		t.Fatalf("action=%+v", a)
	}
	// Reachable through the real decisions handler too.
	w := commsDo(t, f.s.handleDecisions, "GET", "/api/decisions", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"kind":"design_review"`) {
		t.Fatalf("decisions: %d %s", w.Code, w.Body)
	}
	if w := f.override(); w.Code != 200 {
		t.Fatalf("override: %d %s", w.Code, w.Body)
	}
	if items = f.s.designReviewDecisions([]teams.Team{f.current()}); len(items) != 0 {
		t.Fatalf("item after the user decided: %+v", items)
	}
}

func (f *drFixture) override() *httptest.ResponseRecorder {
	return commsDo(f.t, f.s.handleDesignReviewOverride, "POST", "/x", `{"workflow_id":"spec-x","reason":"Schválené používateľom"}`, map[string]string{"id": f.team.ID})
}

func TestDesignReviewEvidence_StaleOnlyWhenDocumentsChange(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	f.waitState(drPass)
	verdict := func() string {
		t.Helper()
		if err := f.s.syncManagedTeamState(); err != nil {
			t.Fatal(err)
		}
		agents, err := f.s.db.ListWorkflowAgents(f.wfID)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range agents {
			if a.AgentType == "delivery-code-reviewer" && a.Phase == "design" {
				return a.Verdict
			}
		}
		t.Fatalf("no design reviewer evidence: %+v", agents)
		return ""
	}
	if v := verdict(); v != "PASS" {
		t.Fatalf("verdict=%q", v)
	}
	drWrite(t, f.root, "main.go", "package main\n// unrelated\n")
	if v := verdict(); v != "PASS" {
		t.Fatalf("an unrelated edit staled the design review: %q", v)
	}
	drWrite(t, f.root, "docs/plans/spec-x-design.md", "edited\n")
	if v := verdict(); v == "PASS" {
		t.Fatal("editing the design document kept the PASS evidence")
	}
}

func TestTeamAPICreate_DesignReviewValidation(t *testing.T) {
	s := teamAPIServer(t)
	body := func(option string) string {
		return `{"members":[{"id":"lead","role":"lead","profile":` + drProfile + `},{"id":"critic","role":"review","profile":` + drProfile + `}],"design_review":` + option + `}`
	}
	for _, c := range []struct{ option, want string }{
		{`{"enabled":true}`, "reviewer_member_id"},
		{`{"enabled":true,"reviewer_member_id":"ghost"}`, "not a member"},
		{`{"enabled":true,"reviewer_member_id":"lead"}`, "team lead"},
		{`{"enabled":true,"reviewer_member_id":"critic","max_rounds":11}`, "max_rounds"},
	} {
		w := commsDo(t, s.handleCreateTeam, "POST", "/api/teams", body(c.option), nil)
		if w.Code != 400 || !strings.Contains(w.Body.String(), c.want) {
			t.Fatalf("%s: %d %s", c.option, w.Code, w.Body)
		}
	}
	w := commsDo(t, s.handleCreateTeam, "POST", "/api/teams", body(`{"enabled":true,"reviewer_member_id":"critic"}`), nil)
	var team teams.Team
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &team) != nil || team.DesignReview == nil || team.DesignReview.MaxRounds != 3 {
		t.Fatalf("valid option: %d %s", w.Code, w.Body)
	}
}

func designHash(t *testing.T, f *drFixture) string {
	t.Helper()
	return designState(f.current(), mustWF(t, f)).FullHash[:12]
}

// A report written exactly as the review brief prescribes is read by the strict parser.
func TestDesignReviewBrief_PrescribedFormatIsParsed(t *testing.T) {
	pass := "## Review Verdict: PASS\n\n### Issues\n\n[suggestion] Name the retry limit — docs/plans/x-design.md:12\n\n### Summary\nSound design.\n\nVerdict: PASS"
	fail := "## Review Verdict: FAIL\n\n### Issues\n\n[must_fix] No rollback path — docs/plans/x-design.md:30\n\n### Summary\nAdd a rollback.\n\nVerdict: FAIL"
	for report, want := range map[string]string{pass: "PASS", fail: "FAIL"} {
		if got := teamReviewVerdict(report).Verdict; got != want {
			t.Errorf("verdict=%q, want %q for:\n%s", got, want, report)
		}
	}
}

func TestDesignReworkBrief_DoesNotPointAtProjectFiles(t *testing.T) {
	f := newDRFixture(t, drOpts{complex: true})
	f.ad.verdicts = []string{"[must_fix] a — docs/plans/spec-x-design.md:1\nVerdict: FAIL"}
	f.authorDesign("v1\n")
	f.expect409(f.leadPut("governance"), "design_review_required", "")
	f.wait(func(tm teams.Team) bool {
		for _, task := range tm.Tasks {
			if task.Purpose == teams.PurposeDesignRework {
				return !strings.Contains(task.Text, ".claude/rules")
			}
		}
		return false
	})
}
