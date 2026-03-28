package scorecards

import (
	"math"
	"sort"
	"time"
)

const (
	confidenceBase         = 0.3
	confidenceRunsDivisor  = 20.0
	confidenceMaxRunsBonus = 0.4
	confidenceBonus20Runs  = 0.15
	confidenceBonus50Runs  = 0.1
	confidenceMaxScore     = 0.95
)

func CalculateSuccessRate(completed, failed int) float64 {
	total := completed + failed
	if total == 0 {
		return 0
	}
	return float64(completed) / float64(total)
}

func CalculateFailureRate(failed, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(failed) / float64(total)
}

func CalculateReviewPassRate(passed, failed int) float64 {
	total := passed + failed
	if total == 0 {
		return 0
	}
	return float64(passed) / float64(total)
}

func CalculateReworkRate(retryCycles, totalRuns int) float64 {
	if totalRuns == 0 {
		return 0
	}
	return float64(retryCycles) / float64(totalRuns)
}

func CalculateRegressionRate(regressions, successes int) float64 {
	if successes == 0 {
		return 0
	}
	return float64(regressions) / float64(successes)
}

func CalculateAvgCycleTime(events []EventForScorecard) int64 {
	if len(events) == 0 {
		return 0
	}

	type spawnKey struct {
		workflowID string
		agentName  string
	}
	spawned := make(map[spawnKey]time.Time)
	var durations []int64

	for _, e := range events {
		key := spawnKey{workflowID: e.WorkflowID, agentName: e.AgentName}
		switch e.Type {
		case "agent.spawned":
			spawned[key] = e.Timestamp
		case "agent.completed":
			if spawnTime, ok := spawned[key]; ok {
				duration := e.Timestamp.Sub(spawnTime).Milliseconds()
				if duration > 0 {
					durations = append(durations, duration)
				}
				delete(spawned, key)
			}
		}
	}

	if len(durations) == 0 {
		return 0
	}

	var sum int64
	for _, d := range durations {
		sum += d
	}
	return sum / int64(len(durations))
}

func CalculateAvgDuration(events []EventForScorecard) int64 {
	started := make(map[string]time.Time)
	var durations []int64

	for _, e := range events {
		switch e.Type {
		case "workflow.started":
			started[e.WorkflowID] = e.Timestamp
		case "workflow.completed", "workflow.failed":
			if startTime, ok := started[e.WorkflowID]; ok {
				duration := e.Timestamp.Sub(startTime).Milliseconds()
				if duration > 0 {
					durations = append(durations, duration)
				}
			}
		}
	}

	if len(durations) == 0 {
		return 0
	}

	var sum int64
	for _, d := range durations {
		sum += d
	}
	return sum / int64(len(durations))
}

func CalculateConfidenceScore(totalRuns int, config ScorecardConfig) float64 {
	if totalRuns < config.MinSampleSize {
		return config.LowConfidenceScore
	}

	score := confidenceBase

	runsBonus := math.Min(float64(totalRuns)/confidenceRunsDivisor, confidenceMaxRunsBonus)
	score += runsBonus

	if totalRuns >= 20 {
		score += confidenceBonus20Runs
	}
	if totalRuns >= 50 {
		score += confidenceBonus50Runs
	}

	if score > confidenceMaxScore {
		score = confidenceMaxScore
	}

	return score
}

func CalculateTrend(current, previous *AgentScorecard, threshold float64) Trend {
	if previous == nil || previous.TotalRuns == 0 {
		return TrendStable
	}

	improvements := 0
	degradations := 0

	successDiff := current.SuccessRate - previous.SuccessRate
	if successDiff >= threshold {
		improvements++
	} else if successDiff <= -threshold {
		degradations++
	}

	failureDiff := previous.FailureRate - current.FailureRate
	if failureDiff >= threshold {
		improvements++
	} else if failureDiff <= -threshold {
		degradations++
	}

	reviewDiff := current.ReviewPassRate - previous.ReviewPassRate
	if reviewDiff >= threshold {
		improvements++
	} else if reviewDiff <= -threshold {
		degradations++
	}

	if improvements > degradations && improvements >= 2 {
		return TrendImproving
	}
	if degradations > improvements && degradations >= 2 {
		return TrendDegrading
	}

	return TrendStable
}

