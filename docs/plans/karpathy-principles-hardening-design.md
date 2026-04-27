# TDD: Karpathy Principles Hardening

## Context

The Karpathy principles (`Think Before Coding`, `Simplicity First`, `Surgical Changes`, `Goal-Driven Execution`) are declared in `.claude/rules/karpathy-principles.md` and mirrored in `cmd/stratus/rules/karpathy-principles.md`. The current enforcement surface is thin:

- The **`delivery-code-reviewer`** agent is the designated enforcer of principles 2 and 3, but its embedded prompt does not carry concrete anchor heuristics — reviewers must infer what "overcomplicated" or "unrelated changes" means on a case-by-case basis.
- The **`spec-complex` coordinator skill** cites the principles in phase headings (plan/design/implement/verify) but does not ask the Plan subagent to declare a simplicity budget, nor does it force a retrospective during `learn`.
- Other **delivery agents** (`delivery-*-engineer`, `delivery-implementation-expert`, `delivery-debugger`, `delivery-system-architect`, `delivery-strategic-architect`, etc.) do not reference the principles at all; they rely solely on reviewer catches.
- **OpenCode parity** is required: each change in `cmd/stratus/agents/` or `cmd/stratus/agents-opencode/` must land in both directories in the same commit, as per the `writeAssetsTo` refactor documented in the project memory.

The strategic analysis collapsed 6 candidate deliverables into 4 actionable ones. This TDD specifies them.

## Goals and Non-Goals

### Goals

1. Give reviewers concrete, verifiable anchor heuristics for principles 2 and 3.
2. Force the spec-complex coordinator's Plan phase to emit a simplicity budget and reject plans that exceed it without justification.
3. Force a short Karpathy retrospective on `learn → complete` for spec-complex workflows, stored as a tagged memory.
4. Cross-link delivery agents to the canonical rules file so the principles are a first-class concern during implementation (not a post-hoc review).
5. Keep canonical `karpathy-principles.md` aligned with the reviewer heuristics (single source of truth).

### Non-Goals

- No changes to `db/governance.go` (governance indexing already works; see strategic analysis).
- No separate plan-template file — the budget section is specified inline in `SKILL.md`.
- No modifications to the bundled `Plan` subagent — the coordinator prompt is the only lever.
- No new API endpoints, no wiki automation, no new hook types. The retrospective mechanism is a tagged memory save via `mcp__stratus__save_memory`.
- No new `orchestration/` state-machine transitions. Phase transitions remain as defined in `orchestration/state.go`.

## Scope (4 deliverables)

| # | Area | Files touched | Surface |
|---|------|---------------|---------|
| 1 | Reviewer hardening | 2 | Prompt edits to `delivery-code-reviewer.md` (CC + OpenCode) |
| 2 | spec-complex coordinator hardening | 1 | `cmd/stratus/skills/spec-complex/SKILL.md` |
| 3 | Delivery-agent cross-links | 22 | One-liner insert across 11 agents x 2 embedded dirs (3 agents excluded per Principle 3) |
| 4 | Canonical rules alignment | 2 | `.claude/rules/karpathy-principles.md` + mirror in `cmd/stratus/rules/` |

Total: **27 files**. Target: **< 600 LOC added**.

---

## Deliverable 1: Reviewer Hardening

### Target files

- `cmd/stratus/agents/delivery-code-reviewer.md`
- `cmd/stratus/agents-opencode/delivery-code-reviewer.md`

### Before / After (prompt diff)

**Before** (structure today): The reviewer prompt lists review checklist items and references `review-verdict-format.md`. Karpathy principles are mentioned only indirectly through the rules file. Principle 2/3 violations are not first-class severity items in the verdict format section.

**After**: Insert a new section titled **"Karpathy Enforcement"** immediately after the existing "Responsibilities" section (or its equivalent heading) and before the "Tools" section. Add the governance-fetch instruction and the anchor heuristics verbatim.

Exact markdown to insert (both CC and OpenCode copies):

