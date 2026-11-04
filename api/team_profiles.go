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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
)

var teamRoleAssets = map[string]string{"lead": "", "implementation": "delivery-implementation-expert", "design": "delivery-system-architect", "review": "delivery-code-reviewer", "diagnosis": "delivery-debugger", "mechanical": ""}

// Team defaults are local launch templates, never changes to global CLI settings.
func teamProfiles(root string) (map[string]map[string]teams.Profile, []string) {
	out := map[string]map[string]teams.Profile{}
	warnings := []string{}
	for _, runtime := range []string{"codex", "claude-code", "opencode"} {
		out[runtime] = map[string]teams.Profile{}
		for role := range teamRoleAssets {
			out[runtime][role] = teams.DefaultProfile(runtime, role, "", "")
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
		}
		if v := fields["effort"]; v != "" {
			profile.Effort = v
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
					}
					if v.Variant != "" {
						p.Effort = v.Variant
					}
					out["opencode"][role] = p
				}
			}
		}
	}
	// Explicit per-project team choices override native role defaults.
	var cfg struct {
		ManagedTeam struct {
			Profiles map[string]map[string]teams.Profile `json:"profiles"`
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
					}
					if p.Effort != "" {
						base.Effort = p.Effort
					}
					out[runtime][role] = base
				}
			}
		}
	}
	return out, warnings
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
