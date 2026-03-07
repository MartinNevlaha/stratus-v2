package workflow_synthesis

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
)

var agentToPhaseMap = map[string]string{
	"debugger":          "analyze",
	"analyzer":          "analyze",
	"architect":         "plan",
	"coder":             "implement",
	"backend-engineer":  "fix",
	"frontend-engineer": "fix",
	"reviewer":          "review",
	"tester":            "verify",
	"test-generator":    "generate",
	"healer":            "heal",
}

var taskTypeToBaseWorkflow = map[string]string{
	"bug_fix":     "bug",
	"feature":     "spec",
	"refactor":    "spec",
	"test":        "e2e",
	"e2e":         "e2e",
	"hotfix":      "bug",
	"enhancement": "spec",
}

type CandidateGenerator struct {
	store  Store
	config Config
}

func NewCandidateGenerator(store Store, config Config) *CandidateGenerator {
	return &CandidateGenerator{
		store:  store,
		config: config,
	}
}

type GenerationResult struct {
	CandidatesGenerated int      `json:"candidates_generated"`
	CandidateIDs        []string `json:"candidate_ids"`
}

func (g *CandidateGenerator) Generate(ctx context.Context) (*GenerationResult, error) {
	patterns, err := g.store.GetTrajectoryPatterns(ctx, 50)
	if err != nil {
		return nil, fmt.Errorf("get trajectory patterns: %w", err)
	}

	result := &GenerationResult{
		CandidateIDs: []string{},
	}

	existingCandidates, err := g.store.ListCandidates(ctx, "candidate", 100)
	if err != nil {
		return nil, fmt.Errorf("list existing candidates: %w", err)
	}

	existingKeys := make(map[string]bool)
	for _, c := range existingCandidates {
		key := fmt.Sprintf("%s:%s", c.TaskType, c.RepoType)
		existingKeys[key] = true
	}

	for _, pattern := range patterns {
		if pattern.Confidence < g.config.MinConfidence {
			continue
		}

		if len(pattern.OptimalAgentSequence) < 2 {
			continue
		}

		taskType := g.normalizeTaskType(pattern.ProblemType)
		repoType := pattern.RepoType
		key := fmt.Sprintf("%s:%s", taskType, repoType)

		if existingKeys[key] {
			continue
		}

		candidate := g.createCandidateFromPattern(&pattern, taskType, repoType)
		if candidate == nil {
			continue
		}

		if err := g.store.SaveCandidate(ctx, candidate); err != nil {
			slog.Error("failed to save workflow candidate", "error", err, "task_type", taskType)
			continue
		}

		result.CandidatesGenerated++
		result.CandidateIDs = append(result.CandidateIDs, candidate.ID)
		existingKeys[key] = true

		if result.CandidatesGenerated >= g.config.MaxCandidatesPerRun {
			break
		}
	}

	return result, nil
}

func (g *CandidateGenerator) createCandidateFromPattern(pattern *db.TrajectoryPattern, taskType, repoType string) *db.WorkflowCandidate {
	steps := g.mapAgentsToSteps(pattern.OptimalAgentSequence, taskType)
	if len(steps) == 0 {
		return nil
	}

	phaseTransitions := g.generatePhaseTransitions(steps, taskType)

	baseWorkflow := taskTypeToBaseWorkflow[taskType]
	if baseWorkflow == "" {
		baseWorkflow = "spec"
	}

	confidence := g.calculateConfidence(pattern)

	workflowName := fmt.Sprintf("%s_v2", taskType)
	if repoType != "" {
		workflowName = fmt.Sprintf("%s_%s_v2", repoType, taskType)
	}

	return &db.WorkflowCandidate{
		WorkflowName:     workflowName,
		TaskType:         taskType,
		RepoType:         repoType,
		BaseWorkflow:     baseWorkflow,
		Steps:            steps,
		PhaseTransitions: phaseTransitions,
		Confidence:       confidence,
		Status:           "candidate",
		SourcePatternID:  pattern.ID,
	}
}

