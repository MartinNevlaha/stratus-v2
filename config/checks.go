package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Limits for the "checks" list of .stratus.json.
const (
	MaxChecks               = 8
	MaxCheckArgs            = 16
	MaxCheckArgLen          = 256
	DefaultCheckTimeoutSec  = 120
	MaxCheckTimeoutSec      = 900
	MaxTotalTimeoutSec      = 3600
	reservedPlaceholderName = "placeholder_scan"
	reservedArtifactsName   = "checks_artifacts"
)

// ErrInvalidChecks is returned when the "checks" list violates its rules.
var ErrInvalidChecks = errors.New("checks: invalid config")

var checkNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// CheckConfig is one project check run on a handoff: an argv (never a shell line)
// executed in the tree being handed off.
type CheckConfig struct {
	Name       string   `json:"name"`
	Cmd        []string `json:"cmd"`
	TimeoutSec int      `json:"timeout_sec,omitempty"` // 0 = DefaultCheckTimeoutSec
}

// ValidCheckName reports whether name is a well-formed check name.
func ValidCheckName(name string) bool {
	return checkNameRe.MatchString(name)
}

// ValidateChecks checks the "checks" list: at most MaxChecks entries; names match
// ^[a-z][a-z0-9_-]{0,31}$, are unique and not the reserved "placeholder_scan"; cmd is
// an argv of 1..MaxCheckArgs elements of 1..MaxCheckArgLen characters without NUL or
// newline; timeout_sec is 0 (default) or 1..MaxCheckTimeoutSec; the effective timeouts
// add up to at most MaxTotalTimeoutSec.
func ValidateChecks(checks []CheckConfig) error {
	if len(checks) > MaxChecks {
		return fmt.Errorf("%w: %d checks configured, at most %d are allowed", ErrInvalidChecks, len(checks), MaxChecks)
	}
	seen := map[string]bool{}
	total := 0
	for i, c := range checks {
		where := fmt.Sprintf("checks[%d]", i)
		switch {
		case !ValidCheckName(c.Name):
			return fmt.Errorf("%w: %s name %q must match ^[a-z][a-z0-9_-]{0,31}$", ErrInvalidChecks, where, c.Name)
		case c.Name == reservedPlaceholderName || c.Name == reservedArtifactsName:
			return fmt.Errorf("%w: %s name %q is reserved", ErrInvalidChecks, where, c.Name)
		case seen[c.Name]:
			return fmt.Errorf("%w: %s name %q is used twice", ErrInvalidChecks, where, c.Name)
		}
		seen[c.Name] = true
		if len(c.Cmd) < 1 || len(c.Cmd) > MaxCheckArgs {
			return fmt.Errorf("%w: %s (%s) cmd must have 1 to %d elements, has %d", ErrInvalidChecks, where, c.Name, MaxCheckArgs, len(c.Cmd))
		}
		for j, arg := range c.Cmd {
			if arg == "" || len(arg) > MaxCheckArgLen {
				return fmt.Errorf("%w: %s (%s) cmd[%d] must be 1 to %d characters", ErrInvalidChecks, where, c.Name, j, MaxCheckArgLen)
			}
			if strings.ContainsAny(arg, "\x00\n") {
				return fmt.Errorf("%w: %s (%s) cmd[%d] must not contain NUL or a newline", ErrInvalidChecks, where, c.Name, j)
			}
		}
		if c.TimeoutSec < 0 || c.TimeoutSec > MaxCheckTimeoutSec {
			return fmt.Errorf("%w: %s (%s) timeout_sec=%d must be 0 (default %d) or 1 to %d", ErrInvalidChecks, where, c.Name, c.TimeoutSec, DefaultCheckTimeoutSec, MaxCheckTimeoutSec)
		}
		total += effectiveTimeoutSec(c)
	}
	if total > MaxTotalTimeoutSec {
		return fmt.Errorf("%w: the timeouts add up to %d s, at most %d s are allowed in total", ErrInvalidChecks, total, MaxTotalTimeoutSec)
	}
	return nil
}

func effectiveTimeoutSec(c CheckConfig) int {
	if c.TimeoutSec == 0 {
		return DefaultCheckTimeoutSec
	}
	return c.TimeoutSec
}
