# Existing chat attachment implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Status:** Codex slice implemented, independently reviewed and verified. Existing-chat Claude/OpenCode connectors remain open native-integration gates. Checkmarks below cover the verified Codex slice, not a universal release.

**Goal:** Join an existing native chat to a Stratus team once, preserve its conversation/model, coordinate from that chat, and deliver scoped work/results without idle model polling.

**Architecture:** Keep owned sessions and attached chats distinct. A session-bound host connector proves identity and observes native turns; the durable team manager owns task routing, scopes and limits. Connector readiness is gated by an actual native admission/completion contract, never by discovery alone.

**Tech Stack:** Go, SQLite, existing MCP stdio/HTTP, supplied Codex app-tools MCP, Svelte. OpenCode/Claude existing-chat adapters remain gated by native evidence.

**Spec:** `docs/superpowers/specs/2026-11-04-existing-chat-attachment-design.md` (user approved).

## Global Constraints

- Reuse `codex/automatic-project-team`; do not restart the production Stratus server or use UVO conversations for tests.
- Preserve original native ID, history, model, reasoning effort, permissions and workflow owner. Existing spec/spec-complex/bug/e2e gates remain authoritative.
- Native metadata supplies identity; a free model argument does not authenticate a chat. Tokens stay in transports and are redacted from API/UI/model output.
- Prefer Codex's native send-message and completion tools through a legitimately authorized host connection. Never fabricate executor identity, replay an old call ID, or implement private desktop IPC.
- No extra model for registration, routing, waiting or lease renewal. Enforce existing budgets and coalescing; missing usage is unknown.
- Uncertain submissions are reconciled or held, never blindly retried. Detach never kills the native host or cancels a user's turn.
- Target Codex desktop, Claude Code and OpenCode. A missing target connector is incomplete scope, not a successful universal release.
- Native probes are disposable, new empty-project conversations only; cap each viable runtime probe at two minimal model inputs before deciding the next step.

## Review Focus

1. A native user turn starts between status inspection and team submission: native queue/admission must serialize it (Tasks 1, 3).
2. Reconnect leaves a stale connector alive: old generations cannot submit, acknowledge or finish new work (Task 2).
3. Native user/model settings change while work waits: preserve the actual chat settings and reroute incompatible role work (Tasks 2, 3).
4. Nonleader chat starts a new team request: correlate its result back to the requester without stealing its personal workflow (Task 4).
5. Application remains open after attachment expires or Stratus stops: manual use remains possible and queued team work is not falsely marked delivered (Tasks 2, 3, 5).

## File responsibilities and contracts

- `teams/attachments.go`, `teams/attachments_test.go`: binding, fenced connection generation, attach/detach and native-turn observation; adjust `types.go`, `manager.go`, `recovery.go` only at lifecycle/routing boundaries.
- `teamruntime/attachments/`: verified connectors and their protocol fixtures, independent of the API package. A connector's close means detach, never native process ownership.
- `api/routes_team_attachments.go`, `api/team_sync.go`, `api/team_workflow.go`: session-bound grants, project scope, workflow evidence and public redaction.
- `mcp/teams.go`, `mcp/server.go`: join/request/leave and genuine per-call native metadata; ordinary argument fields cannot override metadata.
- `cmd/stratus/plugins-opencode/`: existing-client connector and native tool context. No new OpenCode server for an attachment.
- `cmd/stratus/skills/` and `hooks/`: Claude join/lease connector only if its supported native wake mechanism satisfies the readiness contract.
- `frontend/src/routes/Team.svelte`, `frontend/src/lib/teams.ts`: attachment state, verified readiness, lead choice, detach and concrete missing prerequisites.
- `docs/testing/existing-chat-attachments.md`: exact probe evidence and outstanding capability gates; `docs/usage/managed-teams.md`: only validated user behavior.

Runtime-neutral additions, finalized only after Task 1 proves the native primitives:

```go
type AttachmentIdentity struct {
    Runtime, HostID, NativeID, ProjectID, ProjectRoot string
    Profile Profile
}
type AttachmentCapabilities struct {
    Queue, ObserveUserTurns, CorrelateResults, Wake bool
}
type AttachmentBinding struct {
    ID string
    Identity AttachmentIdentity
    Generation uint64
    Capabilities AttachmentCapabilities
    State string
    ExpiresAt *time.Time
}
type NativeTurn struct {
    ID, InputID, Origin, State string // origin: user, team or unknown
    Result *Result
}
```

Member holds an optional `AttachmentBinding`; nil means existing owned behavior.
`Attach(ctx, teamID, memberID string, binding AttachmentBinding, session Session) error`
accepts a host-verified transport, not arbitrary model-provided configuration.
`ObserveNativeTurn(teamID, memberID string, generation uint64, turn NativeTurn) error`
records genuine external user work distinctly from manager-submitted tasks.
`Detach(ctx, teamID, memberID string) error` revokes the connection and leaves the host intact.
Attached `Session.Submit` must use native atomic queue/admission and return only its correlated result. `Interrupt` is unsupported unless it can address exactly the team-owned native turn.

