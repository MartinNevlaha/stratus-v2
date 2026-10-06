# Plan: Ship Stratus' Claude Code Integration as a Plugin

**Status:** implemented 2026-10-06, all three phases; see §0 for where the implementation departs from this plan
**Date:** 2026-10-06
**Verified against:** Claude Code 2.1.291 (docs fetched 2026-10-06, experiments run the same day)
**Karpathy principles:** P1 (Think Before Coding: options and decision points below), P2 (Simplicity First), P3 (Surgical Changes)

---

## 0. Implementation notes (2026-10-06)

Phases 1 and 2 are implemented. Phase 3 was implemented, then reverted. The plugins are skills-directory plugins, not a directory marketplace.

**Skills-directory plugins, not a directory marketplace.** This replaces D5.
- **The problem:** Claude Code keeps marketplace names in one registry for the whole machine (`~/.claude/plugins/known_marketplaces.json`).
  - With a `stratus` marketplace in every project, the registry points at whichever project registered last.
  - An untrusted project's `enabledPlugins` then loaded another project's plugins. Observed: a temp project loaded `stratus-hud` from this repository.
- **The fix:** `init` writes `.claude/skills/stratus/`, `.claude/skills/mdview/` and `.claude/skills/stratus-hud/` instead. Claude Code loads each as `<plugin>@skills-dir`:
  - only from the project's own `.claude/skills`,
  - only after workspace trust (the same gate as project settings hooks),
  - with no registration in `settings.json`.
- **Migration:** `refresh` removes the interim `.claude/stratus-mods` marketplace and its settings entries.

**Phase 1 (hooks):** the 8 command hooks are in `.claude/skills/stratus/hooks/hooks.json`. `writeHooks` removes every `stratus hook *` entry from `settings.json` once that file exists, keeping users' own hooks.

**Phase 3 (skills and agents in the plugin): reverted.** Skills and agents stay project files (`.claude/skills/<name>/`, `.claude/agents/`) with bare names, as before. Three reasons, each verified:
- A plugin skill cannot shadow Claude Code's own commands, so `/bug`, `/resume`, `/code-review` and `/security-review` ran the built-ins instead of Stratus' coordinators. A project skill does shadow them.
- A plugin's agents are named `stratus:<agent>`. A skill's bare `agent:` reference then falls back to the general-purpose agent, which put a scoped-name rewrite into every agent reference and every hook. A project agent's bare name works, also from a plugin skill.
- OpenCode reads `.claude/skills/**/SKILL.md` recursively, so it would load the plugin's nested skills next to any bare copy, and which duplicate wins is not defined.

**Phase 2 (evals):**
- `evals/stratus/` holds three MCP-free cases: `code-review-verdict`, `find-bugs-root-cause` and `create-architecture-adr`.
- `make eval-plugin ARGS=...` (`scripts/plugin-eval.sh`) builds the binary and writes a project as `refresh` does. It then wraps the skills and agents in a plugin around the stratus hooks plugin, since `claude plugin eval` takes a plugin; inside it the agent references are scoped to `stratus:delivery-*`. Finally it runs `claude plugin eval`.
- Scaffolds pin `.stratus.json` to an unreachable port, so hooks never reach another project's Stratus server; eval runs only pass `EVAL_*` variables.
- First results (1 run per case, `--ablation none`, $0.6 in total, before the revert):
  - `create-architecture-adr` 1.00 and `find-bugs-root-cause` 1.00 with `--model haiku`.
  - `code-review-verdict` 1.00 over 2 runs with the default model. Haiku only triggers that skill by itself some of the time.

## 1. Goal

`stratus init` / `refresh` integrate Stratus with Claude Code by copying files into `.claude/` and merging
entries into `.claude/settings.json`. Claude Code plugins can carry skills, agents, hooks, MCP servers and
mods as one unit. Decide how much of Stratus to move into a plugin, and plan the first step.

Non-goals: changing the Go server, the MCP server, the workflow state machine, or the OpenCode integration's
behaviour.

## 2. What a Claude Code install writes today

| Asset | Where | How it is kept current |
|---|---|---|
| 22 skills | `.claude/skills/<name>/` | 3-way hash merge (`sync_state.asset_hashes`), customized files skipped |
| 14 agents | `.claude/agents/delivery-*.md` | same 3-way merge |
| 5 rules | `.claude/rules/*.md` | same 3-way merge |
| 8 command hooks | `.claude/settings.json` → `hooks` | `writeHooks` removes and re-adds every `stratus hook *` entry |
| statusLine, plugin registration | `.claude/settings.json` | written only if absent / non-destructively |
| MCP server `stratus` | `.mcp.json` | written only if absent (`init`) |
| mods `mdview`, `stratus-hud` | `.claude/stratus-mods/` (local directory marketplace `stratus`) | rewritten by `refresh`, enabled via `enabledPlugins` |