```markdown
## Karpathy Enforcement

Before issuing any verdict, fetch the canonical principles:

    retrieve(corpus="governance", query="karpathy")

Principles 2 (Simplicity First) and 3 (Surgical Changes) are first-class review concerns. Use the following **anchor heuristics** — they are guidance, not hard rules. A single anchor firing is never automatically a must_fix; treat each as a prompt to challenge the author.

> **Severity vocabulary:** these anchor heuristics use code-review severities (`[must_fix]` / `[should_fix]` per `.claude/rules/review-verdict-format.md`), NOT governance-review severities (`[must_update]` / `[should_update]`).

### Anchor heuristics

Flag as `[should_fix]` when **any** of the following holds:

- The diff exceeds **500 LOC** of non-generated, non-test code without a referenced ticket, ADR, or workflow id that justifies the size.
- A new abstraction (interface, base class, generic helper, config struct, factory) has **fewer than 2 call sites** in the same diff, and there is no comment or plan reference to a documented near-term second caller.
- The diff contains **unrelated style, rename, or formatting changes** — lines that do not trace to the stated task.
- A new file is added where an existing file of the same responsibility already exists, without a stated reason.
- Error handling is added for scenarios the code path cannot produce (impossible branches).

### Escalation to `[must_fix]`

Escalate to `[must_fix]` only when:

- The author cannot cite a justification when challenged, **or**
- The violation also breaks a spec acceptance criterion, security rule, or data-integrity invariant.

### Verdict format reminder

Principle 2/3 violations are first-class severity items. A review that cites an anchor heuristic but omits the corresponding `[should_fix]` or `[must_fix]` tag is itself a verdict-format violation per `.claude/rules/review-verdict-format.md`.
```

### Acceptance

Verifiable via grep:

```bash
# Section header present in both copies
grep -l "^## Karpathy Enforcement" cmd/stratus/agents/delivery-code-reviewer.md cmd/stratus/agents-opencode/delivery-code-reviewer.md | wc -l
# expected: 2

# Governance-retrieve instruction present
grep -c 'retrieve(corpus="governance", query="karpathy")' cmd/stratus/agents/delivery-code-reviewer.md
# expected: 1

# 500 LOC anchor present
grep -c "500 LOC" cmd/stratus/agents/delivery-code-reviewer.md
# expected: >= 1
```

Manual: run the reviewer on a synthetic 800-LOC diff with no ticket reference and confirm it issues a `[should_fix]` citing the diff-size anchor.

---

## Deliverable 2: Coordinator Hardening

### Target file

- `cmd/stratus/skills/spec-complex/SKILL.md`

### Phase 4 (Plan) — Budget Section Spec

The coordinator must instruct the Plan subagent to emit a **Budget (Principle 2)** section at the end of the plan. The section has exactly **4 fields**. The coordinator rejects the plan output if the section is missing or any field is empty.

#### Exact markdown (coordinator will inline this in its Plan prompt)

```markdown
## Budget (Principle 2)

- **Estimated LOC:** <integer — non-generated, non-test lines to be added or modified>
- **New files:** <integer — count of new source/config files>
- **New abstractions:** <integer — count of new interfaces/base classes/generic helpers/factories; put 0 if none>
- **Out of scope (explicit):** <bulleted list; at least one item; say "none known" if truly nothing>
```

#### Coordinator instructions to insert at Phase 4

```markdown
### Karpathy check on plan output (Principle 2)

After the Plan subagent returns, the coordinator MUST:

1. Confirm a `## Budget (Principle 2)` section is present with all 4 fields populated.
2. If any of the 4 fields is missing, re-prompt the Plan subagent with: *"Your plan is missing a populated Budget (Principle 2) section. Re-emit with all 4 fields filled."*
3. Only advance to `discovery` (or to `implement` in the simple branch) once the budget section is present with all 4 fields.

No task-count comparison, no sanity-multiplier validation, no re-prompt loop counts. The check is presence-only; reviewer (verify phase) judges whether the budget is realistic.
```

### Phase 4 — Plan Rejection Criteria (presence-only)

The coordinator rejects a Plan output when **any** of the following is true:

- Missing the literal heading `## Budget (Principle 2)`.
- Any of the 4 fields is missing from the section.

