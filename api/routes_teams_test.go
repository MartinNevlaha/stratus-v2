package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/projectidentity"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

type teamTestAdapter struct{}

func (teamTestAdapter) Preflight(context.Context, string) teams.Availability {
	return teams.Availability{Runtime: "codex", Available: true, Models: []teams.Model{{ID: "gpt-6.1-sol", Efforts: []string{"high"}}}}
}
func (teamTestAdapter) Open(context.Context, teams.SessionSpec, func(teams.Event)) (teams.Session, error) {
	return nil, context.Canceled
}

func teamAPIServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	d, err := db.Open(filepath.Join(t.TempDir(), "teams.db"))
	if err != nil {
		t.Fatal(err)
	}
	adapters := map[string]teams.Adapter{"codex": teamTestAdapter{}}
	m, err := teams.NewManager(d.SQL(), adapters, func() {})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: d, projectRoot: root, hub: NewHub(), teams: m, teamAdapters: adapters}
	t.Cleanup(func() { m.Close(); d.Close() })
	return s
}

func createAPITeam(t *testing.T, s *Server) teams.Team {
	t.Helper()
	w := commsDo(t, s.handleCreateTeam, "POST", "/api/teams", `{"members":[{"id":"lead","role":"lead","profile":{"runtime":"codex","model":"gpt-6.1-sol","effort":"high"}}]}`, nil)
	if w.Code != 200 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), `"token"`) {
		t.Fatal("private member credential exposed")
	}
	var team teams.Team
	if err := json.Unmarshal(w.Body.Bytes(), &team); err != nil {
		t.Fatal(err)
	}
	return team
}

func TestTeamAPICreateScopesRootAndHidesCredentials(t *testing.T) {
	s := teamAPIServer(t)
	tm := createAPITeam(t, s)
	id, _, err := projectidentity.Resolve(s.projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if tm.ProjectID != id {
		t.Fatalf("project %s", tm.ProjectID)
	}
	actual, err := s.teams.Get(tm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Members[0].Token == "" {
		t.Fatal("no private credential")
	}
	w := commsDo(t, s.handleTeamContext, "GET", "/api/team-context?session_key=made-up", "", nil)
	if w.Code != 403 {
		t.Fatalf("unknown identity allowed: %d", w.Code)
	}
}

func TestTeamAPIPrivateContextUsesCapability(t *testing.T) {
	s := teamAPIServer(t)
	tm := createAPITeam(t, s)
	private, _ := s.teams.Get(tm.ID)
	r := httptest.NewRequest("GET", "/api/team-context?session_key=forged", nil)
	r.Header.Set("X-Stratus-Team-Token", private.Members[0].Token)
	w := httptest.NewRecorder()
	s.handleTeamContext(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("context %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), private.Members[0].Token) {
		t.Fatal("credential leaked")
	}
}

func TestManagedWorkerCannotMutateWorkflow(t *testing.T) {
	s := teamAPIServer(t)
	id, root, _ := projectidentity.Resolve(s.projectRoot)
	tm, err := s.teams.Create(context.Background(), teams.CreateRequest{ProjectID: id, ProjectRoot: root, Members: []teams.MemberConfig{{ID: "reviewer", Role: "review", Profile: teams.Profile{Runtime: "codex", Model: "gpt-6.1-sol", Effort: "high"}}}})
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	h := s.managedTeamGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true; w.WriteHeader(200) }))
	r := httptest.NewRequest("PUT", "/api/workflows/wf/phase", strings.NewReader(`{"phase":"complete"}`))
	r.Header.Set("X-Stratus-Team-Token", tm.Members[0].Token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if hit || w.Code != 403 {
		t.Fatalf("worker crossed ownership guard: %d", w.Code)
	}
}

// The API may receive forged legacy session IDs from a model; the capability is
// the only authority for managed calls, and workers cannot promote themselves.
func TestManagedIdentityRewritesLegacyCommsSender(t *testing.T) {
	s := teamAPIServer(t)
	tm := createAPITeam(t, s)
	private, _ := s.teams.Get(tm.ID)
	member := private.Members[0]
	member.NativeID = "owned"
	body := `{"from":"user","to":"someone","body":"not user authority"}`
	r := httptest.NewRequest("POST", "/api/messages", strings.NewReader(body))
	w := httptest.NewRecorder()
	if !s.bindManagedIdentity(w, r, private, &member) {
		t.Fatalf("bind: %d %s", w.Code, w.Body)
	}
	var got map[string]any
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["from"] != "codex:owned" {
		t.Fatalf("sender=%v", got["from"])
	}
}
func TestTeamControlsRejectCrossOrigin(t *testing.T) {
	s := teamAPIServer(t)
	called := false
	h := s.managedTeamGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	r := httptest.NewRequest("POST", "http://localhost:41778/api/teams", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://unrelated.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if called || w.Code != 403 {
		t.Fatalf("cross-origin team start: %d", w.Code)
	}
}
