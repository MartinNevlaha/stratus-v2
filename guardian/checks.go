package guardian

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
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

// alertMessages maps lang → alertType → format string.
// Add a new outer key for each supported language. The "en" entry is the
// canonical fallback; every type present in "en" should also appear in "sk".
var alertMessages = map[string]map[string]string{
	"en": {
		"stale_workflow":     `Workflow "%s" has been in phase "%s" for over %dh`,
		"stale_worker":       `Swarm worker %s (%s) has not heartbeated for >%v — marked stale`,
		"reviewer_timeout":   `Mission "%s" has been in 'verifying' for >%dmin — reviewer may be stuck`,
		"ticket_timeout":     `Ticket "%s" timed out after >%dmin in_progress — marked failed`,
		"memory_health":      `Memory store has %d events (threshold: %d). Consider running /learn to distill and prune.`,
		"tech_debt":          `Tech debt grew by %d files with TODO/FIXME/HACK (baseline: %d, now: %d)`,
		"coverage_drift":     `Test coverage dropped by %.1f%% (baseline: %.1f%%, now: %.1f%%)`,
		"governance_violation": `Possible governance violation in %s: %s`,
		"governance_match":   `Changed file %s matches governance rules: %s — review manually`,
	},
	"sk": {
		"stale_workflow":     `Workflow "%s" je v fáze "%s" dlhšie ako %d hodín`,
		"stale_worker":       `Swarm worker %s (%s) neodoslal heartbeat dlhšie ako %v — označený ako neaktívny`,
		"reviewer_timeout":   `Misia "%s" je v stave 'verifying' dlhšie ako %d min — recenzent môže byť zaseknutý`,
		"ticket_timeout":     `Ticket "%s" vypršal po viac ako %d min v stave in_progress — označený ako zlyhaný`,
		"memory_health":      `Pamäťové úložisko obsahuje %d udalostí (prahová hodnota: %d). Zvážte spustenie /learn na destilláciu a čistenie.`,
		"tech_debt":          `Technický dlh narástol o %d súborov s TODO/FIXME/HACK (základ: %d, teraz: %d)`,
		"coverage_drift":     `Pokrytie testami kleslo o %.1f%% (základ: %.1f%%, teraz: %.1f%%)`,
		"governance_violation": `Možné porušenie pravidiel správy v %s: %s`,
		"governance_match":   `Zmenený súbor %s zodpovedá pravidlám správy: %s — skontrolujte manuálne`,
	},
}

// alertMessage looks up the format string for lang/alertType, falls back to
// "en" if lang is unknown, and formats it with the provided args. If alertType
// is not found even in "en", it returns a best-effort fallback string and logs
// a warning.
func alertMessage(lang, alertType string, args ...any) string {
	byType, ok := alertMessages[lang]
	if !ok {
		slog.Warn("guardian: unknown language, falling back to en",
			"lang", lang,
			"alert_type", alertType,
		)
		byType = alertMessages["en"]
	}

	format, ok := byType[alertType]
	if !ok {
		// Try English fallback before giving up.
		format, ok = alertMessages["en"][alertType]
		if !ok {
			slog.Warn("guardian: unknown alert type, using fallback message",
				"lang", lang,
				"alert_type", alertType,
			)
			return fmt.Sprintf("guardian alert: %s", alertType)
		}
		slog.Warn("guardian: alert type missing in language, falling back to en",
			"lang", lang,
			"alert_type", alertType,
		)
	}

	return fmt.Sprintf(format, args...)
}

