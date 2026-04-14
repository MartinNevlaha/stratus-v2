package evolution_loop

import "github.com/MartinNevlaha/stratus-v2/config"

// Evolution sentinel errors — re-exported from the config package so callers
// that import evolution_loop can use errors.Is without importing config directly.
//
// The canonical definitions live in config to avoid a circular import
// (evolution_loop already imports config).
var (
	ErrTokenCapRequired      = config.ErrTokenCapRequired
	ErrInvalidScoringWeights = config.ErrInvalidScoringWeights
	ErrInvalidBaselineLimits = config.ErrInvalidBaselineLimits
	ErrInvalidCategory       = config.ErrInvalidCategory
)
