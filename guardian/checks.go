package guardian

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

type alertInput struct {
	Type     string
	Severity string
	Message  string
	Metadata map[string]any
}

// checkStaleWorkflows flags workflows that have been in the same phase too long.
func checkStaleWorkflows(coord *orchestration.Coordinator, cfg config.GuardianConfig) []alertInput {
	workflows, err := coord.ListActive()
	if err != nil || len(workflows) == 0 {
		return nil
	}

	threshold := time.Duration(cfg.StaleWorkflowHours) * time.Hour
	now := time.Now().UTC()

	var alerts []alertInput
	for _, w := range workflows {
		if w.Aborted {
			continue
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, w.UpdatedAt)
		if err != nil {
			// Try without nanoseconds
			updatedAt, err = time.Parse("2006-01-02T15:04:05.999Z", w.UpdatedAt)
			if err != nil {
				continue
			}
		}
		if now.Sub(updatedAt) > threshold {
			alerts = append(alerts, alertInput{
				Type:     "stale_workflow",
				Severity: "warning",
				Message:  fmt.Sprintf("Workflow \"%s\" has been in phase \"%s\" for over %dh", w.Title, w.Phase, cfg.StaleWorkflowHours),
				Metadata: map[string]any{
					"dedup_key":   w.ID,
					"workflow_id": w.ID,
					"phase":       string(w.Phase),
					"title":       w.Title,
				},
			})
		}
	}
	return alerts
}

// checkMemoryHealth warns when the events table grows too large.
func checkMemoryHealth(database *db.DB, cfg config.GuardianConfig) []alertInput {
	count, err := database.CountEvents()
	if err != nil || count < cfg.MemoryThreshold {
		return nil
	}
	return []alertInput{{
		Type:     "memory_health",
		Severity: "info",
		Message:  fmt.Sprintf("Memory store has %d events (threshold: %d). Consider running /learn to distill and prune.", count, cfg.MemoryThreshold),
		Metadata: map[string]any{
			"dedup_key": "memory_health",
			"count":     count,
			"threshold": cfg.MemoryThreshold,
		},
	}}
}

// checkTechDebt counts TODO/FIXME/HACK comments and compares to baseline.
func checkTechDebt(database *db.DB, projRoot string, cfg config.GuardianConfig) []alertInput {
	out, err := runCmd(projRoot, "grep", "-rlE", "--include=*.go", "--include=*.ts", "--include=*.svelte", "TODO|FIXME|HACK", ".")
	if err != nil && len(out) == 0 {
		return nil
	}

	var fileCount int
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			fileCount++
		}
	}

	baselineStr, _ := database.GetGuardianBaseline("tech_debt_count")
	baseline, _ := strconv.Atoi(baselineStr)

	if baseline == 0 {
		// First run: save baseline silently.
		_ = database.SetGuardianBaseline("tech_debt_count", strconv.Itoa(fileCount))
		return nil
	}

	delta := fileCount - baseline
	if delta >= cfg.TechDebtThreshold {
		return []alertInput{{
			Type:     "tech_debt",
			Severity: "warning",
			Message:  fmt.Sprintf("Tech debt grew by %d files with TODO/FIXME/HACK (baseline: %d, now: %d)", delta, baseline, fileCount),
			Metadata: map[string]any{
				"dedup_key":  "tech_debt",
				"file_count": fileCount,
				"baseline":   baseline,
				"delta":      delta,
			},
		}}
	}

	// Update baseline if improved
	if fileCount < baseline {
		_ = database.SetGuardianBaseline("tech_debt_count", strconv.Itoa(fileCount))
	}
	return nil
}

// checkCoverageDrift runs go test -cover and flags if coverage dropped.
func checkCoverageDrift(database *db.DB, projRoot string, cfg config.GuardianConfig) []alertInput {
	out, err := runCmdTimeout(projRoot, 120*time.Second, "go", "test", "-cover", "./...")
	if err != nil && len(out) == 0 {
		return nil
	}

	// Parse "coverage: X.X% of statements" lines and average them.
	total := 0.0
	count := 0
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "coverage: "); idx >= 0 {
			rest := line[idx+len("coverage: "):]
			if pctIdx := strings.Index(rest, "%"); pctIdx >= 0 {
				pct, err := strconv.ParseFloat(strings.TrimSpace(rest[:pctIdx]), 64)
				if err == nil {
					total += pct
					count++
				}
			}
		}
	}
	if count == 0 {
		return nil
	}
	current := total / float64(count)

	baselineStr, _ := database.GetGuardianBaseline("coverage")
	baseline, _ := strconv.ParseFloat(baselineStr, 64)

	if baseline == 0 {
		_ = database.SetGuardianBaseline("coverage", fmt.Sprintf("%.2f", current))
		return nil
	}

	drop := baseline - current
	if drop >= cfg.CoverageDriftPct {
		return []alertInput{{
			Type:     "coverage_drift",
			Severity: "warning",
			Message:  fmt.Sprintf("Test coverage dropped by %.1f%% (baseline: %.1f%%, now: %.1f%%)", drop, baseline, current),
			Metadata: map[string]any{
				"dedup_key": "coverage_drift",
				"baseline":  baseline,
				"current":   current,
				"drop":      drop,
			},
		}}
	}

	// Update baseline if improved
	if current > baseline {
		_ = database.SetGuardianBaseline("coverage", fmt.Sprintf("%.2f", current))
	}
	return nil
}

