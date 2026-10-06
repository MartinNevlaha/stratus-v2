# Stratus plugin evals

`claude plugin eval` cases for Stratus' skills. `make eval-plugin` builds the binary, writes a
project as `stratus refresh` does, and wraps its skills and agents in a plugin around the
`stratus` hooks plugin, since `claude plugin eval` takes a plugin (inside it the skills' agents
are `stratus:delivery-*`, as a plugin's agents are named). It then copies this suite in and runs
the eval. Extra arguments go to `claude plugin eval`, e.g.:

    make eval-plugin ARGS="--runs 1 --ablation none --max-cost-usd 5"

Each run calls the model on your account (cases × runs × arms). The cases need no Bash and no
MCP server: every scaffold writes `.stratus.json` with an unreachable port, so the plugin's hooks
never talk to a Stratus server running for another project. `code-review-verdict` is phrased the
way a user asks, so it also checks that the skill triggers on its own; the other cases call their
command, the way Stratus users run them, and check what the skill does (a user-typed command does
not go through the Skill tool, so those cases look for `forked-command-stratus:<skill>` in the
trace). `code-review-verdict` requires `stratus:code-review`, so Claude Code's own `/code-review`
does not count.

Use the default model, or pin one with `--model`. Haiku answers `code-review-verdict` without
triggering the skill about half the time; the default model triggered it in every run
(2026-10-06).
