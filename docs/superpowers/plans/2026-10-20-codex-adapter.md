# Standalone Codex Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Add a project-local Codex adapter with live workflow visibility while preserving working Claude Code and OpenCode integrations.

**Architecture:** Keep the SQLite stores, workflow coordinator, MCP transport, HTTP API, and Svelte dashboard shared. Isolate Codex event decoding, decisions, installation assets, and instructions. Add explicit runtime ownership before exposing Codex writes, so existing adapters cannot select Codex workflows accidentally.

**Tech Stack:** Go and modernc SQLite; Svelte 5, TypeScript, Vite, Vitest; Node 24 built-in test runner for the existing OpenCode TypeScript plugin; go-toml/v2 v2.4.3 for preserving TOML source outside managed entries.

**Spec:** [Approved Codex adapter specification](../specs/2026-10-20-codex-adapter-design.md).

## Global Constraints

- `stratus init --target both` continues to mean Claude Code plus OpenCode.
- `stratus init` and `stratus refresh` keep the existing default target of `both`.
- No `all` target is introduced.
- Codex's existing `session_id` field stores the internal key `codex:<native-session-id>`; `runtime_session_id` stores the native ID.
- Preserve `.stratus.json` settings, port, data directory, secrets, and existing sync hashes, even when `--force` is supplied for the Codex target.
- Do not change the user's model, reasoning effort, permissions, trust settings, or multi-agent defaults.
- Do not install Codex `SKILL.md` files into `.claude/skills/` or `.agents/skills/`, or modify root `AGENTS.md`.
- Telemetry uses a one-second HTTP timeout; supported guarded delivery calls deny when ownership or phase cannot be verified.
- Keep existing tool names, required legacy parameters, workflow phases, and WebSocket event names valid.
- Never direct integration fixtures at the already-running server on port 41777.
- No browser automation library becomes a Stratus runtime dependency; no transcript parsing is required for Codex monitoring.
- Execution approved: Native implementation through executing-plans, followed by an independent whole-branch review.

## Review Focus

1. A second Codex session repeats an existing workflow ID: reject ownership takeover, including the currently idempotent `Coordinator.Start` path. Pin in Task 1.
2. A project path contains spaces, the hook starts in a nested worktree, and an unrelated server owns 41777: resolve the right root and port without changing process CWD. Pin in Task 6.
3. TOML uses quoted keys, multiline arrays, inline entries, CRLF, or `enabled = false`: preserve unrelated bytes and disabled state, or return a precise conflict before writing. Pin in Task 8.
4. Two identical events arrive concurrently and the first transaction fails: commit the receipt and mutation together; allow a failed event to retry. Pin in Task 3.
5. The browser remembers a terminal tab and clipboard actions refer to a finished Codex workflow missing from the active snapshot: never mount a PTY or offer a Claude resume command for it. Pin in Task 10.

## File boundaries and execution order

| Unit | Primary files | Task |
| --- | --- | --- |
| Ownership and workflow APIs | `orchestration/runtime.go`, existing coordinator and orchestration routes | 1 |
| Wire translation and common event values | `integrations/events.go`, `integrations/codex/{events,tools}.go` | 2 |
| Transactional activity storage | `db/integration_events.go`, schema and agent projections | 3 |
| Codex event HTTP route and activity detail | `api/routes_codex.go`, existing agent routes | 4 |
| Runtime-aware MCP | `mcp/runtime.go`, MCP registration and CLI entry point | 5 |
| Codex hook execution and policy bridge | `integrations/codex/{hooks,policy,project}.go`, `hooks/policy_bridge.go` | 6 |
| Legacy filtering and executable plugin tests | existing Go hooks/MCP and embedded OpenCode plugin | 7 |
| Configuration merge | `cmd/stratus/codex_config.go` | 8 |
| Installer, embedded references, CLI | `cmd/stratus/codex.go`, `cmd/stratus/codex-assets/` | 9 |
| Shared dashboard | existing Svelte views, `frontend/src/lib/runtime.ts` | 10 |
| Coexistence and live verification | `cmd/stratus/codex_e2e_test.go`, `docs/testing/codex-adapter.md` | 11 |

Tasks are sequential: 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11. Use the approved execution method; task separation does not authorize concurrent edits. Before execution, use `superpowers:using-git-worktrees` to reuse/create an isolated checkout. Record baseline failures separately; do not repair unrelated features to make this plan green.

### Task 1: Atomic runtime ownership and session binding

**Files:** Create `orchestration/runtime.go`, `orchestration/runtime_test.go`; modify `orchestration/coordinator.go`, `api/routes_orchestration.go`; create `api/routes_runtime_test.go`.

