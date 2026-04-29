package artifacts

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

type ArtifactBuilder struct {
	eventQuery    EventQuery
	artifactStore ArtifactStore
	classifier    *ProblemClassifier
	config        ArtifactConfig
	repoDetector  *RepoDetector
	mu            sync.Mutex
}

func NewArtifactBuilder(eventQuery EventQuery, artifactStore ArtifactStore, config ArtifactConfig) *ArtifactBuilder {
	if config.MinEventsForBuild <= 0 {
		config = DefaultArtifactConfig()
	}

	return &ArtifactBuilder{
		eventQuery:    eventQuery,
		artifactStore: artifactStore,
		classifier:    NewProblemClassifier(),
		repoDetector:  NewRepoDetector(),
		config:        config,
	}
}

func (b *ArtifactBuilder) Build(ctx context.Context, workflowID string) (*Artifact, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	existing, err := b.artifactStore.GetArtifactByWorkflowID(ctx, workflowID)
	if err != nil {
		slog.Warn("failed to check existing artifact", "workflow_id", workflowID, "error", err)
	} else if existing != nil {
		slog.Debug("artifact already exists for workflow", "workflow_id", workflowID)
		return existing, nil
	}

	events, err := b.eventQuery.GetEventsByWorkflowID(ctx, workflowID)
	if err != nil {
		return nil, err
	}

	if len(events) < b.config.MinEventsForBuild {
		slog.Info("artifact build skipped: insufficient events",
			"workflow_id", workflowID,
			"count", len(events),
			"min_required", b.config.MinEventsForBuild)
		return nil, nil
	}

	artifact := b.buildFromEvents(workflowID, events)

	if err := b.artifactStore.SaveArtifact(ctx, *artifact); err != nil {
		return nil, err
	}

	slog.Info("artifact built",
		"artifact_id", artifact.ID,
		"workflow_id", workflowID,
		"problem_class", artifact.ProblemClass,
		"repo_type", artifact.RepoType)

	return artifact, nil
}

func (b *ArtifactBuilder) BuildRecent(ctx context.Context, since time.Time) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	workflowIDs, err := b.eventQuery.GetCompletedWorkflowIDs(ctx, since, 500)
	if err != nil {
		return 0, err
	}

	if len(workflowIDs) == 0 {
		slog.Debug("no completed workflows to build artifacts for")
		return 0, nil
	}

	var built, skipped int
	for _, workflowID := range workflowIDs {
		existing, err := b.artifactStore.GetArtifactByWorkflowID(ctx, workflowID)
		if err != nil {
			slog.Warn("failed to check existing artifact", "workflow_id", workflowID, "error", err)
			continue
		}
		if existing != nil {
			skipped++
			continue
		}

		events, err := b.eventQuery.GetEventsByWorkflowID(ctx, workflowID)
		if err != nil {
			slog.Warn("failed to get events for workflow", "workflow_id", workflowID, "error", err)
			continue
		}

		if len(events) < b.config.MinEventsForBuild {
			skipped++
			continue
		}

		artifact := b.buildFromEvents(workflowID, events)

		if err := b.artifactStore.SaveArtifact(ctx, *artifact); err != nil {
			slog.Warn("failed to save artifact", "workflow_id", workflowID, "error", err)
			continue
		}

		built++
	}

	slog.Info("artifact batch build complete",
		"built", built,
		"skipped", skipped,
		"total", len(workflowIDs))

	return built, nil
}

