---
name: vexor-cli
description: "Semantic file discovery via `vexor`. Use when file location is unclear or the repo is too large for efficient manual browsing."
context: fork
---

# Vexor CLI Skill

Find files by intent (what they do), not exact text.

Use this before broad `Glob`/`Grep` scans when you do not know the filename yet.

## Use It Like This

- Use `vexor search` first for intent-based discovery.
- Then confirm results with `Read`, `Grep`, and project-specific tools.
- Prefer narrow queries plus `--ext`/`--path` over searching the whole repo repeatedly.

## Fallback (If `vexor` Is Missing)

- Continue with `Glob`, `Grep`, and `Read`.
- State that semantic discovery is unavailable in this environment.
- Do not block the task solely because `vexor` is not installed.

## Command

```bash
vexor search "<QUERY>" [--path <ROOT>] [--mode <MODE>] [--ext .py,.md] [--exclude-pattern <PATTERN>] [--top 5] [--format rich|porcelain]
```

## Common Flags

- `--path` / `-p`: root directory (default: current dir)
- `--mode` / `-m`: indexing/search strategy
- `--ext` / `-e`: limit file extensions (e.g. `.py,.md`)
- `--exclude-pattern`: exclude paths by gitignore-style pattern (repeatable)
- `--top` / `-k`: number of results
- `--format`: `rich` (default) or `porcelain` for scripts

## Modes (Pick the Cheapest That Works)

- `auto`: routes by file type (default)
- `name`: filename-only (fastest)
- `head`: first lines only (fast)
- `code`: code-aware chunking for `.py/.js/.ts/.go` (best for codebases)
- `outline`: Markdown headings/sections (best for docs)
- `full`: chunk full file contents (slowest, highest recall)

## Examples

```bash
# Find CLI entrypoints / commands
vexor search "typer app commands" --top 5

# Search docs by headings/sections
vexor search "user authentication flow" --path docs --mode outline --ext .md

# Locate config loading logic
vexor search "config loader" --path . --mode code --ext .go

# Exclude tests
vexor search "database connection" --path . --exclude-pattern tests/**
```

## Tips

- First search may build an index (takes longer); later searches are fast.
- Treat results as ranked candidates — always verify with `Read` before acting.
- Use longer Bash timeouts when indexing a large repo for the first time.
