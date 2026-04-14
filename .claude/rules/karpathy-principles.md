# Karpathy Principles

Four principles that govern how Claude (and delivery agents) approach every workflow phase. Derived from [Andrej Karpathy's observations](https://x.com/karpathy/status/2015883857489522876) on LLM coding pitfalls. These are project-wide rules — reviewers MUST cite violations and agents MUST follow them.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

- State assumptions explicitly — if uncertain, ask rather than guess
- Present multiple interpretations when the requirement is ambiguous — don't silently pick one
- Push back when a simpler approach exists
- Stop and ask when confused — name what's unclear

**Applies to:** plan / analyze / discovery / design phases.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked
- No abstractions for single-use code
- No "flexibility" or "configurability" that wasn't requested
- No error handling for impossible scenarios
- If 200 lines could be 50, rewrite

**The test:** Would a senior engineer say this is overcomplicated? If yes, simplify.

**Applies to:** implement / fix phases and all delivery agents.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

- Don't "improve" adjacent code, comments, or formatting
- Don't refactor things that aren't broken
- Match existing style, even if you'd do it differently
- If you notice unrelated dead code, mention it — don't delete it
- Remove imports/variables/functions that *your* changes made unused; don't remove pre-existing dead code unless asked

**The test:** Every changed line should trace directly to the user's request (or an active ticket).

**Applies to:** implement / fix phases, all delivery agents, swarm workers.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

- Transform imperative tasks into verifiable goals before starting
- Verify against the explicit success criteria, not against style preferences
- Loop until goals met — don't declare done prematurely
- If success can't be verified, say so explicitly rather than claim success

**Applies to:** verify / review phases, code reviewers.

## Enforcement

- Workflow coordinators (`spec`, `spec-complex`, `bug`, `swarm`) cite the relevant principle at each phase heading.
- `delivery-code-reviewer` MUST flag violations of principles 2 and 3 as `[should_fix]` or `[must_fix]` depending on severity.
- Delivery agents MUST apply principles 2 and 3 during implementation.