**Interfaces:** Define `orchestration.WorkflowOwner{Runtime, NativeSessionID string}` and optional `WorkflowState.Runtime`, `WorkflowState.RuntimeSessionID` JSON fields. Add `SessionKey(owner WorkflowOwner) (string, error)`, `ResolveOwned(active []*WorkflowState, explicitID string, owner WorkflowOwner) (*WorkflowState, error)`, `(*Coordinator).StartOwned(id string, kind WorkflowType, complexity Complexity, title string, owner WorkflowOwner) (*WorkflowState, error)`, and `(*Coordinator).BindOwner(id string, owner WorkflowOwner) (*WorkflowState, error)`. Preserve `Start`, `SetSessionID`, and `UpdateSessionID` for legacy callers; prevent their rebind path from changing explicitly Codex-owned state.

- [x] **1. Write failing tests** `TestStartOwned_PersistsOwnershipAtomically`, `TestStartOwned_RejectsExistingOwner`, `TestBindOwner_RejectsCrossRuntime`, `TestResolveOwned_NoGlobalFallback`, and `TestRuntimeMetadata_SurvivesCoordinatorUpdates`. Use temporary SQLite, not shared data. Pin these assertions:

```go
key, err := SessionKey(WorkflowOwner{Runtime: "codex", NativeSessionID: "s1"})
if err != nil || key != "codex:s1" { t.Fatalf("key=%q err=%v", key, err) }
// Repeating the same owner succeeds; owner s2 using s1's workflow ID must error.
```

- [x] **2. Verify RED:** `go test ./orchestration ./api -run 'Test(StartOwned|BindOwner|ResolveOwned|RuntimeMetadata)' -count=1`. Expect missing interfaces initially, then failing ownership assertions before the fix.
- [x] **3. Implement** the interfaces above under the coordinator mutex, persisting ownership in the first `state_json` save. Extend `POST /api/workflows` and `PATCH /api/workflows/{id}/session` with optional `runtime` and native `runtime_session_id`; reject inconsistent internal/native pairs with 400 and ownership conflicts with 409. Codex repeated IDs are idempotent only for the same native owner. Have legacy `Start` delegate to `StartOwned` with an empty runtime, preserving legacy repeat-ID/session semantics while rejecting a collision with Codex-owned state. An absent legacy session is valid; an absent Codex session is not. Propagate unexpected database/read errors instead of treating them as a new workflow. Preserve legacy request behavior. Broadcast only a fully owned state.
- [x] **4. Verify GREEN:** run the focused command and `go test ./orchestration ./api -count=1`. Add API assertions for legacy JSON, missing native ID, multiple owned workflows, and 409 without state mutation. Do not alter `validTransitions`.
- [x] **5. Commit** the listed files: `feat: add isolated Codex workflow ownership`.

### Task 2: Decode Codex events and tool inputs without Claude assumptions

**Files:** Create `integrations/events.go`, `integrations/codex/events.go`, `integrations/codex/tools.go`, `integrations/codex/events_test.go`, `integrations/codex/testdata/*.json`.

**Interfaces:** `integrations.Event` has string fields `Kind`, `NativeSessionID`, `WorkflowID`, `TurnID`, `NativeAgentID`, `AgentType`, `ToolName`, `ToolUseID`, `Summary`, `Report`; `Files []string`; `MainAgent bool`; `Succeeded *bool`. Its JSON keys are `event`, `session_id`, `workflow_id`, `turn_id`, `agent_id`, `agent_type`, `tool_name`, `tool_use_id`, `summary`, `report`, `files`, `main_agent`, `succeeded`. `codex.HookEvent` decodes documented hook input without borrowing `hooks.HookEvent`. Produce `Decode(io.Reader) (HookEvent, error)`, `Normalize(HookEvent) (integrations.Event, error)`, `AgentKey(sessionID, agentID string) string`, `CoordinatorKey(sessionID, turnID string) string`, `EventKey(integrations.Event) string`, and `PatchPaths(command string) ([]string, error)`.

- [x] **1. Write failing tests** `TestNormalize_Lifecycle`, `TestNormalize_RequiresIdentity`, `TestNormalize_NoInferredAgent`, `TestEventKey_Stable`, and `TestPatchPaths_MovesAndMultipleFiles`. Include exact assertions:

```go
paths, err := PatchPaths("*** Begin Patch\n*** Add File: src/a.go\n+x\n*** Update File: old.go\n*** Move to: new.go\n@@\n-x\n+y\n*** End Patch")
if err != nil || !slices.Equal(paths, []string{"src/a.go", "old.go", "new.go"}) { t.Fatal(paths, err) }
```

