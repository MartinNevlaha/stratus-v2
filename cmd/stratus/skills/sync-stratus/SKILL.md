---
name: sync-stratus
description: "Stratus health check + integration. Audits agents, skills, rules, hooks, config, discovers custom skills, detects tech stack, mines conventions — then offers to apply integration patches."
---

# /sync-stratus — Health Check & Integration

You are an **auditor and integrator**. Work in two phases:

1. **Audit** (read-only): scan the project, build a complete report
2. **Apply** (interactive): show the report, then ask the user which integration recommendations to apply

---

## Phase 1: Audit

### Step 1: Config & Hooks

Check for `.stratus.json`:
- Present? Port configured?

Check for `.mcp.json`:
- Present? `stratus mcp-serve` registered?

Check `.claude/settings.json` for hooks:
- Is `stratus hook phase_guard` registered under `PreToolUse`?
- Is `stratus hook delegation_guard` registered under `PreToolUse`?

---

### Step 2: Skills Audit

Enumerate every `SKILL.md` in `.claude/skills/`.

Expected coordinator skills (installed by `stratus init`):
- `spec` — must NOT have `context: fork`
- `bug` — must NOT have `context: fork`
- `learn` — must NOT have `context: fork`

Detect:
- **Missing coordinator skill** — one of the 3 expected skills is absent
- **Coordinator with `context: fork`** — CRITICAL: coordinator skills must run in the main context to use the Task tool for spawning delivery agents; subagents cannot spawn subagents
- **Skill that bypasses Task tool** — body instructs the coordinator to write code directly instead of delegating

For each user-added skill (anything beyond the 3 expected):
- Does it use `context: fork` appropriately (research/read-only)? Fine.
- Does it claim to orchestrate implementation without Task delegation? Flag it.

---

### Step 2.5: Custom Skills Discovery

**Stratus-owned skills** (baseline — these are installed by `stratus init`):
`spec`, `spec-complex`, `bug`, `learn`, `sync-stratus`, `vexor-cli`, `governance-db`, `run-tests`, `code-review`, `find-bugs`, `security-review`, `create-architecture`, `explain-architecture`, `frontend-design`, `react-native-best-practices`

For each skill in `.claude/skills/` that is NOT in the above list:
1. Read its `SKILL.md` — record `name`, `description`, `context` field
2. Classify:
   - `coordinator-compatible` — no `context: fork` (can use Task tool, can orchestrate)
   - `utility` — has `context: fork` (read-only, research, context-isolated)
3. Search all `.claude/agents/*.md` files for any reference to this skill's name
4. If no agent references it → **Integration Gap**
   - MAJOR if `coordinator-compatible` (unlisted coordinator skill is invisible to routing)
   - MINOR if `utility` (tool available but no agent knows to use it)

For each integration gap, determine the best-fit agent(s) by matching the skill's domain to agent responsibilities:
- Code generation, API, backend logic → `delivery-backend-engineer`
- UI, components, frontend → `delivery-frontend-engineer`, `delivery-ux-designer`
- Database, migrations, queries → `delivery-database-engineer`
- Deployment, CI/CD, infra → `delivery-devops-engineer`
- Mobile (React Native / Expo) → `delivery-mobile-engineer`
- Testing, coverage → `delivery-qa-engineer`
- Research, docs, analysis → multiple agents may benefit

---

### Step 3: Agents Audit

Enumerate every `.md` file in `.claude/agents/`.

Expected delivery agents (installed by `stratus init`):
- `delivery-implementation-expert`
- `delivery-backend-engineer`
- `delivery-frontend-engineer`
- `delivery-database-engineer`
- `delivery-devops-engineer`
- `delivery-qa-engineer`
- `delivery-code-reviewer`
- `delivery-debugger`

Detect:
- **Missing expected agent** — one of the 8 is absent; `stratus init` may not have been run
- **Naming conflict** — two agents with the same `name` in frontmatter
- **Responsibility overlap** — two agents claiming the same domain (implementation, QA, review, etc.)
- **Reviewer with write tools** — `delivery-code-reviewer` or `delivery-debugger` must NOT have Edit/Write; flag any reviewer/debugger agent with write access
- **User agent without frontmatter** — missing `name` or `description` reduces hook enforcement fidelity

---

### Step 3.5: Tech Stack Detection

From the project root, check for the following files and directories:

| Signal | Detected | Relevant Agents |
|--------|----------|-----------------|
| `package.json` | Node/JS project | `delivery-frontend-engineer` |
| `package.json` with `expo` or `react-native` dep | React Native app | `delivery-mobile-engineer` |
| `go.mod` | Go backend | `delivery-backend-engineer` |
| `requirements.txt` or `pyproject.toml` | Python | `delivery-backend-engineer` |
| `Cargo.toml` | Rust | `delivery-backend-engineer` |
| `**/migrations/` directory or `*.sql` files | Database migrations | `delivery-database-engineer` |
| `.github/workflows/` or `Dockerfile` | CI/CD / containerized | `delivery-devops-engineer` |

For test framework detection, read `package.json` scripts and devDependencies for: `jest`, `vitest`, `playwright`, `cypress`. Also check for `pytest` in requirements, `go test` patterns in Makefile.

Report:
- Detected stack (e.g., "Go + Svelte + SQLite")
- Agent coverage: for each relevant agent, ✓ (installed) or ✗ (missing)
- `run-tests` skill compatibility: confirm detected test runner is supported
- Agents that are installed but appear **irrelevant** (e.g., `delivery-mobile-engineer` when no React Native is detected) — flag as INFO, not an error

