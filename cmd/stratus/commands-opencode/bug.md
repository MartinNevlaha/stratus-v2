---
description: "Bug-fixing workflow coordinator (analyze → fix → review → complete). Orchestrates debugging and repair by delegating to specialized delivery agents via @mention."
---

# Bug-Fixing Workflow

You are the **coordinator** for a structured bug-fixing workflow. You orchestrate work by delegating to specialized delivery agents via `@agent-name`. You do NOT write production code directly.

## API Base

```bash
BASE=http://127.0.0.1:$(stratus port)
```

---

## Phase 1: Analyze

> 🎯 **Karpathy — Think Before Coding:** State assumptions explicitly, surface tradeoffs, push back on overcomplication, stop and ask when confused. See `.claude/rules/karpathy-principles.md`.

Start the bug workflow:

```bash
SLUG=$(echo "$ARGUMENTS" | tr '[:upper:] ' '[:lower:]-' | sed 's/[^a-z0-9-]//g' | cut -c1-50)
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d "{\"id\": \"bug-$SLUG\", \"type\": \"bug\", \"title\": \"$ARGUMENTS\"}"
```

### 1a. Explore & Diagnose

- Explore the codebase: Read error messages, stack traces, logs.
- **Delegate to `@delivery-debugger`** for root cause analysis.
- Record delegation:

```bash
curl -sS -X POST $BASE/api/workflows/bug-<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-debugger"}'
```

The debugger will return a structured diagnosis with symptom, root cause, classification, evidence, and recommended fix.

### 1b. Assess Severity — Intelligent Decision

Based on the debugger's diagnosis, **intelligently assess** the fix complexity:

**TRIVIAL (skip to fix):**
- Single file change, isolated scope
- No architecture or API changes
- No database migrations or data transformations
- No security implications
- No cross-service dependencies
- Fix is obvious and localized

**COMPLEX (plan first):**
- Multiple files or components affected
- Architecture or design changes required
- Database schema changes or data migrations
- Security vulnerabilities or auth changes
- Cross-service or cross-cutting concerns
- Risk of regressions in other areas
- Unclear fix approach or multiple options

### 1c. Plan (if COMPLEX)

If the bug is **COMPLEX**, delegate to the built-in `Plan` agent (Task tool, `subagent_type: "Plan"`):

Pass full context:
- The bug description from `$ARGUMENTS`
- Debugger's diagnosis and root cause
- Affected files and components
- Recommended fix approach

The Plan agent will return:
- Ordered fix steps
- Files to modify with changes needed
- Test coverage requirements
- Risk mitigation strategies

**Governance check (if COMPLEX) — delegate to `@delivery-governance-checker`:**

Ask the agent to review the fix plan against project governance:
- Does the proposed fix violate any architectural constraints?
- Are there security or data handling requirements to consider?
- Will this fix require updates to ADRs or documentation?

```bash
curl -sS -X POST $BASE/api/workflows/bug-<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-governance-checker"}'
```

If checker returns `[must_update]` findings → incorporate into the plan before user approval.

Present the plan to the user using the `question` tool. **Wait for explicit approval.**

### 1d. User Approval & Transition

**Present diagnosis to user using the `question` tool.**

- If TRIVIAL: Get approval to proceed directly to fix
- If COMPLEX: Get approval for the plan

On approval, transition to fix:

```bash
curl -sS -X PUT $BASE/api/workflows/bug-<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "fix"}'
```

---

## Phase 2: Fix

> 🎯 **Karpathy — Simplicity First + Surgical Changes:** Minimum code that solves the problem. Touch only what the task requires. No speculative abstractions, no "improvements" to adjacent code. See `.claude/rules/karpathy-principles.md`.

Route to the appropriate delivery agent via `@mention` based on the bug type:

| Bug Type | Agent |
|----------|-------|
| UI, components | `@delivery-frontend-engineer` |
| UI/UX design, design system | `@delivery-ux-designer` |
| API, backend, handlers | `@delivery-backend-engineer` |
| Migrations, queries | `@delivery-database-engineer` |
| CI/CD, infra | `@delivery-devops-engineer` |
| Mobile, React Native | `@delivery-mobile-engineer` |
| General | `@delivery-implementation-expert` |

Delegate via `@agent-name` with the full diagnosis context from Phase 1, then record and transition:

```bash
curl -sS -X POST $BASE/api/workflows/bug-<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "<agent-name>"}'

curl -sS -X PUT $BASE/api/workflows/bug-<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "review"}'
```

---

## Phase 3: Review

> 🎯 **Karpathy — Goal-Driven Execution:** Verify against the explicit success criteria, not style preferences. Loop until goals met; don't declare done prematurely. See `.claude/rules/karpathy-principles.md`.

- **Delegate to `@delivery-code-reviewer`** — verify fix quality, no regressions, and test coverage.
- Record delegation:

```bash
curl -sS -X POST $BASE/api/workflows/bug-<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "delivery-code-reviewer"}'
```

- If reviewer finds `[must_fix]` issues → fix loop: transition back to fix, delegate to the appropriate agent, re-review (max 5 loops).
- On pass, complete:

```bash
curl -sS -X PUT $BASE/api/workflows/bug-<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

- Summarize what was fixed. Save a memory event with key findings:

```bash
save_memory(text="Bug fix: <summary>", type="bugfix", tags=["bug"], importance=0.7)
```

---

## Rules

- **NEVER** use write, edit, or bash on production source files directly.
- Delegate ALL implementation work to delivery agents via `@mention`.
- **ALWAYS get explicit user approval before Phase 2 (Fix).**
- Max 5 fix loops — escalate to user if still broken after 5 attempts.
- Check current state: `curl -sS $BASE/api/workflows/bug-<slug>`

Fix the bug: $ARGUMENTS
