#!/usr/bin/env bash
# Runs the eval suite in evals/stratus against Stratus' skills and agents exactly as `stratus refresh`
# writes them, with this checkout's binary first on PATH so the hooks run the code under test.
# `claude plugin eval` takes a plugin, so the skills and agents are wrapped in one around the stratus
# hooks plugin. Extra arguments go to `claude plugin eval`.
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin" "$work/project"
go build -C "$repo" -o "$work/bin/stratus" ./cmd/stratus
(
	cd "$work/project"
	echo '{"port": 1}' >.stratus.json
	STRATUS_DATA_DIR="$work/data" "$work/bin/stratus" refresh --target claude-code >/dev/null
)

project="$work/project/.claude"
plugin="$work/stratus"
mkdir -p "$plugin/skills" "$plugin/agents"
cp -R "$project/skills/stratus/." "$plugin/" # manifest and hooks
for dir in "$project"/skills/*; do
	# the plugins next to the skills (stratus, mdview, stratus-hud) have no SKILL.md
	if [ -f "$dir/SKILL.md" ]; then
		cp -R "$dir" "$plugin/skills/"
	fi
done
cp "$project"/agents/*.md "$plugin/agents/"
# Sweep overrides: run every agent on one model and/or effort level (see evals/stratus/README.md).
# Only the first match is replaced, which is the frontmatter line.
case "${STRATUS_EVAL_AGENT_MODEL:-}" in
"") ;;
sonnet | opus | haiku | fable | inherit) perl -0pi -e "s/^model: .*\$/model: $STRATUS_EVAL_AGENT_MODEL/m" "$plugin"/agents/*.md ;;
*) echo "STRATUS_EVAL_AGENT_MODEL must be sonnet, opus, haiku, fable or inherit" >&2 && exit 2 ;;
esac
case "${STRATUS_EVAL_AGENT_EFFORT:-}" in
"") ;;
low | medium | high | xhigh | max) perl -0pi -e "s/^effort: .*\\n//m; s/^(model: .*\\n)/\${1}effort: $STRATUS_EVAL_AGENT_EFFORT\\n/m" "$plugin"/agents/*.md ;;
*) echo "STRATUS_EVAL_AGENT_EFFORT must be low, medium, high, xhigh or max" >&2 && exit 2 ;;
esac
# A plugin's agents are named <plugin>:<agent>; a bare name would fall back to the general-purpose agent.
perl -pi -e 's/^agent: delivery-/agent: stratus:delivery-/' "$plugin"/skills/*/SKILL.md
cp -R "$repo/evals/stratus" "$plugin/evals"

PATH="$work/bin:$PATH" claude plugin eval "$plugin" \
	--trust-plugin --scaffold --allow-tools Write Edit \
	--output-dir "$repo/evals/results/$(date +%Y%m%d-%H%M%S)" \
	"$@"