- [x] **2. Verify RED:** `go test ./integrations/... -run 'Test(Normalize|EventKey|PatchPaths)' -count=1`; initially expect missing package/interfaces.
- [x] **3. Implement** decoding with unknown-field tolerance. Normalize lifecycle events to `session_start`, `agent_start`, `tool`, `agent_stop`, `waiting`, `turn_stop`, `interrupt`, `session_end`. Use `codex-agent-` plus SHA-256 of JSON-encoded `[sessionID,agentID]` for agent storage IDs, and `codex-coordinator-` plus SHA-256 of JSON-encoded `[sessionID,turnID]` for an observed coordinator turn. Return no coordinator key without a turn ID; the caller additionally requires a verified main-agent identity. Never reuse a closed turn's row for the next turn. Stable tool keys include event kind, session, turn, and tool-use ID; lifecycle keys include kind, session, and agent, plus turn for coordinator events. Return an empty key when a tool lacks stable identity. Normalize successful patch paths in encounter order with deduplication; a failed/unknown-result edit cannot invalidate the index. A missing agent ID alone never proves `MainAgent`.
- [x] **4. Verify GREEN:** focused command plus `go test ./integrations/... -count=1`. Test unknown fields, malformed JSON, missing session, Bash command strings, delegation argument aliases, unknown tool response shapes, empty patch, add/delete/move, and traversal paths. Paths outside the selected project are not queued for indexing. Bound summaries to 400 Unicode characters and reports to 16 KiB; mark truncation in text.
- [x] **5. Commit:** `feat: normalize Codex lifecycle and tool events`.

### Task 3: Store normalized activity and deduplication atomically

**Files:** Create `db/integration_events.go`, `db/integration_events_test.go`; modify `db/schema.go`, `db/workflow_agents.go`.

**Interfaces:** `db.IntegrationActivity{Event integrations.Event, EventKey, SessionKey, AgentKey, WorkflowID, Phase string, TaskIndex *int}`; `db.IntegrationResult{Recorded, Duplicate bool, AgentID string, DetailEventID int64}`. Add `(*DB).ApplyIntegrationActivity(ctx context.Context, in IntegrationActivity) (IntegrationResult, error)` and `(*DB).ListIntegrationSteps(ctx context.Context, workflowID, agentID string, limit int) ([]Event, error)`. `integrations` imports only the standard library, avoiding an import cycle.

- [x] **1. Write failing tests** `TestIntegrationActivity_Lifecycle`, `TestIntegrationActivity_ConcurrentDuplicate`, `TestIntegrationActivity_RollbackReceipt`, and `TestIntegrationActivity_StopBeforeStart`. Assert the database state, not just a receipt flag:

```go
// Deliver one start, the same tool twice concurrently, then stop and a late start.
a, err := database.GetWorkflowAgent("spec-codex", agentID)
if err != nil || a.ToolCalls != 1 || a.Status != "done" { t.Fatal(a, err) }
```

- [x] **2. Verify RED:** `go test ./db -run '^TestIntegrationActivity_' -count=1`; expect the missing method/table, then lifecycle failures.
- [x] **3. Implement** `integration_event_receipts(runtime TEXT, event_key TEXT, PRIMARY KEY(runtime,event_key))` using `CREATE TABLE IF NOT EXISTS` in the shared schema, applied to old and new DBs. In one SQL transaction, recheck persisted workflow ownership, insert receipt, update the agent/log rows, and persist a Stratus event with `refs.runtime`, `refs.workflow_id`, `refs.agent_id`, and native identity. The ownership recheck prevents a concurrent rebind attaching a stale event; inspect persisted JSON without importing `orchestration` into `db`. Do not call connection-level helpers from inside the transaction: use transaction-local SQL. Duplicate receipts return without mutation. Store waiting/interruption events without changing workflow phase or task completion. Only stable identified tools increment exact counters.
- [x] **4. Verify GREEN:** focused command and `go test ./db -count=1`. Induce a log insert failure with a temporary SQL trigger: no receipt/counter remains, and retry after dropping the trigger succeeds. Verify old DB reopen, a session rebind before persistence, two successive coordinator turns with different rows, missing agent attribution, terminal records that cannot reopen, absent reports/usage, and unsuccessful edits. `ListIntegrationSteps` filters by workflow and agent refs, defaults to 40 recent steps, and returns chronological order.
- [x] **5. Commit:** `feat: persist Codex activity with atomic deduplication`.

### Task 4: Expose Codex telemetry and normalized activity in the API

**Files:** Create `api/routes_codex.go`, `api/routes_codex_test.go`; modify `api/server.go`, `api/routes_workflow_agents.go`.

**Interfaces:** Register `POST /api/integrations/codex/events` to `(*Server).handleCodexEvent(http.ResponseWriter,*http.Request)`. Consume Tasks 1–3. Extend existing agent/transcript projections with optional `runtime` and `usage_available`; Codex detail returns existing `steps` and `usage: null, usage_available: false`.