No counting, parsing, or sanity-multiplier logic. Validation reduces to a presence check on the heading and its 4 sub-fields. Anything beyond presence (e.g. judging whether LOC<200 with task count>10 is reasonable) is the reviewer's job in the verify phase, not the coordinator's job in the plan phase.

### Phase 7 (Learn) — Karpathy citation header

Currently `Phase 7 — Learn` lacks the Karpathy heading citation that other phases (plan/design/implement/verify) carry. Add:

```markdown
## Phase 7 — Learn (Principle 4: Goal-Driven Execution; Principle 2 retrospective)
```

### Phase 7 (Learn) — Retrospective Spec

Insert a new **Step 1a** immediately after the existing Step 1 (lessons capture) and before any transition to `complete`.

#### Exact markdown for Step 1a

```markdown
### Step 1a — Karpathy retrospective (MANDATORY)

Before the coordinator may transition `learn → complete`, it MUST save a memory tagged `karpathy-retro` using the following shape (note: `mcp__stratus__save_memory` has NO `category` field — the real schema is `text, title, type, tags, actor, scope, importance, refs, ttl, dedupe_key, project`):

    mcp__stratus__save_memory
      type: "decision"
      tags: ["karpathy-retro", "<workflow_id>"]
      text: |
        Workflow: <workflow_id>
        Hardest principle: <1|2|3|4>
        Why: <one or more sentences explaining what made this principle difficult in this workflow>
        Mitigation next time: <one or more sentences describing what the next workflow will do differently>

**Minimum content shape** (a reviewer-caught convention; see Enforcement note below):

- `Hardest principle:` line with a single digit `1`, `2`, `3`, or `4`.
- `Why:` line with at least one non-empty sentence (>= 1 period or >= 20 characters of prose).
- `Mitigation next time:` line with at least one non-empty sentence (>= 1 period or >= 20 characters of prose).

**Coordinator acceptance check (prompt-level):** Before issuing `transition_phase → complete`, the coordinator MUST verify that a `save_memory` call has been made in this phase whose `tags` contain both `karpathy-retro` and the current `workflow_id`. If absent, the coordinator re-prompts itself / the user for the retrospective.

**Enforcement (prompt-level, NOT a machine gate):** This is a coordinator-prompt requirement, not a hard state-machine gate. The state machine in `orchestration/coordinator.go` does not inspect memory events. A non-compliant coordinator that skips the retrospective can still transition to `complete`. Acceptance is manual / reviewer-caught. No `orchestration/` code changes are made as part of this workflow.
```

### Acceptance

Verifiable via grep:

```bash
# Budget section spec present
grep -c "^## Budget (Principle 2)" cmd/stratus/skills/spec-complex/SKILL.md
# expected: >= 1

# Phase 7 citation added
grep -c "Phase 7 — Learn (Principle 4" cmd/stratus/skills/spec-complex/SKILL.md
# expected: 1

# Retrospective tag named
grep -c "karpathy-retro" cmd/stratus/skills/spec-complex/SKILL.md
# expected: >= 1

# Step 1a exists
grep -c "Step 1a" cmd/stratus/skills/spec-complex/SKILL.md
# expected: >= 1
```

Manual: dry-run a spec-complex workflow and confirm
(a) a plan missing the Budget section triggers a coordinator re-prompt;
(b) the coordinator self-checks for a `karpathy-retro`-tagged memory before issuing `transition_phase → complete` and re-prompts itself / the user when absent. Note: this is a prompt-level convention; the orchestration state machine does not enforce it, so a non-compliant coordinator can still complete the workflow.

---

## Deliverable 3: Delivery Agent Cross-Links

### Exact one-liner

```markdown
> Follow `.claude/rules/karpathy-principles.md` — especially Principles 2 (Simplicity) and 3 (Surgical Changes). Use `retrieve(corpus='governance', query='karpathy')` if context is missing.
```

The line is a single blockquote (one line in the rendered markdown). It is **the same text** in both the Claude Code copy and the OpenCode copy.