func CalculateWorkflowTrend(current, previous *WorkflowScorecard, threshold float64) Trend {
	if previous == nil || previous.TotalRuns == 0 {
		return TrendStable
	}

	improvements := 0
	degradations := 0

	completionDiff := current.CompletionRate - previous.CompletionRate
	if completionDiff >= threshold {
		improvements++
	} else if completionDiff <= -threshold {
		degradations++
	}

	failureDiff := previous.FailureRate - current.FailureRate
	if failureDiff >= threshold {
		improvements++
	} else if failureDiff <= -threshold {
		degradations++
	}

	reworkDiff := previous.ReworkRate - current.ReworkRate
	if reworkDiff >= threshold {
		improvements++
	} else if reworkDiff <= -threshold {
		degradations++
	}

	if improvements > degradations && improvements >= 2 {
		return TrendImproving
	}
	if degradations > improvements && degradations >= 2 {
		return TrendDegrading
	}

	return TrendStable
}

type AgentEventStats struct {
	AgentName      string
	Spawned        int
	Completed      int
	Failed         int
	ReviewPassed   int
	ReviewFailed   int
	RetryCycles    int
	Regressions    int
	CycleTimes     []int64
	SuccessAgentWF map[string]bool
	FailedAfter    int
}

func AggregateAgentEvents(events []EventForScorecard) map[string]*AgentEventStats {
	stats := make(map[string]*AgentEventStats)

	agentSpawns := make(map[string]time.Time)
	agentWorkflows := make(map[string]string)
	workflowAgentSuccess := make(map[string]map[string]bool)

	for _, e := range events {
		agentName := e.AgentName
		if agentName == "" {
			if a, ok := e.Payload["agent_name"].(string); ok {
				agentName = a
			}
		}
		if agentName == "" {
			continue
		}

		if _, exists := stats[agentName]; !exists {
			stats[agentName] = &AgentEventStats{
				AgentName:      agentName,
				SuccessAgentWF: make(map[string]bool),
			}
		}
		s := stats[agentName]

		switch e.Type {
		case "agent.spawned":
			s.Spawned++
			agentSpawns[e.ID] = e.Timestamp
			agentWorkflows[e.ID] = e.WorkflowID

		case "agent.completed":
			s.Completed++
			if spawnTime, ok := agentSpawns[e.ID]; ok {
				duration := e.Timestamp.Sub(spawnTime).Milliseconds()
				if duration > 0 {
					s.CycleTimes = append(s.CycleTimes, duration)
				}
			}
			wfID := agentWorkflows[e.ID]
			if wfID != "" {
				if workflowAgentSuccess[wfID] == nil {
					workflowAgentSuccess[wfID] = make(map[string]bool)
				}
				workflowAgentSuccess[wfID][agentName] = true
				s.SuccessAgentWF[wfID] = true
			}

		case "agent.failed":
			s.Failed++
			wfID := agentWorkflows[e.ID]
			if wfID != "" && s.SuccessAgentWF[wfID] {
				s.Regressions++
			}
			delete(s.SuccessAgentWF, wfID)

		case "review.passed":
			s.ReviewPassed++

		case "review.failed":
			s.ReviewFailed++
		}
	}

	for agentName, s := range stats {
		if s.Spawned > 0 && s.Failed > 0 {
			s.RetryCycles = detectRetryCycles(events, agentName)
		}
	}

	return stats
}

func detectRetryCycles(events []EventForScorecard, agentName string) int {
	sortedEvents := make([]EventForScorecard, len(events))
	copy(sortedEvents, events)
	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].Timestamp.Before(sortedEvents[j].Timestamp)
	})

	cycles := 0
	lastFailed := false

	for _, e := range sortedEvents {
		eAgent := e.AgentName
		if eAgent == "" {
			if a, ok := e.Payload["agent_name"].(string); ok {
				eAgent = a
			}
		}
		if eAgent != agentName {
			continue
		}

		if e.Type == "agent.failed" {
			lastFailed = true
		} else if e.Type == "agent.spawned" && lastFailed {
			cycles++
			lastFailed = false
		} else if e.Type == "agent.completed" {
			lastFailed = false
		}
	}

	return cycles
}

type WorkflowEventStats struct {
	WorkflowType    string
	Started         int
	Completed       int
	Failed          int
	ReviewPassed    int
	ReviewFailed    int
	PhaseBacktracks int
	Durations       []int64
}

