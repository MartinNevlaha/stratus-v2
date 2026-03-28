---
name: summarize-workflow
description: "Generate a semantic change summary for a completed workflow. Analyzes git diff, governance docs, and vexor results to populate capabilities added/modified/removed, downstream risks, and governance compliance."
disable-model-invocation: true
argument-hint: "[workflow-id]"
---

# Summarize Workflow Changes

Generates a semantic change summary for a completed workflow and stores it via the Stratus API.

```bash
BASE=http://localhost:41777
```

## Step 1 — Identify the workflow

Workflow ID argument: `$ARGUMENTS`

**If no argument was given**, list recent completed workflows:

```bash
curl -sS "$BASE/api/workflows" | jq '[.[] | select(.phase == "complete") | {id, title, updated_at}] | sort_by(.updated_at) | reverse | .[0:5]'
```

Ask the user which workflow to summarize. Wait for their answer.

**If an argument was given**, use it directly as the workflow ID.

## Step 2 — Get the structural summary

```bash
curl -sS "$BASE/api/workflows/<id>/summary"
```

This returns the structurally computed fields (files changed, git stats, governance doc matches, vexor excerpts). If it returns `{"status": "pending"}`, wait a moment and retry — the server is still computing the git diff.

Save the response. The fields `governance_docs_matched` and `vexor_excerpts` are raw context for your analysis.

## Step 3 — Get the git diff for semantic analysis

```bash
# Get the base commit from the workflow state
curl -sS "$BASE/api/workflows/<id>" | jq '.base_commit'

# Then get the full diff (limit to first 200 lines to stay focused)
git diff <base_commit>..HEAD --stat
git diff <base_commit>..HEAD -- '*.go' '*.ts' '*.svelte' '*.py' | head -200
```

## Step 4 — Analyze and generate semantic content

Using the git diff, governance doc matches, and vexor excerpts from Step 2, analyze:

1. **capabilities_added**: New features, endpoints, behaviors that did not exist before
2. **capabilities_modified**: Existing features that changed behavior, interface, or implementation significantly
3. **capabilities_removed**: Features, endpoints, or behaviors that were removed
4. **downstream_risks**: What could break or require attention in dependent systems, clients, or deployments
5. **governance_compliance**: Which governance rules/ADRs this change follows (from `governance_docs_matched`)
6. **test_coverage_delta**: If visible from diff stats (e.g. "+5 test files")

Keep each item concise (one sentence max). Only include arrays that have real content — leave empty if nothing applies.

## Step 5 — Submit the semantic summary

```bash
curl -sS -X PUT "$BASE/api/workflows/<id>/summary" \
  -H 'Content-Type: application/json' \
  -d '{
    "capabilities_added": ["..."],
    "capabilities_modified": ["..."],
    "capabilities_removed": [],
    "downstream_risks": ["..."],
    "governance_compliance": ["..."],
    "test_coverage_delta": ""
  }'
```

The server merges semantic fields with the existing structural data (git stats, file counts).

## Step 6 — Report to user

Tell the user:
- How many files changed and the line delta
- Key capabilities added/modified
- Any downstream risks to be aware of
- The summary is now visible in the dashboard **Change Summary** card
- The summary was written to `docs/change-summaries/<id>.md` and indexed into Vexor — future workflows will find it in similarity searches

---

## Notes

- Works identically for Claude Code and OpenCode
- The structural fields (files_changed, lines_added/removed, governance_docs_matched, vexor_excerpts) are always populated automatically by the server on workflow completion
- This skill enriches the semantic interpretation layer on top
- After `PUT /summary`, the server automatically writes `docs/change-summaries/<id>.md` and triggers Vexor indexing — no manual step needed
- Run `GET /api/workflows/<id>/summary.md` to get a Markdown export