The settings merge has already drifted in practice: this repository's own `.claude/settings.json` kept
`Task`-only delegation matchers (so `workflow_existence_guard` and `delegation_guard` never fired once the
tool became `Agent`) and a dead `executor_routing_guard` entry until 2026-10-06.

## 3. Verified constraints

| # | Fact | Evidence |
|---|---|---|
| C1 | A plugin agent is named `<plugin>:<name>`; the Agent tool **rejects the bare name**. | Experiment: `subagent_type: "delivery-probe"` → `Agent type 'delivery-probe' not found. Available agents: … stratusprobe:delivery-probe` |
| C2 | Hooks see a plugin agent's `agent_type` as `<plugin>:<name>`, so `isDeliverySubagent` (prefix `delivery-`) and `docsOnlyAgents` would stop matching. | hooks.md, "For subagents shipped by a plugin, the agent type is the plugin-scoped identifier" |
| C3 | Tools of a plugin MCP server are named `mcp__plugin_<plugin>_<server>__<tool>` (here `mcp__plugin_stratus_stratus__retrieve`). Stratus references `mcp__stratus__*` 175 times (skills 119, agents 52, rules 4), plus the swarm `worker_instructions` and MCP `instructions`. | mcp.md "Plugin-provided MCP servers"; grep count |
| C4 | Plugin skills are `/stratus:<name>`; the bare `/<name>` still works unless another command owns it. | skills.md, command names |
| C5 | Plugin agents ignore `permissionMode`, `hooks`, `mcpServers`, `initialPrompt`. Stratus agents use none of them (they use `tools`, `model`, `effort`, `color`, `skills`). | plugins/components.md, agents |
| C6 | A plugin's `settings.json` applies only `agent` and `subagentStatusLine`, so `statusLine` and `env` must stay in project settings. | plugins/components.md, default settings |
| C7 | Plugins have no component for rules: `.claude/rules/` stays a file copy. | plugins/components.md component table |
| C8 | OpenCode discovers skills in `.opencode/skills/`, `.claude/skills/` and `.agents/skills/`. | opencode.ai/docs/skills |
| C9 | One `hooks/hooks.json` can hold command `hooks` and mod `modules`, but a mod's `$.state` keys belong to its plugin name: putting `stratus-hud`'s module into a plugin named `stratus` fails validation (3 errors) until its atoms are renamed. | Experiment with `claude plugin validate`; the 3 command hooks registered fine |
| C10 | A plugin from a project's `extraKnownMarketplaces` loads only after workspace trust (the same gate as project settings hooks). A relative `directory` source resolves against the main checkout, so worktrees share it. A directory-marketplace plugin is read from the folder itself: `refresh` + `/reload-plugins` updates it with no version bump. | plugins/org.md; plugin-authoring reference; mods shipped 2026-10-06 |

## 4. Options

| | A. Full plugin | B. Hooks plugin (recommended) | C. Status quo |
|---|---|---|---|
| In the plugin | skills, agents, hooks, mods (MCP stays in `.mcp.json`, see C3) | the 8 command hooks; mods stay separate plugins (C9) | mods only |
| Still copied | rules, statusLine | skills, agents, rules, statusLine | everything else |
| Renames | agent names among 102 `delivery-*` mentions in 14 skills → `stratus:delivery-*`; Go: `hooks/phase_guard.go`, `orchestration/coordinator.go`, `mcp/tools.go`, `mcp/server.go`, onboarding templates/proposals, swarm `agent_type` | none | none |
| Other work | OpenCode skills move to `.opencode/skills/` (C8); dashboard agent/skill editor and agent evolution write `.claude/agents`, which would no longer be the source; agent `skills:` preload of plugin skill names unverified; user customization changes from 3-way merge to project-level overrides | `writeHooks` stops writing hooks and removes old ones; one new plugin dir | none |
| Gain | one unit to enable/disable; namespaced names; no merges | no settings.json hook merges (the drift in §2 cannot recur); `/plugin disable stratus` turns all guards off and on cleanly; hook changes ship with `refresh` like mods | none |
| Risk | high: every delegation and guard depends on the renames being complete | low | settings drift continues |

Not recommended in any option: moving the MCP server into the plugin. C3 renames 175 references for no
functional gain; `.mcp.json` works unchanged.

## 5. Recommendation

1. **Phase 1: Option B now.** It is a small change that removes the settings-merge class of bugs.
2. **Phase 2: skill evals with `claude plugin eval`.** Optional; they do not need A, because a throwaway plugin wrapper around `cmd/stratus/skills` is enough for evals.
3. **Phase 3: Option A minus MCP.** Defer until there is a concrete need, such as name collisions with user skills or a request to install Stratus skills without the binary. §8 lists what it takes.

## 6. Phase 1 tasks (Option B)

### Task 1: core hooks plugin

**Agent:** `delivery-implementation-expert`, **LOC:** ~40 impl + ~60 tests

