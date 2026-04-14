# Implementation Plan: Evolution Wiki Agent Integration

## Context

Evolution wiki findings are created in DB but invisible to agents. Three changes needed:
1. Change wiki page status from "draft" to "published"
2. Add 1.2x retrieve scoring boost for evolution pages
3. Add wiki retrieve instruction to 7 agent prompts

Design doc: `docs/plans/evolution-wiki-agent-integration-design.md`

## Tasks

### Task 0: Change evolution wiki status to "published"
- File: `insight/engine.go:272`
- Change: `Status: "draft"` to `Status: "published"`
- Agent: delivery-backend-engineer

### Task 1: Add evolution boost test in retrieve scoring
- File: `api/routes_retrieval_test.go`
- Write test `TestRunRetrieve_EvolutionBoost` that verifies evolution pages get 1.2x score
- Agent: delivery-qa-engineer

### Task 2: Add 1.2x evolution boost in retrieve scoring
- File: `api/routes_retrieval.go` (wiki scoring loop)
- Add `if p.GeneratedBy == "evolution" { score *= 1.2 }` after staleness penalty
- Agent: delivery-backend-engineer

### Task 3: Update 7 agent prompts with wiki retrieve instruction
- Files: `cmd/stratus/agents/delivery-{backend-engineer,frontend-engineer,database-engineer,implementation-expert,system-architect,strategic-architect,debugger}.md`
- Add: `Use mcp__stratus__retrieve with corpus: "wiki" to check for evolution findings and existing knowledge relevant to this task.`
- Agent: delivery-implementation-expert

### Task 4: Verify all tests pass and binary compiles
- Run: `go test ./api/... ./insight/...` and `go build ./cmd/stratus`
- Agent: delivery-qa-engineer
