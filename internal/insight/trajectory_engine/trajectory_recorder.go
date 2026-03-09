package trajectory_engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/insight/events"
	"github.com/google/uuid"
)

type TrajectoryRecorder struct {
	database *db.DB
	eventBus events.EventBus
	config   Config
	active   map[string]*Trajectory
	subID    events.SubscriptionID
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
}

type TrajectoryStore interface {
	SaveTrajectory(t *Trajectory) error
	GetTrajectoryByWorkflowID(workflowID string) (*Trajectory, error)
}

func NewTrajectoryRecorder(database *db.DB, eventBus events.EventBus, config Config) *TrajectoryRecorder {
	if config.MaxActiveTrajectories <= 0 {
		config = DefaultConfig()
	}

	return &TrajectoryRecorder{
		database: database,
		eventBus: eventBus,
		config:   config,
		active:   make(map[string]*Trajectory),
	}
}

func (r *TrajectoryRecorder) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.eventBus == nil {
		return nil
	}

	r.ctx, r.cancel = context.WithCancel(ctx)
	r.subID = r.eventBus.Subscribe(r.handleEvent)

	slog.Info("trajectory recorder started")
	return nil
}

func (r *TrajectoryRecorder) Stop() {
	r.mu.Lock()

	if r.eventBus != nil && r.subID != 0 {
		r.eventBus.Unsubscribe(r.subID)
		r.subID = 0
	}

	if r.cancel != nil {
		r.cancel()
	}

	activeCount := len(r.active)
	r.mu.Unlock()

	if activeCount > 0 {
		slog.Info("flushing in-progress trajectories before stop", "count", activeCount)
		if err := r.FlushAll(); err != nil {
			slog.Warn("failed to flush trajectories on stop", "error", err)
		}
	}

	slog.Info("trajectory recorder stopped")
}

func (r *TrajectoryRecorder) handleEvent(ctx context.Context, event events.Event) {
	switch event.Type {
	case events.EventWorkflowStarted:
		r.handleWorkflowStarted(event)
	case events.EventPhaseTransition:
		r.handlePhaseTransition(event)
	case events.EventAgentSpawned:
		r.handleAgentSpawned(event)
	case events.EventAgentCompleted:
		r.handleAgentCompleted(event)
	case events.EventAgentFailed:
		r.handleAgentFailed(event)
	case events.EventReviewPassed, events.EventReviewFailed:
		r.handleReview(event)
	case events.EventWorkflowCompleted:
		r.handleWorkflowCompleted(event)
	case events.EventWorkflowFailed, events.EventWorkflowAborted:
		r.handleWorkflowEnded(event, string(event.Type))
	}
}

func (r *TrajectoryRecorder) handleWorkflowStarted(event events.Event) {
	workflowID := extractWorkflowID(event.Payload)
	if workflowID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.active[workflowID]; exists {
		return
	}

	if len(r.active) >= r.config.MaxActiveTrajectories {
		r.evictOldest()
	}

	trajectory := &Trajectory{
		ID:           uuid.NewString(),
		WorkflowID:   workflowID,
		Steps:        []TrajectoryStep{},
		StartedAt:    event.Timestamp,
		WorkflowType: extractWorkflowType(event.Payload),
		TaskType:     extractTaskType(event.Payload),
	}

	r.active[workflowID] = trajectory
	slog.Debug("trajectory started", "workflow_id", workflowID)
}

func (r *TrajectoryRecorder) handlePhaseTransition(event events.Event) {
	workflowID := extractWorkflowID(event.Payload)
	if workflowID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	trajectory, exists := r.active[workflowID]
	if !exists {
		return
	}

	fromPhase := extractString(event.Payload, "from_phase")
	toPhase := extractString(event.Payload, "to_phase")

	step := TrajectoryStep{
		StepNumber: len(trajectory.Steps) + 1,
		ActionType: "phase_transition",
		Phase:      toPhase,
		Timestamp:  event.Timestamp,
		Success:    true,
		Metadata: map[string]any{
			"from_phase": fromPhase,
			"to_phase":   toPhase,
		},
	}

	trajectory.Steps = append(trajectory.Steps, step)
}

