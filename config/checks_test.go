package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateChecks(t *testing.T) {
	ok := func(name string) CheckConfig { return CheckConfig{Name: name, Cmd: []string{"true"}} }
	many := make([]CheckConfig, 9)
	for i := range many {
		many[i] = ok(string(rune('a'+i)) + "x")
	}
	longArg := strings.Repeat("a", 257)
	manyArgs := make([]string, 17)
	for i := range manyArgs {
		manyArgs[i] = "x"
	}
	cases := []struct {
		name    string
		checks  []CheckConfig
		wantErr string // "" = valid
	}{
		{"nil is valid", nil, ""},
		{"simple", []CheckConfig{ok("test"), {Name: "lint-2_x", Cmd: []string{"npm", "run", "lint"}, TimeoutSec: 900}}, ""},
		{"max entries", many[:8], ""},
		{"too many", many, "at most 8"},
		{"empty name", []CheckConfig{ok("")}, "must match"},
		{"upper case name", []CheckConfig{ok("Test")}, "must match"},
		{"leading digit", []CheckConfig{ok("1x")}, "must match"},
		{"name too long", []CheckConfig{ok("a" + strings.Repeat("b", 32))}, "must match"},
		{"name 32 chars", []CheckConfig{ok("a" + strings.Repeat("b", 31))}, ""},
		{"reserved", []CheckConfig{ok("placeholder_scan")}, "reserved"},
		{"duplicate", []CheckConfig{ok("test"), ok("test")}, "used twice"},
		{"no cmd", []CheckConfig{{Name: "t"}}, "1 to 16 elements"},
		{"too many args", []CheckConfig{{Name: "t", Cmd: manyArgs}}, "1 to 16 elements"},
		{"16 args", []CheckConfig{{Name: "t", Cmd: manyArgs[:16]}}, ""},
		{"empty arg", []CheckConfig{{Name: "t", Cmd: []string{"go", ""}}}, "cmd[1]"},
		{"long arg", []CheckConfig{{Name: "t", Cmd: []string{longArg}}}, "cmd[0]"},
		{"newline arg", []CheckConfig{{Name: "t", Cmd: []string{"a\nb"}}}, "NUL or a newline"},
		{"nul arg", []CheckConfig{{Name: "t", Cmd: []string{"a\x00b"}}}, "NUL or a newline"},
		{"negative timeout", []CheckConfig{{Name: "t", Cmd: []string{"x"}, TimeoutSec: -1}}, "timeout_sec"},
		{"timeout too big", []CheckConfig{{Name: "t", Cmd: []string{"x"}, TimeoutSec: 901}}, "timeout_sec"},
		{"timeout 1", []CheckConfig{{Name: "t", Cmd: []string{"x"}, TimeoutSec: 1}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateChecks(c.checks)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidChecks) {
				t.Fatalf("error %v does not wrap ErrInvalidChecks", err)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error %q does not mention %q", err, c.wantErr)
			}
		})
	}
}

func TestSave_RoundTripKeepsChecks(t *testing.T) {
	cfg := Default()
	cfg.Checks = []CheckConfig{{Name: "test", Cmd: []string{"npm", "test"}, TimeoutSec: 300}, {Name: "lint", Cmd: []string{"make", "lint"}}}
	path := filepath.Join(t.TempDir(), ".stratus.json")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Checks) != 2 || back.Checks[0].Name != "test" || back.Checks[0].TimeoutSec != 300 || strings.Join(back.Checks[1].Cmd, " ") != "make lint" {
		t.Fatalf("checks after round trip: %+v", back.Checks)
	}
	if strings.Contains(string(data), `"timeout_sec": 0`) {
		t.Errorf("a zero timeout should be omitted:\n%s", data)
	}
}

func TestSave_NoChecksOmitsTheField(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".stratus.json")
	cfg := Default()
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), `"checks"`) {
		t.Errorf("unexpected checks field:\n%s", data)
	}
}

func TestLoad_InvalidChecksDoNotFailTheLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".stratus.json"), []byte(`{"port":4242,"checks":[{"name":"BAD","cmd":[]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	cfg := Load()
	if cfg.Port != 4242 || len(cfg.Checks) != 1 {
		t.Fatalf("load: port=%d checks=%+v", cfg.Port, cfg.Checks)
	}
	if err := ValidateChecks(cfg.Checks); !errors.Is(err, ErrInvalidChecks) {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateChecks_TotalTimeoutIsCapped(t *testing.T) {
	four := []CheckConfig{
		{Name: "a", Cmd: []string{"x"}, TimeoutSec: 900}, {Name: "b", Cmd: []string{"x"}, TimeoutSec: 900},
		{Name: "c", Cmd: []string{"x"}, TimeoutSec: 900}, {Name: "d", Cmd: []string{"x"}, TimeoutSec: 900},
	}
	if err := ValidateChecks(four); err != nil {
		t.Fatalf("3600 s in total should be valid: %v", err)
	}
	err := ValidateChecks(append(four, CheckConfig{Name: "e", Cmd: []string{"x"}, TimeoutSec: 1}))
	if !errors.Is(err, ErrInvalidChecks) || !strings.Contains(err.Error(), "3601") || !strings.Contains(err.Error(), "3600") {
		t.Fatalf("3601 s in total: %v", err)
	}
	// An unset timeout counts as the 120 s default.
	err = ValidateChecks([]CheckConfig{
		{Name: "a", Cmd: []string{"x"}, TimeoutSec: 900}, {Name: "b", Cmd: []string{"x"}, TimeoutSec: 900},
		{Name: "c", Cmd: []string{"x"}, TimeoutSec: 900}, {Name: "d", Cmd: []string{"x"}, TimeoutSec: 780},
		{Name: "e", Cmd: []string{"x"}},
	})
	if err != nil {
		t.Fatalf("2700+780+120 s = 3600 should be valid, got: %v", err)
	}
	err = ValidateChecks([]CheckConfig{
		{Name: "a", Cmd: []string{"x"}, TimeoutSec: 900}, {Name: "b", Cmd: []string{"x"}, TimeoutSec: 900},
		{Name: "c", Cmd: []string{"x"}, TimeoutSec: 900}, {Name: "d", Cmd: []string{"x"}, TimeoutSec: 900},
		{Name: "e", Cmd: []string{"x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "3720") {
		t.Fatalf("default timeout must count as 120 s: %v", err)
	}
}

func TestValidateChecks_ArtifactsNameIsReserved(t *testing.T) {
	err := ValidateChecks([]CheckConfig{{Name: "checks_artifacts", Cmd: []string{"x"}}})
	if !errors.Is(err, ErrInvalidChecks) || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("got %v", err)
	}
}
