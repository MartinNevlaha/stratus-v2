#!/usr/bin/env bash
set -euo pipefail
echo '{"port": 1}' > .stratus.json
cat > calc.go <<'GO'
package calc

import "errors"

// ErrEmpty is returned when there are no numbers to average.
var ErrEmpty = errors.New("calc: no numbers")

// Average returns the integer mean of xs, truncated toward zero, or ErrEmpty when xs is empty.
func Average(xs []int) (int, error) {
	if len(xs) == 0 {
		return 0, ErrEmpty
	}
	total := 0
	for _, x := range xs {
		total += x
	}
	return total / len(xs), nil
}
GO
cat > calc_test.go <<'GO'
package calc

import (
	"errors"
	"testing"
)

func TestAverage(t *testing.T) {
	tests := []struct {
		name string
		xs   []int
		want int
	}{
		{"single", []int{4}, 4},
		{"several", []int{1, 2, 3, 4}, 2},
		{"negative", []int{-3, -5}, -4},
	}
	for _, tt := range tests {
		got, err := Average(tt.xs)
		if err != nil || got != tt.want {
			t.Errorf("%s: Average(%v) = %d, %v; want %d, nil", tt.name, tt.xs, got, err, tt.want)
		}
	}
}

func TestAverage_Empty(t *testing.T) {
	if _, err := Average(nil); !errors.Is(err, ErrEmpty) {
		t.Errorf("Average(nil) error = %v, want ErrEmpty", err)
	}
}
GO
