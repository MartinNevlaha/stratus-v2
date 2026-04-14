package code_analyst

// FileSignals holds raw signals collected for a single file.
type FileSignals struct {
	FilePath        string
	CommitCount     int
	LineCount       int
	TechDebtMarkers int
	TestFile        bool
	Language        string
}

// FileScore holds the composite ranking score for a file.
type FileScore struct {
	FilePath        string  `json:"file_path"`
	ChurnRate       float64 `json:"churn_rate"`
	Coverage        float64 `json:"coverage"`
	ComplexityProxy float64 `json:"complexity_proxy"`
	CompositeScore  float64 `json:"composite_score"`
	CommitCount     int     `json:"commit_count"`
	LineCount       int     `json:"line_count"`
	TechDebtMarkers int     `json:"tech_debt_markers"`
	LastAnalyzedAt  *string `json:"last_analyzed_at,omitempty"`
	LastGitHash     *string `json:"last_git_hash,omitempty"`
}

// AnalysisResult holds the output of analyzing a single file.
type AnalysisResult struct {
	Findings   []Finding
	TokensUsed int
}

// Finding is a single code quality finding from LLM analysis.
type Finding struct {
	Category    string         `json:"category"`
	Severity    string         `json:"severity"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	LineStart   int            `json:"line_start"`
	LineEnd     int            `json:"line_end"`
	Confidence  float64        `json:"confidence"`
	Suggestion  string         `json:"suggestion"`
	Evidence    map[string]any `json:"evidence,omitempty"`
}

// RunResult summarizes a completed code analysis run.
type RunResult struct {
	RunID            string
	FilesScanned     int
	FilesAnalyzed    int
	FindingsCount    int
	WikiPagesCreated int
	WikiPagesUpdated int
	DurationMs       int64
	TokensUsed       int
}