---

### Step 4: Rules Audit

Enumerate every `.md` file in `.claude/rules/` and `~/.claude/rules/` (global, if accessible).

Expected rules (installed by `stratus init`):
- `review-verdict-format`
- `tdd-requirements`
- `error-handling`

Detect:
- **Missing expected rule**
- **Rule that overrides delegation** — instructs the main Claude instance to write/fix code directly
- **Global rule conflict** — `~/.claude/rules/` or `~/.claude/CLAUDE.md` with instructions like "always fix errors immediately" or "write code as requested" that bypass the coordinator model

---

### Step 5: CLAUDE.md Audit + Convention Mining

Scan ALL CLAUDE.md files:
- `~/.claude/CLAUDE.md` — global level (highest precedence, applies to all projects)
- `<project>/CLAUDE.md` — project level (applies to all contributors)
- `<project>/**/CLAUDE.md` — subdirectory level (scoped to that subtree)

Use Glob pattern `**/CLAUDE.md` from the project root to find all instances.

**Conflict detection** (existing behavior):
- Does it instruct Claude to write code directly (bypassing Task delegation)?
- Does it override or disable hook enforcement?
- Does it conflict with coordinator rules (`/spec`, `/bug` must not write production code)?
- Is the content a stub or outdated?

Note: if `delegation_guard` hook is active, direct-write instructions are mechanically blocked — downgrade severity from CRITICAL/MAJOR to MINOR.

**Convention mining** (new):
For each CLAUDE.md, also scan for lines that express coding conventions — standards, naming rules, formatting requirements, structural patterns. For each convention found:
1. Check if an equivalent governance rule already exists in `.claude/rules/`
2. If no matching rule → **Convention Gap** (MINOR): record the line and suggest a rule filename

Examples of convention signals to look for:
- "always use X pattern"
- "never do Y"
- "all Z must have..."
- "file naming: ..."
- "API responses must include..."
- "errors should be..."

---

### Step 6: Conflict Classification

Classify each issue:

- **CRITICAL** — breaks delegation enforcement (coordinator has `context: fork`, reviewer/debugger has write access, missing hooks)
- **MAJOR** — inconsistent routing, agent overlap, coordinator bypasses Task tool, coordinator-compatible skill not referenced
- **MINOR** — missing optional files, naming issues, stub content, CLAUDE.md conflicts blocked by hooks, unintegrated utility skills, convention gaps

---

### Step 7: Build the Report

Assemble the complete report (do not show it yet — show it at the start of Phase 2):

```
## /sync-stratus Report

### Environment
[.stratus.json present?, hooks registered?, MCP registered?]

### 1. Issues

#### CRITICAL
[file path — description — impact]

#### MAJOR
[file path — description]

#### MINOR
[file path — description]

### 2. Custom Skills Discovered
[X custom skills found (list names + classification)]
[Y integration gaps (skill → suggested agent(s))]
[none if all custom skills are referenced]

### 3. Tech Stack
[Detected: X + Y + Z]
[Agent coverage: backend ✓/✗, frontend ✓/✗, DB ✓/✗, mobile ✓/✗ (relevant?), devops ✓/✗]
[run-tests compatibility: ✓/✗]

### 4. Integration Recommendations
[For each gap, show:]
[  `skill-name` (utility/coordinator-compatible) → suggested agent(s)]
[  Action: add to <agent>.md > Skills section]
[  Suggested line: "Use the `skill-name` skill for <inferred purpose>."]

[For each convention gap, show:]
[  CLAUDE.md:<line> — "<quoted text>"]
[  Action: create `.claude/rules/<suggested-name>.md`]

### 5. Summary
[X critical, Y major, Z minor issues]
[X integration patches pending, Y governance rules to create]
[Overall: HEALTHY | DEGRADED | BROKEN]
```

---

## Phase 2: Apply

Show the report. Then, if there are any integration recommendations:

```
Found N integration recommendation(s):
  1. Add `<skill>` → <agent> Skills section
  2. Add `<skill>` → <agent> Skills section
  ...
  N. Create rule from CLAUDE.md:<line>

Apply all? Type 'y' to apply all, 'n' to skip, or list numbers to apply selectively (e.g. '1 3').
```

Wait for user response. Then apply only the confirmed items:

**For each skill integration patch:**
1. Read the target agent file (`.claude/agents/<agent>.md`)
2. Locate the `## Skills` section
   - If the section exists: append a new bullet under it
   - If the section does not exist: insert it after `## Tools` (or after `## Workflow` if Tools is also absent)
3. The line to add: `- Use the \`<skill-name>\` skill for <inferred purpose based on skill description>.`
4. Edit the file

**For each governance rule suggestion:**
1. Generate a concise governance rule document from the CLAUDE.md convention text
2. Write it to `.claude/rules/<suggested-name>.md`
3. Format: a short `# Title` heading, then the rule body in plain language with examples if useful

**After applying all confirmed patches:**

```
Applied N change(s):
  ✓ <agent>.md — added `<skill>` to Skills
  ✓ <agent>.md — added `<skill>` to Skills
  ✓ .claude/rules/<name>.md — created governance rule

Run /sync-stratus again to verify the updated installation.
```

**Never touch:**
- Coordinator skill files (`spec`, `bug`, `learn`, `spec-complex`)
- `.stratus.json`, `.mcp.json`, `.claude/settings.json` (hooks)
- Any file the user said 'n' or did not confirm

---

## Tools

Read, Glob, Grep, Edit, Write
