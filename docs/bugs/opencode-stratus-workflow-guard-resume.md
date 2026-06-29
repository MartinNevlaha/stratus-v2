# OpenCode Stratus Workflow Guard Resume Failure

## Summary

Resume delegation failed for workflow `dms-chat-modern-redesign` even though Stratus API reported the workflow as active.

The failure is in the OpenCode Stratus plugin workflow guard, not in the Stratus workflow API state.

## Observed Failure

Attempting to delegate to a delivery agent returned:

```text
No active workflow registered. Use /spec or /bug command first.
```

The workflow was visible through Stratus:

```text
id: dms-chat-modern-redesign
type: spec
phase: discovery
session_id: ${CLAUDE_SESSION_ID}
```

Dispatch also returned a valid active workflow:

```text
workflow_id: dms-chat-modern-redesign
type: spec
phase: discovery
tasks: 0
current_task: null
```

## Affected Files

The relevant OpenCode plugin logic exists in two places:

```text
cmd/stratus/plugins-opencode/stratus.ts
.opencode/plugin/stratus.ts
```

The source template is likely:

```text
cmd/stratus/plugins-opencode/stratus.ts
```

The local project copy is:

```text
.opencode/plugin/stratus.ts
```

Both need the same fix if the local copy is expected to work before regeneration/reinstall.

## Root Cause

The plugin only extracts explicit workflow IDs matching this regex:

```ts
/\b(?:bug|spec|e2e)-[a-z0-9][a-z0-9-]{0,120}\b/
```

Current implementation:

```ts
function extractWorkflowIDFromTaskArgs(args: Record<string, unknown>): string | null {
  const parts = [args["prompt"], args["command"], args["description"]]
    .filter((v): v is string => typeof v === "string")
    .join("\n")

  const match = parts.match(/\b(?:bug|spec|e2e)-[a-z0-9][a-z0-9-]{0,120}\b/)
  return match?.[0] ?? null
}
```

This works for workflow IDs like:

```text
spec-company-agent-ft-routing
bug-login-failure
e2e-checkout-flow
```

It does not work for:

```text
dms-chat-modern-redesign
```

Because that ID does not start with `spec-`, `bug-`, or `e2e-`.

After explicit ID extraction fails, the plugin falls back to session lookup:

```ts
return getWorkflowForSessionStrict(sessionID)
```

That also failed because the resume flow wrote the literal string:

```text
${CLAUDE_SESSION_ID}
```

as the workflow session ID. In this OpenCode environment, `CLAUDE_SESSION_ID` was not set in the shell environment. The plugin uses OpenCode's internal `input.sessionID`, not the shell variable.

Result: no explicit workflow match and no session match, so the workflow guard incorrectly blocks the `Task` delegation.

## Why The API Was Not The Problem

These calls returned valid workflow state:

```text
stratus_get_workflow(dms-chat-modern-redesign)
stratus_delivery_dispatch(dms-chat-modern-redesign)
GET /api/workflows/dms-chat-modern-redesign/dispatch
```

The failure happens after that, inside the OpenCode plugin hook:

```ts
"tool.execute.before": async (input, output) => {
  if (toolName === "task") {
    // workflow_existence_guard
  }
}
```

## Recommended Fix

For `Task` guard resolution, first compare the task text against actual workflow IDs from `/api/dashboard/state`. Use the regex only as a fallback.

This allows explicit workflow IDs that do not use the `spec-`, `bug-`, or `e2e-` prefix while still requiring the workflow to exist in Stratus.

Suggested replacement logic:

```ts
function getTaskText(args: Record<string, unknown>): string {
  return [args["prompt"], args["command"], args["description"]]
    .filter((v): v is string => typeof v === "string")
    .join("\n")
}

function extractWorkflowIDFromTaskArgs(args: Record<string, unknown>): string | null {
  const text = getTaskText(args)
  const match = text.match(/\b(?:bug|spec|e2e)-[a-z0-9][a-z0-9-]{0,120}\b/)
  return match?.[0] ?? null
}

async function getWorkflowForTaskStrict(
  args: Record<string, unknown>,
  sessionID?: string,
): Promise<Workflow | null> {
  const taskText = getTaskText(args)
  const state = await fetchDashboardStateStrict()

  for (const wf of state.workflows) {
    if (taskText.includes(wf.id)) return wf
  }

  const explicitWorkflowID = extractWorkflowIDFromTaskArgs(args)
  if (explicitWorkflowID) {
    const wf = await fetchWorkflowByID(explicitWorkflowID)
    if (wf) return wf
  }

  if (!sessionID) return null

  for (const wf of state.workflows) {
    if (wf.session_id === sessionID) return wf
  }

  return null
}
```

## Additional Recommendation

The resume/spec/bug skill text should not imply that `${CLAUDE_SESSION_ID}` is always available in OpenCode shell commands.

Current resume flow says to PATCH:

```bash
curl -sS -X PATCH $BASE/api/workflows/<id>/session \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\": \"${CLAUDE_SESSION_ID}\"}"
```

In OpenCode, this can become an empty value or a literal placeholder, depending on quoting and environment.

Better options:

- Prefer explicit workflow ID matching in the plugin for `Task` delegation.
- Treat session update as optional when resuming by explicit workflow ID.
- If session ownership is required, expose OpenCode's actual `input.sessionID` through the plugin/API rather than relying on shell env.

## Expected Outcome After Fix

This prompt should pass the workflow guard:

```text
Resume workflow `dms-chat-modern-redesign` in phase `discovery` for a complex spec.
```

The plugin should resolve `dms-chat-modern-redesign` by checking actual active workflow IDs from dashboard state, then enforce the normal phase-agent allowlist.

For `spec` + `discovery`, `delivery-strategic-architect` is already allowed, so delegation should proceed.
