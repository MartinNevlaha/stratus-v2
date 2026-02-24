---
name: governance-db
description: "Query project governance docs (rules, ADRs, architecture standards) via the stratus MCP `retrieve` tool. Use before making plan/review/gate decisions."
context: fork
---

# GovernanceDb Skill

Query project governance context through the stratus MCP `retrieve` tool.

Use this when your task depends on project rules, architecture decisions, standards, or
quality criteria that may be documented outside the current file.

## Goal

Find governing constraints before making a verdict.

Use GovernanceDb to avoid:
- Inventing standards that already exist
- Missing ADR constraints that change implementation choices
- Reviewing against generic best practices when project-specific rules exist

## What Is in GovernanceDb

Depending on project indexing, GovernanceDb includes:
- `.claude/rules/*.md`
- `CLAUDE.md` files (all levels)
- ADRs / design docs / architecture notes
- Quality gate criteria and process docs

## Usage

```text
retrieve(query="<topic>", corpus="governance")
```

## Query Patterns

Prefer concrete, decision-oriented queries:

- `"authentication standard"`
- `"file size limit"`
- `"coverage threshold"`
- `"error handling policy"`
- `"GDPR requirements"`
- `"release checklist"`
- `"database selection ADR"`

## Review / Gate Workflow

1. Get governance constraints → `retrieve(corpus="governance")`
2. Get implementation evidence → `Read`, `Grep`, test output
3. Map findings to explicit requirements
4. Cite file paths / lines for verdicts

Do not issue FAIL without tying it to a concrete project requirement.

## Fallback (If `retrieve` Is Unavailable)

- Read `CLAUDE.md` and `.claude/rules/*.md` directly with `Read`/`Glob`
- Search `docs/architecture/`, `docs/decisions/`, `docs/adr/` with `Glob`/`Grep`
- State that GovernanceDb retrieval is unavailable and direct reads were used instead
