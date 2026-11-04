# Existing-chat attachment verification

Date: 2026-11-04. Codex attachment is implemented; Claude/OpenCode existing-chat
connectors remain unsupported. This document separates observed native behavior
from protocol fixtures and unsupported assumptions.

## Codex desktop

Installed desktop build: 26.1027.30824; bundled Codex: 0.166.0-alpha.5.1.
The desktop-owned app-server uses stdio. No default daemon control socket
was found, so starting another daemon would not prove access to existing chats.

The supplied `codex-app-tools` **standard MCP facade** is a viable connection.
A helper launched by the actual joining chat supplied that real source thread
identity, omitted old turn/call IDs, and used the provided launcher/server.
No private IPC protocol, native history modification or second resumed writer
was implemented. Model and reasoning overrides were omitted.

Read-only initialization, tool discovery and reading the source chat's status
passed. Standalone `wait_threads` returned a host error, so the probe used
bounded `read_thread` status checks outside the model. Such checks do not
invoke a model and their conversation payload is not forwarded to agents.

After explicit user authorization for the named isolated test chat, observed:

1. The original model turn finished and native status became idle.
2. The same session-bound helper successfully sent one native follow-up to
   that exact chat while its original model turn was no longer running.
3. A new response completed in the same native chat and correctly recalled
   the marker from its earlier response.
4. The helper exited, closing only its own supplied MCP subprocess.

Test chat: `01a1a6ff-622a-75f2-adb2-d54ccefcc8cd` (local).
Observed stages: `armed`, `idle_observed`, `send_attempted`, `send_accepted`,
`complete` with `same_thread=true`, `remembered_initial_marker=true`.
An earlier setup turn was declined by the test agent for ambiguous authority;
it made no background send. The later explicit user consent authorized the
successful setup plus the single automatic wake. No UVO chat was used.

### Admission and completion limits

Static inspection of this shipped build's handler, together with the
[public app-server contract](https://learn.chatgpt.com/docs/app-server#start-a-turn),
shows native `toolOutput` admission. When a regular turn has become active,
the output joins that turn without an explicit interrupt. This is serialized
native admission, **not** a guarantee of a separate later turn.

The exposed send result contains only `threadId`. It provides no caller
idempotency key, expected-turn guard or per-input completion receipt. Therefore:

- Stratus attempts each claimed delivery once and holds ambiguous outcomes.
- Explicit authenticated task acknowledgement/result supply task correlation.
- Generic native idle, a final message or successful send does not complete
  an assigned task or advance its workflow.
- Actual user-origin work remains distinct when a team message is coalesced
  into its native turn. Native permission/user-input states remain authoritative.
- The native interface does not expose the actual model/effort in these
  snapshots; the attachment leaves them unknown and never overrides them.

The idle-wake probe is live evidence. Concurrency, approval, disconnect and
scope guarantees additionally require the implementation's targeted tests;
no live concurrent-human-turn experiment has been claimed.

## Claude Code

Installed version observed: 2.1.318. Skill `Stop` hooks with `asyncRewake`
can wake an idle session, but enforce their configured timeout (default
600 seconds for Stop). No indefinite value or automatic timeout rearm was
established. Plain async hooks do not themselves wake an idle chat.
[Native hook contract](https://code.claude.com/docs/en/hooks#run-hooks-in-the-background).

A finite waiter must expire visibly and cannot wake a model merely to renew
itself. Explicit task ACK/result is needed; an unrelated Stop/final response
is not correlation. Persistent Channels is another supported mechanism but
requires native startup opt-in; hot-enabling it was not verified.
[Channels](https://code.claude.com/docs/en/channels).

No production attachment capability is claimed from documentation alone.

## OpenCode

Installed CLI observed: 1.18.29. The current host schema exposes persistent
`POST /api/session/{id}/prompt` with an explicit stable `msg_…` ID,
`prompt.text`, `delivery:"queue"` and `resume:true`. V1-created sessions are
readable through V2 and vice versa with the same native identity.

V2 `/wait` returned 503, `Session wait is not available yet`. Public history
and SSE expose admission/promotion IDs, but completion uses assistant message
IDs and requires a verified association to the admitted input. The legacy
`prompt_async` endpoint has no equivalent queue option.

An isolated-XDG probe did not expose the configured new models; this was not
treated as evidence that the normal host lacks them. The ordinary installed
catalog includes exact `openai/gpt-6-luna` and `openai/gpt-6.1-sol`. The bounded
normal-environment probe used `openai/gpt-6-luna` / `medium` in a new empty
project and sent exactly two inputs. The first V1 input completed with
`STRATUS_V1_OK` and an exact assistant `parentID`. The second V2 queued input
was accepted (HTTP 200, admitted sequence 1) and promoted (sequence 7), but
no associated assistant result appeared during the next 40 seconds after
the first response completed. V2 active state was empty. The test then
stopped only its own host; the later connection-refused observation was
cleanup, not the queue failure's cause.

Evidence: `/tmp/stratus-opencode-queue-normal-ip8q9lel/report.json`.
This does not establish safe automatic execution of the native queue.
Existing-chat OpenCode attachment remains disabled; no fallback to legacy
`prompt_async` or a second resumed writer is claimed.

## Implementation verification

The cross-module suite runs the actual HTTP routes, SQLite team manager, MCP
stdio server and Codex connector against disposable native-service and model
fixtures. It covers both original-chat → worker → original-chat and forwarded
requester → lead → worker → lead → requester. A waiting handoff does not wake
the requester; the final synthesis is delivered once with its durable request
lineage. No real host/model is used by these tests.

Regression coverage includes:

- Per-call native identity, credential redaction, project/generation fencing,
  reconnect and idempotent stale leave, without interrupting the native host.
- Send/observation ordering, ACK before send receipt, retained old terminals,
  ambiguous submission without replay and a bounded missing-ACK timeout.
- Separate manual authority while an uncertain team delivery holds automation;
  explicit result never invents native turn completion.
- Nested/multiple delegation, isolated request batching, canonical lead reuse,
  complex-profile routing, budget holds, restart and stop of waiting work.
- Dashboard detach/rejoin guidance, unknown model display and hidden manual
  bookkeeping; configuration merge preserves existing user settings/comments.

Final Go suites, focused race suites and vet passed. Frontend: 60 tests pass,
check reports zero errors with 52 existing warnings, and production build passes
with the existing large-chunk warning. The Go executable was rebuilt after the
frontend and final code fix at `/tmp/stratus-chat-attachments`.

Independent Astra review identified and rechecked the admission, lifecycle,
requester-return and single-lead fixes; no actionable finding remains. Native live evidence above remains a
separate, deliberately bounded test of idle wake and preserved context.

## Scope of the evidence

All native probes use new isolated test chats/projects. No production Stratus
restart, global model setting change or existing project conversation mutation
is part of this verification. No percentage token saving is claimed.
