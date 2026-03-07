package artifacts

import (
	"regexp"
	"strings"
)

type ProblemClassifier struct {
	patterns []classificationPattern
}

type classificationPattern struct {
	problemClass ProblemClass
	keywords     []string
	filePatterns []string
}

func NewProblemClassifier() *ProblemClassifier {
	return &ProblemClassifier{
		patterns: getDefaultClassificationPatterns(),
	}
}

func getDefaultClassificationPatterns() []classificationPattern {
	return []classificationPattern{
		{
			problemClass: ProblemRaceCondition,
			keywords:     []string{"race condition", "concurrent", "mutex", "lock", "atomic", "goroutine", "thread safe", "synchronization"},
			filePatterns: []string{"*_test.go", "*_lock*", "*_mutex*"},
		},
		{
			problemClass: ProblemNullPointer,
			keywords:     []string{"nil pointer", "null pointer", "nil dereference", "undefined", "cannot read", "null reference", "NullPointerException"},
		},
		{
			problemClass: ProblemTypeError,
			keywords:     []string{"type error", "type mismatch", "cannot convert", "invalid type", "unexpected type", "interface conversion"},
		},
		{
			problemClass: ProblemAuthIssue,
			keywords:     []string{"auth", "authentication", "authorization", "unauthorized", "forbidden", "token", "jwt", "session", "permission"},
			filePatterns: []string{"*auth*", "*middleware*", "*guard*"},
		},
		{
			problemClass: ProblemDatabaseTx,
			keywords:     []string{"transaction", "deadlock", "isolation", "commit", "rollback", "constraint", "foreign key", "unique violation", "database error"},
			filePatterns: []string{"*repository*", "*dao*", "*_sql*", "*migration*"},
		},
		{
			problemClass: ProblemAPIIntegration,
			keywords:     []string{"api", "http", "request", "response", "endpoint", "rest", "graphql", "fetch", "client", "timeout", "connection refused"},
			filePatterns: []string{"*client*", "*api*", "*http*", "*handler*"},
		},
		{
			problemClass: ProblemConfiguration,
			keywords:     []string{"config", "env", "environment", "settings", "options", "yaml", "json parse", "toml", "dotenv"},
			filePatterns: []string{".env*", "config.*", "settings.*"},
		},
		{
			problemClass: ProblemTestFailure,
			keywords:     []string{"test fail", "assertion", "expected", "got", "mock", "stub", "fixture"},
			filePatterns: []string{"*_test.*", "*_spec.*", "*.test.*"},
		},
		{
			problemClass: ProblemPerformance,
			keywords:     []string{"performance", "slow", "timeout", "memory", "cpu", "optimization", "benchmark", "latency", "throughput"},
		},
		{
			problemClass: ProblemMemoryLeak,
			keywords:     []string{"memory leak", "goroutine leak", "heap", "gc", "out of memory", "oom"},
		},
		{
			problemClass: ProblemConcurrency,
			keywords:     []string{"concurrent", "parallel", "async", "await", "channel", "goroutine", "worker", "pool"},
		},
		{
			problemClass: ProblemValidation,
			keywords:     []string{"validation", "validate", "invalid", "required", "schema", "format", "input"},
			filePatterns: []string{"*validator*", "*schema*", "*dto*"},
		},
		{
			problemClass: ProblemBuildError,
			keywords:     []string{"build fail", "compile error", "syntax error", "undefined symbol", "linker", "dependency"},
		},
		{
			problemClass: ProblemDependency,
			keywords:     []string{"dependency", "module", "package", "import", "version", "conflict", "mismatch"},
			filePatterns: []string{"go.mod", "package.json", "requirements.txt", "Cargo.toml"},
		},
	}
}

func (c *ProblemClassifier) Classify(events []EventForArtifact, filesChanged []string, agentsUsed []string) ProblemClass {
	scores := make(map[ProblemClass]int)

	for _, event := range events {
		for _, pattern := range c.patterns {
			if c.matchesEventKeywords(event, pattern) {
				scores[pattern.problemClass] += 3
			}
		}
	}

	for _, file := range filesChanged {
		for _, pattern := range c.patterns {
			if c.matchesFilePattern(file, pattern) {
				scores[pattern.problemClass] += 2
			}
		}
	}

	phaseLoop := detectPhaseLoop(events)
	if phaseLoop {
		if strings.Contains(phaseLoopPattern(events), "review") {
			scores[ProblemValidation] += 2
		}
		if strings.Contains(phaseLoopPattern(events), "fix") {
			scores[ProblemTestFailure] += 2
		}
	}

	var bestClass ProblemClass = ProblemUnknown
	bestScore := 0
	for class, score := range scores {
		if score > bestScore {
			bestScore = score
			bestClass = class
		}
	}

	return bestClass
}

func (c *ProblemClassifier) matchesEventKeywords(event EventForArtifact, pattern classificationPattern) bool {
	payloadStr := payloadToString(event.Payload)
	textLower := strings.ToLower(payloadStr)

	for _, keyword := range pattern.keywords {
		if strings.Contains(textLower, strings.ToLower(keyword)) {
			return true
		}
	}

	if errMsg, ok := event.Payload["error"].(string); ok {
		errLower := strings.ToLower(errMsg)
		for _, keyword := range pattern.keywords {
			if strings.Contains(errLower, strings.ToLower(keyword)) {
				return true
			}
		}
	}

	if reason, ok := event.Payload["reason"].(string); ok {
		reasonLower := strings.ToLower(reason)
		for _, keyword := range pattern.keywords {
			if strings.Contains(reasonLower, strings.ToLower(keyword)) {
				return true
			}
		}
	}

	return false
}

