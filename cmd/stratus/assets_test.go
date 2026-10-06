package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/agents"
)

// Skills an agent preloads only help when the agent has the tools they call; any of
// the listed tools will do.
var skillNeeds = map[string][]string{
	"governance-db": {"mcp__stratus", "mcp__stratus__retrieve"}, // the stratus MCP retrieve tool
	"run-tests":     {"Bash"},
	"vexor-cli":     {"Bash"},
}

func TestEmbeddedAgents_PreloadUsableSkills(t *testing.T) {
	files, err := filepath.Glob("agents/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("no embedded agents found: %v", err)
	}
	for _, f := range files {
		agent, err := agents.ParseAgentFile(f)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, skill := range agent.Skills {
			if _, err := os.Stat(filepath.Join("skills", skill, "SKILL.md")); err != nil {
				t.Errorf("%s preloads skill %q, which is not embedded", agent.Name, skill)
			}
			if tools, ok := skillNeeds[skill]; ok && !slices.ContainsFunc(tools, func(tool string) bool { return slices.Contains(agent.Tools, tool) }) {
				t.Errorf("%s preloads %q but its tools lack any of %v: %v", agent.Name, skill, tools, agent.Tools)
			}
		}
	}
}

// Reviewers, the debugger and the strategic architect get the Stratus MCP tools that read,
// never the whole server, which could move phases or register workflows; the first three
// write nothing at all. delivery-system-architect keeps the server: swarm routes ADR tickets
// to it as a worker. Architects' Write/Edit are held to docs/ by the phase guard.
func TestEmbeddedAgents_ReadOnlyAgentsGetReadOnlyStratusTools(t *testing.T) {
	for _, name := range []string{"delivery-code-reviewer", "delivery-governance-checker", "delivery-debugger", "delivery-strategic-architect"} {
		agent, err := agents.ParseAgentFile(filepath.Join("agents", name+".md"))
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, tool := range agent.Tools {
			writes := tool == "Edit" || tool == "Write"
			if tool == "mcp__stratus" || (writes && name != "delivery-strategic-architect") {
				t.Errorf("%s is read-only but has %s: %v", name, tool, agent.Tools)
			}
		}
		if !slices.Contains(agent.Tools, "mcp__stratus__retrieve") {
			t.Errorf("%s lacks mcp__stratus__retrieve: %v", name, agent.Tools)
		}
	}
}

// Each coordinator offers the same Autopilot /goal the stratus-hud pane fills in, and the
// coordinator it falls back to must ship with Stratus.
func TestCoordinators_OfferAutopilotGoal(t *testing.T) {
	goalLine := regexp.MustCompile("(?m)^/goal Stratus workflow .*$")
	fallback := regexp.MustCompile(`\.claude/skills/([a-z0-9-]+)/SKILL\.md`)
	for skill, want := range map[string]string{"spec": "resume", "spec-complex": "spec-complex", "bug": "resume", "e2e": "e2e", "swarm": "swarm"} {
		data, err := os.ReadFile(filepath.Join("skills", skill, "SKILL.md"))
		if err != nil {
			t.Fatalf("read %s: %v", skill, err)
		}
		goal := goalLine.FindString(string(data))
		if goal == "" {
			t.Errorf("%s offers no Autopilot /goal", skill)
			continue
		}
		for _, part := range []string{"mcp__stratus__get_workflow output for", `shows phase "complete"`, "AskUserQuestion", "Stop after 40 turns."} {
			if !strings.Contains(goal, part) {
				t.Errorf("%s goal lacks %q", skill, part)
			}
		}
		if len(goal)-len("/goal ") > 4000 {
			t.Errorf("%s goal is %d characters; /goal takes up to 4000", skill, len(goal)-len("/goal "))
		}
		m := fallback.FindStringSubmatch(goal)
		if m == nil || m[1] != want {
			t.Errorf("%s goal falls back to %v, want the %s skill", skill, m, want)
			continue
		}
		if _, err := os.Stat(filepath.Join("skills", m[1], "SKILL.md")); err != nil {
			t.Errorf("%s goal names %s, which is not embedded", skill, m[1])
		}
	}
}
