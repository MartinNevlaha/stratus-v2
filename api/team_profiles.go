package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/teams"
)

var teamRoleAssets = map[string]string{"lead": "", "implementation": "delivery-implementation-expert", "design": "delivery-system-architect", "review": "delivery-code-reviewer", "diagnosis": "delivery-debugger", "mechanical": ""}

var teamRuntimeNames = []string{"codex", "claude-code", "opencode"}

// Sources of a role's profile, from weakest to strongest layer.
const (
	profileSourceDefault   = "default"
	profileSourceAgentFile = "agent-file"
	profileSourceOpenCode  = "opencode"
	profileSourceProject   = "project"
)

// teamProfileSet is the merged profile layers plus where each entry came from.
// ComplexEfforts only has entries the project saved explicitly.
type teamProfileSet struct {
	Profiles       map[string]map[string]teams.Profile
	Sources        map[string]map[string]string
	ComplexEfforts map[string]map[string]string
	Warnings       []string
}

func teamProfiles(root string) (map[string]map[string]teams.Profile, []string) {
	set := loadTeamProfiles(root)
	return set.Profiles, set.Warnings
}

// Team defaults are local launch templates, never changes to global CLI settings.
func loadTeamProfiles(root string) teamProfileSet {
	out := map[string]map[string]teams.Profile{}
	sources := map[string]map[string]string{}
	complexEfforts := map[string]map[string]string{}
	warnings := []string{}
	for _, runtime := range teamRuntimeNames {
		out[runtime] = map[string]teams.Profile{}
		sources[runtime] = map[string]string{}
		complexEfforts[runtime] = map[string]string{}
		for role := range teamRoleAssets {
			out[runtime][role] = teams.DefaultProfile(runtime, role, "", "")
			sources[runtime][role] = profileSourceDefault
		}
	}
	// Keep role aliases exactly as configured; the native adapter checks availability.
	for role, asset := range teamRoleAssets {
		if asset == "" {
			continue
		}
		p := filepath.Join(root, ".claude", "agents", asset+".md")
		raw, e := os.ReadFile(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			warnings = append(warnings, e.Error())
			continue
		}
		fields := frontmatterScalars(raw)
		profile := out["claude-code"][role]
		if v := fields["model"]; v != "" {
			profile.Model = v
			sources["claude-code"][role] = profileSourceAgentFile
		}
		if v := fields["effort"]; v != "" {
			profile.Effort = v
			sources["claude-code"][role] = profileSourceAgentFile
		}
		out["claude-code"][role] = profile
	}
	var opencode struct {
		Agent map[string]struct {
			Model   string `json:"model"`
			Variant string `json:"variant"`
		} `json:"agent"`
	}
	if raw, e := os.ReadFile(filepath.Join(root, "opencode.json")); e == nil {
		if e = json.Unmarshal(raw, &opencode); e != nil {
			warnings = append(warnings, "Cannot read opencode.json role overrides: "+e.Error())
		} else {
			for role, asset := range teamRoleAssets {
				if v, ok := opencode.Agent[asset]; ok {
					p := out["opencode"][role]
					if v.Model != "" {
						p.Model = v.Model
						sources["opencode"][role] = profileSourceOpenCode
					}
					if v.Variant != "" {
						p.Effort = v.Variant
						sources["opencode"][role] = profileSourceOpenCode
					}
					out["opencode"][role] = p
				}
			}
		}
	}
	// Explicit per-project team choices override native role defaults.
	var cfg struct {
		ManagedTeam struct {
			Profiles map[string]map[string]config.ManagedTeamProfile `json:"profiles"`
		} `json:"managed_team"`
	}
	if raw, e := os.ReadFile(filepath.Join(root, ".stratus.json")); e == nil {
		if e = json.Unmarshal(raw, &cfg); e != nil {
			warnings = append(warnings, "Cannot read project team profiles: "+e.Error())
		} else {
			for runtime, roles := range cfg.ManagedTeam.Profiles {
				if out[runtime] == nil {
					continue
				}
				for role, p := range roles {
					if _, ok := out[runtime][role]; !ok {
						continue
					}
					base := out[runtime][role]
					if p.Model != "" {
						base.Model = p.Model
						sources[runtime][role] = profileSourceProject
					}
					if p.Effort != "" {
						base.Effort = p.Effort
						sources[runtime][role] = profileSourceProject
					}
					if p.ComplexEffort != "" {
						complexEfforts[runtime][role] = p.ComplexEffort
					}
					out[runtime][role] = base
				}
			}
		}
	}
	return teamProfileSet{Profiles: out, Sources: sources, ComplexEfforts: complexEfforts, Warnings: warnings}
}
func frontmatterScalars(raw []byte) map[string]string {
	fields := map[string]string{}
	scan := bufio.NewScanner(bytes.NewReader(raw))
	if !scan.Scan() || strings.TrimSpace(scan.Text()) != "---" {
		return fields
	}
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && (key == "model" || key == "effort") {
			fields[key] = strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	return fields
}

// Fingerprint actual review inputs, including uncommitted and untracked files.
// It is computed only for a review dispatch/completion, never on a timer.
func teamCodeRevision(root string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = root
		return cmd.Output()
	}
	h := sha256.New()
	head, err := run("rev-parse", "--verify", "HEAD")
	if err != nil {
		if _, e := run("rev-parse", "--git-dir"); e != nil {
			return "", fmt.Errorf("review requires a Git working tree: %w", e)
		}
	}
	h.Write(head)
	for _, args := range [][]string{{"diff", "--binary"}, {"diff", "--cached", "--binary"}, {"status", "--porcelain=v1", "-z"}} {
		b, e := run(args...)
		if e != nil {
			return "", e
		}
		h.Write(b)
	}
	paths, err := run("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	var total int64
	for _, path := range strings.Split(string(paths), "\x00") {
		if path == "" {
			continue
		}
		full := filepath.Join(root, path)
		info, e := os.Lstat(full)
		if e != nil {
			return "", e
		}
		h.Write([]byte(path + "\x00"))
		if info.Mode()&os.ModeSymlink != 0 {
			link, e := os.Readlink(full)
			if e != nil {
				return "", e
			}
			h.Write([]byte(link))
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		total += info.Size()
		if total > 64<<20 {
			return "", fmt.Errorf("untracked review inputs exceed 64 MB; narrow the worktree before review")
		}
		f, e := os.Open(full)
		if e != nil {
			return "", e
		}
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return "", e
		}
	}
	return "tree:" + hex.EncodeToString(h.Sum(nil)), nil
}

// PUT /api/teams/profiles saves per-role choices as the project's team profile.
// Body: {"<runtime>":{"<role>":{"model":"...","effort":"...","complex_effort":"..."}}}.
// Running teams keep their frozen profiles; new teams read the saved layer
// through the preflight.
func (s *Server) handlePutTeamProfiles(w http.ResponseWriter, r *http.Request) {
	_, root, err := s.teamProject()
	if err != nil {
		s.teamError(w, err)
		return
	}
	var body map[string]map[string]config.ManagedTeamProfile
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid profiles body, expected {runtime:{role:{model,effort,complex_effort}}}: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := s.validateTeamProfiles(ctx, root, body); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := saveManagedTeamProfiles(root, body, func(saved map[string]map[string]config.ManagedTeamProfile) {
		if s.cfg != nil {
			s.cfg.ManagedTeam = &config.ManagedTeamConfig{Profiles: saved}
		}
	})
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "save project team profile: "+err.Error())
		return
	}
	json200(w, map[string]any{"profiles": saved})
}

