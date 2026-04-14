package scoring

// Hypothesis is a minimal candidate shape. The generators (T6) produce these;
// scorer consumes them. Fields are kept stable — proposal_writer (T7) will persist
// a superset.
type Hypothesis struct {
	Category   string   // e.g., "refactor_opportunity"
	Title      string
	Rationale  string   // short natural-language justification
	FileRefs   []string // absolute or project-relative paths
	SymbolRefs []string // optional: function/type names
	SignalRefs []string // sorted, for idempotency hash
}

// StaticScores holds the deterministic signal scores derived from the baseline Bundle.
// Each field is clamped to [0, 1].
type StaticScores struct {
	Churn        float64 // 0..1
	TestGap      float64
	TODO         float64
	Staleness    float64
	ADRViolation float64
}

// LLMScores holds scores produced by the LLM judge (T5 — not this task).
// Fields are placeholders for the blend function.
// Note on Effort: higher Effort means MORE effort required (i.e., harder).
// The blender inverts this: it uses (1 - Effort) to compute the LLM effort contribution,
// so that cheaper proposals score higher.
type LLMScores struct {
	Impact     float64 // 0..1
	Effort     float64 // 0..1 (higher = more effort required, i.e. harder)
	Confidence float64
	Novelty    float64
}

// Blended holds the final weighted score and per-component breakdown.
type Blended struct {
	Final float64 // 0..1, clamped
	Static StaticScores
	LLM    LLMScores
	// Breakdown contains per-component contribution (value*weight), useful for
	// debugging and telemetry.
	Breakdown map[string]float64
}

// clamp01 clamps x to [0, 1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

