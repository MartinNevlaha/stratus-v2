# Managed automatic project teams

**Spec:** `docs/superpowers/specs/2026-11-04-automatic-project-team-design.md` (approved 2026-11-04).
**Goal:** Launch a project team, submit ordinary tasks, route by role/model, automatically deliver relevant results into owned native sessions without polling models.
**Baseline:** branch `codex/automatic-project-team`, commit `dab2507`; `go test ./...` passes.

## Constraints and interfaces

Reuse this clean feature checkout. Preserve existing workflow phase graphs and owner checks, project overrides, native permission prompts and existing sessions. No live UVO traffic. Exact Codex templates: GPT-6.1 Sol/high implementation and lead, GPT-6 Astra/high design/review/diagnosis (xhigh for complex/spec-complex), GPT-6 Luna/medium mechanical. No silent fallback. No LLM for routing or waiting.

The new `teams` package owns durable state and runtime-neutral contracts, and imports only standard library plus uuid. `teamruntime` imports `teams`, implements native transports and never imports api. `api` wires both with `db.SQL()`. Do not mutate orchestration phase graphs.

Native contract (Go exported JSON fields use snake_case):
- `Profile {Runtime, Model, Effort string}`; `SessionSpec {ProjectRoot, TeamID, MemberID, NativeID, Instructions string; Profile Profile}`.
- `Input {ID, Text string}`; `Result {Text, NativeID, Model, Effort string; InputTokens, OutputTokens *int64}`.
- `Event {Kind, ID, Text, NativeID, Model, Effort string}`; kinds `output`, `approval`, `status`, `identity`.
- `Availability {Runtime string; Available bool; Reason string; Models []Model}`; `Model {ID string; Efforts []string}`.
- `Adapter` methods `Preflight(context.Context, string) Availability`, `Open(context.Context, SessionSpec, func(Event)) (Session,error)`.
- `Session` methods `Submit(context.Context, Input) (Result,error)`, `Approve(context.Context, string, bool) error`, `Interrupt(context.Context) error`, `Close() error`.

Manager public contract:
- `NewManager(*sql.DB, map[string]Adapter, func()) (*Manager,error)`; recovery loads teams paused, uncertain running jobs need user decision.
- `Create(ctx, CreateRequest) (*Team,error)`, `List(projectID string) ([]Team,error)`, `Get(id string) (*Team,error)`, `Action(ctx,id,action string) (*Team,error)` (`pause`, `resume`, `stop`), `Enqueue(ctx,id string, TaskRequest) (*Task,error)`, `Approve(ctx,teamID,memberID,approvalID string,allow bool) error`, `Close() error`.
- `CreateRequest {ProjectID,ProjectRoot string; Members []MemberConfig; Limits Limits}`; `MemberConfig {ID,Role string; Profile Profile}`.
- `TaskRequest {ID,From,To,Role,Text,WorkflowID,WorkflowType,Complexity,Revision string; Profile *Profile}`. `From` is `user` or authenticated member ID; To optional; native session maps to member in API. Parent job/return target preserved for automatic results. Request IDs deduplicate. API enforces workflow owner; manager resolves exact compatible role/profile and creates owned worker member when a selected runtime needs another profile.
- `Team {ID,ProjectID,ProjectRoot,State string; Members []Member; Tasks []Task; Limits Limits; ...}`. `Member {ID,Role string; Profile Profile; NativeID,State,ObservedModel,ObservedEffort,Error string; Approval *Event; ...}`. `Task {ID,From,To,Role,Text,WorkflowID,WorkflowType,Complexity,Revision,State,Result,Error string; ...}`. Add fields as needed, notify root of additions.
- `Limits {MaxConcurrent,MaxAutoRounds,MaxTurns int; MaxTokens int64}`. Unknown native usage remains unknown, turn budget always enforced. Defaults conservative 2/4/40; hard caps validated.
- `DefaultProfile(runtime,role,workflowType,complexity string) Profile`; caller-specified/project role overrides win.

## Task 1: durable team manager and policy

Owner: core implementer. Files: `teams/*.go` only.
1. Write failing tests for dedup, exact routing/override, idle zero Submit calls, serialized native sessions, auto result delivery, pause/stop, persisted restart paused, ambiguous in-flight jobs, max turns/rounds/tokens, approval ordering.
2. Implement SQL snapshot persistence under a manager mutex, event-driven workers with one turn per member and bounded global concurrency, explicit failed/needs_user states. Persist before submission and after completion; never replay ambiguous accepted work automatically. Bounded compact handoffs with visible truncation and artifact references, separate reviewer contexts, caller scope validation.
3. Run `go test -race ./teams`. Expected: all tests pass including actual concurrent fixtures; no idle model work. Supply public interfaces to API and adapter implementers early.

## Task 2: native runtime adapters

Owner: native implementer. Files: `teamruntime/*` only.
1. Write subprocess/HTTP protocol fixture tests first for two turns same native identity, output vs accepted vs completed, permission request/answer, cancellation, unsupported model/effort and transport failure.
2. Implement owned Codex app-server JSON-RPC, Claude streaming CLI/SDK supported control protocol, owned OpenCode loopback HTTP+events. Investigate installed protocol with schema/help; do not attach live sessions. Model/effort validation before dispatch; unknown or unsupported exact choices require user, no fallback. No bypass flags. All owned processes cleaned on stop; shutdown cannot kill unrelated processes.
3. Expose `Adapters() map[string]teams.Adapter`. Run `go test -race ./teamruntime`. Expected: passing protocol acceptance/recovery fixtures; readiness reports errors honestly.