### Insertion rule

Insert the one-liner **immediately before** the `## Tools` heading. If an agent's file uses a different heading for its tool list (e.g. `## Available Tools`, `## Capabilities`), insert immediately before that heading. If an agent has no tools heading at all, insert immediately after the YAML frontmatter (after the closing `---`) and before the first `## ` heading.

No existing line is modified. No blank lines are removed. One blank line separates the quote from the surrounding content on each side.

### Files list (22 — narrowed per Principle 3 surgical scope)

The `cmd/stratus/agents/` and `cmd/stratus/agents-opencode/` directories each contain **14** delivery agents. The cross-link applies to **11** of them per directory (22 total). Three are intentionally excluded.

**Verified actual set (14 per directory):**

```
delivery-backend-engineer.md
delivery-code-reviewer.md
delivery-database-engineer.md
delivery-debugger.md
delivery-devops-engineer.md
delivery-frontend-engineer.md
delivery-governance-checker.md
delivery-implementation-expert.md
delivery-mobile-engineer.md
delivery-qa-engineer.md
delivery-skill-creator.md
delivery-strategic-architect.md
delivery-system-architect.md
delivery-ux-designer.md
```

**Include — 11 agents per directory (22 files total):**

- `delivery-backend-engineer.md`
- `delivery-frontend-engineer.md`
- `delivery-database-engineer.md`
- `delivery-devops-engineer.md`
- `delivery-mobile-engineer.md`
- `delivery-qa-engineer.md`
- `delivery-implementation-expert.md`
- `delivery-debugger.md` (writes diagnostic reports; Principle 3 applies to scope of investigation)
- `delivery-ux-designer.md` (writes design artifacts; Principle 2 applies)
- `delivery-system-architect.md` (produces specs/ADRs; Principles 2/3 apply to design complexity)
- `delivery-strategic-architect.md` (produces specs/ADRs; Principles 2/3 apply to design complexity)

**Exclude — 3 agents per directory:**

- `delivery-code-reviewer.md` — receives the full "Karpathy Enforcement" section from Deliverable 1; a one-liner here would be redundant.
- `delivery-governance-checker.md` — meta-reviewer; checks other artifacts for compliance, not a producer of artifacts itself.
- `delivery-skill-creator.md` — tooling for agent/skill authoring; scope is meta and unrelated to delivery diffs.

**Note for implementer:** before editing, re-run `ls cmd/stratus/agents/delivery-*.md` and `ls cmd/stratus/agents-opencode/delivery-*.md` and confirm both sides still enumerate the same 14 filenames. If the sets differ, surface the mismatch as a blocker — do not silently add/drop files.

Total: **22 files** edited under D3 (11 agents × 2 directories). Combined with Deliverable 1 (which edits the reviewer in both directories), **12 of 14** delivery agents per directory will reference `karpathy-principles.md` after this workflow.

### Special case: delivery-code-reviewer

Per the narrowed scope above, the reviewer is **excluded from the D3 one-liner** because it already receives the full "Karpathy Enforcement" section from Deliverable 1. Adding a redundant one-liner pointer would violate Principle 3 (surgical scope).

### Acceptance

Verifiable via grep:

```bash
# Confirm 14 delivery agents per directory (sanity check on directory inventory)
ls cmd/stratus/agents/delivery-*.md | wc -l            # expected: 14
ls cmd/stratus/agents-opencode/delivery-*.md | wc -l   # expected: 14

# 12 of 14 agents reference karpathy-principles.md (11 via D3 one-liner + 1 via D1 full section)
grep -l 'karpathy-principles.md' cmd/stratus/agents/delivery-*.md | wc -l            # expected: 12
grep -l 'karpathy-principles.md' cmd/stratus/agents-opencode/delivery-*.md | wc -l   # expected: 12

# Excluded agents do NOT contain the rule reference (skill-creator, governance-checker)
grep -L 'karpathy-principles.md' cmd/stratus/agents/delivery-*.md
# expected output (2 files): delivery-governance-checker.md, delivery-skill-creator.md

# Exact one-liner present in each of the 11 included agents (substring check)
grep -c "especially Principles 2 (Simplicity) and 3 (Surgical Changes)" cmd/stratus/agents/delivery-backend-engineer.md
# expected: 1 (repeat for each of the 11 included agents)
```