- [x] **1. Write failing tests** `TestCodexEvent_OwnedLifecycle`, `TestCodexEvent_AmbiguousSession`, `TestCodexEvent_NoCrossRuntime`, and `TestCodexTranscript_UsesNormalizedEvents`:

```go
// With two Codex workflows for s1 and no explicit workflow_id:
if response.Code != 409 { t.Fatal(response.Code, response.Body.String()) }
// Agent/log/event counts for both workflows must remain unchanged.
```

- [x] **2. Verify RED:** `go test ./api -run '^TestCodex(Event|Transcript)_' -count=1`; expect the absent route and ownership/detail failures.
- [x] **3. Implement** validation and strict ownership selection before Task 3 writes. Use 400 for malformed required identity, 409 for ambiguity/ownership mismatch, 200 with `recorded:false` for no active owned workflow, and 200 with `duplicate:true` for repeat delivery. `session_start` registers the namespaced session without requiring a workflow. Reuse `agent_started`, `agent_activity`, `agent_status`, `workflow_log`, and `workflow_waiting` broadcasts only after commit. Never call `closeSwarmTickets`, `recordAgentUsage`, or Claude transcript helpers for Codex. Successful validated edits queue the existing dirty-index mechanism after commit.
- [x] **4. Verify GREEN:** focused command and `go test ./api -count=1`. Verify a Codex transcript with a bogus `transcript_path` still reads only normalized events; legacy transcript tests remain unchanged. Interruption/end produces an honest activity record, never a task completion. A duplicate produces no duplicate broadcast. Add matching optional fields to every agent-list projection, including mission-control rows.
- [x] **5. Commit:** `feat: expose Codex activity through shared dashboard APIs`.

### Task 5: Runtime-aware MCP registration, dispatch, mutation and resume

**Files:** Create `mcp/runtime.go`, `mcp/runtime_test.go`; modify `mcp/tools.go`, `mcp/server.go`, `cmd/stratus/main.go`; create `cmd/stratus/mcp_runtime_test.go`.

**Interfaces:** Add `mcp.RuntimeOptions{Runtime string}` and `RegisterToolsForRuntime(s *Server, apiBase string, client *http.Client, opts RuntimeOptions)`. Keep `RegisterTools` as the legacy wrapper. Add `apiClient.patch(path string, body any) (any,error)`, `apiClient.guardMutation(toolName string, args map[string]any, opts RuntimeOptions) error`, and the `bind_workflow_session` tool with required `workflow_id` and native `session_id`. Codex mode supplies `runtime:"codex"` and `runtime_session_id` and normalizes the internal key itself.

- [x] **1. Write failing tests** `TestRuntimeMCP_RegisterOwnership`, `TestRuntimeMCP_MutationsCheckOwnership`, `TestRuntimeMCP_BindAndDispatch`, and `TestMCPRuntimeFlag_LegacyDefault`:

```go
// A Codex register_workflow call with native session "s1" must send:
if body["session_id"] != "codex:s1" || body["runtime_session_id"] != "s1" || body["runtime"] != "codex" { t.Fatal(body) }
```

- [x] **2. Verify RED:** `go test ./mcp ./cmd/stratus -run 'Test(RuntimeMCP|MCPRuntimeFlag)' -count=1`.
- [x] **3. Implement** `stratus mcp-serve --runtime codex`; reject unknown runtime flags rather than silently selecting legacy. Preserve initialize/tool-list protocol and legacy instructions; Codex initialization gives native-session instructions and no Claude delegation assumptions. Check ownership once before the first request in every workflow mutation, especially `transition_phase`, which writes tasks/plan before phase. Filter dispatch candidates by runtime; an explicit wrong-runtime ID never falls through. New binding calls the PATCH route from Task 1. Shared read-only tools retain their existing behavior.
- [x] **4. Verify GREEN:** focused command and `go test ./mcp ./cmd/stratus -count=1`. Mock API logs must show zero mutation requests after a rejected ownership check. Test missing session, literal `${CLAUDE_SESSION_ID}`, identical native IDs across runtimes, no active workflow, multiple session matches, and unchanged legacy protocol versions. `guardMutation` handles direct workflow IDs and resolves swarm ownership through existing worker/mission routes and mission ticket lists; a missing relationship denies the mutation. Keep memory/wiki tools outside workflow ownership checks. Do not introduce a new swarm execution mode.
- [x] **5. Commit:** `feat: add runtime-aware Codex MCP sessions`.

### Task 6: Codex hook process, project resolution, and guarded operations

**Files:** Create `integrations/codex/hooks.go`, `integrations/codex/policy.go`, `integrations/codex/project.go` and matching `_test.go` files; create `hooks/policy_bridge.go`, `hooks/policy_bridge_test.go`, `cmd/stratus/codex_hook.go`; modify `cmd/stratus/main.go`.

