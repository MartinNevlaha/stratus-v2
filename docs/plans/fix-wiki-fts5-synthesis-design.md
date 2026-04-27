# Technical Design: Fix Wiki FTS5 Search & Synthesis for Natural Language Queries

**Status:** Approved
**Date:** 2026-04-19
**Scope:** `db/memory.go`, `internal/insight/wiki_engine/synthesizer.go`, `internal/insight/wiki_engine/synthesizer_test.go`

## Problem

Two bugs prevent wiki synthesis from working with natural language queries (e.g. "What is the architecture of this project?") on Gemma 4 via Ollama:

### Bug 1: FTS5 AND-semantics kills natural language queries

`buildFTS5Query()` in `db/memory.go:114` converts every whitespace-separated token into a mandatory FTS5 MATCH term with implicit AND:

```
"What is the architecture of this project" → "What"* "is"* "the"* "architecture"* "of"* "this"* "project"*
```

FTS5 AND requires ALL terms to match. Stop words like "is", "the", "of", "this" are typically absent from the FTS5 index, so the entire MATCH returns 0 rows. The synthesizer receives empty pages and wastes an LLM call.

This affects ALL FTS5 consumers: events, wiki, and governance search.

### Bug 2: Synthesizer calls LLM with empty context

When `SearchPages()` returns 0 results, `buildSourceContext()` produces an empty string. The synthesizer still calls the LLM with the full `WikiSynthesis + ObsidianMarkdown` system prompt (~1800 chars of syntax rules) and a user message containing only "Query: ...\n\nSource pages:\n".

Local models like Gemma 4:e4b (small context window, lower instruction-following) latch onto the system prompt content and produce answers about Obsidian Flavored Markdown instead of acknowledging "no results found".

## Design

### Fix 1: `buildFTS5Query` — OR semantics with stop-word filtering

**File:** `db/memory.go`

Change `buildFTS5Query` to:
1. Filter out English stop words (reuse existing stopword list from `internal/insight/product_intelligence/domain_detector.go:327`)
2. Use FTS5 `OR` operator between terms instead of implicit AND
3. Handle edge case: if all terms are filtered, fall back to a single significant token or return empty string

**Before:**
```go
func buildFTS5Query(raw string) string {
    terms := strings.Fields(strings.TrimSpace(raw))
    parts := make([]string, 0, len(terms))
    for _, t := range terms {
        t = strings.ReplaceAll(t, `"`, `""`)
        parts = append(parts, `"`+t+`"*`)
    }
    return strings.Join(parts, " ")
}
```

**After:**
```go
var ftsStopWords = map[string]bool{
    "the": true, "and": true, "for": true, "are": true, "but": true,
    "not": true, "you": true, "all": true, "can": true, "had": true,
    "her": true, "was": true, "one": true, "our": true, "out": true,
    "has": true, "have": true, "been": true, "will": true, "your": true,
    "from": true, "they": true, "this": true, "that": true, "with": true,
    "what": true, "when": true, "where": true, "which": true, "while": true,
    "about": true, "after": true, "before": true, "between": true, "into": true,
    "through": true, "during": true, "above": true, "below": true, "under": true,
    "again": true, "further": true, "then": true, "once": true, "here": true,
    "there": true, "should": true, "would": true, "could": true, "being": true,
    "over": true, "just": true, "more": true, "some": true, "such": true,
    "only": true, "also": true, "than": true, "too": true, "very": true,
    "use": true, "using": true, "used": true, "may": true,
    "is": true, "it": true, "an": true, "a": true, "do": true,
    "does": true, "did": true, "how": true, "if": true, "or": true,
    "in": true, "on": true, "at": true, "to": true, "of": true,
    "by": true, "as": true, "be": true, "we": true, "me": true,
    "my": true, "its": true, "no": true, "so": true, "up": true,
}

func buildFTS5Query(raw string) string {
    terms := strings.Fields(strings.TrimSpace(raw))
    parts := make([]string, 0, len(terms))
    for _, t := range terms {
        lower := strings.ToLower(t)
        if ftsStopWords[lower] {
            continue
        }
        t = strings.ReplaceAll(t, `"`, `""`)
        parts = append(parts, `"`+t+`"*`)
    }
    if len(parts) == 0 {
        return ""
    }
    return strings.Join(parts, " OR ")
}
```

**Behavior change:**
- "What is the architecture of this project" → `"What"* OR "architecture"* OR "project"*`
- "auth error" → `"auth"* OR "error"` (unchanged behavior, both terms present)
- "the" → `""` (empty, no search)
- Short keywords like "a", "is" are filtered out

**Impact on existing consumers:**
- `SearchEvents()`: benefits from OR — natural language memory queries will match more events
- `SearchWikiPages()`: **fixes the core bug** — natural language wiki queries will find pages
- `SearchDocs()`: governance search benefits similarly

### Fix 2: Synthesizer early-return on empty context

**File:** `internal/insight/wiki_engine/synthesizer.go`

In `SynthesizeAnswer()`, after `buildSourceContext()`, check if the context is empty and return a structured "no results" response without calling the LLM:

```go
func (s *Synthesizer) SynthesizeAnswer(ctx context.Context, query string, maxSources int, persist bool) (*SynthesisResult, error) {
    // ... existing search + ranking code ...
    sourceContext := s.buildSourceContext(ctx, ranked)
    if sourceContext == "" {
        return &SynthesisResult{
            Answer:     "No relevant wiki pages found for this query.",
            Citations:  []Citation{},
            TokensUsed: 0,
        }, nil
    }
    // ... existing LLM call code ...
}
```

**Rationale:**
- Saves an unnecessary LLM call (~2-5 seconds on Ollama, wasted tokens)
- Prevents confusing LLM responses about system prompt content
- The API consumer (dashboard, MCP tool) gets a clear, deterministic answer
- `persist: true` with empty context will not create junk wiki pages

### Fix 3: Unit tests

**File:** `internal/insight/wiki_engine/synthesizer_test.go`

Add test `TestSynthesizeAnswer_EmptySearchReturnsNoResults` that verifies:
- When store returns empty results, answer is "No relevant wiki pages found"
- Citations is empty
- TokensUsed is 0
- No LLM call is made

**File:** `db/memory_test.go` (or existing test file)

Add tests for `buildFTS5Query`:
- Natural language query produces OR with stop words filtered
- Short/stop-only query returns empty
- Mixed case stop words are filtered
- Terms with special chars (quotes) are escaped

## Non-Goals

- No changes to FTS5 schema or tokenizers
- No changes to LLM prompt content
- No changes to the retrieve endpoint (it uses the same `buildFTS5Query` indirectly via wiki/governance search)
- No Gemma 4-specific code paths

## Risk Assessment

**Low risk:**
- OR semantics is more permissive (returns more results, fewer false negatives)
- Stop-word filtering removes noise from queries
- Early-return is a strict improvement (no LLM call = no confusing output)
- All changes are backward compatible (same function signatures, same API contracts)