func (b *ArtifactBuilder) buildFromEvents(workflowID string, events []EventForArtifact) *Artifact {
	sortEventsByTime(events)

	artifact := &Artifact{
		ID:           uuid.NewString(),
		WorkflowID:   workflowID,
		AgentsUsed:   []string{},
		FilesChanged: []string{},
		Metadata:     make(map[string]any),
		CreatedAt:    time.Now().UTC(),
	}

	var startedAt, completedAt *time.Time
	var reviewPassed, reviewFailed int

	agentSet := make(map[string]bool)
	fileSet := make(map[string]bool)

	for _, event := range events {
		switch event.Type {
		case "workflow.started":
			startedAt = &event.Timestamp
			artifact.WorkflowType = extractWorkflowType(event.Payload)

		case "workflow.completed":
			completedAt = &event.Timestamp
			artifact.Success = true

		case "workflow.failed":
			completedAt = &event.Timestamp
			artifact.Success = false

		case "agent.spawned", "agent.completed":
			agentName := extractAgentName(event.Payload)
			if agentName != "" {
				agentSet[agentName] = true
			}

		case "review.passed":
			reviewPassed++
			artifact.ReviewResult = ReviewPass

		case "review.failed":
			reviewFailed++
			artifact.ReviewResult = ReviewFail
		}

		extractFilesFromEvent(event.Payload, fileSet, b.config.MaxFilesToTrack)
	}

	for agent := range agentSet {
		artifact.AgentsUsed = append(artifact.AgentsUsed, agent)
	}
	slices.Sort(artifact.AgentsUsed)
	if len(artifact.AgentsUsed) > b.config.MaxAgentsToTrack {
		artifact.AgentsUsed = artifact.AgentsUsed[:b.config.MaxAgentsToTrack]
	}

	for file := range fileSet {
		artifact.FilesChanged = append(artifact.FilesChanged, file)
	}
	slices.Sort(artifact.FilesChanged)
	if len(artifact.FilesChanged) > b.config.MaxFilesToTrack {
		artifact.FilesChanged = artifact.FilesChanged[:b.config.MaxFilesToTrack]
	}

	if startedAt != nil && completedAt != nil {
		artifact.CycleTimeMin = int(completedAt.Sub(*startedAt).Minutes())
	}

	if artifact.ReviewResult == "" {
		if reviewPassed > 0 || reviewFailed > 0 {
			if reviewPassed >= reviewFailed {
				artifact.ReviewResult = ReviewPass
			} else {
				artifact.ReviewResult = ReviewFail
			}
		} else {
			artifact.ReviewResult = ReviewNone
		}
	}

	artifact.RepoType = b.repoDetector.Detect(artifact.FilesChanged)
	artifact.ProblemClass = b.classifier.Classify(events, artifact.FilesChanged, artifact.AgentsUsed)
	artifact.TaskType = b.classifier.InferTaskType(events, artifact.WorkflowType)
	artifact.RootCause = b.classifier.ExtractRootCause(events)
	artifact.SolutionPattern = b.classifier.InferSolutionPattern(artifact.FilesChanged, artifact.WorkflowType, artifact.ProblemClass)

	artifact.Metadata["event_count"] = len(events)
	artifact.Metadata["review_passed"] = reviewPassed
	artifact.Metadata["review_failed"] = reviewFailed
	artifact.Metadata["agent_count"] = len(artifact.AgentsUsed)
	artifact.Metadata["file_count"] = len(artifact.FilesChanged)

	return artifact
}