// validateTeamProfiles checks runtime, role, model and effort; every runtime is
// preflighted once so the model and effort are confirmed by the real catalog.
func (s *Server) validateTeamProfiles(ctx context.Context, root string, body map[string]map[string]config.ManagedTeamProfile) error {
	if len(body) == 0 {
		return fmt.Errorf("no profiles to save")
	}
	runtimes := make([]string, 0, len(body))
	for runtime := range body {
		runtimes = append(runtimes, runtime)
	}
	sort.Strings(runtimes)
	for _, runtime := range runtimes {
		roles := body[runtime]
		if !containsString(teamRuntimeNames, runtime) {
			return fmt.Errorf("unknown runtime %q (allowed: %s)", runtime, strings.Join(teamRuntimeNames, ", "))
		}
		if len(roles) == 0 {
			return fmt.Errorf("runtime %s has no roles to save", runtime)
		}
		adapter := s.teamAdapters[runtime]
		if adapter == nil {
			return fmt.Errorf("%s runtime adapter is unavailable", runtime)
		}
		availability := adapter.Preflight(ctx, root)
		names := make([]string, 0, len(roles))
		for role := range roles {
			names = append(names, role)
		}
		sort.Strings(names)
		for _, role := range names {
			if _, ok := teamRoleAssets[role]; !ok {
				return fmt.Errorf("unknown role %q for %s", role, runtime)
			}
			p := roles[role]
			if strings.TrimSpace(p.Effort) == "" {
				return fmt.Errorf("%s/%s: effort is required", runtime, role)
			}
			if err := teams.ValidateProfile(availability, teams.Profile{Runtime: runtime, Model: p.Model, Effort: p.Effort}); err != nil {
				return fmt.Errorf("%s/%s: %w", runtime, role, err)
			}
			if p.ComplexEffort != "" {
				if err := teams.ValidateProfile(availability, teams.Profile{Runtime: runtime, Model: p.Model, Effort: p.ComplexEffort}); err != nil {
					return fmt.Errorf("%s/%s complex effort: %w", runtime, role, err)
				}
			}
		}
	}
	return nil
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// saveManagedTeamProfiles merges profiles into managed_team.profiles of
// <root>/.stratus.json and leaves every other key untouched. It returns the
// complete saved profile map. The whole read, merge, write and onSaved
// sequence runs under the config write lock, so it is serialized against
// Config.Save: a Save either lands before it (and is read back here) or after
// it (and marshals the ManagedTeam that onSaved just set). onSaved updates the
// in-memory config, which therefore changes together with the file.
func saveManagedTeamProfiles(root string, update map[string]map[string]config.ManagedTeamProfile, onSaved func(map[string]map[string]config.ManagedTeamProfile)) (map[string]map[string]config.ManagedTeamProfile, error) {
	var saved map[string]map[string]config.ManagedTeamProfile
	err := config.WithWriteLock(func() error {
		var err error
		saved, err = mergeManagedTeamProfiles(root, update)
		if err != nil {
			return err
		}
		if onSaved != nil {
			onSaved(saved)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// mergeManagedTeamProfiles does the file work of saveManagedTeamProfiles; the
// caller holds the config write lock.
func mergeManagedTeamProfiles(root string, update map[string]map[string]config.ManagedTeamProfile) (map[string]map[string]config.ManagedTeamProfile, error) {
	path := filepath.Join(root, ".stratus.json")
	file := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &file); err != nil {
			return nil, fmt.Errorf("parse .stratus.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read .stratus.json: %w", err)
	}
	section := map[string]json.RawMessage{}
	if raw, ok := file["managed_team"]; ok {
		if err := json.Unmarshal(raw, &section); err != nil {
			return nil, fmt.Errorf("parse managed_team: %w", err)
		}
	}
	saved := map[string]map[string]config.ManagedTeamProfile{}
	if raw, ok := section["profiles"]; ok {
		if err := json.Unmarshal(raw, &saved); err != nil {
			return nil, fmt.Errorf("parse managed_team.profiles: %w", err)
		}
	}
	for runtime, roles := range update {
		if saved[runtime] == nil {
			saved[runtime] = map[string]config.ManagedTeamProfile{}
		}
		for role, p := range roles {
			saved[runtime][role] = p
		}
	}
	var err error
	if section["profiles"], err = json.Marshal(saved); err != nil {
		return nil, fmt.Errorf("encode managed_team.profiles: %w", err)
	}
	if file["managed_team"], err = json.Marshal(section); err != nil {
		return nil, fmt.Errorf("encode managed_team: %w", err)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode .stratus.json: %w", err)
	}
	if err := config.WriteFileAtomic(path, data); err != nil {
		return nil, fmt.Errorf("write .stratus.json: %w", err)
	}
	return saved, nil
}
