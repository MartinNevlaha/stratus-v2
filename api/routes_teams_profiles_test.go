package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/projectidentity"
)

func putTeamProfiles(t *testing.T, s *Server, body string) (int, string) {
	t.Helper()
	w := commsDo(t, s.handlePutTeamProfiles, "PUT", "/api/teams/profiles", body, nil)
	return w.Code, w.Body.String()
}

func teamProfilesRoot(t *testing.T, s *Server) string {
	t.Helper()
	_, root, err := projectidentity.Resolve(s.projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPutTeamProfiles_ValidSavesAndKeepsOtherKeys(t *testing.T) {
	s := teamAPIServer(t)
	root := teamProfilesRoot(t, s)
	path := filepath.Join(root, ".stratus.json")
	os.WriteFile(path, []byte(`{"port":4321,"language":"sk","unknown_future_key":{"a":[1,2]},"managed_team":{"extra":true,"profiles":{"codex":{"design":{"model":"gpt-6.1-sol","effort":"high"}}}}}`), 0o600)

	code, out := putTeamProfiles(t, s, `{"codex":{"implementation":{"model":"gpt-6.1-sol","effort":"high","complex_effort":"high"}}}`)
	if code != 200 {
		t.Fatalf("status %d %s", code, out)
	}
	raw, _ := os.ReadFile(path)
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"port", "language", "unknown_future_key", "managed_team"} {
		if _, ok := file[key]; !ok {
			t.Fatalf("key %q lost: %s", key, raw)
		}
	}
	var team struct {
		Extra    bool                                            `json:"extra"`
		Profiles map[string]map[string]config.ManagedTeamProfile `json:"profiles"`
	}
	if err := json.Unmarshal(file["managed_team"], &team); err != nil {
		t.Fatal(err)
	}
	if !team.Extra {
		t.Fatal("other managed_team keys must be preserved")
	}
	if team.Profiles["codex"]["design"].Model != "gpt-6.1-sol" {
		t.Fatal("previously saved role must be preserved")
	}
	saved := team.Profiles["codex"]["implementation"]
	if saved.Model != "gpt-6.1-sol" || saved.Effort != "high" || saved.ComplexEffort != "high" {
		t.Fatalf("saved = %+v", saved)
	}
	// The saved layer is what the preflight and new teams read.
	set := loadTeamProfiles(root)
	if set.Sources["codex"]["implementation"] != "project" || set.ComplexEfforts["codex"]["implementation"] != "high" {
		t.Fatalf("saved profile not visible to preflight: %+v %+v", set.Sources["codex"], set.ComplexEfforts["codex"])
	}
}

func TestPutTeamProfiles_KeepsFileModeAndLeavesNoTempFiles(t *testing.T) {
	s := teamAPIServer(t)
	root := teamProfilesRoot(t, s)
	path := filepath.Join(root, ".stratus.json")
	os.WriteFile(path, []byte(`{"llm":{"api_key":"secret"}}`), 0o600)
	os.Chmod(path, 0o600)
	body := `{"codex":{"lead":{"model":"gpt-6.1-sol","effort":"high"}}}`
	if code, out := putTeamProfiles(t, s, body); code != 200 {
		t.Fatalf("status %d %s", code, out)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("existing file mode must be preserved: %v %v", info, err)
	}
	os.Remove(path)
	if code, out := putTeamProfiles(t, s, body); code != 200 {
		t.Fatalf("status %d %s", code, out)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("new file must be private: %v %v", info, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(root, ".stratus.json.*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func TestPutTeamProfiles_CreatesFileAndUpdatesRunningConfig(t *testing.T) {
	s := teamAPIServer(t)
	cfg := config.Default()
	s.cfg = &cfg
	if code, out := putTeamProfiles(t, s, `{"codex":{"lead":{"model":"gpt-6.1-sol","effort":"high"}}}`); code != 200 {
		t.Fatalf("status %d %s", code, out)
	}
	if cfg.ManagedTeam == nil || cfg.ManagedTeam.Profiles["codex"]["lead"].Model != "gpt-6.1-sol" {
		t.Fatalf("in-memory config not updated: %+v", cfg.ManagedTeam)
	}
	// Another settings save from the in-memory config must not drop the section.
	path := filepath.Join(teamProfilesRoot(t, s), ".stratus.json")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"managed_team"`) || !strings.Contains(string(raw), "gpt-6.1-sol") {
		t.Fatalf("Config.Save dropped managed_team: %s", raw)
	}
}

func TestTeamPreflight_ExposesProfileSourcesAndKeepsShape(t *testing.T) {
	s := teamAPIServer(t)
	if code, out := putTeamProfiles(t, s, `{"codex":{"review":{"model":"gpt-6.1-sol","effort":"high","complex_effort":"high"}}}`); code != 200 {
		t.Fatalf("status %d %s", code, out)
	}
	w := commsDo(t, s.handleTeamPreflight, "GET", "/api/teams/preflight", "", nil)
	if w.Code != 200 {
		t.Fatalf("preflight %d %s", w.Code, w.Body)
	}
	var out struct {
		Profiles       map[string]map[string]teamsProfile `json:"profiles"`
		ProfileSources map[string]map[string]string       `json:"profile_sources"`
		ComplexEfforts map[string]map[string]string       `json:"complex_efforts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Profiles["codex"]["review"].Model != "gpt-6.1-sol" || out.ProfileSources["codex"]["review"] != "project" || out.ProfileSources["codex"]["lead"] != "default" || out.ComplexEfforts["codex"]["review"] != "high" {
		t.Fatalf("preflight: %s", w.Body)
	}
}

type teamsProfile struct{ Model, Effort string }

func TestPutTeamProfiles_InvalidReturns400WithReason(t *testing.T) {
	s := teamAPIServer(t)
	cases := []struct{ name, body, want string }{
		{"not json", `nope`, "invalid profiles body"},
		{"empty", `{}`, "no profiles"},
		{"unknown runtime", `{"mystery":{"review":{"model":"m","effort":"high"}}}`, `unknown runtime \"mystery\"`},
		{"runtime without roles", `{"codex":{}}`, "no roles"},
		{"unknown role", `{"codex":{"janitor":{"model":"gpt-6.1-sol","effort":"high"}}}`, `unknown role \"janitor\"`},
		{"unknown model", `{"codex":{"review":{"model":"gpt-9","effort":"high"}}}`, "gpt-9"},
		{"missing model", `{"codex":{"review":{"effort":"high"}}}`, "explicit model"},
		{"unknown effort", `{"codex":{"review":{"model":"gpt-6.1-sol","effort":"ludicrous"}}}`, "ludicrous"},
		{"unknown complex effort", `{"codex":{"review":{"model":"gpt-6.1-sol","effort":"high","complex_effort":"ludicrous"}}}`, "complex effort"},
		{"misspelled field", `{"codex":{"review":{"model":"gpt-6.1-sol","effort":"high","complexeffort":"high"}}}`, "complexeffort"},
		{"missing effort", `{"codex":{"review":{"model":"gpt-6.1-sol"}}}`, "effort is required"},
		{"empty effort", `{"codex":{"review":{"model":"gpt-6.1-sol","effort":""}}}`, "effort is required"},
		{"runtime adapter missing", `{"opencode":{"review":{"model":"p/m","effort":"high"}}}`, "opencode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out := putTeamProfiles(t, s, c.body)
			if code != 400 || !strings.Contains(out, c.want) {
				t.Fatalf("status %d body %s, want 400 containing %q", code, out, c.want)
			}
			if _, err := os.Stat(filepath.Join(teamProfilesRoot(t, s), ".stratus.json")); err == nil {
				t.Fatal("invalid request must not write .stratus.json")
			}
		})
	}
}

func TestPutTeamProfiles_ConcurrentWithConfigSaveKeepsBothChanges(t *testing.T) {
	s := teamAPIServer(t)
	cfg := config.Default()
	s.cfg = &cfg
	path := filepath.Join(teamProfilesRoot(t, s), ".stratus.json")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	const rounds = 30
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			cfg.Language = []string{"en", "sk"}[i%2]
			if err := cfg.Save(path); err != nil {
				t.Errorf("save: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			body := `{"codex":{"lead":{"model":"gpt-6.1-sol","effort":"high"}}}`
			if code, out := putTeamProfiles(t, s, body); code != 200 {
				t.Errorf("put status %d %s", code, out)
				return
			}
		}
	}()
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Language    string                    `json:"language"`
		ManagedTeam *config.ManagedTeamConfig `json:"managed_team"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("file is not valid JSON: %v\n%s", err, raw)
	}
	if file.Language != "sk" {
		t.Fatalf("last settings change lost: language=%q", file.Language)
	}
	if file.ManagedTeam == nil || file.ManagedTeam.Profiles["codex"]["lead"].Model != "gpt-6.1-sol" {
		t.Fatalf("saved profiles lost: %s", raw)
	}
}

func TestPutTeamProfiles_ThenConfigSaveKeepsManagedTeam(t *testing.T) {
	s := teamAPIServer(t)
	cfg := config.Default()
	s.cfg = &cfg
	path := filepath.Join(teamProfilesRoot(t, s), ".stratus.json")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	if code, out := putTeamProfiles(t, s, `{"codex":{"lead":{"model":"gpt-6.1-sol","effort":"high"}}}`); code != 200 {
		t.Fatalf("status %d %s", code, out)
	}
	s.cfg.Language = "sk"
	if err := s.cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var back config.Config
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Language != "sk" || back.ManagedTeam == nil || back.ManagedTeam.Profiles["codex"]["lead"].Model != "gpt-6.1-sol" {
		t.Fatalf("Save after PUT lost managed_team or language: %s", raw)
	}
}