**Interfaces:** `codex.Run(kind string, input io.Reader, stdout, stderr io.Writer, client *http.Client) int`; `ResolveProject(cwd string) (config.Config,error)`; `hooks.DeliveryPolicyInput{Role, Phase, Command, ProjectRoot string, WritePaths []string, Delegation bool, WorkflowResolved bool}`; `hooks.EvaluateDeliveryPolicy(DeliveryPolicyInput) hooks.Decision`. The bridge calls existing pure command/path/role checks and has no HTTP or environment-based ownership lookup. Codex emits its own response schema, never serializes `hooks.Decision` directly.

- [x] **1. Write failing tests** `TestCodexHook_DenialJSON`, `TestCodexHook_TelemetryOutage`, `TestCodexHook_ProjectRootWithSpaces`, `TestCodexHook_UnknownSchema`, and `TestDeliveryPolicy_BridgeParity`:

```go
// PreToolUse of apply_patch by a delivery engineer in verify:
if decision["permissionDecision"] != "deny" || decision["hookEventName"] != "PreToolUse" { t.Fatal(decision) }
// The outer JSON contains hookSpecificOutput; allow responses omit continue:false.
```

- [x] **2. Verify RED:** `go test ./integrations/codex ./hooks -run 'Test(CodexHook|DeliveryPolicy)' -count=1`.
- [x] **3. Implement** `stratus codex-hook <handler>` with handlers `context`, `activity`, `guard`. Resolve `.stratus.json` upward from event `cwd`, honor `project_root`/port and existing environment precedence without `os.Chdir`. Accept an injected HTTP client in tests; production uses a one-second timeout. `context` reads `.codex/stratus/instructions.md`, prints native session identity and owned workflows. `guard` evaluates normalized delivery roles, all patch paths, delegation and shell writes; unresolved owned delivery operations deny, exploration allows. The bridge preserves architect Markdown-only checks and spec verify/bug review restrictions; existing Claude hook decision generation is unchanged. `activity` posts Task 4 payloads and reports failures on stderr, always allowing the host operation.
- [x] **4. Verify GREEN:** focused command plus `go test ./integrations/... ./hooks ./cmd/stratus -count=1`. Cover nested worktree roots, spaces, a non-default isolated port, API timeout, unsupported schema, read-only Bash, Bash writes, mixed docs/source patch, a malformed patch during guarded delivery, and stdout containing JSON only. `PermissionRequest` records waiting but never grants permission. Stop/end never forces a continuation.
- [x] **5. Commit:** `feat: bridge Codex hooks to Stratus policy and telemetry`.

### Task 7: Preserve legacy selection with Codex workflows present

**Files:** Modify `hooks/phase_guard.go`, `hooks/session_start.go`, `mcp/runtime.go`, `cmd/stratus/plugins-opencode/stratus.ts`; extend `hooks/phase_guard_test.go`, `mcp/runtime_test.go`; create `tests/opencode/stratus.test.mjs`.

**Interfaces:** Legacy selection discards only entries with `runtime === "codex"`, including direct workflow fetch results. Do not export a new OpenCode API or copy its resolver into the test. Tests import the real embedded `Stratus` plugin and invoke `tool.execute.before`/`after`.

- [x] **1. Write failing tests** `TestLegacyResolution_ExcludesCodex`, `TestLegacyResolution_ExplicitCodexDenied`, and Node tests `legacy verify remains blocked with a Codex workflow present`, `Codex-only state does not block unrelated legacy edits`, `explicit Codex task cannot bypass filtering`, `legacy dirty-index notification unchanged`:

```js
// Use node:assert/strict and the real plugin callback with mocked fetch.
await assert.rejects(before({tool:"edit",sessionID:"legacy-s"},{args:{}}), /verify/);
await assert.rejects(before({tool:"task",sessionID:"legacy-s"},{args:{subagent_type:"delivery-backend-engineer",prompt:"Workflow ID: spec-codex"}}));
```

- [x] **2. Verify RED:** `go test ./hooks ./mcp -run '^TestLegacyResolution_' -count=1` and `node --test tests/opencode/stratus.test.mjs`. Node 24's native TypeScript loading handles the source's erasable types; the plugin's `import type` adds no runtime dependency.
- [x] **3. Implement** the minimum runtime filter at dashboard candidate collection and direct ID lookup in all three legacy resolution paths. Preserve session precedence, ambiguity errors, architect policies, fallback among legacy workflows, and non-Stratus behavior. Filter Codex workflows from the legacy session-start continuation notice so it never recommends a Claude command for them. Test fetch mocks must reject unexpected URLs; never make live requests. Do not edit the installed `.opencode/plugin/stratus.ts` by hand. Record the compatible embedded plugin hash for Task 9's installation preflight; an older installed copy is not upgraded merely because the binary changed.
- [x] **4. Verify GREEN:** both commands and `go test ./hooks ./mcp ./cmd/stratus -count=1`. Compare plugin behavior with only legacy fixtures before and after adding unrelated Codex fixtures. Test quoted workflow labels, inactive explicit IDs fetched by API, and wrong-runtime direct fetches.
- [x] **5. Commit:** `fix: isolate legacy workflow resolution from Codex`.