func (c *ProblemClassifier) matchesFilePattern(file string, pattern classificationPattern) bool {
	fileLower := strings.ToLower(file)
	for _, filePattern := range pattern.filePatterns {
		matched, err := matchSimplePattern(fileLower, strings.ToLower(filePattern))
		if err == nil && matched {
			return true
		}
	}
	return false
}

func matchSimplePattern(s, pattern string) (bool, error) {
	regexPattern := "^" + strings.ReplaceAll(pattern, "*", ".*") + "$"
	matched, err := regexp.MatchString(regexPattern, s)
	return matched, err
}

func payloadToString(payload map[string]any) string {
	var parts []string
	for k, v := range payload {
		parts = append(parts, k+": "+toString(v))
	}
	return strings.Join(parts, " ")
}

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		var strs []string
		for _, item := range val {
			strs = append(strs, toString(item))
		}
		return strings.Join(strs, " ")
	case map[string]any:
		return payloadToString(val)
	default:
		return ""
	}
}

func detectPhaseLoop(events []EventForArtifact) bool {
	phaseTransitions := 0
	for _, event := range events {
		if event.Type == "workflow.phase_transition" {
			phaseTransitions++
		}
	}
	return phaseTransitions > 5
}

func phaseLoopPattern(events []EventForArtifact) string {
	var phases []string
	for _, event := range events {
		if event.Type == "workflow.phase_transition" {
			if to, ok := event.Payload["to_phase"].(string); ok {
				phases = append(phases, to)
			}
		}
	}
	return strings.Join(phases, "->")
}

func (c *ProblemClassifier) InferTaskType(events []EventForArtifact, workflowType string) TaskType {
	switch workflowType {
	case "bug":
		return TaskTypeBugFix
	case "spec":
		for _, event := range events {
			if title, ok := event.Payload["title"].(string); ok {
				titleLower := strings.ToLower(title)
				if strings.Contains(titleLower, "test") || strings.Contains(titleLower, "spec") {
					return TaskTypeTest
				}
				if strings.Contains(titleLower, "refactor") || strings.Contains(titleLower, "clean") {
					return TaskTypeRefactor
				}
				if strings.Contains(titleLower, "doc") || strings.Contains(titleLower, "readme") {
					return TaskTypeDocs
				}
				if strings.Contains(titleLower, "add") || strings.Contains(titleLower, "implement") ||
					strings.Contains(titleLower, "feature") || strings.Contains(titleLower, "new") {
					return TaskTypeFeature
				}
			}
		}
		return TaskTypeFeature
	case "e2e":
		return TaskTypeTest
	default:
		return TaskTypeUnknown
	}
}

func (c *ProblemClassifier) ExtractRootCause(events []EventForArtifact) string {
	for _, event := range events {
		if reason, ok := event.Payload["reason"].(string); ok && reason != "" {
			return reason
		}
		if errMsg, ok := event.Payload["error"].(string); ok && errMsg != "" {
			return errMsg
		}
		if description, ok := event.Payload["description"].(string); ok && description != "" {
			return description
		}
	}
	return ""
}

func (c *ProblemClassifier) InferSolutionPattern(filesChanged []string, workflowType string, problemClass ProblemClass) string {
	if len(filesChanged) == 0 {
		return ""
	}

	patterns := make(map[string]int)

	for _, file := range filesChanged {
		if strings.Contains(file, "test") {
			patterns["add_tests"]++
		}
		if strings.Contains(file, "service") || strings.Contains(file, "handler") {
			patterns["service_fix"]++
		}
		if strings.Contains(file, "repository") || strings.Contains(file, "dao") {
			patterns["data_layer_fix"]++
		}
		if strings.Contains(file, "middleware") || strings.Contains(file, "interceptor") {
			patterns["middleware_fix"]++
		}
		if strings.Contains(file, "model") || strings.Contains(file, "entity") || strings.Contains(file, "dto") {
			patterns["model_fix"]++
		}
		if strings.Contains(file, "config") || strings.Contains(file, "settings") {
			patterns["config_fix"]++
		}
		if strings.HasSuffix(file, ".sql") || strings.Contains(file, "migration") {
			patterns["database_fix"]++
		}
	}

	switch problemClass {
	case ProblemRaceCondition:
		if patterns["service_fix"] > 0 {
			return "synchronization_wrapper"
		}
		return "concurrency_fix"
	case ProblemNullPointer:
		return "null_check"
	case ProblemDatabaseTx:
		if patterns["service_fix"] > 0 && patterns["data_layer_fix"] > 0 {
			return "transaction_wrapper"
		}
		return "database_fix"
	case ProblemAuthIssue:
		return "auth_middleware_fix"
	case ProblemAPIIntegration:
		return "api_client_fix"
	case ProblemTestFailure:
		return "test_fix"
	case ProblemConfiguration:
		return "config_fix"
	case ProblemPerformance:
		return "optimization"
	}

	var bestPattern string
	bestCount := 0
	for pattern, count := range patterns {
		if count > bestCount {
			bestCount = count
			bestPattern = pattern
		}
	}

	return bestPattern
}