// checkGovernanceViolations checks recently modified files against governance rules.
// Uses LLM when configured; falls back to FTS-only match.
func checkGovernanceViolations(ctx context.Context, database *db.DB, llm *llmClient, projRoot string) []alertInput {
	// Get recently changed files from git.
	// Use git log instead of diff HEAD~1 to handle single-commit repos gracefully.
	out, err := runCmd(projRoot, "git", "log", "--diff-filter=d", "--name-only", "-1", "--format=")
	if err != nil || len(out) == 0 {
		return nil
	}

	var alerts []alertInput
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		file := strings.TrimSpace(scanner.Text())
		if file == "" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(file))
		if ext != ".go" && ext != ".ts" && ext != ".svelte" {
			continue
		}

		// Search governance docs for rules relevant to this file.
		searchTerm := filepath.Base(file)
		docs, err := database.SearchDocs(searchTerm, "rule", "", 3)
		if err != nil || len(docs) == 0 {
			// Also try by extension-based keyword
			keyword := extensionKeyword(ext)
			if keyword != "" {
				docs, _ = database.SearchDocs(keyword, "rule", "", 3)
			}
		}
		if len(docs) == 0 {
			continue
		}

		if llm.configured() {
			// Use LLM to determine if the file actually violates the rules.
			var rulesText strings.Builder
			for _, d := range docs {
				rulesText.WriteString("--- Rule: ")
				rulesText.WriteString(d.Title)
				rulesText.WriteString("\n")
				rulesText.WriteString(d.Content)
				rulesText.WriteString("\n\n")
			}
			prompt := fmt.Sprintf(
				"File changed: %s\n\nRelevant governance rules:\n%s\n"+
					"Does this file likely violate any of the rules above? "+
					"Answer ONLY with YES or NO, then a brief one-sentence reason.",
				file, rulesText.String(),
			)
			answer, err := llm.Complete(ctx, "You are a codebase governance checker. Be concise.", prompt)
			if err != nil {
				continue
			}
			upper := strings.ToUpper(strings.TrimSpace(answer))
			if !strings.HasPrefix(upper, "YES") {
				continue
			}
			var reason string
			if _, after, found := strings.Cut(answer, "\n"); found {
				reason = strings.TrimSpace(after)
			} else if len(answer) > 4 {
				reason = strings.TrimSpace(answer[4:])
			}
			snippet := rulesText.String()
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			alerts = append(alerts, alertInput{
				Type:     "governance_violation",
				Severity: "warning",
				Message:  fmt.Sprintf("Possible governance violation in %s: %s", file, reason),
				Metadata: map[string]any{
					"dedup_key": "gov_" + file,
					"file":      file,
					"rules":     snippet,
				},
			})
		} else {
			// No LLM: just flag that there are matching rules for this changed file.
			var ruleNames []string
			for _, d := range docs {
				ruleNames = append(ruleNames, d.Title)
			}
			alerts = append(alerts, alertInput{
				Type:     "governance_violation",
				Severity: "info",
				Message:  fmt.Sprintf("Changed file %s matches governance rules: %s — review manually", file, strings.Join(ruleNames, ", ")),
				Metadata: map[string]any{
					"dedup_key": "gov_" + file,
					"file":      file,
					"rules":     ruleNames,
				},
			})
		}
	}
	return alerts
}

func extensionKeyword(ext string) string {
	switch ext {
	case ".go":
		return "golang"
	case ".ts":
		return "typescript"
	case ".svelte":
		return "frontend"
	}
	return ""
}

func runCmd(dir string, name string, args ...string) ([]byte, error) {
	return runCmdTimeout(dir, 30*time.Second, name, args...)
}

func runCmdTimeout(dir string, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	// grep returns exit code 1 when no matches — treat as empty, not error
	if err != nil && len(out) > 0 {
		return out, nil
	}
	return out, err
}