- `cmd/stratus/mods/stratus/.claude-plugin/plugin.json`: `name: stratus`, version = binary version at release.
- `cmd/stratus/mods/stratus/hooks/hooks.json`: the hooks `writeHooks` registers today:
  - PreToolUse: `phase_guard`, `workflow_existence_guard`, `delegation_guard`, `bash_write_guard`
  - PostToolUse: `watcher`
  - SessionStart: `session_start`
  - TeammateIdle: `teammate_idle`
  - TaskCompleted: `task_completed`
  - All with the same matchers and commands (`stratus hook <name>`, binary on PATH).
- `cmd/stratus/mods/.claude-plugin/marketplace.json`: add the `stratus` entry.
- Test: `hooks.json` and the `cmdHook` handler map list the same hook names, so a handler cannot ship unregistered or a command point at a missing handler.

### Task 2: settings migration

**Agent:** `delivery-implementation-expert`, **LOC:** ~30 impl + ~50 tests

- `writeHooks` no longer adds hook entries. It removes every `stratus hook *` command (the existing `removeStratusHook`), drops empty event arrays and an empty `hooks` object, and adds `enabledPlugins["stratus@stratus"] = true` non-destructively.
- statusLine, `extraKnownMarketplaces`, and the mods' `enabledPlugins` stay as today.
- Tests:
  - an old settings file loses all Stratus hooks but keeps a user's own hooks in the same event,
  - a fresh install has no `hooks` key,
  - `stratus@stratus` is enabled unless the user set `false`.

### Task 3: docs and summary

**Agent:** `delivery-implementation-expert`, **LOC:** ~20

- `printInitSummary`: hooks are listed as "Hooks registered by the `stratus` plugin".
- `README.md` hooks section, `CLAUDE.md` (where hooks are registered), and `rules/workflow-governance.md` (enforcement section): note that the guards come from the `stratus` plugin and stop when it is disabled.

### Verification

1. `go test ./...`; `claude plugin validate .claude/stratus-mods`.
2. A real session in this repo after `go install` and `refresh`: the debug log shows the command hooks registered from `stratus@stratus` and none from `settings.json`.
3. Re-run the end-to-end checks from 2026-10-06, now with plugin-registered hooks:
   - a blocked delivery subagent continues with the reason,
   - an architect is held to `docs/`,
   - `SessionStart` lists active workflows.
4. `refresh` on a project initialized by v0.16.4: its settings hooks are removed and the plugin takes over. In a project folder that is not yet trusted, the guards are absent until trust is accepted, the same as settings hooks today.

### Rollback

Disable `stratus@stratus` and run the previous binary's `refresh`, which re-adds the settings hooks. Both
mechanisms call the same `stratus hook <name>` handlers, so nothing else changes.

## 7. Phase 2 (optional): skill evals

`claude plugin eval` runs eval cases (`evals/<case>/prompt.md` plus graders such as `tool_used`, `tool_order`,
`file_exists`, `llm`) against a plugin and reports pass rates, optionally against a no-plugin baseline.

Candidate cases:
- `/spec` registers a workflow before the first delivery delegation,
- `/bug` transitions `analyze → fix` via `transition_phase`,
- `/code-review` ends with `Verdict: PASS|FAIL`.

Each case runs real sessions, so it costs tokens. Run it manually or nightly, not on every commit.

## 8. Phase 3 (reverted, see §0): skills and agents in the plugin

What it would take, beyond Phase 1:

- Rename agent references to `stratus:delivery-*` (C1): the agent names among 102 `delivery-*` mentions in 14 skills, the dispatch and agent names in `orchestration/coordinator.go`, `mcp/tools.go`, `mcp/server.go`, the onboarding templates and proposals, and the swarm worker `agent_type`.
- Teach `isDeliverySubagent` and `docsOnlyAgents` the `stratus:` prefix (C2), keeping bare names for OpenCode and user overrides.
- Write OpenCode's skills to `.opencode/skills/` instead of `.claude/skills/` (C8), so Claude Code does not load each skill twice.
- Decide what the dashboard agent/skill editor and agent evolution edit, since plugin files are overwritten by `refresh`. The likely answer is a project-level override copy.
- Verify that an agent's `skills:` list can preload plugin skills by bare name; if not, use `stratus:` names there too.
- Migration: remove unmodified copies by hash, keep customized ones with a warning (they become project-level overrides).

## 9. Decisions for Martin

1. **D1:** Approve Phase 1 (Option B)?
2. **D2:** Keep `mdview` and `stratus-hud` as separate plugins in the same marketplace (recommended, no change), or merge `stratus-hud` into `stratus` (requires renaming its state atoms, C9)?
3. **D3:** Phase 2 evals: yes or no, and the token budget per run?
4. **D4:** Phase 3: defer (recommended) or plan it now?
5. **D5:** Distribution: keep the local directory marketplace written by the binary (recommended: the hooks call the same binary, so plugin and binary stay in lockstep), or publish a GitHub marketplace from this repository (plugin updates without the binary, but version skew between hooks and handlers becomes possible)?
