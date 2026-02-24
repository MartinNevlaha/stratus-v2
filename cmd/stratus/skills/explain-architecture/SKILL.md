---
name: explain-architecture
description: "Explain the architecture of the project or a specific component. Read-only — consults docs and code, never modifies."
context: fork
---

# Explain Architecture

Explain the architectural concept of "$ARGUMENTS".

## Process

1. **Find architecture documentation**
   - Look in `docs/architecture/`, `docs/`, `CLAUDE.md` (all levels)
   - Search `docs/decisions/` or `docs/adr/` for relevant ADRs
   - Use `vexor-cli` to locate the component or subsystem in code

2. **Identify the subsystem**
   - Which component, layer, or service does this concept belong to?
   - What are its boundaries and responsibilities?

3. **Quote the relevant documentation**
   - Cite the exact file and section
   - Don't paraphrase when quoting is more precise

4. **Explain the "Why"**
   - What problem does this design solve?
   - What alternatives were considered (check ADRs)?
   - What trade-offs were accepted?

5. **Highlight risks and constraints**
   - Known limitations of this design
   - Things that must stay true for the design to hold
   - Areas under active evolution

## Output Format

```
## Architecture: <concept>

### What It Does
<one paragraph on the component's role and boundaries>

### How It Works
<key mechanisms, data flows, key code locations with file:line>

### Why This Design
<rationale from ADRs or docs, trade-offs accepted>

### Key Files
- `path/to/file.go:42` — <what it does>

### Constraints
<what must stay true, known limitations>
```