func (r *TrajectoryRecorder) handleAgentSpawned(event events.Event) {
	workflowID := extractWorkflowID(event.Payload)
	if workflowID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	trajectory, exists := r.active[workflowID]
	if !exists {
		return
	}

	agentName := extractAgentName(event.Payload)
	phase := extractString(event.Payload, "phase")

	step := TrajectoryStep{
		StepNumber: len(trajectory.Steps) + 1,
		AgentName:  agentName,
		ActionType: "agent_spawned",
		Phase:      phase,
		Timestamp:  event.Timestamp,
		Success:    true,
		Metadata:   map[string]any{"agent_type": extractString(event.Payload, "agent_type")},
	}

	trajectory.Steps = append(trajectory.Steps, step)
}

func (r *TrajectoryRecorder) handleAgentCompleted(event events.Event) {
	workflowID := extractWorkflowID(event.Payload)
	if workflowID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	trajectory, exists := r.active[workflowID]
	if !exists {
		return
	}

	agentName := extractAgentName(event.Payload)
	durationMs := extractDurationMs(event.Payload)

	for i := range trajectory.Steps {
		if trajectory.Steps[i].AgentName == agentName && trajectory.Steps[i].ActionType == "agent_spawned" {
			trajectory.Steps[i].DurationMs = durationMs
			trajectory.Steps[i].Success = true
			trajectory.Steps[i].OutputSummary = extractString(event.Payload, "result_summary")
			break
		}
	}

	step := TrajectoryStep{
		StepNumber: len(trajectory.Steps) + 1,
		AgentName:  agentName,
		ActionType: "agent_completed",
		Timestamp:  event.Timestamp,
		Success:    true,
		DurationMs: durationMs,
	}

	trajectory.Steps = append(trajectory.Steps, step)
}

func (r *TrajectoryRecorder) handleAgentFailed(event events.Event) {
	workflowID := extractWorkflowID(event.Payload)
	if workflowID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	trajectory, exists := r.active[workflowID]
	if !exists {
		return
	}

	agentName := extractAgentName(event.Payload)
	durationMs := extractDurationMs(event.Payload)

	step := TrajectoryStep{
		StepNumber:    len(trajectory.Steps) + 1,
		AgentName:     agentName,
		ActionType:    "agent_failed",
		Timestamp:     event.Timestamp,
		Success:       false,
		DurationMs:    durationMs,
		OutputSummary: extractString(event.Payload, "error"),
	}

	trajectory.Steps = append(trajectory.Steps, step)
}

func (r *TrajectoryRecorder) handleReview(event events.Event) {
	workflowID := extractWorkflowID(event.Payload)
	if workflowID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	trajectory, exists := r.active[workflowID]
	if !exists {
		return
	}

	success := event.Type == events.EventReviewPassed
	actionType := "review_passed"
	if !success {
		actionType = "review_failed"
	}

	step := TrajectoryStep{
		StepNumber:    len(trajectory.Steps) + 1,
		ActionType:    actionType,
		Timestamp:     event.Timestamp,
		Success:       success,
		OutputSummary: extractString(event.Payload, "feedback"),
		Metadata: map[string]any{
			"reviewer": extractString(event.Payload, "reviewer"),
		},
	}

	trajectory.Steps = append(trajectory.Steps, step)
}

func (r *TrajectoryRecorder) handleWorkflowCompleted(event events.Event) {
	r.handleWorkflowEnded(event, "success")
}

func (r *TrajectoryRecorder) removeActiveTrajectory(workflowID string) *Trajectory {
	r.mu.Lock()
	defer r.mu.Unlock()
	trajectory, exists := r.active[workflowID]
	if !exists {
		return nil
	}
	delete(r.active, workflowID)
	return trajectory
}

func (r *TrajectoryRecorder) handleWorkflowEnded(event events.Event, result string) {
	workflowID := extractWorkflowID(event.Payload)
	if workflowID == "" {
		return
	}

	trajectory := r.removeActiveTrajectory(workflowID)
	if trajectory == nil {
		return
	}

	now := event.Timestamp
	trajectory.CompletedAt = &now
	trajectory.FinalResult = result
	trajectory.StepCount = len(trajectory.Steps)

	if trajectory.StartedAt != (time.Time{}) && trajectory.CompletedAt != nil {
		trajectory.CycleTimeMin = int(trajectory.CompletedAt.Sub(trajectory.StartedAt).Minutes())
	}

	r.enrichFromArtifact(trajectory)

	if err := r.database.SaveTrajectory(convertToDBTrajectory(trajectory)); err != nil {
		slog.Error("failed to save trajectory", "workflow_id", workflowID, "error", err)
		return
	}

	slog.Info("trajectory recorded",
		"workflow_id", workflowID,
		"steps", trajectory.StepCount,
		"result", result,
		"cycle_time_min", trajectory.CycleTimeMin)
}

