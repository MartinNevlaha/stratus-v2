# Codex adapter verification

Verified on 2026-10-20 in an isolated managed worktree. The existing checkout,
installed adapters, and server on port 41777 were not used as test targets.

## Automated evidence

The full Go suite, executable OpenCode plugin suite (4 tests), and frontend suite
(7 files, 38 tests) passed. Svelte checking reported zero errors and 52 existing
warnings in four files. The frontend production build and a fresh Go executable
build passed; the existing large-chunk build warning remains.

```sh
go test ./...
node --test tests/opencode/stratus.test.mjs
npm --prefix frontend run test
npm --prefix frontend run check
npm --prefix frontend run build
go build -o /tmp/stratus-codex-verification ./cmd/stratus
```

`TestCodexE2E_*` builds the executable, starts the real API against temporary
SQLite data on a free loopback port, and exchanges real newline-delimited JSON-RPC
with its stdio MCP server. It covers ownership, two runtimes with identical native
IDs, lifecycle delivery through the hook executable, Stop without completion,
disabling Codex while retaining legacy files/MCP, and the CLI target matrix.
The MCP ownership case failed against a binary built from the pre-feature commit
`883bca0`, then passed against the implementation. Fixtures never use port 41777.

Unit tests additionally cover receipt rollback/concurrent duplicates, out-of-order
lifecycle, guarded writes and outages, nested project paths, configuration source
preservation/conflicts, disabled handlers, customized assets, and completed-workflow
continuation. Claude transcript handling remains covered by the existing suite.

## Browser evidence

The native Codex browser opened the fixture's `/?view=dashboard`. It displayed the
Codex workflow and its generic continuation text. Posting an identified agent-start
event to the fixture API made its reviewer row appear through the live WebSocket
without a reload. The row displayed `Usage unavailable`. Test-only middleware
counted **zero** requests to `/api/terminal/ws` across the session. This proves the
browser/API path, not that the installed Codex host emitted that event.

To repeat, keep this opt-in harness running while using the native browser:

```sh
STRATUS_BROWSER_MANIFEST=/tmp/stratus-codex-browser.json \
  go test ./cmd/stratus -run '^TestCodexE2E_LiveBrowser$' -count=1 -v
```

The manifest provides `root`, `bin`, `data_dir`, `port`, and `url`. The harness
initializes a disposable Git repository and installs `both` then `codex`. Prepend
the manifest executable's directory to PATH for every child client; export
`STRATUS_DATA_DIR` and `STRATUS_PORT` using the manifest values. It expires after
eight minutes. `/__qa/status` returns the terminal-request counter and
`/__qa/complete` finishes it; both endpoints exist only in the test harness.

An example normalized event for its seeded workflow is:

```json
{"event":"agent_start","session_id":"browser-session","agent_id":"browser-agent","agent_type":"reviewer","turn_id":"browser-turn","summary":"Live browser event verification"}
```

POST it as JSON to the manifest URL plus `/api/integrations/codex/events`. No raw
transcript or token usage is required. Only use this synthetic identity in the
disposable harness.

## Actual client probes and limits

Installed clients: Codex `0.162.0-alpha.17.2`, Claude Code `2.1.304`, OpenCode
`1.18.29`; Go `1.26.0`, Node `24.13.1`. Each probe ran with the fixture executable
first on PATH and its dedicated data directory and port, without model, trust,
feature, or permission overrides:

```sh
codex exec --json --skip-git-repo-check -C "$fixture_root" "$probe"
claude -p --output-format json --max-turns 2 "$probe"
opencode run --format json --dir "$fixture_root" "$probe"
```

`probe` requested a read-only report of available Stratus MCP tools and active
project hooks, forbidding edits, workflow creation, and shell commands. These were
availability probes, not delegation/guard acceptance tests. Claude Code exposed
the Stratus tools including `register_workflow`. OpenCode made a successful
read-only Stratus status call and exposed registration. Neither probe established
that its delivery guards were active. Codex exposed no Stratus tools in that
session. `codex mcp list --json` also contained no Stratus entry in a newly
initialized disposable Git repository with the adapter installed.

The disposable project had not gone through user-owned project/hook trust review.
That is a likely reason for unavailable Codex project configuration, not a proven
diagnosis of this client build. Codex documents that project configuration requires
[project trust](https://learn.chatgpt.com/docs/config-file/config-basic) and hooks
require [review of their exact definition](https://learn.chatgpt.com/docs/hooks).
No trust bypass or global configuration modification was used. No actual Codex
hook payload, native agent identity, accepted denial, or post-denial continuation
was observed. Checked-in payloads are contract fixtures, not captured live events.

| Acceptance criterion | Result |
| --- | --- |
| AC1–AC3 | Automated installation, preservation, target and legacy filtering contracts passed |
| AC4 | Real stdio/API transport passed; actual Codex host discovery remains unverified |
| AC5 | Ownership/resume isolation passed through real MCP and API fixtures |
| AC6 | Adapter contract and persistence passed; **live host acceptance withheld** |
| AC7 | Guard contract tests passed; **live denial/recovery acceptance withheld** |
| AC8 | Native browser/WebSocket and zero-terminal smoke passed |
| AC9 | Existing transition tests and Stop non-completion passed |
| AC10 | Mixed-runtime/recovery fixtures passed; **full three-client live acceptance withheld** |

## User-owned activation and recovery

After reviewing/installing the current binary in a chosen project, refresh an
existing OpenCode plugin with `stratus refresh --target opencode` before installing
Codex. A customized plugin produces an explicit prerequisite conflict: review its
changes instead of overwriting it. Run `stratus init --target codex`, start the
project server, and open a fresh Codex session. Review the project through its
normal trust flow and use `/hooks` to review the generated definitions. Installation
alone does not prove activation. Do not change disabled user settings to make a
check pass.

Before release, use fresh trusted sessions to register separate owned workflows,
exercise supported delivery tools and denial/continuation, and observe actual hook
fields in the dashboard. Verify native session/turn/agent attribution before
enabling corresponding monitoring claims; absence is unavailable, never inferred.
Repeat legacy smoke checks after disabling only Codex's managed entries.

For rollback, set `enabled = false` on `.codex/config.toml`'s
`mcp_servers.stratus` table, and remove only handlers whose commands are exactly
`stratus codex-hook context`, `stratus codex-hook guard`, or
`stratus codex-hook activity` from `.codex/hooks.json`. Preserve other handlers,
instructions, `.stratus.json`, `.claude/`, and `.opencode/`. Restart the clients.
For an interrupted installation, resolve the reported conflict and rerun the same
Codex init/refresh; writes are atomic per file and repeated merges are idempotent.
