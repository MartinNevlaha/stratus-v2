---
name: sync-stratus
description: "Health check for stratus installation. Audits agents, skills, rules, hooks, and config. Produces a report only — no files modified."
context: fork
---

# /sync-stratus — Installation Health Check

You are a **read-only auditor**. Scan the current project's stratus installation and produce a structured health report.

**This skill produces a report only. It does NOT modify any files.**

---

## Step 1: Config & Hooks

Check for `.stratus.json`:
- Present? Port configured?

Check for `.mcp.json`:
- Present? `stratus mcp-serve` registered?

Check `.claude/settings.json` for hooks:
- Is `stratus hook phase_guard` registered under `PreToolUse`?
- Is `stratus hook delegation_guard` registered under `PreToolUse`?

---

## Step 2: Skills Audit

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

## Step 3: Agents Audit

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

## Step 4: Rules Audit

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

## Step 5: CLAUDE.md Audit

Scan ALL CLAUDE.md files:
- `~/.claude/CLAUDE.md` — global level (highest precedence, applies to all projects)
- `<project>/CLAUDE.md` — project level (applies to all contributors)
- `<project>/**/CLAUDE.md` — subdirectory level (scoped to that subtree)

Use Glob pattern `**/CLAUDE.md` from the project root to find all instances.

For each file found:
- Record its path and precedence level (global / project / scoped)
- Does it instruct Claude to write code directly (bypassing Task delegation)?
- Does it override or disable hook enforcement (e.g. "ignore hooks", "always implement directly")?
- Does it conflict with coordinator rules (`/spec`, `/bug` must not write production code)?
- Is the content a stub or outdated (empty body, references non-existent agents/skills)?

Note: if `delegation_guard` hook is active, direct-write instructions in CLAUDE.md are mechanically blocked — downgrade severity from CRITICAL/MAJOR to MINOR (confusing but not dangerous).

---

## Step 6: Conflict Classification

Classify each issue:

- **CRITICAL** — breaks delegation enforcement (coordinator has `context: fork`, reviewer/debugger has write access, missing hooks)
- **MAJOR** — inconsistent routing, agent overlap, coordinator bypasses Task tool
- **MINOR** — missing optional files, naming issues, stub content, CLAUDE.md conflicts blocked by hooks

---

## Step 7: Output

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

### 2. Summary
[X critical, Y major, Z minor issues]
[Overall: HEALTHY | DEGRADED | BROKEN]

### 3. Recommended Fixes
[ordered list — lowest risk first]
[If agents/skills/rules are missing: run `stratus init` to reinstall]
```

---

## Rules

- Do NOT modify any files
- Do NOT run any commands (no Bash)
- Use Read, Glob, Grep only
