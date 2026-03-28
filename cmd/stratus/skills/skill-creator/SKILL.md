---
name: skill-creator
description: "Create new skills, modify and improve existing skills, and measure skill performance. Use when users want to create a skill from scratch, edit, or optimize an existing skill, run evals to test a skill, benchmark skill performance with variance analysis, or optimize a skill's description for better triggering accuracy. Make sure to use this skill whenever the user mentions creating a skill, writing a skill, making a skill, skill creation, skill editing, or wants to improve an existing skill, even if they don't explicitly use the word 'skill'."
---

# Skill Creator

A skill for creating new skills and iteratively improving them.

At a high level, the process of creating a skill goes like this:

- Decide what you want the skill to do and roughly how it should do it
- Write a draft of the skill
- Create a few test prompts and run claude-with-access-to-the-skill on them
- Help the user evaluate the results both qualitatively and quantitatively
  - While the runs happen in the background, draft some quantitative evals if there aren't any
  - Use the `eval-viewer/generate_review.py` script to show the user the results
- Rewrite the skill based on feedback from the user's evaluation
- Repeat until you're satisfied
- Expand the test set and try again at larger scale

Your job when using this skill is to figure out where the user is in this process and then jump in and help them progress through these stages.

## Communicating with the user

Pay attention to context cues to understand how to phrase your communication. In the default case:

- "evaluation" and "benchmark" are borderline, but OK
- for "JSON" and "assertion" you want to see serious cues from the user that they know what those things are before using them without explaining them

---

## Creating a skill

### Capture Intent

Start by understanding the user's intent. The current conversation might already contain a workflow the user wants to capture. If so, extract answers from the conversation history first.

1. What should this skill enable Claude to do?
2. When should this skill trigger? (what user phrases/contexts)
3. What's the expected output format?
4. Should we set up test cases to verify the skill works?

### Interview and Research

Proactively ask questions about edge cases, input/output formats, example files, success criteria, and dependencies.

### Write the SKILL.md

Based on the user interview, fill in these components:

- **name**: Skill identifier
- **description**: When to trigger, what it does. This is the primary triggering mechanism. Make descriptions a little "pushy" to combat undertriggering.
- **compatibility**: Required tools, dependencies (optional)
- **the rest of the skill body**

### Skill Writing Guide

#### Anatomy of a Skill

```
skill-name/
├── SKILL.md (required)
│   ├── YAML frontmatter (name, description required)
│   └── Markdown instructions
└── Bundled Resources (optional)
    ├── scripts/    - Executable code for deterministic/repetitive tasks
    ├── references/ - Docs loaded into context as needed
    └── assets/     - Files used in output (templates, icons, fonts)
```

#### Progressive Disclosure

Skills use a three-level loading system:
1. **Metadata** (name + description) - Always in context (~100 words)
2. **SKILL.md body** - In context whenever skill triggers (<500 lines ideal)
3. **Bundled resources** - As needed (unlimited, scripts can execute without loading)

**Key patterns:**
- Keep SKILL.md under 500 lines; if approaching this limit, add hierarchy with clear pointers.
- Reference files clearly from SKILL.md with guidance on when to read them
- For large reference files (>300 lines), include a table of contents

#### Writing Patterns

Prefer using the imperative form in instructions.

**Defining output formats:**
```markdown
## Report structure
ALWAYS use this exact template:
# [Title]
## Executive summary
## Key findings
## Recommendations
```

**Examples pattern:**
```markdown
## Commit message format
**Example 1:**
Input: Added user authentication with JWT tokens
Output: feat(auth): implement JWT-based authentication
```

### Writing Style

Try to explain to the model why things are important. Use theory of mind and try to make the skill general and not super-narrow to specific examples.

### Test Cases

After writing the skill draft, come up with 2-3 realistic test prompts. Share them with the user. Then run them.

Save test cases to `evals/evals.json`:

```json
{
  "skill_name": "example-skill",
  "evals": [
    {
      "id": 1,
      "prompt": "User's task prompt",
      "expected_output": "Description of expected result",
      "files": []
    }
  ]
}
```

See `references/schemas.md` for the full schema.

## Running and evaluating test cases

This section is one continuous sequence — don't stop partway through. Do NOT use `/skill-test` or any other testing skill.

Put results in `<skill-name>-workspace/` as a sibling to the skill directory. Within the workspace, organize results by iteration (`iteration-1/`, `iteration-2/`, etc.) and within that, each test case gets a directory (`eval-0/`, `eval-1/`, etc.).

### Step 1: Spawn all runs (with-skill AND baseline) in the same turn

For each test case, spawn two subagents in the same turn — one with the skill, one without.

**With-skill run:**

```
Execute this task:
- Skill path: <path-to-skill>
- Task: <eval prompt>
- Input files: <eval files if any, or "none">
- Save outputs to: <workspace>/iteration-<N>/eval-<ID>/with_skill/outputs/
```

**Baseline run** (same prompt, no skill at all):
Save to `without_skill/outputs/`.

Write an `eval_metadata.json` for each test case:

```json
{
  "eval_id": 0,
  "eval_name": "descriptive-name-here",
  "prompt": "The user's task prompt",
  "assertions": []
}
```

### Step 2: While runs are in progress, draft assertions

Draft quantitative assertions for each test case. Good assertions are objectively verifiable and have descriptive names.

Update `eval_metadata.json` and `evals/evals.json` with the assertions.

### Step 3: As runs complete, capture timing data

Save timing data to `timing.json`:

```json
{
  "total_tokens": 84852,
  "duration_ms": 23332,
  "total_duration_seconds": 23.3
}
```

### Step 4: Grade, aggregate, and launch the viewer

Once all runs are done:

1. **Grade each run** — spawn a grader subagent that reads `agents/grader.md` and evaluates each assertion.

2. **Aggregate into benchmark**:
   ```bash
   python -m scripts.aggregate_benchmark <workspace>/iteration-N --skill-name <name>
   ```

3. **Do an analyst pass** — read the benchmark data and surface patterns. See `agents/analyzer.md`.

4. **Launch the viewer**:
   ```bash
   python <skill-creator-path>/eval-viewer/generate_review.py \
     <workspace>/iteration-N \
     --skill-name "my-skill" \
     --benchmark <workspace>/iteration-N/benchmark.json
   ```
   For headless environments, use `--static <output_path>` to write a standalone HTML file.

5. **Tell the user** about the results.

### Step 5: Read the feedback

Read `feedback.json` when the user is done reviewing.

---

## Improving the skill

### How to think about improvements

1. **Generalize from the feedback.** We're trying to create skills that can be used many times across many different prompts. Rather than overfitty changes, try branching out and using different patterns.

2. **Keep the prompt lean.** Remove things that aren't pulling their weight.

3. **Explain the why.** Try hard to explain the **why** behind everything you're asking the model to do.

4. **Look for repeated work across test cases.** If all test cases resulted in similar helper scripts, bundle them in `scripts/`.

### The iteration loop

After improving the skill:

1. Apply improvements
2. Rerun all test cases into `iteration-<N+1>/`
3. Launch the reviewer with `--previous-workspace`
4. Wait for user review
5. Read feedback, improve again, repeat

---

## Advanced: Blind comparison

For rigorous comparison between two versions, read `agents/comparator.md` and `agents/analyzer.md`. This is optional and most users won't need it.

---

## Description Optimization

The description field in SKILL.md frontmatter is the primary mechanism that determines whether Claude invokes a skill. After creating or improving a skill, offer to optimize the description.

### Step 1: Generate trigger eval queries

Create 20 eval queries — a mix of should-trigger and should-not-trigger. Save as JSON. Make queries realistic and specific with detail.

### Step 2: Review with user

Present the eval set for review using the HTML template from `assets/eval_review.html`.

### Step 3: Run the optimization loop

```bash
python -m scripts.run_loop \
  --eval-set <path-to-trigger-eval.json> \
  --skill-path <path-to-skill> \
  --model <model-id> \
  --max-iterations 5 \
  --verbose
```

### Step 4: Apply the result

Take `best_description` from the JSON output and update the skill's SKILL.md frontmatter.

---

## OpenCode-Specific Instructions

When the user is using OpenCode (or asks about OpenCode compatibility), adapt the following:

- **Running test cases**: OpenCode uses subagents similarly to Claude Code. The Task tool works the same way.
- **Agent files**: OpenCode agents use a different frontmatter format (see below). If the user asks about creating an agent alongside the skill, generate both formats.
- **Skills location**: Skills are stored in `.claude/skills/` for both Claude Code and OpenCode targets.
- **Description optimization**: Requires `claude` CLI which may not be available. Skip if not found.

**OpenCode agent frontmatter format:**
```yaml
---
description: Agent description here
mode: subagent
tools:
  read: true
  grep: true
  glob: true
  edit: false
  write: false
  bash: false
---
```

**Claude Code agent frontmatter format:**
```yaml
---
name: agent-name
description: "Agent description here"
tools: Read, Grep, Glob, Edit, Write, Bash
model: sonnet
skills:
  - skill-name
---
```

---

## Cowork-Specific Instructions

If you're in Cowork:

- You have subagents, so the main workflow works
- Use `--static <output_path>` for the eval viewer (no display)
- GENERATE THE EVAL VIEWER BEFORE evaluating inputs yourself
- Feedback downloads as `feedback.json` — read it from there

---

## Reference files

The `agents/` directory contains instructions for specialized subagents:

- `agents/grader.md` — How to evaluate assertions against outputs
- `agents/comparator.md` — How to do blind A/B comparison between two outputs
- `agents/analyzer.md` — How to analyze why one version beat another

The `references/` directory has additional documentation:
- `references/schemas.md` — JSON structures for evals.json, grading.json, etc.

---

The core loop:

- Figure out what the skill is about
- Draft or edit the skill
- Run test prompts
- Evaluate outputs with the user
- Create benchmark.json and run eval-viewer
- Repeat until satisfied
- Package the final skill