Manual: diff two paired files (`cmd/stratus/agents/delivery-backend-engineer.md` vs `cmd/stratus/agents-opencode/delivery-backend-engineer.md`) and confirm the one-liner appears at the same relative position in both.

---

## Deliverable 4: Rules Canonical Update

### Target files

- `.claude/rules/karpathy-principles.md`
- `cmd/stratus/rules/karpathy-principles.md`

### Before / After for the "Enforcement" section

**Before** (current text, quoted from `.claude/rules/karpathy-principles.md`):

```markdown
## Enforcement

- Workflow coordinators (`spec`, `spec-complex`, `bug`, `swarm`) cite the relevant principle at each phase heading.
- `delivery-code-reviewer` MUST flag violations of principles 2 and 3 as `[should_fix]` or `[must_fix]` depending on severity.
- Delivery agents MUST apply principles 2 and 3 during implementation.
```

**After** — replace the section body with:

```markdown
## Enforcement

- Workflow coordinators (`spec`, `spec-complex`, `bug`, `swarm`) cite the relevant principle at each phase heading. The `spec-complex` coordinator additionally, **at the prompt level** (no state-machine gate): (a) requires a populated `## Budget (Principle 2)` section in every plan, and (b) requires a `karpathy-retro`-tagged memory to be saved before transitioning `learn → complete`. These are coordinator-prompt requirements; `orchestration/coordinator.go` does not inspect plan content or memory events, so non-compliance is reviewer-caught rather than machine-blocked.
- `delivery-code-reviewer` MUST flag violations of principles 2 and 3 as `[should_fix]` or `[must_fix]` per the anchor heuristics:
  - `[should_fix]` when the diff exceeds **500 LOC** without a referenced ticket/ADR, OR a new abstraction has **fewer than 2 callers** without a documented near-term second caller, OR the diff contains unrelated style/rename changes.
  - Escalate to `[must_fix]` only when the author cannot cite a justification, or the violation also breaks a spec criterion, security rule, or data-integrity invariant.
- All delivery agents MUST reference this rule file in their agent prompt and MUST apply principles 2 and 3 during implementation. Delivery agents SHOULD call `retrieve(corpus="governance", query="karpathy")` when context is missing.
```

Both files receive **identical** replacement text. They must remain byte-identical after the edit, because `cmd/stratus/rules/` is what the binary embeds and `.claude/rules/` is what workflows read from disk in source checkouts.

### Acceptance

Verifiable via grep + diff:

```bash
# Both files contain the new anchor-heuristic wording
grep -c "500 LOC" .claude/rules/karpathy-principles.md cmd/stratus/rules/karpathy-principles.md
grep -c "fewer than 2 callers" .claude/rules/karpathy-principles.md cmd/stratus/rules/karpathy-principles.md
grep -c "karpathy-retro" .claude/rules/karpathy-principles.md cmd/stratus/rules/karpathy-principles.md

