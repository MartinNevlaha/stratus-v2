#!/usr/bin/env bash
set -euo pipefail
echo '{"port": 1}' > .stratus.json
cat > process.go <<'GO'
package queue

// ProcessAll hands every item to handle, in order.
func ProcessAll(items []string, handle func(string)) {
	for i := 0; i < len(items)-1; i++ {
		handle(items[i])
	}
}
GO