### Task 8: Merge Codex configuration without losing user content

**Files:** Create `cmd/stratus/codex_config.go`, `cmd/stratus/codex_config_test.go`; modify `go.mod`, `go.sum`.

**Interfaces:** `mergeCodexTOML(src []byte) ([]byte,error)` and `mergeCodexHooks(src []byte, generated []byte) ([]byte,error)`. Return original-equivalent bytes for an already-current config; reject conflicting Stratus configuration before disk writes. These functions are pure; Task 9 owns persistence.

- [x] **1. Write failing tests** `TestCodexConfig_PreservesSource`, `TestCodexConfig_DisabledEntry`, `TestCodexConfig_AlternateTOML`, `TestCodexConfig_Conflict`, `TestCodexHooks_IdempotentMerge`. Preserve an exact byte prefix:

```go
src := []byte("# user comment\r\nmodel = 'user-choice'\r\n")
got, err := mergeCodexTOML(src)
if err != nil || !bytes.HasPrefix(got, src) { t.Fatal(string(got), err) }
```

- [x] **2. Verify RED:** `go test ./cmd/stratus -run 'TestCodex(Config|Hooks)_' -count=1`.
- [x] **3. Implement** after adding `github.com/pelletier/go-toml/v2@v2.4.3`. Validate TOML with the semantic decoder; use parser source ranges for precise managed edits, never reserialize the entire user document. Add `command="stratus"`, `args=["mcp-serve","--runtime","codex"]` only under `mcp_servers.stratus`; preserve matching entries' extra keys and `enabled=false`. A different executable/URL or inline Stratus hook definition conflicts. Hook ownership is a parsed command exactly invoking `stratus codex-hook context|activity|guard`, optionally with its quoted absolute binary path, not a substring containing `stratus`. Preserve unrelated JSON fields and handler order. Handle quoted/dotted keys and multiline/inline forms semantically; if an existing user-owned structure cannot be safely extended, return a path-specific conflict with unchanged bytes.
- [x] **4. Verify GREEN:** focused command and `go test ./cmd/stratus -count=1`. Include invalid TOML/JSON, repeated merge, inline hooks, user hook alongside managed handler, shell commands merely mentioning Stratus, CRLF, comments, multiline arrays, disabled MCP, conflicting command, and syntactically valid alternate table forms.
- [x] **5. Commit:** `feat: preserve user settings when merging Codex config`.

### Task 9: Install and refresh the standalone Codex resources

**Files:** Create `cmd/stratus/codex.go`, `cmd/stratus/codex_test.go`, `cmd/stratus/codex-assets/instructions.md`, and `cmd/stratus/codex-assets/workflows/{spec,bug,e2e,resume}.md`; modify `cmd/stratus/main.go`, `README.md`.

**Interfaces:** `installCodex(root string, refresh bool) error` coordinates Tasks 6 and 8 plus embedded assets. `writeCodexSyncState(src []byte, hashes map[string]string, skipped []string) ([]byte,error)` updates only the Codex sync entries in raw shared configuration. Existing `writeAssetsTo` provides the three-way policy; Codex hashes start with `codex-assets/`.

- [x] **1. Write failing tests** `TestCodexInstall_ExistingProjectPreserved`, `TestCodexInstall_DefaultTargetsUnchanged`, `TestCodexRefresh_CustomizedAsset`, `TestCodexInstall_PreflightFailureWritesNothing`, `TestCodexInstall_OldOpenCodePluginNeedsRefresh`, and `TestCodexInstall_PartialWriteCanResume`:

```go
// Fixture .stratus.json contains port 42881, custom data_dir, a fake secret,
// unknown keys, and legacy sync hashes; init with --target codex and --force.
if err := installCodex(root, false); err != nil { t.Fatal(err) }
// shared is the decoded .stratus.json after installation.
if shared["port"] != float64(42881) || shared["fixture_secret"] != "fake-value" { t.Fatal(shared) }
// Legacy hashes and .claude/.opencode files must also remain unchanged.
```

