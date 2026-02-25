---
name: learn
description: "Trigger pattern learning: analyze codebase for candidates, generate proposals, record decisions."
disable-model-invocation: true
---

# Learning Workflow

You analyze the codebase for repeating patterns and anti-patterns, create learning candidates, and generate proposals for improvements (rules, ADRs, skill templates).

## API Base

```bash
BASE=http://127.0.0.1:41777
```

---

## Steps

### 1. Analyze codebase for patterns

Use Grep, Glob, Read to find:
- Repeated code structures (candidates for templates/abstractions)
- Inconsistent patterns (candidates for rules)
- Architectural decisions worth documenting (ADR candidates)
- Missing or outdated documentation

### 2. Save pattern candidates

For each pattern found:

```bash
curl -sS -X POST $BASE/api/learning/candidates \
  -H 'Content-Type: application/json' \
  -d '{
    "detection_type": "pattern|anti_pattern|inconsistency|missing_doc",
    "description": "What was found",
    "confidence": 0.8,
    "files": ["path/to/file1.ts", "path/to/file2.ts"],
    "count": 3
  }'
```

### 3. Generate proposals

For each high-confidence candidate, generate a proposal with the **full file content** and target path.
The server will automatically write the file when the user accepts it in the dashboard.

Path conventions by type:
- **rule** → `.claude/rules/<slug>.md`
- **template** → `.claude/templates/<slug>.md`
- **adr** → `docs/decisions/<slug>.md`
- **skill** → `.claude/skills/<slug>/SKILL.md`

```bash
curl -sS -X POST $BASE/api/learning/proposals \
  -H 'Content-Type: application/json' \
  -d '{
    "candidate_id": "<id from step 2>",
    "type": "rule|skill|adr|template",
    "title": "Short human-readable title",
    "description": "Why this pattern matters and what problem it solves",
    "proposed_content": "# Full content of the file to be written\n\n...",
    "proposed_path": ".claude/rules/<slug>.md",
    "confidence": 0.8
  }'
```

**Important:** `proposed_content` and `proposed_path` are required for auto-apply to work.
Without them, accepting the proposal in the dashboard will only mark it as decided — no file is written.

### 4. Review pending proposals

```bash
curl -sS $BASE/api/learning/proposals?status=pending
```

Tell the user proposals are ready in the **Learning tab** of the dashboard.
They can accept/reject/snooze each one there; accepting automatically writes the file and re-indexes governance.

---

## When to use /learn

- After completing a `/spec` or `/bug` workflow
- When you notice repeated patterns across the codebase
- When the user asks "what have we learned?"
- Before major refactoring to capture current patterns
