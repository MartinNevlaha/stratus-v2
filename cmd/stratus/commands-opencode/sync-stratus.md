---
description: "Audit the Stratus installation health — checks agents, skills, rules, MCP, plugin, and config."
---

# /sync-stratus — Health Check & Integration

You are an **auditor and integrator**. Work in two phases:

1. **Audit** (read-only): scan the project, build a complete report
2. **Apply** (interactive): show the report, then ask the user which recommendations to apply

---

## Phase 1: Audit

### Step 1: Config & MCP

Check for `.stratus.json`:
- Present? Port configured?

Check for `opencode.json`:
- Present? `stratus mcp-serve` registered under `mcp.stratus`?
- Plugin `stratus` registered under `plugins.stratus`?

### Step 2: Skills Audit

Enumerate every `SKILL.md` in `.claude/skills/`.

Expected coordinator skills (installed by `stratus init`):
- `spec`, `spec-complex`, `bug`, `learn`

Detect:
- **Missing coordinator skill**
- **Skill with incorrect context** — coordinator skills must NOT have `context: fork`

For each user-added skill (not in the baseline list):
- Read its `SKILL.md` — record `name`, `description`, `context`
- Check if any agent in `.opencode/agents/` references it
- If no agent references it → **Integration Gap**

### Step 3: Agents Audit

Enumerate every `.md` file in `.opencode/agents/`.

Expected delivery agents:
- `delivery-implementation-expert`, `delivery-backend-engineer`, `delivery-frontend-engineer`
- `delivery-ux-designer`, `delivery-database-engineer`, `delivery-devops-engineer`
- `delivery-mobile-engineer`, `delivery-system-architect`, `delivery-strategic-architect`
- `delivery-qa-engineer`, `delivery-code-reviewer`, `delivery-governance-checker`, `delivery-debugger`

Detect:
- **Missing expected agent**
- **Naming conflict** — two agents with the same `name`
- **Reviewer with write permissions** — review agents should have `permission: {edit: deny, write: deny}`

### Step 4: Plugin Audit

Check `.opencode/plugin/stratus.ts`:
- Present?
- Contains phase_guard (before hook)?
- Contains watcher (after hook)?

### Step 5: Rules Audit

Enumerate `.claude/rules/`:

Expected rules:
- `review-verdict-format`, `tdd-requirements`, `error-handling`

Detect missing rules.

### Step 6: Tech Stack Detection

| Signal | Detected | Relevant Agents |
|--------|----------|-----------------|
| `package.json` | Node/JS | `delivery-frontend-engineer` |
| `package.json` with `expo`/`react-native` | React Native | `delivery-mobile-engineer` |
| `go.mod` | Go | `delivery-backend-engineer` |
| `requirements.txt`/`pyproject.toml` | Python | `delivery-backend-engineer` |
| `**/migrations/` or `*.sql` | Database | `delivery-database-engineer` |
| `.github/workflows/` or `Dockerfile` | CI/CD | `delivery-devops-engineer` |

Report detected stack, agent coverage, and irrelevant agents.

### Step 7: Build Report

```
## /sync-stratus Report

### Environment
[opencode.json present?, MCP registered?, plugin present?]

### Issues
#### CRITICAL
[file — description — impact]
#### MAJOR
[file — description]
#### MINOR
[file — description]

### Tech Stack
[Detected: X + Y + Z]
[Agent coverage: ✓/✗ per domain]

### Integration Recommendations
[For each gap: skill → suggested agent]

### Summary
[X critical, Y major, Z minor]
[Overall: HEALTHY | DEGRADED | BROKEN]
```

---

## Phase 2: Apply

Show the report. If there are integration recommendations, ask the user which to apply using the `question` tool.

For each confirmed skill integration patch:
1. Read the target agent file (`.opencode/agents/<agent>.md`)
2. Append a bullet under the `## Skills` section
3. Line: `- Use the \`<skill-name>\` skill for <purpose>.`

After applying:
```
Applied N change(s):
  ✓ <agent>.md — added `<skill>` to Skills

Run /sync-stratus again to verify.
```

**Never touch:** coordinator skill files, `.stratus.json`, `opencode.json`, plugin files.