- [x] **2. Verify RED:** `go test ./cmd/stratus -run 'TestCodex(Install|Refresh)_' -count=1`.
- [x] **3. Implement** a Codex branch before the legacy existing-config early return/reset. Preflight both config merges and all destinations; write through temporary files and atomic rename. If `.opencode/plugin/stratus.ts` exists but differs from the compatible embedded plugin hash, stop before writes and name the prerequisite `stratus refresh --target opencode`; customized copies remain untouched and require review through the existing sync process. This reports a conflict rather than claiming coexistence with an unknown installed plugin. Start an asset hash map from existing entries; preserve unknown JSON keys, skip customized files on init as well as refresh, and report skips. Install missing initial files without Docker, LLM, Vexor indexing, browser startup, or server startup. Generate SessionStart/context, PreToolUse/guard, and remaining supported lifecycle/activity registrations exactly once. Set hook process timeout to three seconds; telemetry's HTTP limit remains one second.
- [x] **4. Add instruction references and verify GREEN:** document Superpowers phase mapping, explicit native session registration, binding on resume, discovered MCP names, native-browser URL using `stratus port` and `?view=dashboard`, unavailable-capability reporting, and user-owned trust setup. Explain that installed hooks may need review/activation; do not enable features automatically. Document disabling only managed hooks/MCP. Run focused tests and `go test ./cmd/stratus -count=1`; snapshot all legacy asset output except Task 7's filter. A subprocess CLI matrix covers default, `both`, `claude-code`, `opencode`, `codex`, repeated init and refresh, with/without `--force`.
- [x] **5. Commit:** `feat: install and refresh the standalone Codex adapter`.

### Task 10: Show Codex workflow status without an embedded terminal

**Files:** Create `frontend/src/lib/runtime.ts`, `frontend/src/lib/runtime.test.ts`, `frontend/src/App.test.ts`, `frontend/src/components/AgentTranscript.test.ts`, `frontend/vitest.config.ts`; modify `frontend/src/App.svelte`, `frontend/src/lib/types.ts`, `frontend/src/lib/api.ts`, `frontend/src/routes/Overview.svelte`, `frontend/src/components/{GuardianWidget,AgentCard,AgentTranscript,WorkflowDetail,MissionControl}.svelte`; update test dependencies in `frontend/package.json`, `frontend/package-lock.json`.

**Interfaces:** `isDashboardOnly(search: string): boolean`; `continuationText(wf: WorkflowState): string`; `usageLabel(usageAvailable: boolean | undefined, input: number, output: number): string`. Add optional `runtime`, `runtime_session_id`, `usage_available` to corresponding TS types; `AgentTranscript.usage` accepts `null`. Use Svelte's `mount`/`unmount` and `jsdom` for DOM tests. The new Vitest config merges the existing Vite config and enables Svelte's browser resolution condition only for tests; DOM test files select `jsdom` via per-file environment, while existing pure tests keep the node environment.

- [x] **1. Write failing tests** `dashboard view never mounts Terminal`, `remembered terminal tab still has no PTY`, `Codex continuation has no Claude syntax`, `finished Codex alert resolves its runtime`, and `unknown usage renders unavailable`:

```ts
expect(isDashboardOnly('?view=dashboard')).toBe(true)
expect(continuationText(codexWorkflow)).toBe('Continue Stratus workflow spec-codex')
expect(usageLabel(false, 0, 0)).toBe('Usage unavailable')
// Mock Terminal's module: assert mount count is zero, not merely hidden CSS.
```

- [x] **2. Verify RED:** `npm --prefix frontend run test -- src/lib/runtime.test.ts src/App.test.ts src/components/AgentTranscript.test.ts`. Add `jsdom` as a development-only dependency for these DOM tests, preserving the lockfile. Failures must concern missing behavior rather than an unconfigured DOM.
- [x] **3. Implement** full-width dashboard mode by conditionally never mounting the PTY subtree; preserve the ordinary URL's always-mounted terminal and split state. Add Codex badges and route all continuation/clipboard actions, including Guardian alerts, through runtime-aware text. For a workflow absent from the active snapshot, fetch its existing detail API before offering an action; while unresolved, offer only a generic workflow link. Keep legacy actions unchanged. Use Task 4's normalized transcript projection, no Claude parser fallback. Render missing Codex usage/report as unavailable in cards, detail, transcript, and mission control. Keep the Claude/OpenCode asset editor unchanged.
- [x] **4. Verify GREEN:** focused command, then `npm --prefix frontend run test`, `npm --prefix frontend run check`, and `npm --prefix frontend run build`. Test ordinary layout, tab switching without remount, saved terminal state, unavailable storage, completed workflow alerts, empty steps, and `usage:null` without errors. Review generated `cmd/stratus/static/` output; rebuild Go only after frontend build.
- [x] **5. Commit** frontend files and generated embedded assets: `feat: show Codex workflows in a dashboard-only browser view`.

### Task 11: Verify coexistence, live host behavior, and recovery

**Files:** Create `cmd/stratus/codex_e2e_test.go`, `docs/testing/codex-adapter.md`; modify only documentation or fixes justified by failed acceptance checks. No production install/refresh in this checkout for validation.