// checkStaleWorkflows flags workflows that have been in the same phase too long.
func checkStaleWorkflows(coord *orchestration.Coordinator, cfg config.GuardianConfig, lang string) []alertInput {
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
				Message:  alertMessage(lang, "stale_workflow", w.Title, string(w.Phase), cfg.StaleWorkflowHours),
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

// checkStaleWorkers flags swarm workers that have stopped heartbeating.
func checkStaleWorkers(database *db.DB, cfg config.GuardianConfig, lang string) []alertInput {
	threshold := 5 * time.Minute
	if cfg.StaleWorkflowHours > 0 {
		// Use a fraction of the stale workflow threshold for workers (workers should heartbeat more frequently)
		threshold = time.Duration(cfg.StaleWorkflowHours) * time.Hour / 12
		if threshold < 5*time.Minute {
			threshold = 5 * time.Minute
		}
	}

	staleWorkers, err := database.ListStaleWorkers(threshold)
	if err != nil || len(staleWorkers) == 0 {
		return nil
	}

	var alerts []alertInput
	for _, w := range staleWorkers {
		// Mark as stale in DB
		_ = database.UpdateWorkerStatus(w.ID, "stale")
		alerts = append(alerts, alertInput{
			Type:     "stale_worker",
			Severity: "warning",
			Message:  alertMessage(lang, "stale_worker", w.ID, w.AgentType, threshold),
			Metadata: map[string]any{
				"dedup_key":      "stale_worker_" + w.ID,
				"worker_id":      w.ID,
				"mission_id":     w.MissionID,
				"agent_type":     w.AgentType,
				"last_heartbeat": w.LastHeartbeat,
			},
		})
	}
	return alerts
}

// checkStaleVerifying alerts when a mission has been stuck in 'verifying' longer
// than cfg.ReviewerTimeoutMinutes (default 30). This catches reviewer agents that
// silently hang without producing an evidence verdict.
func checkStaleVerifying(database *db.DB, cfg config.GuardianConfig, lang string) []alertInput {
	minutes := cfg.ReviewerTimeoutMinutes
	if minutes <= 0 {
		minutes = 30
	}
	threshold := time.Duration(minutes) * time.Minute

	missions, err := database.ListStaleVerifyingMissions(threshold)
	if err != nil || len(missions) == 0 {
		return nil
	}

	var alerts []alertInput
	for _, m := range missions {
		alerts = append(alerts, alertInput{
			Type:     "reviewer_timeout",
			Severity: "warning",
			Message:  alertMessage(lang, "reviewer_timeout", m.Title, minutes),
			Metadata: map[string]any{
				"dedup_key":       "reviewer_timeout_" + m.ID,
				"mission_id":      m.ID,
				"verifying_since": m.VerifyingSince,
			},
		})
	}
	return alerts
}

// checkOverdueTickets alerts when a ticket has been in_progress longer than
// cfg.TicketTimeoutMinutes (default 30) and marks it failed to unblock the worker.
func checkOverdueTickets(database *db.DB, cfg config.GuardianConfig, lang string) []alertInput {
	minutes := cfg.TicketTimeoutMinutes
	if minutes <= 0 {
		minutes = 30
	}
	threshold := time.Duration(minutes) * time.Minute

	tickets, err := database.ListOverdueTickets(threshold)
	if err != nil || len(tickets) == 0 {
		return nil
	}

	var alerts []alertInput
	for _, t := range tickets {
		reason := fmt.Sprintf("timeout: ticket in_progress for >%dmin", minutes)
		_ = database.UpdateTicketStatus(t.ID, "failed", reason, 5, 3)

		// Send TICKET_TIMEOUT signal to the assigned worker if known.
		if t.WorkerID != nil && *t.WorkerID != "" {
			_ = database.CreateSignal(
				fmt.Sprintf("guardian-%s", t.ID[:8]),
				t.MissionID, "guardian", *t.WorkerID,
				"TICKET_TIMEOUT",
				fmt.Sprintf(`{"ticket_id":%q,"reason":%q}`, t.ID, reason),
			)
		}

		alerts = append(alerts, alertInput{
			Type:     "ticket_timeout",
			Severity: "warning",
			Message:  alertMessage(lang, "ticket_timeout", t.Title, minutes),
			Metadata: map[string]any{
				"dedup_key":  "ticket_timeout_" + t.ID,
				"ticket_id":  t.ID,
				"mission_id": t.MissionID,
				"worker_id":  t.WorkerID,
			},
		})
	}
	return alerts
}

// checkMemoryHealth warns when the events table grows too large.
func checkMemoryHealth(database *db.DB, cfg config.GuardianConfig, lang string) []alertInput {
	count, err := database.CountEvents()
	if err != nil || count < cfg.MemoryThreshold {
		return nil
	}
	return []alertInput{{
		Type:     "memory_health",
		Severity: "info",
		Message:  alertMessage(lang, "memory_health", count, cfg.MemoryThreshold),
		Metadata: map[string]any{
			"dedup_key": "memory_health",
			"count":     count,
			"threshold": cfg.MemoryThreshold,
		},
	}}
}

// checkTechDebt counts TODO/FIXME/HACK comments and compares to baseline.
func checkTechDebt(database *db.DB, projRoot string, cfg config.GuardianConfig, lang string) []alertInput {
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
			Message:  alertMessage(lang, "tech_debt", delta, baseline, fileCount),
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
func checkCoverageDrift(database *db.DB, projRoot string, cfg config.GuardianConfig, lang string) []alertInput {
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
			Message:  alertMessage(lang, "coverage_drift", drop, baseline, current),
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
func checkGovernanceViolations(ctx context.Context, database *db.DB, llm guardianLLM, projRoot string, lang string) []alertInput {
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
				Message:  alertMessage(lang, "governance_violation", file, reason),
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
				Message:  alertMessage(lang, "governance_match", file, strings.Join(ruleNames, ", ")),
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
