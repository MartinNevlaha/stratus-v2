# Owned native runtimes

`Adapters()` implements `teams.Adapter` using native sessions owned by Stratus.
`Open` initializes a transport, checks the configured model and effort, and
returns without a model turn. `Submit` waits for the native terminal result;
acceptance, output deltas and permission requests do not complete a task.
Cancellation never replays an uncertain submission. `Close` terminates only the
owned process group, including its MCP children. Windows is explicitly
unavailable until child-process ownership is implemented there.

The initialization context bounds startup; it does not own the returned
session's lifetime. The same process and native session receive subsequent
turns. Existing terminal or desktop sessions are never attached or controlled.

## Protocols and evidence

Checked on 2026-11-04 against these locally installed releases:

| Runtime | Version | Transport and terminal completion |
| --- | --- | --- |
| Codex | 0.166.0-alpha.5.1 | `app-server --stdio`; `turn/start` then matching `turn/completed` |
| Claude Code | 2.1.318 | Bidirectional `stream-json`; successful root `result` |
| OpenCode | 1.18.29 | Owned authenticated loopback `serve`; `prompt_async`, SSE idle, matching completed assistant message |

Codex wire fields were checked against the installed CLI's
`app-server generate-json-schema` output. Claude's control requests and
permission answers follow the [official Python SDK transport](https://github.com/anthropics/claude-agent-sdk-python/blob/main/src/claude_agent_sdk/_internal/transport/subprocess_cli.py)
and [control protocol implementation](https://github.com/anthropics/claude-agent-sdk-python/blob/main/src/claude_agent_sdk/_internal/query.py),
with flags confirmed by installed CLI help. OpenCode endpoints and event shapes
were checked against the owned server's `/doc` OpenAPI response and the
[official server documentation](https://opencode.ai/docs/server/).

No-model preflight verifies Codex account/model metadata, Claude native
`auth status --json` plus its initialization catalog, and OpenCode's connected
provider catalog. These checks are not a guarantee of provider quota or future
model access. No credentials, account identifiers, or capability tokens are
included in status output.

The default tests use subprocess fixtures that speak the real JSON/HTTP wire
formats. They cover two turns in one session, acceptance versus completion,
explicit one-use allow/deny, interruption, unknown profiles, model drift,
transport failure, environment/config propagation, and cleanup.

```sh
go test -race ./teamruntime
STRATUS_RUN_NATIVE_PREFLIGHT=1 go test ./teamruntime -run TestInstalledNativePreflight -v
```

The billable `STRATUS_TEAM_LIVE=1` test requires separate explicit authorization.
It makes at most two short turns per runtime in an empty temporary Git project,
requests no tools, cancels if a permission prompt appears, and neither retries
nor falls back to another model. One authorized run passed all three runtimes,
confirming the same native identity and recall of a random token on turn two:

| Requested profile | Reported model / effort | Turn 1 input/output tokens | Turn 2 input/output tokens |
| --- | --- | --- | --- |
| Codex `gpt-6-luna`, medium | `gpt-6-luna` / medium | 21801 / 5 | 21832 / 22 |
| Claude `haiku`, medium | `claude-haiku-5-5` / unknown | 28290 / 4 | 36179 / 65 |
| OpenCode `openai/gpt-6-luna`, medium | `openai/gpt-6-luna` / medium | 15718 / 5 | 15749 / 13 |

These are native usage counts, including reported cache input. Native startup
context is substantial. This test establishes transport continuity, not cost
savings or real-tool permission behavior; permission decisions are fixture-tested.

## Profiles, configuration, and limits

Models and efforts must appear in native catalogs. Claude aliases are accepted
only through its reported `value` to `resolvedModel` mapping; requested and
observed models remain separate. Missing observed effort stays unknown. Reported
model changes are errors, with no silent substitution.

Each owned process inherits native user/project settings. Only the Stratus MCP
entry is overridden to the current server executable and endpoint. Unrelated
MCP entries remain enabled. `STRATUS_TEAM_TOKEN` and `STRATUS_TEAM_API_BASE`
reach the child MCP process through its environment, never through prompts.
Claude's inherited `CLAUDECODE` nesting marker is removed for the owned SDK-style
process. Native permission checks remain enabled; approvals are single-use and
never persisted as permission rules.

The current approval form supports allow/deny. Native structured questions or
other unsupported input requests stop with an actionable error. There is no
automatic replay, transcript reconstruction, session takeover, or new shadow
session. Provider errors, unknown configuration, and interrupted submissions
remain visible for user action.