**Interfaces:** Integration tests invoke a newly built binary in a temporary project, with temporary `STRATUS_DATA_DIR`, a loopback listener on port 0 via a test fixture, and a real stdio MCP exchange. Produce `docs/testing/codex-adapter.md` with client versions, exact isolated commands, observed event fields/capabilities, AC results, and recovery steps; sanitized live payloads can update Task 2 fixtures.

- [x] **1. Write failing integration cases** `TestCodexE2E_MCPAndEvents`, `TestCodexE2E_MixedRuntimes`, `TestCodexE2E_StopDoesNotComplete`, and `TestCodexE2E_DisableLeavesLegacyUsable`. Assert a real protocol round trip and persisted state:

```go
// initialize → tools/list → register_workflow(s1) → lifecycle/tool events.
if codexState.Runtime != "codex" || codexState.SessionID != "codex:s1" { t.Fatal(codexState) }
// Stop leaves phase unchanged; both legacy fixture sessions still select their own work.
```

- [x] **2. Verify RED:** run `go test ./cmd/stratus -run '^TestCodexE2E_' -count=1` before completing the harness assertions. If a new case passes immediately, verify its assertion against the pre-feature checkout or temporarily remove the relevant feature locally, then restore it; do not manufacture a failure by breaking unrelated setup.
- [x] **3. Verify the complete automated gates:** run `go test ./...`, `node --test tests/opencode/stratus.test.mjs`, the three frontend commands from Task 10, and build `./cmd/stratus` to a temporary output path. Expected: zero test failures, no type-check errors, a successful frontend build, and a fresh binary containing that build. Rerun only affected gates after any repairs, then confirm the complete required set has passed on the final changes.
- [x] **4. Run isolated live acceptance and record outcomes:** create a disposable repository/data directory and choose a free dedicated port, never 41777. Put the freshly built executable named `stratus` first on PATH for these test sessions, so generated MCP/hook commands cannot run an older globally installed binary. Start the built server there and install all three integrations explicitly. In fresh trusted Claude Code, OpenCode, and Codex sessions, verify MCP initialization, owned workflow/delegation selection, supported guards, and continuation after a guard denial. In Codex, observe real lifecycle/tool payloads and a browser update without reload; instrument the test server's terminal creation handler to establish zero calls in dashboard mode. Confirm no Claude transcript reads. Disable only Codex managed entries, start fresh legacy sessions, and repeat their smoke checks. If trust, credentials, events, or agent identity prevent a check, record it as unverified and withhold the affected AC; do not claim full compatibility from fixtures alone. Follow host-required trust prompts through the user.
- [x] **5. Commit** verification documentation and any independently verified repairs: `test: verify Codex and legacy adapter coexistence`. Run `superpowers:requesting-code-review` under the selected execution method before integration; resolve actionable findings and repeat their relevant checks. Leave release, global installation, and server replacement outside this plan.

## Acceptance coverage

| Spec criterion | Implemented and verified by |
| --- | --- |
| AC1 installation without shared settings reset | Tasks 8, 9, 11 |
| AC2 safe repeat init/refresh and customization | Tasks 8, 9 |
| AC3 legacy target and plugin contracts | Tasks 1, 5, 7, 9, 11 |
| AC4 stdio MCP and correct project API | Tasks 5, 6, 11 |
| AC5 native identity, ownership, resume isolation | Tasks 1, 5, 7, 11 |
| AC6 lifecycle, failure, interruption, deduplication | Tasks 2, 3, 4, 6, 11 |
| AC7 guarded writes and usable exploration | Tasks 2, 6, 7, 11 |
| AC8 live browser status and no terminal | Tasks 4, 10, 11 |
| AC9 existing state machine and verified completion | Tasks 1, 6, 9, 11 |
| AC10 all three clients coexist and recover | Tasks 7, 9, 11 |

## Execution handoff

Review this plan before implementation. Recommended method: **Native** execution through `superpowers:executing-plans`, because the eleven tasks depend on shared ownership and event interfaces and should be implemented sequentially. Complete the required whole-branch independent review afterward. The alternative is **Subagent-driven** execution through `superpowers:subagent-driven-development`, with separate implementer/reviewer passes per task and a final branch review.

The approved specification does not approve this newly written plan or select an execution method. Preserve the user's eventual choice and follow it without asking again at each task.

Dependency reference: [go-toml v2.4.3 release](https://github.com/pelletier/go-toml/releases/tag/v2.4.3) and [parser source-range API](https://pkg.go.dev/github.com/pelletier/go-toml/v2/unstable). Node 24.13.1 and Go 1.26.0 were available during planning; Node 24 is a test-runner prerequisite here, not a new binary runtime dependency.
