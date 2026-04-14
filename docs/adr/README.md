# Architecture Decision Records

This directory contains ADRs for Stratus v2 — durable records of significant architectural decisions, their context, and their consequences.

## Format

Each ADR is a standalone markdown file named `NNNN-short-slug.md` with this structure:

```
# ADR-NNNN: Short title

**Status:** Proposed | Accepted | Superseded by ADR-XXXX
**Date:** YYYY-MM-DD

## Context
Why this decision is being made. Constraints, forces, prior art.

## Decision
What we are doing.

## Consequences
Trade-offs, migration cost, what becomes easier, what becomes harder.

## Rejected alternatives
Other options we considered and why we did not pick them.
```

## When to write one

- Cross-cutting refactors that touch more than one subsystem
- Tech stack or protocol choices that constrain future work
- Breaking changes to persisted data shapes or public APIs
- Decisions that future contributors would reasonably ask "why?" about

## Index

| # | Title | Status |
|---|-------|--------|
| [0001](0001-llm-client-unification.md) | LLM client unification | Accepted |
| [0002](0002-dev-loop.md) | Unified dev loop via air + Vite | Accepted |
