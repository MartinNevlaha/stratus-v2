# Stratus Fix Report: Exact-ID Runtime Activation

Workflow: `spec-exact-id-retrieval-coverage`
Phase: `implement`
Task: `9 - Approve and enable typed runtime routing`
Date: `2026-07-11`

## Current State

Task 9 was completed after a post-restore replacement gate was created because the original historical PostgreSQL fixture scope was removed by database restore.

Gate artifact:

`tests/eval/reports/exact_id_coverage/20260711T142700Z_post_restore_replacement_gate.json`

Operator approval was given in chat with `ok` for decision-ID-only activation using the post-restore gate. Runtime routing is enabled for decision exact identifiers only.

## Resolution Applied

- Durable approval was recorded in the post-restore gate artifact as `approved_decision_id_only`.
- Decision exact identifiers now enter the typed parser-first, PostgreSQL-first lookup route when PG/Qdrant deps are available.
- Method-guideline identifiers remain unactivated by this gate and continue through existing fallback/legacy behavior.
- Qdrant collection selection is derived from the resolved RLS-authorized KB identity.
- Agent citation extraction now accepts typed top-level `citation_hints` from `lookup_decision`/`lookup_decision_by_id` results.
- Registry and agent-registry dispatch forward multi-KB scope without changing public tool names or graph topology.

Verification:

```bash
uv run pytest tests/uvo_rag_api/graph/tools/test_exact_id_lookup_adapter.py tests/uvo_rag_api/graph/tools/test_lookup_decision_by_id.py tests/uvo_rag_api/graph/tools/test_agent_envelope.py tests/uvo_rag_api/graph/tools/test_agent_registry.py tests/uvo_rag_api/graph/tools/test_registry.py
uv run pytest tests/uvo_rag_api/graph/tools/test_exact_id_lookup.py
jq empty tests/eval/reports/exact_id_coverage/20260711T142700Z_post_restore_replacement_gate.json
git diff --check
```

Results: focused routing suite `115 passed`; exact-ID core suite `41 passed`; JSON validation passed; diff whitespace check passed.

## Valid Gate Evidence

The replacement gate validates current post-restore decision data:

- `6537-6000/2019-OD`: healthy, one scoped PG match, indexed, Qdrant originals present.
- `12951-6000/2024-OD`: healthy, one scoped PG match, indexed, Qdrant originals present.
- `8553-6000/2026-OD`: healthy, one scoped PG match, indexed, Qdrant originals present.

RLS checks passed for unrelated scope with zero visible KBs/documents/chunks. Qdrant authenticated read through the API container succeeded.

Historical representatives are now absent due to restore and should be classified as `FIXTURE_SCOPE_NOT_LOADED_AFTER_RESTORE`, not dependency failure:

- `5633-6000/2024-OD`
- `11049-6000/2019-OD`
- `14671-6000/2021-OD`
- `5207-6000/2025-OD`

## Activation Scope

Enable typed runtime routing for decision exact identifiers only.

Do not enable newly-gated method-guideline behavior from this gate. The restored PostgreSQL currently has zero method-guideline documents, so there is no live representative method-guideline coverage.

## Must Fix Before Activation

Status: resolved by the implementation above.

1. Update durable approval evidence.

File:

`tests/eval/reports/exact_id_coverage/20260711T142700Z_post_restore_replacement_gate.json`

Issue:

The artifact still has:

```json
"operator_approval_status": "pending_explicit_approval"
```

Expected:

Record the explicit chat approval and scope, for example:

```json
"operator_approval_status": "approved_decision_id_only",
"operator_approval_source": "chat_ok_2026-07-11",
"operator_approval_scope": "post_restore_decision_exact_id_runtime_activation_only; method_guideline_live_gate_not_covered"
```

2. Fix Qdrant collection selection in typed adapter.

File:

`uvo_rag_api/graph/tools/exact_id_lookup_adapter.py`

Issue:

The adapter currently requires caller-supplied `qdrant_collection`. Authorized identity can resolve across multiple KBs, so the Qdrant collection must derive from the resolved, RLS-authorized KB/document identity. It must not come from model input, a primary arbitrary `kb_id`, or the first KB in scope.

Expected:

Resolve document identity under trusted team/KB scope first, then derive the Qdrant collection from the resolved authorized KB metadata before context lookup.

3. Enforce decision-only activation.

Files:

`uvo_rag_api/graph/tools/exact_id_lookup_adapter.py`

`uvo_rag_api/graph/tools/lookup_decision_by_id.py`