func sortEventsByTime(events []EventForArtifact) {
	slices.SortFunc(events, func(a, b EventForArtifact) int {
		return a.Timestamp.Compare(b.Timestamp)
	})
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

func extractAgentName(payload map[string]any) string {
	if agentName, ok := payload["agent_name"].(string); ok {
		return agentName
	}
	if agentType, ok := payload["agent_type"].(string); ok {
		return agentType
	}
	return ""
}

func extractFilesFromEvent(payload map[string]any, fileSet map[string]bool, maxFiles int) {
	if len(fileSet) >= maxFiles {
		return
	}

	if files, ok := payload["files_changed"].([]any); ok {
		for _, f := range files {
			if file, ok := f.(string); ok && file != "" {
				fileSet[file] = true
				if len(fileSet) >= maxFiles {
					return
				}
			}
		}
	}

	if file, ok := payload["file"].(string); ok && file != "" {
		fileSet[file] = true
	}

	if file, ok := payload["path"].(string); ok && file != "" {
		fileSet[file] = true
	}

	if files, ok := payload["files"].([]any); ok {
		for _, f := range files {
			if file, ok := f.(string); ok && file != "" {
				fileSet[file] = true
				if len(fileSet) >= maxFiles {
					return
				}
			}
		}
	}
}

func (b *ArtifactBuilder) GetStats(ctx context.Context) (*ArtifactStats, error) {
	total, err := b.artifactStore.CountArtifacts(ctx)
	if err != nil {
		return nil, err
	}

	artifacts, err := b.artifactStore.ListArtifacts(ctx, ArtifactFilterOptions{Limit: 1000})
	if err != nil {
		return nil, err
	}

	stats := &ArtifactStats{
		TotalArtifacts: total,
		ByProblemClass: make(map[string]int),
		ByRepoType:     make(map[string]int),
		ByWorkflowType: make(map[string]int),
	}

	var successCount int
	var totalCycleTime int
	var cycleTimeCount int

	for _, a := range artifacts {
		if a.Success {
			successCount++
		}
		if a.CycleTimeMin > 0 {
			totalCycleTime += a.CycleTimeMin
			cycleTimeCount++
		}

		stats.ByProblemClass[string(a.ProblemClass)]++
		stats.ByRepoType[string(a.RepoType)]++
		stats.ByWorkflowType[a.WorkflowType]++
	}

	if len(artifacts) > 0 {
		stats.OverallSuccessRate = float64(successCount) / float64(len(artifacts))
	}
	if cycleTimeCount > 0 {
		stats.AvgCycleTimeMin = float64(totalCycleTime) / float64(cycleTimeCount)
	}

	return stats, nil
}

type RepoDetector struct {
	filePatterns map[string]RepoType
}

func NewRepoDetector() *RepoDetector {
	return &RepoDetector{
		filePatterns: map[string]RepoType{
			"go.mod":           RepoTypeGolang,
			"go.sum":           RepoTypeGolang,
			"package.json":     RepoTypeNodeJS,
			"nest-cli.json":    RepoTypeNestJS,
			"tsconfig.json":    RepoTypeTypeScript,
			"vite.config":      RepoTypeReact,
			"vite.config.ts":   RepoTypeReact,
			"vite.config.js":   RepoTypeReact,
			"vue.config":       RepoTypeVue,
			"requirements.txt": RepoTypePython,
			"pyproject.toml":   RepoTypePython,
			"Cargo.toml":       RepoTypeRust,
			"pom.xml":          RepoTypeJava,
			"build.gradle":     RepoTypeJava,
		},
	}
}

func (d *RepoDetector) Detect(files []string) RepoType {
	for _, file := range files {
		for pattern, repoType := range d.filePatterns {
			if file == pattern || file == "/"+pattern {
				if repoType == RepoTypeNodeJS {
					if hasFile(files, "nest-cli.json") {
						return RepoTypeNestJS
					}
					if hasFile(files, "tsconfig.json") && !hasFile(files, "vite.config") && !hasFile(files, "vite.config.ts") && !hasFile(files, "vite.config.js") {
						return RepoTypeTypeScript
					}
					if hasFilePrefix(files, "vite.config") {
						return RepoTypeReact
					}
					if hasFile(files, "vue.config") || hasFile(files, "vue.config.js") {
						return RepoTypeVue
					}
				}
				return repoType
			}
		}
	}

	return RepoTypeUnknown
}

func hasFile(files []string, target string) bool {
	for _, f := range files {
		if f == target || f == "/"+target {
			return true
		}
	}
	return false
}

func hasFilePrefix(files []string, prefix string) bool {
	for _, f := range files {
		if len(f) >= len(prefix) && f[:len(prefix)] == prefix {
			return true
		}
		if len(f) > 1 && f[0] == '/' && len(f)-1 >= len(prefix) && f[1:len(prefix)+1] == prefix {
			return true
		}
	}
	return false
}
