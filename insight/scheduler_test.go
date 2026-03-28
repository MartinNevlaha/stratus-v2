package insight

import "testing"

func TestNormalizeIntervalHours(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{name: "negative interval", input: -5, expected: 1},
		{name: "zero interval", input: 0, expected: 1},
		{name: "positive interval", input: 3, expected: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeIntervalHours(tt.input); got != tt.expected {
				t.Fatalf("normalizeIntervalHours(%d) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}
