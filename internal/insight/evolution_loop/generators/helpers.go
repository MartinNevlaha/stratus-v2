package generators

import (
	"sort"
	"strings"
	"unicode"
)

// normalizeTitle lowercases, trims, collapses whitespace to a single space,
// and strips punctuation except '-' and '_'.
func normalizeTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))

	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		if r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		}
		// strip all other punctuation / symbols silently
	}
	return strings.TrimSpace(b.String())
}

// signalHashInputs returns the canonical string passed to sha256 for idempotency
// hashing of a hypothesis. It sorts signals before joining so that order does not
// matter. The caller (T7 proposal_writer) is responsible for actually hashing.
func signalHashInputs(category, title string, signals []string) string {
	return SignalHashInputs(category, title, signals)
}

// SignalHashInputs is the exported form of signalHashInputs used by the
// proposal_writer (T7) to compute idempotency hashes server-side.
func SignalHashInputs(category, title string, signals []string) string {
	sorted := make([]string, len(signals))
	copy(sorted, signals)
	sort.Strings(sorted)
	parts := []string{category, title}
	parts = append(parts, sorted...)
	return strings.Join(parts, "|")
}
