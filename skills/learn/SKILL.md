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

For high-confidence candidates, generate proposals:

```bash
curl -sS -X POST $BASE/api/events \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "learning_update",
    "title": "Proposal: <title>",
    "text": "<detailed description of the proposed rule/template/ADR>",
    "tags": ["proposal", "rule|template|adr"],
    "importance": 0.7
  }'
```

### 4. Review pending proposals

```bash
curl -sS $BASE/api/learning/proposals
```

Present proposals to the user. For each, the user can decide via the Learning tab in the dashboard or via:

```bash
curl -sS -X POST $BASE/api/learning/proposals/<id>/decide \
  -H 'Content-Type: application/json' \
  -d '{"decision": "accept|reject|ignore|snooze"}'
```

### 5. Apply accepted proposals

For accepted proposals:
- **rule** → write to `.claude/rules/<name>.md`
- **template** → write to `.claude/templates/<name>.md`
- **adr** → write to `docs/decisions/<name>.md`
- **skill** → write to `.claude/skills/<name>/SKILL.md`

---

## When to use /learn

- After completing a `/spec` or `/bug` workflow
- When you notice repeated patterns across the codebase
- When the user asks "what have we learned?"
- Before major refactoring to capture current patterns