func AggregateWorkflowEvents(events []EventForScorecard) map[string]*WorkflowEventStats {
	stats := make(map[string]*WorkflowEventStats)

	workflowStarts := make(map[string]time.Time)
	workflowPhases := make(map[string]string)

	phaseOrder := []string{"plan", "discovery", "design", "governance", "accept", "implement", "verify", "learn", "complete", "analyze", "fix", "review", "setup", "generate", "heal"}
	phaseIndex := make(map[string]int)
	for i, p := range phaseOrder {
		phaseIndex[p] = i
	}

	for _, e := range events {
		wfType := e.WorkflowID
		if wfType == "" {
			continue
		}
		if wf, ok := e.Payload["workflow_type"].(string); ok && wf != "" {
			wfType = wf
		}

		if _, exists := stats[wfType]; !exists {
			stats[wfType] = &WorkflowEventStats{WorkflowType: wfType}
		}
		s := stats[wfType]

		switch e.Type {
		case "workflow.started":
			s.Started++

		case "workflow.completed":
			s.Completed++
			if startTime, ok := workflowStarts[e.WorkflowID]; ok {
				duration := e.Timestamp.Sub(startTime).Milliseconds()
				if duration > 0 {
					s.Durations = append(s.Durations, duration)
				}
			}

		case "workflow.failed":
			s.Failed++
			if startTime, ok := workflowStarts[e.WorkflowID]; ok {
				duration := e.Timestamp.Sub(startTime).Milliseconds()
				if duration > 0 {
					s.Durations = append(s.Durations, duration)
				}
			}

		case "workflow.phase_transition":
			newPhase := e.Phase
			if newPhase == "" {
				if p, ok := e.Payload["new_phase"].(string); ok {
					newPhase = p
				}
			}
			oldPhase := workflowPhases[e.WorkflowID]
			if oldPhase != "" && newPhase != "" {
				oldIdx, oldOk := phaseIndex[oldPhase]
				newIdx, newOk := phaseIndex[newPhase]
				if oldOk && newOk && newIdx < oldIdx {
					s.PhaseBacktracks++
				}
			}
			workflowPhases[e.WorkflowID] = newPhase

		case "review.passed":
			s.ReviewPassed++

		case "review.failed":
			s.ReviewFailed++
		}
	}

	return stats
}

func ComputeAgentScorecard(
	agentName string,
	window Window,
	windowStart, windowEnd time.Time,
	events []EventForScorecard,
	config ScorecardConfig,
) AgentScorecard {
	card := NewAgentScorecard(agentName, window, windowStart, windowEnd)

	agentEvents := make([]EventForScorecard, 0)
	for _, e := range events {
		eAgent := e.AgentName
		if eAgent == "" {
			if a, ok := e.Payload["agent_name"].(string); ok {
				eAgent = a
			}
		}
		if eAgent == agentName {
			agentEvents = append(agentEvents, e)
		}
	}

	if len(agentEvents) == 0 {
		return card
	}

	stats := AggregateAgentEvents(agentEvents)
	s, exists := stats[agentName]
	if !exists {
		return card
	}

	card.TotalRuns = s.Spawned
	card.SuccessRate = CalculateSuccessRate(s.Completed, s.Failed)
	card.FailureRate = CalculateFailureRate(s.Failed, s.Spawned)
	card.ReviewPassRate = CalculateReviewPassRate(s.ReviewPassed, s.ReviewFailed)
	card.ReworkRate = CalculateReworkRate(s.RetryCycles, s.Spawned)

	if len(s.CycleTimes) > 0 {
		var sum int64
		for _, ct := range s.CycleTimes {
			sum += ct
		}
		card.AvgCycleTimeMs = sum / int64(len(s.CycleTimes))
	}

	card.RegressionRate = CalculateRegressionRate(s.Regressions, s.Completed)
	card.ConfidenceScore = CalculateConfidenceScore(card.TotalRuns, config)

	return card
}

func ComputeWorkflowScorecard(
	workflowType string,
	window Window,
	windowStart, windowEnd time.Time,
	events []EventForScorecard,
	config ScorecardConfig,
) WorkflowScorecard {
	card := NewWorkflowScorecard(workflowType, window, windowStart, windowEnd)

	wfEvents := make([]EventForScorecard, 0)
	for _, e := range events {
		eType := e.WorkflowID
		if wt, ok := e.Payload["workflow_type"].(string); ok && wt != "" {
			eType = wt
		}
		if eType == workflowType || e.WorkflowID == workflowType {
			wfEvents = append(wfEvents, e)
		}
	}

	if len(wfEvents) == 0 {
		return card
	}

	stats := AggregateWorkflowEvents(wfEvents)
	s, exists := stats[workflowType]
	if !exists {
		return card
	}

	totalRuns := s.Started
	card.TotalRuns = totalRuns
	card.CompletionRate = CalculateSuccessRate(s.Completed, s.Failed)
	card.FailureRate = CalculateFailureRate(s.Failed, totalRuns)
	card.ReviewRejectionRate = CalculateFailureRate(s.ReviewFailed, s.ReviewPassed+s.ReviewFailed)
	card.ReworkRate = CalculateReworkRate(s.PhaseBacktracks, totalRuns)

	if len(s.Durations) > 0 {
		var sum int64
		for _, d := range s.Durations {
			sum += d
		}
		card.AvgDurationMs = sum / int64(len(s.Durations))
	}

	card.ConfidenceScore = CalculateConfidenceScore(card.TotalRuns, config)

	return card
}
