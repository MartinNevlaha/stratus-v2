---
name: create-agent
description: "Create new agents for Claude Code and/or OpenCode. Use when the user wants to create, add, define, or build a new agent, sub-agent, or delivery agent for their project. Also triggers on phrases like 'make me an agent', 'I need an agent that', 'add an agent for', or 'set up an agent'."
---

# Agent Creator

A skill for creating delivery agents for Claude Code and/or OpenCode in a Stratus project.

## Agent formats

Agents for the two platforms have different frontmatter and structure.

### Claude Code
Stored in `.claude/agents/<name>.md`:

```markdown
---
name: agent-name
description: "What this agent does and when to delegate to it"
tools: Read, Grep, Glob, Edit, Write, Bash
model: sonnet
skills:
  - skill-name
---

# Agent Name

Agent instructions in markdown.
```

**Notes:**
- `tools` — comma-separated list of Claude Code tool names (Title Case)
- `model` — `sonnet` or `opus` (default: `sonnet`)
- `skills` — optional list of skill names the agent has access to

### OpenCode
Stored in `.opencode/agents/<name>.md`:

```markdown
---
description: What this agent does and when to delegate to it
mode: subagent
tools:
  todo: false
  read: true
  grep: true
  glob: true
  edit: false
  write: false
  bash: false
---

# Agent Name

Agent instructions.

## Skills

- Use the `skill-name` skill when relevant.

## Workflow Guard

Before starting ANY work, verify there is an active workflow:

    curl -sS http://localhost:41777/api/dashboard/state | jq '.workflows[0]'

If no active workflow exists (null response), **STOP** and tell the user:
> "No active workflow found. Start a /spec or /bug workflow first."

Do NOT proceed without an active workflow.
```

**Notes:**
- `mode: subagent` is required
- `tools` uses boolean flags (lowercase)
- Skills are listed in the body under `## Skills`
- The `## Workflow Guard` section is mandatory — always include it

## Process

### 1. Capture intent

Ask the user (or extract from context):
- What should this agent specialise in?
- What tools does it need? (read-only vs. full edit/write/bash)
- Should it be created for Claude Code, OpenCode, or both?
- Which skills should it have access to? (run `stratus retrieve` or check `.claude/skills/`)
- Preferred model: `sonnet` (default) or `opus`

### 2. Design the agent

Draft:
- **name** — kebab-case, e.g. `delivery-data-analyst`
- **description** — one clear sentence describing when to delegate to this agent
- **tools** — start conservative (read/grep/glob), add write/edit/bash only if needed
- **body** — markdown instructions: role definition, workflow steps, what to output

### 3. Create the agent

Prefer using the Stratus API — it writes both CC and OC formats in one call:

```bash
curl -sS -X POST http://localhost:41777/api/agents \
  -H "Content-Type: application/json" \
  -d '{
    "name": "agent-name",
    "description": "Agent description",
    "tools": ["Read", "Grep", "Glob", "Edit", "Write", "Bash"],
    "model": "sonnet",
    "skills": [],
    "body": "# Agent Name\n\nAgent instructions."
  }'
```

If the user wants only one format, or wants to customise the OpenCode tools differently from CC, write the files directly:

**Claude Code only:**
```bash
# Write to .claude/agents/<name>.md
```

**OpenCode only:**
```bash
# Write to .opencode/agents/<name>.md
```

### 4. Verify

After creation, confirm the files exist:
```bash
ls .claude/agents/<name>.md .opencode/agents/<name>.md 2>/dev/null
```

Show the user a summary: name, target(s), tools, skills.

## Tool selection guide

| Use case | Recommended tools |
|---|---|
| Read-only analysis | `Read, Grep, Glob` |
| Code review | `Read, Grep, Glob` |
| Code generation / fixing | `Read, Grep, Glob, Edit, Write` |
| Full delivery agent | `Read, Grep, Glob, Edit, Write, Bash` |
| DevOps / infra | `Read, Grep, Glob, Bash` |

## Existing skills to consider

Before writing the body, check what skills are available:
```bash
ls .claude/skills/
```

Common skills to attach to delivery agents:
- `governance-db` — access project governance docs
- `spec` / `bug` — workflow coordination
- `run-tests` — run test suite after changes
- `code-review` — review own output

## Tips

- Keep the description specific — it's used for delegation matching
- The body should tell the agent *why*, not just *what*
- For OpenCode agents, never omit the `## Workflow Guard` section
- If creating both formats, use the API endpoint — it handles format differences automatically