### Task 1: prove native attachment primitives before production plumbing

**Files:** disposable probes outside repository; `docs/testing/existing-chat-attachments.md`.
**Interfaces:** produces evidence for the capabilities above and an exact supported transport per target.

- [x] Inspect actual host startup/SDK contracts without reading chat histories or exposing credentials.
- [x] Codex: test the bundled standard MCP facade as a host-provided connector with genuine join-time caller context, if its authorization contract permits that. Preserve native tool approval handling. Otherwise require an existing authenticated app-server endpoint of the original host. A new daemon is not proof of access to desktop chats.
- [x] Claude: establish the lifetime of asyncRewake and result correlation; a finite lease must expire visibly without a model turn just to renew. If persistent Channels is required, record the startup prerequisite instead of pretending an existing unconfigured chat is connected.
- [x] OpenCode: test current V2 queue against an existing V1-created native session. Verify promotion after human work and assistant result-to-input correlation; generic idle and HTTP 200 are insufficient.
- [x] For each viable route, use at most two minimal isolated model inputs to verify same identity/context and correlated completion. Stop the probe on missing prerequisites instead of retrying model calls.
- [x] Record exact observed versions, evidence, limits and failed gates. Do not implement downstream assumptions for a failed native contract; resolve the specific prerequisite or report the remaining integration constraint.

### Task 2: durable attachments and external-turn authority

**Files:** `teams/attachments.go`, `teams/attachments_test.go`, `teams/types.go`, `teams/manager.go`, `teams/recovery.go`.
**Interfaces:** produces `Attach`, `ObserveNativeTurn`, `Detach` and binding types specified above.

- [x] Write failing tests `TestAttachmentPreservesNativeIdentityAndProfile`, `TestAttachmentFencesStaleConnection`, `TestAttachmentStopDoesNotOwnNativeHost`, `TestExternalUserTurnCanDelegateWithinScope`, `TestExternalTurnIsNotCountedAsAutomaticWork`.
- [x] Run `go test ./teams -run 'Attachment|ExternalUserTurn|ExternalTurn'`; verify missing behavior fails.
- [x] Implement unique native-chat claims, connection generations and persisted state. Mark external origin explicitly; never invent a manager-dispatched task for user work. Observe native active turn independently from queued team submission.
- [x] Route mismatched profiles to owned workers; preserve attached profile on followups. Revocation prevents future dispatch and late completion from opening another automatic round.
- [x] Run `go test -race ./teams`; commit the tested core changes.

### Task 3: native connectors with admission and completion fixtures

**Files:** `teamruntime/attachments/*`; native OpenCode plugin and Claude join integration as supported by Task 1.
**Interfaces:** consumes verified native contracts from Task 1; produces attached `Session` and native-turn observations for Task 2.

- [x] Write protocol fixtures for duplicate input, accepted-then-disconnected submission, user input racing queue admission, model change, permission wait, stale events and lease expiry. The fixture must distinguish admission from completion using the verified host contract. Codex correlates an explicit task ACK/result with its native turn; it does not expose a per-input assistant receipt.
- [x] Run connector tests and observe the missing behavior.
- [x] Implement only verified native routes, with bounded payloads, persisted receipts/cursors and native permissions intact. Existing native SDK/client remains the transport owner. Standard MCP metadata must be genuine, not supplied through model arguments.
- [x] Run `go test -race ./teamruntime/...` and relevant plugin tests; commit. Unsupported routes return an actionable reason and never fall back to opening a second process on the same native session.

### Task 4: one-time join and original-chat coordination

**Files:** `api/routes_team_attachments.go`, API guards/sync/workflow tests, `mcp/teams.go`, MCP metadata tests, native join tools/skills.
**Interfaces:** consumes binding and external native-turn authority. Agent-facing operations: join current chat, leave current chat, submit a team request, existing status/delegate/message.

- [x] Write failing integration tests for forged native metadata, cross-project join, credential redaction/revocation, nonleader request/result routing, lead selection while work is active, existing workflow owner mismatch and stale review evidence.
- [x] Run `go test ./api ./mcp -run 'Attachment|Attached|Join'`; verify failures.
- [x] Bind join to the real host context. For a project without a team, create it by default using existing role profiles and attached lead (the join tool can opt out); otherwise join as member and require explicit lead selection. Preserve one coordinator.
- [x] Forward new nonleader team requests through the existing lead with requester correlation, returning results to the original chat. Apply existing phase, owner, revision, round and token checks to all attached paths.
- [x] Run `go test -race ./api ./mcp ./teams`; commit.

### Task 5: visible readiness, detach and user documentation