func (g *CandidateGenerator) mapAgentsToSteps(agents []string, taskType string) []db.WorkflowStep {
	steps := make([]db.WorkflowStep, 0, len(agents))
	seenPhases := make(map[string]bool)

	for _, agent := range agents {
		phase, ok := agentToPhaseMap[agent]
		if !ok {
			phase = "implement"
		}

		if seenPhases[phase] && phase != "fix" && phase != "verify" {
			continue
		}
		seenPhases[phase] = true

		steps = append(steps, db.WorkflowStep{
			Phase:     phase,
			AgentHint: agent,
		})
	}

	if len(steps) == 0 {
		return nil
	}

	steps = g.addFeedbackLoops(steps, taskType)

	for i := range steps {
		steps[i].NextPhases = g.determineNextPhases(steps, i, taskType)
	}

	return steps
}

func (g *CandidateGenerator) addFeedbackLoops(steps []db.WorkflowStep, taskType string) []db.WorkflowStep {
	hasVerify := false
	hasReview := false
	hasImplement := false
	hasFix := false

	for _, s := range steps {
		switch s.Phase {
		case "verify":
			hasVerify = true
		case "review":
			hasReview = true
		case "implement":
			hasImplement = true
		case "fix":
			hasFix = true
		}
	}

	if !hasVerify && (hasImplement || hasFix) {
		steps = append(steps, db.WorkflowStep{Phase: "verify"})
	}

	baseWorkflow := taskTypeToBaseWorkflow[taskType]
	if baseWorkflow == "bug" && !hasReview && hasFix {
		steps = append(steps, db.WorkflowStep{Phase: "review"})
	}

	steps = append(steps, db.WorkflowStep{Phase: "complete"})

	return steps
}

func (g *CandidateGenerator) determineNextPhases(steps []db.WorkflowStep, currentIndex int, taskType string) []string {
	if currentIndex >= len(steps)-1 {
		return []string{}
	}

	currentPhase := steps[currentIndex].Phase
	nextPhase := steps[currentIndex+1].Phase

	switch currentPhase {
	case "verify":
		return []string{"implement", nextPhase}
	case "review":
		if nextPhase != "complete" {
			return []string{"fix", nextPhase}
		}
		return []string{"fix", nextPhase}
	default:
		return []string{nextPhase}
	}
}

func (g *CandidateGenerator) generatePhaseTransitions(steps []db.WorkflowStep, taskType string) map[string]string {
	transitions := make(map[string]string)

	for i, step := range steps {
		if i < len(steps)-1 {
			transitions[step.Phase] = steps[i+1].Phase
		}
	}

	return transitions
}

func (g *CandidateGenerator) calculateConfidence(pattern *db.TrajectoryPattern) float64 {
	occurrenceWeight := float64(pattern.OccurrenceCount) / 100.0
	if occurrenceWeight > 1.0 {
		occurrenceWeight = 1.0
	}

	cycleTimeBonus := 0.0
	if pattern.AvgCycleTimeMin > 0 && pattern.AvgCycleTimeMin < 30 {
		cycleTimeBonus = 0.2
	} else if pattern.AvgCycleTimeMin > 0 && pattern.AvgCycleTimeMin < 60 {
		cycleTimeBonus = 0.1
	}

	confidence := (occurrenceWeight * 0.3) + (pattern.SuccessRate * 0.5) + cycleTimeBonus

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func (g *CandidateGenerator) normalizeTaskType(problemType string) string {
	problemType = strings.ToLower(strings.TrimSpace(problemType))

	mappings := map[string]string{
		"bug":         "bug_fix",
		"bugfix":      "bug_fix",
		"bug_fix":     "bug_fix",
		"fix":         "bug_fix",
		"feature":     "feature",
		"new_feature": "feature",
		"refactor":    "refactor",
		"refactoring": "refactor",
		"test":        "test",
		"testing":     "test",
		"e2e":         "e2e",
		"e2e_test":    "e2e",
		"hotfix":      "hotfix",
		"enhancement": "enhancement",
		"improvement": "enhancement",
	}

	if mapped, ok := mappings[problemType]; ok {
		return mapped
	}

	return problemType
}
