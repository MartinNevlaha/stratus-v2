package trajectory_engine

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/google/uuid"
)

type TrajectoryAnalyzer struct {
	database *db.DB
	config   Config
}

func NewTrajectoryAnalyzer(database *db.DB, config Config) *TrajectoryAnalyzer {
	if config.MinPatternOccurrences <= 0 {
		config = DefaultConfig()
	}

	return &TrajectoryAnalyzer{
		database: database,
		config:   config,
	}
}

func (a *TrajectoryAnalyzer) Analyze(ctx context.Context) (*AnalysisResult, error) {
	trajectories, err := a.database.ListTrajectories(db.TrajectoryFilters{Limit: a.config.AnalysisBatchSize})
	if err != nil {
		return nil, fmt.Errorf("list trajectories: %w", err)
	}

	if len(trajectories) == 0 {
		return &AnalysisResult{}, nil
	}

	result := &AnalysisResult{
		TrajectoriesAnalyzed: len(trajectories),
	}

	result.FailurePoints = a.DetectFailurePoints(trajectories)
	result.SuccessfulPaths = a.DetectSuccessfulPaths(trajectories)
	result.AgentStepPatterns = a.DetectAgentStepPatterns(trajectories)
	result.Inefficiencies = a.DetectWorkflowInefficiencies(trajectories)

	patterns := a.ExtractPatterns(trajectories)
	result.PatternsExtracted = len(patterns)

	for i := range patterns {
		if err := a.database.SaveTrajectoryPattern(convertToDBPattern(&patterns[i])); err != nil {
			slog.Warn("failed to save trajectory pattern", "pattern_id", patterns[i].ID, "error", err)
		}
	}

	slog.Info("trajectory analysis complete",
		"trajectories_analyzed", result.TrajectoriesAnalyzed,
		"failure_points", len(result.FailurePoints),
		"successful_paths", len(result.SuccessfulPaths),
		"patterns_extracted", result.PatternsExtracted)

	return result, nil
}

func (a *TrajectoryAnalyzer) DetectFailurePoints(trajectories []db.Trajectory) []FailurePoint {
	stepFailures := make(map[string]*FailurePoint)

	for _, t := range trajectories {
		for _, step := range t.Steps {
			if !step.Success {
				key := fmt.Sprintf("%s|%s", step.AgentName, step.ActionType)
				fp, exists := stepFailures[key]
				if !exists {
					fp = &FailurePoint{
						AgentName:     step.AgentName,
						ActionType:    step.ActionType,
						CommonReasons: []string{},
					}
					stepFailures[key] = fp
				}
				fp.Occurrences++
				fp.StepNumber = step.StepNumber

				if step.OutputSummary != "" && len(fp.CommonReasons) < 10 {
					fp.CommonReasons = append(fp.CommonReasons, step.OutputSummary)
				}
			}
		}
	}

	totalSteps := 0
	for _, t := range trajectories {
		totalSteps += len(t.Steps)
	}

	var result []FailurePoint
	for _, fp := range stepFailures {
		fp.FailureRate = float64(fp.Occurrences) / float64(max(totalSteps, 1))
		if fp.Occurrences >= a.config.MinPatternOccurrences {
			result = append(result, *fp)
		}
	}

	slices.SortFunc(result, func(a, b FailurePoint) int {
		return int((b.FailureRate * float64(b.Occurrences)) - (a.FailureRate * float64(a.Occurrences)))
	})

	if len(result) > 20 {
		result = result[:20]
	}

	return result
}

func (a *TrajectoryAnalyzer) DetectSuccessfulPaths(trajectories []db.Trajectory) []AgentStepPattern {
	sequenceStats := make(map[string]*AgentStepPattern)

	for _, t := range trajectories {
		if t.FinalResult != "success" {
			continue
		}

		sequence := a.extractAgentSequence(t.Steps)
		if len(sequence) == 0 {
			continue
		}

		key := strings.Join(sequence, "→")
		stats, exists := sequenceStats[key]
		if !exists {
			stats = &AgentStepPattern{
				Sequence:    sequence,
				SequenceKey: key,
			}
			sequenceStats[key] = stats
		}

		stats.Occurrences++
		stats.SuccessRate += 1
		stats.AvgDuration += int64(t.CycleTimeMin)
	}

	var result []AgentStepPattern
	for _, stats := range sequenceStats {
		stats.SuccessRate = stats.SuccessRate / float64(stats.Occurrences)
		stats.AvgDuration = stats.AvgDuration / int64(stats.Occurrences)

		if stats.Occurrences >= a.config.MinPatternOccurrences {
			result = append(result, *stats)
		}
	}

	slices.SortFunc(result, func(a, b AgentStepPattern) int {
		if b.SuccessRate != a.SuccessRate {
			return int((b.SuccessRate - a.SuccessRate) * 1000)
		}
		return b.Occurrences - a.Occurrences
	})

	if len(result) > 15 {
		result = result[:15]
	}

	return result
}