**Files:** Team UI, frontend team types/tests, native installers where connector setup is needed, `docs/usage/managed-teams.md`.
**Interfaces:** consumes redacted attachment state and capability reasons from Task 4.

- [x] Add UI behavior checks for expired/disconnected connection, missing native bridge, current versus unknown model, preserved manual chat use and detach while work is active.
- [x] Implement attached/owned labels, verified readiness, optional lead selection and detach. Show exact setup prerequisites; registration alone never displays automatic wake as ready.
- [x] Run `npm run check`, `npm test`, `npm run build` in `frontend`; rebuild Go binary after embedding the new UI.
- [x] Document only validated entry points; report unverified target connectors explicitly. Commit.

### Task 6: acceptance and whole-change review

**Files:** regression tests from findings, final evidence in this plan and testing document.

- [x] Exercise the complete original-chat → lead → worker → original-chat path on every supported target; preserve identity/context and show one result per input. Stay within recorded native test budgets; do not touch UVO sessions.
- [x] Run `go test ./...`, focused Go race suites, relevant frontend checks/build, and `go build -o /tmp/stratus-chat-attachments ./cmd/stratus`.
- [x] Obtain a fresh whole-change review focused on authority, duplicate work, native user-turn races, permissions, token loops and lifecycle ownership. Fix material findings with regressions.
- [x] Record supported/unsupported hosts accurately. Commit reviewable changes without installing, restarting production or claiming universal completion while a target is missing.

## Execution record

- Approved design and implementation scope retained; suitable existing checkout reused.
  No production restart, installation, push or UVO-chat traffic.
- Codex: the official session-bound MCP facade completed the explicitly authorized
  isolated same-chat idle-wake test, preserving context. The current native send is
  tool-output admission and may coalesce with a racing user turn. It supplies no
  per-input receipt, so explicit authenticated `team_ack` / `team_result` correlate
  work; ambiguous sends are not retried. Standalone native wait failed; bounded
  metadata reads run outside the model instead.
- Claude: finite asyncRewake lifetime remains an integration constraint. Persistent
  Channels requires startup opt-in. Existing-chat adapter is not implemented.
- OpenCode: normal configured environment, exactly two disposable inputs. First V1
  response completed; second V2 input was admitted/promoted but produced no assistant
  result over 40 seconds after the first completion. Existing-chat adapter disabled.
- Durable attachment core, native Codex connector, API broker and MCP tools are
  implemented. Fixtures cover scoped identity, unknown origin/model, project and
  generation isolation, ACK/result correlation, uncertain delivery, native ownership,
  budgets, preserved human workflow authority, EOF and reconnect.
- Cross-module fixtures run the real API/SQLite manager, MCP stdio server and Codex
  connector against disposable native/model fixtures. Original-chat delegation,
  same-chat result, terminal receipts, metadata rejection and generation fencing pass.
  This is integration evidence, not a second live-model acceptance test.
- Dashboard distinguishes attached versus owned sessions, offers detach/lead controls
  and recovery guidance, and does not expose manual bookkeeping as assigned work.
  Five DOM tests pass. Installer forwards only host-connection environment names and
  preserves existing TOML settings/comments; captured caller IDs are never forwarded.
- Final verification passed: `go test ./...`, focused Go race suites,
  `go vet`, frontend tests (60 across 11 files), check/build and Go build. Svelte check reports zero errors
  and 52 pre-existing warnings; bundle-size warning remains.
- Independent Astra review found observation/send ordering, uncertainty-state,
  reconnect, canonical-lead and requester-return gaps. All material findings were
  fixed and re-reviewed. Durable request lineage holds intermediate waiting results
  and delivers the final synthesis once, including nested/multiple delegation.
- A fresh integration run caught stale-connection leave returning an error after the
  closed-binding change. Leave is now idempotent without using revoked credentials;
  its regression and the full API/MCP race suites pass. No remaining review finding.
- Final commands: `go test ./... -count=1`; `go test -race ./teams ./teamruntime/...
  ./api ./mcp -count=1` (API/MCP rerun after the final idempotent-leave fix);
  `go vet ./teams ./teamruntime/... ./api ./mcp ./cmd/stratus`; frontend `npm test`,
  `npm run check`, `npm run build`; `go build -o /tmp/stratus-chat-attachments ./cmd/stratus`.
  The last code change was included in the final build. Reviewable changes are saved
  on `codex/automatic-project-team`; no installation, production restart or push.

## Remaining acceptance gates

- The original-chat-to-worker-to-original-chat application path is covered by fixtures;
  the explicitly authorized live test proves the native wake primitive and memory,
  not an additional live run of the complete new production bridge.
- [ ] Resolve the Claude existing-chat connection lifetime/startup prerequisite.
- [ ] Establish OpenCode queued execution and correlated completion on the original host.

Claude and OpenCode native prerequisites above remain unsatisfied. This work is
  not a completed universal existing-chat release, regardless of passing Go tests.
