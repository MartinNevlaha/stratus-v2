# Implementation Plan: Fix Wiki FTS5 Search & Synthesis

Design doc: `docs/plans/fix-wiki-fts5-synthesis-design.md`

## Tasks (ordered)

### Task 0: Add stop-word filtering + OR semantics to `buildFTS5Query`
- **File:** `db/memory.go`
- Add `ftsStopWords` map with comprehensive English stop words
- Modify `buildFTS5Query` to filter stop words and join with ` OR `
- Edge cases: all-stop query returns empty, single-term returns just that term

### Task 1: Add synthesizer early-return on empty context
- **File:** `internal/insight/wiki_engine/synthesizer.go`
- After `buildSourceContext()`, if empty, return `SynthesisResult` with "No relevant wiki pages found" without calling LLM

### Task 2: Add unit tests for `buildFTS5Query`
- **File:** `db/memory_test.go` (add if not exists, or use existing test file)
- Test: natural language query → OR with stop words filtered
- Test: stop-only query → empty string
- Test: single meaningful term → just that term
- Test: special chars (quotes) escaped

### Task 3: Add synthesizer test for empty search
- **File:** `internal/insight/wiki_engine/synthesizer_test.go`
- Test: empty search results → deterministic "no results" answer, 0 tokens, no LLM call
- Verify existing `TestSynthesizeAnswer_NoResults` still passes (that test expects LLM to be called — this behavior changes)

### Task 4: Verify with integration test
- Run `go test ./db/... ./internal/insight/wiki_engine/...`
- Verify FTS5 search returns results for "What is the architecture of this project?"
- Verify wiki synthesis returns "no results" instead of Obsidian markdown gibberish