## Task 3: project-scoped existing Comms

Owner: isolation implementer. Files: `projectidentity/*`, `db/comms*.go`, comms migrations in `db/db.go`, `api/routes_comms.go`, `mcp/comms.go`, `hooks/{comms,session_start}.go`, `integrations/codex/comms.go`, OpenCode plugin registration and focused tests.
1. Failing isolation tests for same basename/different roots, verified worktrees, cross-project handles/runtime/broadcast/task access and legacy unknown peers.
2. Add canonical project identity helper `projectidentity.Resolve(root string) (id,canonical string,err error)`; register actual root from runtimes. Session-derived filtering enforced in server/db; old history readable in dashboard but legacy unknown peers not automatically grouped. Keep old registration compatible without trusting basenames. Expose scoped helper methods to root.
3. `go test ./projectidentity ./db ./hooks ./integrations/codex ./api ./mcp`. Expected: isolated groups and existing tests preserved or adjusted only for deliberate scoped behavior.

## Task 4: API, MCP, lifecycle and dashboard

Owner: root. Files: `api/routes_teams*.go`, `api/server.go`, `mcp/teams*.go`, MCP registration sites, `frontend/src/routes/Team.svelte`, frontend api/store/nav, server shutdown site.
1. Write failing API/MCP tests for create/actions/status/delegation, unknown peers, cross-team scope, workflow ownership, stale revision and denied worker transitions.
2. Wire manager, recover on startup, shutdown owned sessions. Add `/api/teams` list/create, `/api/teams/preflight`, `GET /api/teams/{id}`, `POST /api/teams/{id}/actions`, `/tasks`, `/approvals`; compact agent endpoints `/api/team-context`, `/api/team-delegate`, `/api/team-message` derive team/member from native session key. MCP tools team_status/team_delegate/team_message carry only concise relevant context; no model wait tool. Results queued back to requester, never transition workflow by themselves.
3. Dashboard Team tab: readiness and editable roles/models before launch, start/pause/resume/stop, conversation/task input, per-member requested/observed models, queue/results, usage unknown vs measured, limits, native permission approval. Websocket updates + reconnect reconciliation; browser closure leaves server running.
4. `go test ./api ./mcp`; `cd frontend && npm run check && npm test && npm run build`. Expected: passes, responsive actionable states, no fabricated readiness or usage.

## Task 5: acceptance, documentation and review

1. Read implementation interfaces together, integrate project profiles, workflow delegate grants and independent review revision checks; fix integration gaps with regression tests.
2. Run whole Go suite and focused race tests; frontend checks/tests/build; `go build -o /tmp/stratus-managed-team ./cmd/stratus` after frontend build. Isolated bounded native preflight/smoke, no existing project sessions used. Do not label unexercised native behavior verified.
3. Update user docs with launch flow, models, savings mechanisms, recovery and measured limitations. Fresh independent final review focuses on lost/duplicate work, cross-project/owner leakage, token loops, permission responses, process cleanup, runtime real protocol fidelity, preserving project model overrides. Fix material findings and rerun impacted tests.
4. Commit reviewable changes on feature branch; report evidence and remaining runtime limitations precisely. No claim of quantified token savings without comparable measurements.

## Progress

- Baseline Go suite passed before changes.
- Tasks 1–4 implemented: durable manager, three native transports, project isolation, API/MCP, dashboard and scoped workflow evidence.
- Native smoke: six total minimal prompts across three new isolated native sessions; second turn retained ID and context in all three. No existing UVO sessions used. Permission flows additionally exercised with protocol fixtures.
- Independent final review (GPT-6 Astra) found three Important integration defects: Claude alias/result comparison, missing member recovery, direct complex-worker followup routing. All addressed with regression tests; requested vs observed identity remains distinct, recovery preserves uncertain task history, direct followups retain the selected profile and conversation.
- Recovery additionally handles an uncertain native permission response by closing the owned transport without resending the decision. Requires an explicit dashboard action.
- UI checked in a disposable local server: readiness for all three runtimes, exact profile table, create team with zero model turns, stop team with zero model turns. No production restart.
- Frontend checks and 55 tests passed; production bundle built. Existing frontend warnings remain outside this change.
- Go package and focused race suites passed; final post-review verification recorded below.
- No quantified savings claimed: native context overhead still exists; routing, waiting, coalescing and budgets require no additional routing model.
- One deliberate platform limit: native team launching is unavailable on Windows until owned descendant process cleanup is supported. Linux native lifecycle was exercised.

### Final verification, 2026-11-04

- `go test ./...`: PASS after final fixes.
- `go test -race ./teams ./teamruntime`: PASS after final fixes.
- Broader `go test -race ./projectidentity ./api ./mcp ./db ./orchestration`: PASS.
- `go vet ./teams ./teamruntime ./api ./mcp ./projectidentity`: PASS.
- Frontend `npm run check`: 0 errors (52 existing warnings in unrelated components); `npm test`: 55/55; `npm run build`: PASS.
- `go build -o /tmp/stratus-managed-team ./cmd/stratus`: PASS after frontend rebuild.
- Source whitespace check: PASS (`git diff --cached --check -- . ':!cmd/stratus/static/assets/*'`). Generated minified Svelte bundle contains a whitespace character-set literal; it is retained byte-for-byte from the successful build.
- Kept the existing feature checkout; no merge, push, production restart or global model changes.
