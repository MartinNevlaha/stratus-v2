# Stratus plugin evals

`claude plugin eval` cases for Stratus' skills. `make eval-plugin` builds the binary, writes a
project as `stratus refresh` does, and wraps its skills and agents in a plugin around the
`stratus` hooks plugin, since `claude plugin eval` takes a plugin (inside it the skills' agents
are `stratus:delivery-*`, as a plugin's agents are named). It then copies this suite in and runs
the eval. Extra arguments go to `claude plugin eval`, e.g.:

    make eval-plugin ARGS="--runs 1 --ablation none --max-cost-usd 5"

Each run calls the model on your account (cases × runs × arms). The cases need no Bash and no
MCP server: every scaffold writes `.stratus.json` with an unreachable port, so the plugin's hooks
never talk to a Stratus server running for another project. The `code-review-*` cases are phrased
the way a user asks, so they also check that the skill triggers on its own; the other cases call their
command, the way Stratus users run them, and check what the skill does (a user-typed command does
not go through the Skill tool, so those cases look for `forked-command-stratus:<skill>` in the
trace). The `code-review-*` cases require `stratus:code-review`, so Claude Code's own `/code-review`
does not count.

Use the default model, or pin one with `--model`. Haiku answers `code-review-verdict` without
triggering the skill about half the time; the default model triggered it in every run
(2026-10-06).

## Cases

| Case | What passing means |
|---|---|
| `code-review-verdict` | The skill triggers from a plain request, finds the empty-input division, and ends with a verdict |
| `code-review-clean-pass` | Correct, tested code gets `PASS`, not a false alarm |
| `code-review-sql-injection` | A query built by string concatenation gets `FAIL` and is named as injection |
| `create-architecture-adr` | An ADR is written under `docs/`, and nothing outside it |
| `find-bugs-root-cause` | The off-by-one is named, with no Edit or Write |

## Model and effort sweeps

Agents take their model and effort from their frontmatter in `cmd/stratus/agents/`, not from
`--model`. To check a different setting, run the suite with every agent overridden:

    STRATUS_EVAL_AGENT_EFFORT=medium make eval-plugin ARGS="--runs 3 --ablation none --max-cost-usd 5"
    STRATUS_EVAL_AGENT_MODEL=fable STRATUS_EVAL_AGENT_EFFORT=low make eval-plugin ARGS="--runs 3 --ablation none --max-cost-usd 5"

`STRATUS_EVAL_AGENT_MODEL` takes `sonnet`, `opus`, `haiku`, `fable` or `inherit`;
`STRATUS_EVAL_AGENT_EFFORT` takes `low`, `medium`, `high`, `xhigh` or `max`. Effort levels are
calibrated per model, so compare levels on the same model rather than carrying one over.

Decide what counts as success before the run: compare each arm with the current frontmatter at the
same `--runs` (3 or more), and adopt a cheaper model or lower effort only when every case keeps at
least 2 of 3 passes and no case drops below the current arm.