# Byte-identical
diff .claude/rules/karpathy-principles.md cmd/stratus/rules/karpathy-principles.md
# expected: (no output)
```

---

## Budget (Principle 2)

*(Eating our own dog food — this TDD declares the budget for its own implementation.)*

### Estimated LOC

Approximately **400 LOC** added across 27 files:

| Deliverable | Files | LOC per file | Subtotal |
|-------------|-------|--------------|----------|
| D1 Reviewer hardening | 2 | ~55 | ~110 |
| D2 Coordinator hardening | 1 | ~110 | ~110 |
| D3 Delivery cross-links | 22 | 3 (one-liner + blank lines) | ~66 |
| D4 Rules canonical update | 2 | ~55 (replaces ~8) net ~47 | ~94 |

Gross add: ~400. Net add (after counting replacements in D4): ~380. Well under the 600 LOC cap.

### New files

**0.** All changes are edits to existing files. No new files, no new packages, no new embeds.

### New abstractions

**None.** No new Go types, no new MCP tools, no new endpoints, no new skill files, no new hook handlers.

### Out of scope (explicit)

- `db/governance.go` (indexing already works).
- A separate plan-template file (Budget spec lives inline in `SKILL.md`).
- Modifications to the bundled `Plan` subagent.
- Any retrospective hook beyond the memory save.
- Changes to `orchestration/state.go` phase transitions.
- Non-spec-complex coordinators (`spec` simple, `bug`, `e2e`, `swarm`). If they need similar hardening, that is a follow-up workflow.

---

## Rollout / Sync Notes

1. **Hash-based refresh skip behavior**: `stratus refresh` performs smart 3-way merge and skips files whose on-disk hash has diverged from the previous embed. Users who edited their local `.claude/rules/karpathy-principles.md` or delivery agent copies **will not auto-pick-up** these changes on refresh. Release notes MUST call this out and suggest `stratus refresh --force` for affected files (if the force flag exists) or a manual copy.
2. **Claude Code + OpenCode parity**: every paired file (CC copy + OpenCode copy) MUST be edited in the **same commit**. Do not split by target. A failed parity check blocks merge.
3. **Rebuild + release required for binary distribution**: changes under `cmd/stratus/agents/`, `cmd/stratus/agents-opencode/`, `cmd/stratus/skills/`, and `cmd/stratus/rules/` are embedded via `go:embed`. They do not reach end users until a new binary is built and released. Follow the memory rule: use `make release VERSION=x.y.z` — never manual tagging.
4. **Frontend is untouched**. No `npm run build` needed for this workflow.
5. **Skills directory is shared**: `cmd/stratus/skills/spec-complex/SKILL.md` is read natively by both Claude Code (via `.claude/skills/`) and OpenCode (which reads `.claude/skills/` directly — no duplication). Only one edit for D2.
6. **Memory tag name `karpathy-retro`** is a new convention. Audit existing tag conventions before merge to avoid collision. (Quick check: `grep -r "karpathy-retro" . --include='*.go' --include='*.md'` should be empty before this workflow.)

---

## Test Strategy

This workflow is a prompt/docs change. There is no production Go code to unit-test. Verification is layered:

### Layer 1 — Grep-based acceptance (automated-capable)

Each deliverable above includes an explicit `grep` recipe. A short shell script can bundle them into a CI-style check:

```bash
# D1
[ "$(grep -l '^## Karpathy Enforcement' cmd/stratus/agents/delivery-code-reviewer.md cmd/stratus/agents-opencode/delivery-code-reviewer.md | wc -l)" = "2" ] || echo "D1 FAIL"

# D2
grep -q '^## Budget (Principle 2)' cmd/stratus/skills/spec-complex/SKILL.md || echo "D2 budget FAIL"
grep -q 'Phase 7 — Learn (Principle 4' cmd/stratus/skills/spec-complex/SKILL.md || echo "D2 phase7 FAIL"
grep -q 'karpathy-retro' cmd/stratus/skills/spec-complex/SKILL.md || echo "D2 retro tag FAIL"
grep -q 'Step 1a' cmd/stratus/skills/spec-complex/SKILL.md || echo "D2 step1a FAIL"

# D3 — 12 = 11 cross-linked + 1 reviewer-with-full-section
[ "$(grep -l 'karpathy-principles.md' cmd/stratus/agents/delivery-*.md | wc -l)" = "12" ] || echo "D3 CC FAIL"
[ "$(grep -l 'karpathy-principles.md' cmd/stratus/agents-opencode/delivery-*.md | wc -l)" = "12" ] || echo "D3 OC FAIL"

