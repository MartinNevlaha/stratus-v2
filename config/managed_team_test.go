package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigSave_ManagedTeamRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".stratus.json")
	raw := `{"port":1234,"managed_team":{"profiles":{"codex":{"review":{"model":"gpt-6-astra","effort":"high","complex_effort":"xhigh"}}}}}`
	cfg := Default()
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(saved, &back); err != nil {
		t.Fatal(err)
	}
	got := back.ManagedTeam.Profiles["codex"]["review"]
	if got.Model != "gpt-6-astra" || got.Effort != "high" || got.ComplexEffort != "xhigh" {
		t.Fatalf("managed_team lost in Save: %+v", got)
	}
}

func TestConfigSave_EmptyManagedTeamIsOmitted(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".stratus.json")
	cfg := Default()
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "managed_team") {
		t.Fatal("empty managed_team must be omitted")
	}
}
