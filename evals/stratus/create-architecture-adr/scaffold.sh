#!/usr/bin/env bash
set -euo pipefail
echo '{"port": 1}' > .stratus.json
mkdir -p docs/decisions
printf '# Architecture\n\nA small Go service; storage is still undecided.\n' > docs/architecture.md