func (r *TrajectoryRecorder) enrichFromArtifact(trajectory *Trajectory) {
	artifact, err := r.database.GetArtifactByWorkflowID(trajectory.WorkflowID)
	if err != nil || artifact == nil {
		return
	}

	if trajectory.TaskType == "" {
		trajectory.TaskType = artifact.TaskType
	}
	if trajectory.RepoType == "" {
		trajectory.RepoType = artifact.RepoType
	}
	if trajectory.WorkflowType == "" {
		trajectory.WorkflowType = artifact.WorkflowType
	}
}

func (r *TrajectoryRecorder) evictOldest() {
	var oldestID string
	var oldestTime time.Time

	for id, t := range r.active {
		if oldestID == "" || t.StartedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = t.StartedAt
		}
	}

	if oldestID != "" {
		delete(r.active, oldestID)
		slog.Warn("evicted oldest trajectory due to buffer limit", "workflow_id", oldestID)
	}
}

func (r *TrajectoryRecorder) GetActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.active)
}

func (r *TrajectoryRecorder) FlushAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for workflowID, trajectory := range r.active {
		now := time.Now().UTC()
		trajectory.CompletedAt = &now
		trajectory.FinalResult = "aborted"
		trajectory.StepCount = len(trajectory.Steps)

		if err := r.database.SaveTrajectory(convertToDBTrajectory(trajectory)); err != nil {
			errs = append(errs, err)
			continue
		}
		delete(r.active, workflowID)
	}

	r.active = make(map[string]*Trajectory)

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func convertToDBTrajectory(t *Trajectory) *db.Trajectory {
	steps := make([]db.TrajectoryStep, len(t.Steps))
	for i, s := range t.Steps {
		steps[i] = db.TrajectoryStep{
			StepNumber:    s.StepNumber,
			AgentName:     s.AgentName,
			ActionType:    s.ActionType,
			Phase:         s.Phase,
			InputContext:  s.InputContext,
			OutputSummary: s.OutputSummary,
			Success:       s.Success,
			DurationMs:    s.DurationMs,
			Timestamp:     s.Timestamp,
			Metadata:      s.Metadata,
		}
	}

	return &db.Trajectory{
		ID:           t.ID,
		WorkflowID:   t.WorkflowID,
		TaskType:     t.TaskType,
		RepoType:     t.RepoType,
		WorkflowType: t.WorkflowType,
		Steps:        steps,
		StepCount:    t.StepCount,
		FinalResult:  t.FinalResult,
		CycleTimeMin: t.CycleTimeMin,
		StartedAt:    t.StartedAt,
		CompletedAt:  t.CompletedAt,
	}
}

func extractWorkflowID(payload map[string]any) string {
	if id, ok := payload["workflow_id"].(string); ok {
		return id
	}
	return ""
}

func extractAgentName(payload map[string]any) string {
	if name, ok := payload["agent_name"].(string); ok {
		return name
	}
	if typ, ok := payload["agent_type"].(string); ok {
		return typ
	}
	return ""
}

func extractWorkflowType(payload map[string]any) string {
	if wt, ok := payload["workflow_type"].(string); ok {
		return wt
	}
	if t, ok := payload["type"].(string); ok {
		return t
	}
	return ""
}

func extractTaskType(payload map[string]any) string {
	if tt, ok := payload["task_type"].(string); ok {
		return tt
	}
	return ""
}

func extractString(payload map[string]any, key string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

func extractDurationMs(payload map[string]any) int64 {
	if v, ok := payload["duration_ms"].(float64); ok {
		return int64(v)
	}
	if v, ok := payload["duration_ms"].(int64); ok {
		return v
	}
	return 0
}
