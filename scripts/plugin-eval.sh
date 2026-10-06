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
# A plugin's agents are named <plugin>:<agent>; a bare name would fall back to the general-purpose agent.
perl -pi -e 's/^agent: delivery-/agent: stratus:delivery-/' "$plugin"/skills/*/SKILL.md
cp -R "$repo/evals/stratus" "$plugin/evals"

PATH="$work/bin:$PATH" claude plugin eval "$plugin" \
	--trust-plugin --scaffold --allow-tools Write Edit \
	--output-dir "$repo/evals/results/$(date +%Y%m%d-%H%M%S)" \
	"$@"
