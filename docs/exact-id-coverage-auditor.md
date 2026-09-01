# Exact-ID Coverage Auditor Runbook

**Component:** `scripts/audit_exact_id_coverage.py`  
**Purpose:** a bounded, read-only operator audit of exact document identifiers. It is
not a chat feature, runtime lookup path, repair tool, or corpus-management job.

This runbook implements the controls in the [exact-ID design](../plans/spec-exact-id-retrieval-coverage-design.md)
and [implementation plan](../plans/spec-exact-id-retrieval-coverage.md).

## Authorization and scope gate

Run the auditor only from a trusted operator host that has already authenticated the
operator and derived scope from authorization state—not from a chat request, model
arguments, URL parameters supplied by a user, or arbitrary environment input. The
host must inject an `AuditOperatorContext` with both:

- `authenticated=True`; and
- `can_audit_exact_id_coverage=True`.

It must also inject a trusted, non-empty `team_ids`, `kb_ids`, KB-to-document-kind
mapping, and Qdrant collection associated with that scope. The module checks that
these fields are non-empty, but the host is responsible for establishing that the
collection and every supplied ID belong to the authorized scope.

**Never invoke globally or unscoped.** Missing permission, team scope, KB scope, or
collection is rejected before any lookup. Do not widen a failed lookup to another
team, KB, collection, identifier form, or an all-corpus scan. Unauthorized existence
must remain indistinguishable from scoped absence.

The module is intentionally not a standalone CLI. Running it as a script exits with
`authenticate an operator and call audit_exact_id_coverage from a trusted host`.
There are no CLI flags, including no `--dry-run`, credentials, scope flags, or
standalone database/Qdrant connection path. Do not work around that refusal with an
ad-hoc command.

## Read-only execution contract

The authorized host calls `audit_exact_id_coverage(request, pg_pool=..., qdrant_client=...)`.
For valid identifiers, the auditor first proves its PostgreSQL path under
`with_team_context(...)` in a `readonly=True` transaction. Identity lookup remains
RLS-scoped; Qdrant is queried only after relational identity resolution and only for
the resolved document/team/KB/kind and `original` chunks. Qdrant results still require
the RLS-authorized `chunks_metadata` join.

The auditor performs no writes and cannot enqueue or start a job. In particular, it
does **not** repair, reindex, reparse, ingest, delete, upsert, alter collections, or
persist an audit history. It must never read, emit, summarize, or handle source
content. Its metadata is operational evidence only: it is not legal substance and
must never be presented as a citation or used to make a legal claim.

## Enforced bounds and timeouts

Reject the request rather than increasing a bound:

| Control | Maximum actually accepted |
| --- | ---: |
| Explicit identifiers per run | 500 |
| Input page size | 100 |
| Qdrant batch-size setting | 64 |
| Concurrent per-identifier work | 4 |
| Per-store wrapper timeout | 10 seconds |
| Whole audit timeout | 120 seconds |
| Ambiguity-limit setting | 20 |
| Parsed identifier input | 160 characters |
| Parser aliases | 12 |
| Relational identity candidates | 20 |
| Citation-context Qdrant points | 64 |

Lower positive limits are accepted; zero, negative, and larger values are rejected.
The `AuditLimits` batch and ambiguity settings are validation controls; current
downstream lookup uses its own fixed 64-point and 20-candidate caps rather than a
per-request override. A timeout or dependency failure is not evidence that a document
is absent and must not cause scope widening.

## Safe report and redaction

`AuditReport.to_dict()` returns only:

```json
{
  "total_identifiers": 2,
  "counts": {"healthy": 1, "pg_document_not_found": 1},
  "findings": [
    {"identifier_kind": "decision", "discrepancy": "healthy"},
    {"identifier_kind": "decision", "discrepancy": "pg_document_not_found"}
  ]
}
```

`identifier_kind` is omitted for an invalid identifier. Counts are bounded by the
input cap. Do not add raw or hashed identifiers, aliases, UUIDs, team/KB/collection
names, titles, addressees, source text, chunks, point payloads, raw queries/SQL,
cross-team counts, credentials, or exception bodies to the report, logs, tickets, or
telemetry. Store any approved redacted evidence under
`tests/eval/reports/exact_id_coverage/` using the established timestamped-report
process.

## Implemented discrepancy mapping

These are all values of the implemented `DiscrepancyCode` enum. Values are lowercase
in the report; do not substitute similarly named design examples that are not emitted
by this auditor.

| Report value | Meaning | Required handoff |
| --- | --- | --- |
| `invalid_identifier` | Parser rejected the supplied exact-ID form; no store query occurs. | Correct or add a fixture only through the approved parser-change process; then re-audit. |
| `pg_document_not_found` | No matching document is visible in the trusted scoped kind/KB set. | Have the scoped corpus owner verify intake/scope using approved operations; do not globally search. |
| `pg_identifier_ambiguous` | More than one authorized immutable document matched. | Hand to data stewardship for duplicate/identity resolution through an approved operation; never pick one in the auditor. |
| `pg_index_status_not_indexed` | Resolved document status is `pending`, `indexing`, or `failed`. | Hand to the approved indexing/recovery owner. |
| `qdrant_no_original_points` | Successful scoped context lookup found no valid original points for an otherwise unmanaged/null index state. | Hand to the approved corpus-indexing owner. |
| `chunk_metadata_missing` | Indexed/context coverage is inconsistent; no authorized original chunk mapping survived. | Hand to the approved ingestion/index-consistency owner. |
| `dependency_unavailable` | PostgreSQL or required context dependency could not determine a safe result. | Restore the dependency; do not classify as absence or trigger remediation. |
| `healthy` | A resolved document produced authorized original citation context. | No remediation; retain only the redacted outcome if evidence is required. |

There is **no implemented remediation enum** in `audit_exact_id_coverage.py`.
Accordingly, the table is a handoff decision aid, not an emitted remediation code or
an instruction for the auditor to mutate data. Examples named only in the design
(such as payload/scope/doc-ID mismatch subtypes) are not report values unless and
until code adds and tests them.

## Remediation handoff and re-audit

1. Preserve the redacted report and scope authorization evidence.
2. Give the finding category and approved scope to the responsible corpus, indexing,
   ingestion, or data-stewardship operator.
3. That operator may use **only an already approved, separately authorized operational
   procedure** (for example, the applicable [reparse runbook](phase-e-reparse.md)).
   The auditor neither selects nor runs the procedure. Do not create an ad-hoc repair,
   reindex, reparse, write, or job command from this runbook.
4. After the approved operation finishes, run this same scoped read-only audit again.
   **Post-remediation re-audit is mandatory**; do not mark the discrepancy resolved
   based solely on a job submission or metadata update.

## MCP recovery

For the separate live validation/gate investigation, first verify the local UVO stack
and `uvo-dev-mcp`, then rerun only scoped read-only PostgreSQL and Qdrant checks. If
MCP calls still report session/initialization failure after `uvo-dev-mcp` recovers,
restart OpenCode so it reconnects to `http://127.0.0.1:8080/sse`. Do not replace that
recovery path with direct global queries or a chat-triggered audit.

## Safe verification

There is no production dry-run command: standalone execution deliberately refuses
because host authorization cannot be injected. Verify the contract with the focused
test instead:

```bash
uv run pytest tests/scripts/test_audit_exact_id_coverage.py
```

The test covers permission/unscoped rejection, all configured bound overflows,
read-only scope setup, safe report serialization, and invalid identifiers performing
no store calls. It is a test invocation, not authorization to run the auditor from
chat or to perform remediation.
