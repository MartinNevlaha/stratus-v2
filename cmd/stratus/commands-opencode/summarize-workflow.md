---
description: "Generate a semantic change summary for a completed workflow. Analyzes git diff, governance docs, and vexor results to produce capabilities added/modified/removed, downstream risks, and governance compliance."
---

# Summarize Workflow Changes

Generates a semantic change summary for a completed workflow and stores it via the Stratus API.

```bash
BASE=http://localhost:41777
```

## Step 1 — Identify the workflow

If a workflow ID was provided as an argument, use it directly. Otherwise, list recent completed workflows and ask the user which one to summarize:

```bash
curl -sS "$BASE/api/workflows" | jq '[.[] | select(.phase == "complete") | {id, title, updated_at}] | sort_by(.updated_at) | reverse | .[0:5]'
```

## Step 2 — Get the structural summary

```bash
curl -sS "$BASE/api/workflows/<id>/summary"
```

If the response is `{"status":"pending"}`, wait a moment and retry — the server is still computing the git diff. The fields `governance_docs_matched` and `vexor_excerpts` are raw context for your analysis.

## Step 3 — Get the git diff for semantic analysis

```bash
# Get base commit from workflow state
curl -sS "$BASE/api/workflows/<id>" | jq '.base_commit'

# Review changes
git diff <base_commit>..HEAD --stat
git diff <base_commit>..HEAD -- '*.go' '*.ts' '*.svelte' '*.py' | head -200
```

## Step 4 — Analyze and generate semantic content

Using the git diff, governance doc matches, and vexor excerpts, determine:

1. **capabilities_added**: New features or behaviors that didn't exist before
2. **capabilities_modified**: Existing features with changed behavior or interface
3. **capabilities_removed**: Features or behaviors that were removed
4. **downstream_risks**: What could break in dependent systems, clients, or deployments
5. **governance_compliance**: Which governance rules/ADRs this change follows
6. **test_coverage_delta**: Coverage change if visible from diff stats

Keep each item concise (one sentence). Only include arrays with real content.

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

## Step 6 — Report to user

Summarize the key changes: files changed, capabilities added/modified, and any downstream risks. The summary is now visible in the Stratus dashboard **Change Summary** card and available at `GET /api/workflows/<id>/summary.md`.

After the PUT, the server automatically writes `docs/change-summaries/<id>.md` and triggers Vexor indexing — future similarity searches will find this summary when analyzing related changes.
