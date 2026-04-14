package baseline

import (
	"regexp"
	"strings"
	"sync"
)

// redactPattern pairs a compiled regexp with a replacement strategy.
// When wholeLine is true the entire matched line is replaced with [REDACTED];
// otherwise only the matched substring is replaced.
type redactPattern struct {
	re        *regexp.Regexp
	wholeLine bool
}

var (
	patOnce     sync.Once
	patterns    []redactPattern
)

func initPatterns() {
	raw := []struct {
		expr      string
		wholeLine bool
	}{
		// AWS access key — 20-char key starting with AKIA
		{`AKIA[0-9A-Z]{16}`, false},
		// AWS secret access key
		{`(?i)aws[_-]?secret[_-]?access[_-]?key["'\s:=]+[A-Za-z0-9/+=]{40}`, false},
		// Generic long token (api_key / secret / token / password / bearer)
		{`(?i)(api[_-]?key|secret|token|password|bearer)["'\s:=]+[A-Za-z0-9_\-\.]{20,}`, false},
		// PEM private key block — nuke the entire line
		{`-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`, true},
		// .env-style: KEY=<long value> — redact only the RHS
		{`(?m)^([A-Z_][A-Z0-9_]*\s*=\s*)\S{16,}$`, false},
		// GCP service account private_key marker
		{`"private_key":\s*"-----BEGIN`, true},
		// GitHub tokens
		{`gh[pousr]_[A-Za-z0-9]{36,}`, false},
		// GitLab personal access tokens
		{`glpat-[A-Za-z0-9_\-]{20,}`, false},
	}

	patterns = make([]redactPattern, 0, len(raw))
	for _, r := range raw {
		patterns = append(patterns, redactPattern{
			re:        regexp.MustCompile(r.expr),
			wholeLine: r.wholeLine,
		})
	}
}

// redactString applies all secret patterns to s and returns the cleaned string.
func redactString(s string) string {
	patOnce.Do(initPatterns)

	for _, p := range patterns {
		if p.wholeLine {
			// Replace the entire line that contains the match.
			lines := strings.Split(s, "\n")
			for i, line := range lines {
				if p.re.MatchString(line) {
					lines[i] = "[REDACTED]"
				}
			}
			s = strings.Join(lines, "\n")
		} else {
			// For .env-style pattern the group 1 captures the LHS; we want to
			// keep that and only replace the RHS. The pattern captures a
			// submatch when there's a named group (there isn't) — so we check
			// for the specific .env pattern by number of submatches.
			if p.re.NumSubexp() >= 1 {
				s = p.re.ReplaceAllStringFunc(s, func(match string) string {
					submatches := p.re.FindStringSubmatch(match)
					if len(submatches) >= 2 {
						// Keep prefix captured by group 1; redact the rest.
						return submatches[1] + "[REDACTED]"
					}
					return "[REDACTED]"
				})
			} else {
				s = p.re.ReplaceAllString(s, "[REDACTED]")
			}
		}
	}
	return s
}

// Redact applies secret-redaction in-place to the mutable fields of b
// (VexorHits[].Snippet, TODOs[].Text, GitCommits[].Subject) and returns b.
// If b is nil, Redact returns nil.
func Redact(b *Bundle) *Bundle {
	if b == nil {
		return nil
	}

	for i := range b.VexorHits {
		b.VexorHits[i].Snippet = redactString(b.VexorHits[i].Snippet)
	}

	for i := range b.TODOs {
		b.TODOs[i].Text = redactString(b.TODOs[i].Text)
	}

	for i := range b.GitCommits {
		b.GitCommits[i].Subject = redactString(b.GitCommits[i].Subject)
	}

	return b
}
