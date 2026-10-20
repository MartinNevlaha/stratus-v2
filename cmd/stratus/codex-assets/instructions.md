# Stratus in Codex

Use the existing Stratus MCP connection by its discovered tool names. Superpowers owns design, approval, planning, implementation, and review; Stratus records phases, tasks, memory, and observed activity. Do not duplicate Superpowers approval gates.

Register a workflow before tracked delivery. Pass the exact native Codex session ID from SessionStart as `session_id`; never use a Claude environment placeholder. Use `bind_workflow_session` with the explicit workflow ID and current native session ID when continuing a Codex-owned workflow in a new session. Cross-runtime migration is not supported.

Read the relevant project reference before coordinating: `.codex/stratus/workflows/spec.md`, `bug.md`, `e2e.md`, or `resume.md`. Retrieve relevant code, governance, and wiki context before planning or reviewing. Advance phases only through the discovered Stratus tools. Mark tasks done only after the actual work and verification; ending a turn does not complete a task or workflow.

When explicitly delegating a delivery role, put these separate lines at the top of its brief:

Workflow ID: <existing-workflow-id>
Delivery role: delivery-<role>

Use only native delegation capabilities actually available. Do not assume Claude agent profiles, slash commands, or permission interfaces exist. Native default workers can be observed but are not automatically classified as delivery roles. A worker receiving SubagentStart identity context must call `bind_agent_role` with its exact native parent session ID, native worker ID, workflow ID, and explicit delivery role from its readable brief. Do not use a canonical agent path as a substitute for the native ID. Bind before tracked delivery work. Reused turns keep the same bound role; a conflicting role or workflow needs a separate native worker. Opaque delegation messages are unavailable to hooks and must not be decoded or guessed.

Reviewers must return an explicit `Verdict: PASS` or `Verdict: FAIL` and label findings `[must_fix]`, `[should_fix]`, or `[suggestion]`. The dashboard marks newer reviews without an explicit verdict as unverified, and work after review as outdated. A historical PASS does not approve current work. Hooks are guardrails for identifiable operations, not a complete enforcement or monitoring boundary. Missing per-tool agent identity and token usage are unavailable; do not invent them or parse Codex transcripts to fill gaps.

For status, get the configured port with `stratus port` from this project, then open `http://localhost:<port>/?view=dashboard` through the host's native browser capability. Keep that dashboard beside the conversation when useful. If no browser capability exists, provide the clickable URL. Do not start a second server automatically; if the API is unavailable, explain that `stratus serve` must be running in this project. Project MCP and hooks may need trust review and activation in a new session; do not change permissions or global settings yourself.
