#!/usr/bin/env bash
set -euo pipefail
echo '{"port": 1}' > .stratus.json
cat > calc.go <<'GO'
package calc

// Sum adds the numbers.
func Sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

// Average returns the mean of the numbers.
func Average(xs []int) int {
	return Sum(xs) / len(xs)
}
GO
