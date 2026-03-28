package trajectory_engine

type Config struct {
	MaxActiveTrajectories  int     `json:"max_active_trajectories"`
	MinPatternOccurrences  int     `json:"min_pattern_occurrences"`
	MinConfidenceThreshold float64 `json:"min_confidence_threshold"`
	AnalysisBatchSize      int     `json:"analysis_batch_size"`
	MaxExampleTrajectories int     `json:"max_example_trajectories"`
}

func DefaultConfig() Config {
	return Config{
		MaxActiveTrajectories:  1000,
		MinPatternOccurrences:  3,
		MinConfidenceThreshold: 0.6,
		AnalysisBatchSize:      500,
		MaxExampleTrajectories: 5,
	}
}