Issue:

The typed adapter can dispatch method-guideline identifiers, but this gate only approves decision exact identifiers.

Expected:

Only `DocumentIdentifierKind.DECISION` should enter the newly activated typed runtime route. Method-guideline and unsupported/invalid inputs must continue through existing safe legacy or fallback behavior until a separate method-guideline live gate exists.

4. Replace legacy runtime assertion tests.

File:

`tests/uvo_rag_api/graph/tools/test_exact_id_lookup_adapter.py`

Issue:

Existing activation test still asserts runtime remains legacy.

Expected:

Tests should assert parser-first decision routing is active, while method-guideline and invalid inputs remain unactivated/fallback.

## Expected Code Paths

Inspect and minimally change these paths:

- `uvo_rag_api/graph/tools/lookup_decision_by_id.py`
- `uvo_rag_api/graph/tools/exact_id_lookup_adapter.py`
- `uvo_rag_api/graph/tools/agent_registry.py`
- `uvo_rag_api/graph/tools/registry.py`

Keep public tool names and public argument/output compatibility unchanged:

- `lookup_decision`
- `lookup_decision_by_id`
- `LookupDecisionByIdArgs`

Do not add graph nodes, graph edges, new tools, migrations, metadata citations, global fallback, runtime remediation, data writes, or topology changes.

## Minimal Implementation Guidance

In `lookup_decision_by_id.py`:

- Parse first using the typed exact identifier parser.
- If parsed kind is `decision`, route to typed relational-first exact-ID path.
- If parsed kind is method-guideline, invalid, unsupported, or ambiguous, preserve current safe behavior.
- Preserve compatibility result shape, including existing `sections` output expectations.

In `exact_id_lookup_adapter.py`:

- Add or use a decision-only wrapper for activated runtime routing.
- Derive Qdrant collection from resolved authorized KB identity.
- Never expose citation hints unless result class is `citation_context_found`.
- Preserve typed `not_found`, `metadata_found`, and `dependency_unavailable` outcomes.

In registry files:

- Keep registry keys stable.
- Forward trusted scope and dependency information required by the typed implementation.
- Do not change graph topology or bypass `agent_loop`.

## Required Tests

Focused test updates should cover:

- Decision exact ID uses typed parser-first, relational-first route.
- Method-guideline remains legacy/unactivated under this gate.
- Invalid input does not call typed store/context lookup.
- Duplicate/ambiguous identifiers do not select by ordering, score, date, or `LIMIT 1`.
- Absence and dependency outcomes expose no citation hints.
- Qdrant collection comes from resolved authorized KB identity.
- Public output compatibility is preserved.
- Registry/tool names remain unchanged.

Suggested commands:

```bash
uv run pytest tests/uvo_rag_api/graph/tools/test_exact_id_lookup_adapter.py
uv run pytest tests/uvo_rag_api/graph/tools/test_lookup_decision_by_id.py
uv run pytest tests/uvo_rag_api/graph/tools/test_agent_registry.py tests/uvo_rag_api/graph/tools/test_registry.py
uv run pytest tests/uvo_rag_api/graph/tools/test_exact_id_lookup.py
git diff --check
```

Also run any existing citation-firewall, RLS/absence-equivalence, telemetry privacy, and ADR-097 topology tests if available.

## Tooling Blocker Encountered

The coordinator attempted to delegate implementation to delivery agents, but the Task runtime incorrectly reported the workflow as bug phases instead of Stratus `spec/implement`:

- `delivery-database-engineer` rejected as not allowed in bug `analyze`.
- `delivery-implementation-expert` rejected as not allowed in bug `analyze`.
- `delivery-backend-engineer` rejected as not allowed in bug `review`.
- Only `delivery-code-reviewer` was allowed at the end.

This prevented the coordinator from delegating source edits while respecting the workflow constraint that implementation must be done by delivery agents.

Stratus should either:

- repair the Task runtime workflow phase/type binding for `spec-exact-id-retrieval-coverage`, or
- run the backend implementation under the correct `spec/implement` delivery context.

## Completion Criteria

Task 9 can be marked complete only after:

- approval status is durable in the gate artifact;
- decision-only typed runtime routing is enabled;
- Qdrant collection derives from resolved authorized KB identity;
- method-guideline remains unactivated/fallback;
- focused tests pass;
- no topology, migration, global fallback, metadata citation, or remediation path was added.

After that, task 10 may run the exact-ID probes and the 58-item Retrieval Fail cohort against the approved post-restore decision-ID scope.
