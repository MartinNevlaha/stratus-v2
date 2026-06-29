# Remaining Stratus Bugs Report

## Summary

This report captures the remaining bugs found while debugging the OpenCode/Stratus resume failure.

The highest-priority active bug is the OpenCode workflow guard resume failure. The next larger bug cluster is the code analyst false-positive pipeline.

## Bug 1 — OpenCode workflow guard resume failure

**Status:** fixed — `getWorkflowForTaskStrict` now matches the task text against actual active workflow IDs from `/api/dashboard/state` first, with the `spec-`/`bug-`/`e2e-` regex and session ownership as fallbacks. Applied to both plugin copies.

**Detailed report:** `docs/bugs/opencode-stratus-workflow-guard-resume.md`

### Symptom

Delegating to a delivery agent during resume failed with:

```text
No active workflow registered. Use /spec or /bug command first.
```

This happened even though Stratus returned the workflow as active:

```text
id: dms-chat-modern-redesign
type: spec
phase: discovery
```

### Root Cause

The OpenCode plugin workflow guard extracted explicit workflow IDs only with this regex:

```ts
/\b(?:bug|spec|e2e)-[a-z0-9][a-z0-9-]{0,120}\b/
```

Workflow IDs without those prefixes, such as `dms-chat-modern-redesign`, were not recognized.

After that failed, the plugin fell back to session ownership. That also failed because `${CLAUDE_SESSION_ID}` was not available as a shell environment variable in OpenCode, and the workflow session was stored as a literal placeholder.

### Files

```text
cmd/stratus/plugins-opencode/stratus.ts
.opencode/plugin/stratus.ts
```

### Recommended Fix

Resolve task workflow context by checking the task prompt/command/description against actual active workflow IDs from `/api/dashboard/state`. Keep the existing `spec-`/`bug-`/`e2e-` regex only as fallback.

### Verification Needed

- Verify both plugin copies are consistent.
- Add or run a test for workflow IDs without `spec-`, `bug-`, or `e2e-` prefix.
- Confirm `delivery-strategic-architect` can be delegated for `spec/discovery` when the prompt contains `dms-chat-modern-redesign`.
- Confirm phase-agent allowlist still blocks invalid agents.

## Bug 2 — Code analyst produces false-positive findings

**Status:** fixed in commit `d5b6c53` — line-number prefixing, truncation notice, intentional-pattern prompt guidance, and an adversarial verification pass are all implemented in `internal/insight/code_analyst/analyzer.go` + `prompts.go`. Package tests pass.

**Detailed report:** `docs/bugs/code-analyst-false-positives.md`

### Symptom

The LLM code analyst repeatedly returns findings that are incorrect or have wrong line numbers.

Observed failures include:

- wrong `line_start` / `line_end`
- findings based on truncated code
- confident but false claims about control-flow bugs
- flagging intentional fail-open or lint-suppressed patterns as critical bugs

### Root Causes

#### 1. Source code is sent without line numbers

The prompt asks for exact line numbers, but the model receives raw source text without line prefixes.

This causes systematic line-reference drift.

Primary area:

```text
internal/insight/code_analyst/analyzer.go
internal/insight/code_analyst/prompts.go
```

#### 2. Files are hard-truncated at 32 KB without warning

Large files are cut before being sent to the model, but the prompt does not tell the model that it sees only a partial file.

This causes false control-flow findings when cleanup, error handling, or terminal logic appears after the truncation boundary.

#### 3. No refutation/verification pass

The analyzer parses and filters model output, but does not run a second pass to try to disprove each finding against the actual code.

#### 4. Self-reported confidence is trusted too much

False findings can self-report high confidence, so the threshold filter does not reliably remove them.

#### 5. Prompt does not sufficiently respect intentional patterns

The model may report intentional patterns as bugs even when code comments or suppressions explain the intent.

Examples:

```text
# noqa: BLE001 — fail-open
documented fail-open streaming behavior
framework-specific idioms
```

### Recommended Fix Order

1. Prefix analyzed source content with 1-based line numbers.
2. Add an explicit truncation marker when only part of a file is sent.
3. Update the prompt to require line references from provided line prefixes only.
4. Add prompt guidance to respect documented intentional patterns.
5. Add an optional adversarial verification pass for high-severity findings.

### Minimal First Patch

The smallest useful fix is to modify the analyzer prompt payload:

```text
1<TAB>package example
2<TAB>
3<TAB>func main() {}
```

And when truncated:

```text
[FILE TRUNCATED: showing first N of M bytes / approximate first L lines. Do not infer behavior beyond shown code.]
```

### Verification Needed

- Add unit tests for line-number prefixing.
- Add unit tests for truncation marker behavior.
- Run analyzer against a file over 32 KB and verify the prompt warns about truncation.
- Confirm generated findings use valid line ranges from the supplied line prefixes.

## Recommended Priority

1. Finish Bug 1 first because it blocks workflow orchestration and delivery-agent delegation.
2. Then fix Code Analyst Bug 1 and Bug 2 together because they are small and remove most false-positive noise.
3. Add the verification/refutation pass later because it is more expensive and may need additional design.
