package teams

import (
	"fmt"
	"strings"
)

// CanonicalRole accepts both team roles and existing delivery-* agent names.
func CanonicalRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	switch {
	case r == "member":
		return "member"
	case r == "" || r == "lead" || r == "coordinator":
		return "lead"
	case strings.Contains(r, "review") || strings.Contains(r, "governance"):
		return "review"
	case strings.Contains(r, "architect") || r == "design" || r == "plan" || r == "planning":
		return "design"
	case strings.Contains(r, "debug") || r == "diagnosis" || r == "analyze" || r == "analysis":
		return "diagnosis"
	case r == "mechanical" || r == "trivial":
		return "mechanical"
	default:
		return "implementation"
	}
}

// DefaultProfile returns the built-in profile for a role; see ResolveProfile.
func DefaultProfile(runtime, role, workflowType, complexity string) Profile {
	p, _ := ResolveProfile(runtime, role, workflowType, complexity)
	return p
}

// ResolveProfile returns the built-in profile for a runtime and role together
// with a short human-readable reason for the choice.
func ResolveProfile(runtime, role, workflowType, complexity string) (Profile, string) {
	p := Profile{Runtime: runtime}
	canonical := CanonicalRole(role)
	reason := "predvolené pre rolu " + canonical
	switch runtime {
	case "codex":
		p.Model, p.Effort = "gpt-6.1-sol", "high"
		switch canonical {
		case "design", "review", "diagnosis":
			p.Model = "gpt-6-astra"
			reason = "rola " + canonical + " → " + p.Model
			if isComplex(workflowType, complexity) {
				p.Effort = "xhigh"
				reason += ", náročná úloha → xhigh"
			}
		case "mechanical":
			p.Model, p.Effort = "gpt-6-luna", "medium"
			reason = "rola mechanical → " + p.Model
		}
	case "claude", "claude-code":
		p.Model, p.Effort = "sonnet", "high"
		switch canonical {
		case "design", "review", "diagnosis":
			p.Model = "opus"
			reason = "rola " + canonical + " → opus"
		case "mechanical":
			p.Model, p.Effort = "haiku", "medium"
			reason = "rola mechanical → haiku"
		}
	default:
		// OpenCode provider/model IDs are local configuration; inventing one would
		// silently replace the user's provider. Its profile must be supplied.
		reason = runtime + " nemá vstavaný model, treba profil projektu"
	}
	return p, reason
}

func normalizedLimits(l Limits) (Limits, error) {
	if l.MaxConcurrent == 0 {
		l.MaxConcurrent = 2
	}
	if l.MaxAutoRounds == 0 {
		l.MaxAutoRounds = 4
	}
	if l.MaxTurns == 0 {
		l.MaxTurns = 40
	}
	if l.MaxConcurrent < 1 || l.MaxConcurrent > 16 || l.MaxAutoRounds < 1 || l.MaxAutoRounds > 32 || l.MaxTurns < 1 || l.MaxTurns > 10000 || l.MaxTokens < 0 {
		return l, fmt.Errorf("invalid team limits (concurrency 1–16, automatic rounds 1–32, turns 1–10000, tokens >= 0)")
	}
	return l, nil
}

// ValidateProfile reports whether the runtime confirms the profile's model and effort.
func ValidateProfile(a Availability, p Profile) error {
	return validateAvailability(a, p)
}

func validateAvailability(a Availability, p Profile) error {
	if !a.Available {
		return fmt.Errorf("%s is not ready: %s", p.Runtime, a.Reason)
	}
	if p.Model == "" {
		return fmt.Errorf("%s requires an explicit model profile", p.Runtime)
	}
	for _, model := range a.Models {
		if model.ID == p.Model {
			if p.Effort == "" {
				return nil
			}
			for _, e := range model.Efforts {
				if e == p.Effort {
					return nil
				}
			}
			return fmt.Errorf("%s model %s does not confirm effort %s", p.Runtime, p.Model, p.Effort)
		}
	}
	return fmt.Errorf("%s does not confirm availability of model %s", p.Runtime, p.Model)
}

// Native adapters may verify an alias using their authenticated model catalog.
// Keep that equivalence separate from the actual model reported in the result.
func resultMatchesProfile(result Result, requested Profile) bool {
	modelMatches := result.Model == "" || result.Model == requested.Model
	if binding := result.ModelBinding; binding != nil && binding.Requested == requested.Model && binding.Resolved != "" && binding.Resolved == result.Model {
		modelMatches = true
	}
	return modelMatches && (result.Effort == "" || result.Effort == requested.Effort)
}
