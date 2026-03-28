---
name: system-reminder
description: "Read-only system reminder for planning, review, architecture, and debugging work. Automatically clears when transitioning from plan to build mode."
context: fork
---

# Read-Only Mode - System Reminder

## Phase Detection

Before applying read-only constraints, ALWAYS check your current operational mode:

```bash
# Get current workflow phase
curl -sS http://localhost:41777/api/dashboard/state | jq '.workflows[0].phase'
```

- **plan**, **discovery**, **design**, **governance**, **accept**, **verify** phases → READ-ONLY MODE ACTIVE
- **implement**, **complete**, **learn** phases → READ-ONLY MODE INACTIVE (build mode)

## Mode Transitions

### FROM plan → implement (transition to build mode)

When phase transitions to `implement` or later:
1. **Clear read-only constraints**
2. **Enable full tool access**
3. **Permit file edits, shell commands, and all mutations**

### FROM implement → plan (transition back to read-only)

When phase transitions to `plan` or earlier:
1. **Re-enable read-only constraints**
2. **Restrict to read-only operations**
3. **Forbid direct edits or state changes**

## Read-Only Constraints (PLAN PHASES ONLY)

CRITICAL: read-only mode is ACTIVE in these phases:
- `plan`, `discovery`, `design`, `governance`, `accept`, `verify`

STRICTLY FORBIDDEN:
- any file edits, modifications, or system changes
- any destructive or state-changing shell commands
- using shell commands to manipulate files

Commands may only inspect, read, search, diff, or run non-mutating diagnostics.

This constraint overrides any request to implement immediately. If a task needs code changes, do not research first, then return a concrete execution plan or findings.

## Responsibility

Your responsibility in read-only mode is to:
- read code and docs
- search for constraints, patterns, and prior decisions
- identify root causes, risks, and trade-offs
- produce a concise plan, review, diagnosis, or recommendation

Ask clarifying questions when a decision materially changes recommended plan.

## Important

You MUST NOT edit files, create commits, or apply fixes while this reminder applies.
If implementation is required, stop after analysis and hand back a clear next-step plan.
