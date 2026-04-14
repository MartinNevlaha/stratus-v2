package evolution_loop

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	reNonAlphanumDash = regexp.MustCompile(`[^a-z0-9-]+`)
	reMultiDash       = regexp.MustCompile(`-{2,}`)
)

// toKebab converts an arbitrary string into a URL-safe kebab-case slug.
// Steps: lower-case, replace spaces/underscores with '-', strip non-alnum-dash,
// collapse runs of dashes, trim leading/trailing dashes.
func toKebab(s string) string {
	s = strings.ToLower(s)

	// Replace spaces and underscores with dashes.
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || r == '_' {
			b.WriteRune('-')
		} else {
			b.WriteRune(r)
		}
	}
	s = b.String()

	// Strip characters that are not a-z, 0-9, or '-'.
	s = reNonAlphanumDash.ReplaceAllString(s, "")

	// Collapse multiple dashes.
	s = reMultiDash.ReplaceAllString(s, "-")

	// Trim leading/trailing dashes.
	s = strings.Trim(s, "-")
	return s
}

// truncateRunes returns s truncated to at most n runes.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
