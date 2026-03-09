package workflow_synthesis

type Config struct {
	MinConfidence         float64
	MinSampleSize         int
	MinSuccessRateDelta   float64
	MinCycleTimeReduction float64
	MaxCandidatesPerRun   int
	DefaultExperimentSize int
}

func DefaultConfig() Config {
	return Config{
		MinConfidence:         0.70,
		MinSampleSize:         20,
		MinSuccessRateDelta:   0.10,
		MinCycleTimeReduction: 0.15,
		MaxCandidatesPerRun:   5,
		DefaultExperimentSize: 100,
	}
}