# D4
diff .claude/rules/karpathy-principles.md cmd/stratus/rules/karpathy-principles.md || echo "D4 parity FAIL"
grep -q '500 LOC' .claude/rules/karpathy-principles.md || echo "D4 anchor FAIL"
```

### Layer 2 — Manual rehearsal (end-to-end)

- **Reviewer rehearsal**: feed the reviewer a synthetic diff of 800 LOC with no ticket reference; confirm a `[should_fix]` citing the 500-LOC anchor is issued.
- **Coordinator Plan-rejection rehearsal**: run a spec-complex workflow with a deliberately minimal plan (no Budget section); confirm the coordinator re-prompts the Plan subagent once for the missing Budget section.
- **Coordinator Learn-retrospective rehearsal**: run a spec-complex workflow to `learn` and attempt `transition_phase → complete` without first saving a `karpathy-retro` memory; confirm the coordinator (per its prompt) re-prompts for the retrospective rather than transitioning. Note: the state machine itself does not block — this rehearsal verifies the coordinator-prompt behavior, not an orchestration gate.

### Layer 3 — Existing Go tests

`go test ./...` must continue to pass. Since no Go code is edited, no test regression is expected. Run it anyway as a smoke test after rebuild.

### Layer 4 — Build smoke test

Build the binary and extract its embedded assets to a temp directory, then grep the extracted files (more reliable than `strings` against a stripped binary):

```bash
cd frontend && npm run build
cd .. && go build -o /tmp/stratus-test ./cmd/stratus

# Extract embedded assets to a clean temp dir
mkdir -p /tmp/stratus-verify && cd /tmp/stratus-verify && /tmp/stratus-test init --target both

# 12 agents per directory should reference the rule file (11 via D3 one-liner + 1 via D1 full section)
grep -l 'karpathy-principles.md' .claude/agents/delivery-*.md | wc -l   # expected: 12
grep -l 'karpathy-principles.md' .opencode/agents/delivery-*.md | wc -l # expected: 12

# Each of the 11 included agents (per directory) should have the exact one-liner substring
grep -c 'especially Principles 2 (Simplicity) and 3 (Surgical Changes)' .claude/agents/delivery-backend-engineer.md
# expected: 1 (repeat for each of the 11 included agents in both directories)
```

---

## Risks

| # | Risk | Likelihood | Impact | Mitigation |
|---|------|-----------|--------|------------|
| R1 | Local edits in `.claude/rules/` won't auto-update on `stratus refresh` due to hash-based skip | High | Medium | Document in release notes; instruct users to re-apply or use `--force` |
| R2 | OpenCode copy drifts from CC copy in later patches | Medium | Medium | Parity acceptance grep in CI; PR template checkbox |
| R3 | Reviewer over-flags small diffs that happen to exceed 500 LOC due to codegen | Medium | Low | Heuristic explicitly excludes generated + test code; reviewer escalates only on challenge-failure |
| R4 | `karpathy-retro` tag collides with existing conventions | Low | Low | Pre-flight grep before merge (see Rollout Note 6) |
| R5 | Plan subagent ignores the Budget instruction | Low | Medium | Coordinator re-prompts once; on second failure, surfaces the missing-budget error to the user rather than hanging |
| R6 | The 11-file inclusion list drifts (new producer agent added but not cross-linked, or a previously-excluded meta agent gains producer responsibilities) | Medium | Low | Acceptance grep pins count to 12 (11 + reviewer); adding a new producer agent or reclassifying an excluded one requires updating both this TDD and the inclusion/exclusion list in D3 |
| R7 | End users on older binaries never see the change until they upgrade | Certain | Low | Expected — this is a release-gated change. Call out in release notes. |
| R8 | Reviewers interpret "anchor heuristics" as hard rules and cause review friction | Medium | Medium | Prompt text explicitly says "guidance, not hard rules" and requires author-challenge before escalation |
| R9 | Prompt-level enforcement (Budget section, retrospective memory) is bypassable — a non-compliant coordinator can skip both and still transition to `complete` | Medium | Low | **Accepted tradeoff.** The alternative is a new hook or state-machine gate, which would be a clear Principle 2 scope creep (new orchestration code, new hook handler, new tests) for a feature whose purpose is institutional memory rather than audit. Occasional misses are tolerable; reviewers in subsequent workflows can flag missing retrospectives via memory queries. |