func (a *TrajectoryAnalyzer) DetectAgentStepPatterns(trajectories []db.Trajectory) []AgentStepPattern {
	sequenceStats := make(map[string]*AgentStepPattern)

	for _, t := range trajectories {
		sequence := a.extractAgentSequence(t.Steps)
		if len(sequence) == 0 {
			continue
		}

		key := strings.Join(sequence, "→")
		stats, exists := sequenceStats[key]
		if !exists {
			stats = &AgentStepPattern{
				Sequence:    sequence,
				SequenceKey: key,
			}
			sequenceStats[key] = stats
		}

		stats.Occurrences++
		if t.FinalResult == "success" {
			stats.SuccessRate += 1
		}
		stats.AvgDuration += int64(t.CycleTimeMin)
		if t.TaskType != "" && !slices.Contains(stats.TaskTypes, t.TaskType) {
			stats.TaskTypes = append(stats.TaskTypes, t.TaskType)
		}
	}

	var result []AgentStepPattern
	for _, stats := range sequenceStats {
		stats.SuccessRate = stats.SuccessRate / float64(stats.Occurrences)
		stats.AvgDuration = stats.AvgDuration / int64(stats.Occurrences)

		if stats.Occurrences >= a.config.MinPatternOccurrences {
			result = append(result, *stats)
		}
	}

	slices.SortFunc(result, func(a, b AgentStepPattern) int {
		return b.Occurrences - a.Occurrences
	})

	if len(result) > 20 {
		result = result[:20]
	}

	return result
}

func (a *TrajectoryAnalyzer) DetectWorkflowInefficiencies(trajectories []db.Trajectory) []WorkflowInefficiency {
	var inefficiencies []WorkflowInefficiency

	loopCount := 0
	loopTimeLost := int64(0)
	for _, t := range trajectories {
		loops := a.detectPhaseLoops(t.Steps)
		if len(loops) > 0 {
			loopCount++
			loopTimeLost += int64(t.CycleTimeMin)
		}
	}
	if loopCount >= a.config.MinPatternOccurrences {
		inefficiencies = append(inefficiencies, WorkflowInefficiency{
			Type:        "phase_loop",
			Description: "Workflows contain phase transition loops",
			Impact:      "Increases cycle time and resource usage",
			Suggestion:  "Review phase transition logic to prevent unnecessary iterations",
			Confidence:  float64(loopCount) / float64(len(trajectories)),
			Occurrences: loopCount,
			AvgTimeLost: loopTimeLost / int64(max(loopCount, 1)),
		})
	}

	reviewFailCount := 0
	reviewFailTimeLost := int64(0)
	for _, t := range trajectories {
		if a.hasReviewFailures(t.Steps) {
			reviewFailCount++
			reviewFailTimeLost += int64(t.CycleTimeMin)
		}
	}
	if reviewFailCount >= a.config.MinPatternOccurrences {
		inefficiencies = append(inefficiencies, WorkflowInefficiency{
			Type:        "review_failures",
			Description: "Workflows have high review failure rates",
			Impact:      "Delays completion and requires rework",
			Suggestion:  "Improve initial code quality or add pre-review checks",
			Confidence:  float64(reviewFailCount) / float64(len(trajectories)),
			Occurrences: reviewFailCount,
			AvgTimeLost: reviewFailTimeLost / int64(max(reviewFailCount, 1)),
		})
	}

	agentFailureCount := 0
	for _, t := range trajectories {
		if a.hasAgentFailures(t.Steps) {
			agentFailureCount++
		}
	}
	if agentFailureCount >= a.config.MinPatternOccurrences {
		inefficiencies = append(inefficiencies, WorkflowInefficiency{
			Type:        "agent_failures",
			Description: "Agents frequently fail during execution",
			Impact:      "Wastes computation resources and delays progress",
			Suggestion:  "Review agent configuration or add better error handling",
			Confidence:  float64(agentFailureCount) / float64(len(trajectories)),
			Occurrences: agentFailureCount,
		})
	}

	return inefficiencies
}

