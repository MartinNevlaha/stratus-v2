// Package teamruntime owns native agent processes. It never attaches a process
// belonging to a user's terminal or desktop, and never retries submitted work.
package teamruntime

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"time"

	"github.com/MartinNevlaha/stratus-v2/teams"
)

type nativeAdapter struct {
	runtime string
	binary  string
	prefix  []string
}

func Adapters() map[string]teams.Adapter {
	return map[string]teams.Adapter{
		"codex":       &nativeAdapter{runtime: "codex", binary: "codex"},
		"claude-code": &nativeAdapter{runtime: "claude-code", binary: "claude"},
		"opencode":    &nativeAdapter{runtime: "opencode", binary: "opencode"},
	}
}

func (a *nativeAdapter) command(root string, args ...string) *exec.Cmd {
	path := a.binary
	if resolved, err := resolveBinary(a.runtime, a.binary); err == nil {
		path = resolved
	}
	cmd := exec.Command(path, append(append([]string{}, a.prefix...), args...)...)
	cmd.Dir = root
	return cmd
}

func (a *nativeAdapter) Preflight(ctx context.Context, root string) teams.Availability {
	ctx, cancel := bounded(ctx)
	defer cancel()
	out := teams.Availability{Runtime: a.runtime, Models: []teams.Model{}}
	if runtime.GOOS == "windows" {
		out.Reason = "Managed native sessions require owned child-process cleanup; Windows support is not implemented."
		return out
	}
	if _, err := resolveBinary(a.runtime, a.binary); err != nil {
		out.Reason = err.Error()
		return out
	}
	if a.runtime == "opencode" {
		s, err := a.openOpenCode(ctx, teams.SessionSpec{ProjectRoot: root, Profile: teams.Profile{Runtime: a.runtime}}, nil, true)
		if err != nil {
			out.Reason = err.Error()
			return out
		}
		defer s.Close()
		out.Available = true
		out.Models = s.models
		out.Reason = "Native server and connected provider model catalog verified; no model turn was run."
		return out
	}
	s, err := a.openStream(ctx, teams.SessionSpec{ProjectRoot: root, Profile: teams.Profile{Runtime: a.runtime}}, nil, true)
	if err != nil {
		out.Reason = err.Error()
		return out
	}
	defer s.Close()
	out.Available = true
	out.Models = s.models
	out.Reason = "Native handshake and model catalog verified; no model turn was run."
	return out
}

func (a *nativeAdapter) Open(ctx context.Context, spec teams.SessionSpec, emit func(teams.Event)) (teams.Session, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("managed native sessions are unavailable on Windows until owned child-process cleanup is supported")
	}
	if err := validateSpec(spec, a.runtime); err != nil {
		return nil, err
	}
	ctx, cancel := bounded(ctx)
	defer cancel()
	if a.runtime == "opencode" {
		s, err := a.openOpenCode(ctx, spec, emit, false)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	s, err := a.openStream(ctx, spec, emit, false)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func validateSpec(spec teams.SessionSpec, runtime string) error {
	if !filepath.IsAbs(spec.ProjectRoot) {
		return fmt.Errorf("project root must be absolute")
	}
	if spec.Profile.Runtime != runtime || spec.Profile.Model == "" {
		return fmt.Errorf("an explicit %s model is required", runtime)
	}
	return nil
}

func validateModel(models []teams.Model, p teams.Profile) error {
	for _, m := range models {
		if m.ID == p.Model {
			if p.Effort != "" && !slices.Contains(m.Efforts, p.Effort) {
				return fmt.Errorf("model %s does not advertise effort %q", p.Model, p.Effort)
			}
			return nil
		}
	}
	return fmt.Errorf("model %q is not advertised by the authenticated runtime; no fallback was used", p.Model)
}

func sortModels(ms []teams.Model) { sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID }) }
func bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 30*time.Second)
}
