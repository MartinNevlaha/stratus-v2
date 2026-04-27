# Plan: Verify Agent Wiki Access

## Audit Findings

### Wiki MCP Tools Available (3 tools)
| Tool | Purpose | Status |
|------|---------|--------|
| `stratus_retrieve(corpus="wiki")` | Semantic search across wiki pages (merged with code/governance in auto mode) | ✅ Works |
| `stratus_wiki_search` | Full-text search of wiki pages | ✅ Works |
| `stratus_wiki_query` | LLM-powered Q&A over wiki content (with `persist` option) | ✅ Works |

### Agent Wiki Coverage

**12 of 14 agents** have wiki instructions in their prompts.

| Agent | `retrieve(wiki)` | `wiki_search` | `wiki_query` | Evolution mention |
|-------|:-:|:-:|:-:|:-:|
| delivery-backend-engineer | ✅ | ❌ | ❌ | ✅ |
| delivery-frontend-engineer | ✅ | ❌ | ❌ | ✅ |
| delivery-database-engineer | ✅ | ❌ | ❌ | ✅ |
| delivery-implementation-expert | ✅ | ❌ | ❌ | ✅ |
| delivery-system-architect | ✅ | ❌ | ❌ | ✅ |
| delivery-strategic-architect | ✅ | ❌ | ❌ | ✅ |
| delivery-debugger | ✅ | ❌ | ❌ | ✅ |
| delivery-code-reviewer | ✅ | ❌ | ❌ | ❌ |
| delivery-qa-engineer | ✅ | ❌ | ❌ | ❌ |
| delivery-devops-engineer | ✅ | ❌ | ❌ | ❌ |
| delivery-mobile-engineer | ✅ | ❌ | ❌ | ❌ |
| delivery-ux-designer | ✅ | ❌ | ❌ | ❌ |
| **delivery-governance-checker** | **❌** | **❌** | **❌** | **❌** |
| **delivery-skill-creator** | **❌** | **❌** | **❌** | **❌** |

### Key Gaps

1. **2 agents have NO wiki instructions at all**: delivery-governance-checker, delivery-skill-creator
2. **ALL agents only know about `retrieve(corpus="wiki")`** — none know about `wiki_search` or `wiki_query`
3. **6 agents lack the "evolution findings" mention** (code-reviewer, qa, devops, mobile, ux-designer, governance-checker)
4. Both `.claude/agents/` and `cmd/stratus/agents/` need to stay in sync

## Proposed Tasks

1. Add wiki retrieve instruction to delivery-governance-checker and delivery-skill-creator (both locations)
2. Add dedicated wiki tools reference (`wiki_search`, `wiki_query`) to all 14 agent prompts (both locations)
3. Add evolution wiki mention to the 6 agents missing it (both locations)
4. Verify binary builds with updated agent prompts