func (a *TrajectoryAnalyzer) ExtractPatterns(trajectories []db.Trajectory) []TrajectoryPattern {
	type problemKey struct {
		problemType string
		repoType    string
	}

	patternData := make(map[problemKey]*patternAccumulator)

	for _, t := range trajectories {
		if t.TaskType == "" || t.FinalResult == "" {
			continue
		}

		key := problemKey{
			problemType: t.TaskType,
			repoType:    t.RepoType,
		}

		acc, exists := patternData[key]
		if !exists {
			acc = &patternAccumulator{
				sequences: make(map[string]*sequenceData),
			}
			patternData[key] = acc
		}

		acc.total++
		if t.FinalResult == "success" {
			acc.successes++
		}

		sequence := a.extractAgentSequence(t.Steps)
		seqKey := strings.Join(sequence, "→")

		seqData, exists := acc.sequences[seqKey]
		if !exists {
			seqData = &sequenceData{
				sequence:      sequence,
				trajectoryIDs: []string{},
			}
			acc.sequences[seqKey] = seqData
		}

		seqData.count++
		if t.FinalResult == "success" {
			seqData.successes++
		}
		seqData.totalCycleTime += t.CycleTimeMin
		if len(seqData.trajectoryIDs) < a.config.MaxExampleTrajectories {
			seqData.trajectoryIDs = append(seqData.trajectoryIDs, t.ID)
		}
	}

	var patterns []TrajectoryPattern
	for key, acc := range patternData {
		if acc.total < a.config.MinPatternOccurrences {
			continue
		}

		var bestSequence *sequenceData
		for _, seqData := range acc.sequences {
			if seqData.count < a.config.MinPatternOccurrences {
				continue
			}
			if bestSequence == nil {
				bestSequence = seqData
				continue
			}

			seqRate := float64(seqData.successes) / float64(seqData.count)
			bestRate := float64(bestSequence.successes) / float64(bestSequence.count)

			if seqRate > bestRate || (seqRate == bestRate && seqData.count > bestSequence.count) {
				bestSequence = seqData
			}
		}

		if bestSequence == nil {
			continue
		}

		successRate := float64(bestSequence.successes) / float64(bestSequence.count)
		confidence := a.calculateConfidence(bestSequence.count, successRate)

		if confidence < a.config.MinConfidenceThreshold {
			continue
		}

		avgCycleTime := 0
		if bestSequence.count > 0 {
			avgCycleTime = bestSequence.totalCycleTime / bestSequence.count
		}

		pattern := TrajectoryPattern{
			ID:                   uuid.NewString(),
			ProblemType:          key.problemType,
			RepoType:             key.repoType,
			OptimalAgentSequence: bestSequence.sequence,
			SuccessRate:          successRate,
			OccurrenceCount:      bestSequence.count,
			AvgCycleTimeMin:      avgCycleTime,
			ExampleTrajectoryIDs: bestSequence.trajectoryIDs,
			Confidence:           confidence,
		}

		patterns = append(patterns, pattern)
	}

	slices.SortFunc(patterns, func(a, b TrajectoryPattern) int {
		if b.Confidence != a.Confidence {
			return int((b.Confidence - a.Confidence) * 1000)
		}
		return b.OccurrenceCount - a.OccurrenceCount
	})

	return patterns
}

func (a *TrajectoryAnalyzer) GetOptimalSequence(problemType, repoType string) ([]string, float64, error) {
	patterns, err := a.database.GetTrajectoryPatternsByProblemType(problemType, repoType)
	if err != nil {
		return nil, 0, err
	}

	if len(patterns) == 0 {
		return nil, 0, nil
	}

	return patterns[0].OptimalAgentSequence, patterns[0].Confidence, nil
}

func (a *TrajectoryAnalyzer) extractAgentSequence(steps []db.TrajectoryStep) []string {
	var sequence []string
	seen := make(map[string]bool)

	for _, step := range steps {
		if step.AgentName == "" {
			continue
		}

		if step.ActionType == "agent_spawned" || step.ActionType == "agent_completed" {
			key := step.AgentName
			if !seen[key] {
				sequence = append(sequence, step.AgentName)
				seen[key] = true
			}
		}
	}

	return sequence
}

func (a *TrajectoryAnalyzer) detectPhaseLoops(steps []db.TrajectoryStep) []string {
	phaseVisits := make(map[string]int)
	var loops []string

	for _, step := range steps {
		if step.Phase != "" && step.ActionType == "phase_transition" {
			phaseVisits[step.Phase]++
			if phaseVisits[step.Phase] > 1 {
				loops = append(loops, step.Phase)
			}
		}
	}

	return loops
}

func (a *TrajectoryAnalyzer) hasReviewFailures(steps []db.TrajectoryStep) bool {
	for _, step := range steps {
		if step.ActionType == "review_failed" {
			return true
		}
	}
	return false
}

func (a *TrajectoryAnalyzer) hasAgentFailures(steps []db.TrajectoryStep) bool {
	for _, step := range steps {
		if step.ActionType == "agent_failed" {
			return true
		}
	}
	return false
}

func (a *TrajectoryAnalyzer) calculateConfidence(occurrences int, successRate float64) float64 {
	sampleWeight := float64(occurrences) / float64(a.config.MinPatternOccurrences*5)
	if sampleWeight > 1.0 {
		sampleWeight = 1.0
	}
	return (sampleWeight * 0.4) + (successRate * 0.6)
}

type patternAccumulator struct {
	total     int
	successes int
	sequences map[string]*sequenceData
}

type sequenceData struct {
	sequence       []string
	count          int
	successes      int
	totalCycleTime int
	trajectoryIDs  []string
}

func convertToDBPattern(p *TrajectoryPattern) *db.TrajectoryPattern {
	return &db.TrajectoryPattern{
		ID:                   p.ID,
		ProblemType:          p.ProblemType,
		RepoType:             p.RepoType,
		OptimalAgentSequence: p.OptimalAgentSequence,
		SuccessRate:          p.SuccessRate,
		OccurrenceCount:      p.OccurrenceCount,
		AvgCycleTimeMin:      p.AvgCycleTimeMin,
		ExampleTrajectoryIDs: p.ExampleTrajectoryIDs,
		Confidence:           p.Confidence,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
