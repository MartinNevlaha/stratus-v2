package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/orchestration"
)

const gitTimeout = 30 * time.Second

// generateChangeSummary runs asynchronously after a workflow reaches "complete".
// It computes git diff stats, searches governance docs, and queries vexor,
// then stores the structural summary in the workflow state.
// If baseCommit is empty (e.g. non-git repo or SetBaseCommit failed), it returns early.
func (s *Server) generateChangeSummary(workflowID, baseCommit string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("change summary: panic recovered for workflow %s: %v", workflowID, r)
		}
	}()

	if baseCommit == "" {
		log.Printf("change summary: skipped for workflow %s — no base commit recorded", workflowID)
		return
	}

	summary := &orchestration.ChangeSummary{
		CapabilitiesAdded:    []string{},
		CapabilitiesModified: []string{},
		CapabilitiesRemoved:  []string{},
		DownstreamRisks:      []string{},
		GovernanceCompliance: []string{},
		GovernanceDocs:       []string{},
		VexorExcerpts:        []string{},
	}

	// 1. Git diff --stat for line counts
	statOut, err := runGit(s.projectRoot, "diff", "--stat", baseCommit+"..HEAD")
	if err == nil {
		parseGitStat(statOut, summary)
	}

	// 2. Changed file paths
	nameOut, err := runGit(s.projectRoot, "diff", "--name-only", baseCommit+"..HEAD")
	if err != nil || strings.TrimSpace(nameOut) == "" {
		summary.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
		if _, saveErr := s.coordinator.SetChangeSummary(workflowID, summary); saveErr != nil {
			log.Printf("change summary: save error: %v", saveErr)
		}
		return
	}

	changedFiles := strings.Split(strings.TrimSpace(nameOut), "\n")
	summary.FilesChanged = len(changedFiles)

	// 3. FTS governance search — query by unique top-level module names
	modules := uniqueModules(changedFiles)
	seenDocs := map[string]bool{}
	for _, mod := range modules {
		docs, err := s.db.SearchDocs(mod, "", "", 3)
		if err != nil {
			continue
		}
		for _, doc := range docs {
			key := doc.FilePath
			if seenDocs[key] {
				continue
			}
			seenDocs[key] = true
			label := doc.Title
			if label == "" {
				label = filepath.Base(doc.FilePath)
			}
			summary.GovernanceDocs = append(summary.GovernanceDocs, label+" ("+doc.FilePath+")")
			if doc.DocType == "rule" || doc.DocType == "adr" {
				summary.GovernanceCompliance = append(summary.GovernanceCompliance, label)
			}
		}
	}

	// 4. Vexor similarity search — find similar past changes
	if s.vexor.Available() && len(changedFiles) > 0 {
		query := "changes in " + strings.Join(modules, ", ")
		results, err := s.vexor.Search(query, 5, "hybrid")
		if err == nil {
			for _, r := range results {
				excerpt := r.Heading
				if r.Excerpt != "" {
					excerpt += ": " + r.Excerpt
				}
				summary.VexorExcerpts = append(summary.VexorExcerpts, excerpt)
			}
		}
	}

	summary.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	state, err := s.coordinator.SetChangeSummary(workflowID, summary)
	if err != nil {
		log.Printf("change summary: save error: %v", err)
		return
	}

	s.hub.BroadcastJSON("workflow_updated", state)
	s.indexSummaryFile(state)
}

// runGit executes a git command in the project root with a 30s timeout.
func runGit(projectRoot string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: timeout after %v", strings.Join(args, " "), gitTimeout)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// parseGitStat parses `git diff --stat` output to extract lines added/removed.
// The last line looks like: " 3 files changed, 42 insertions(+), 10 deletions(-)"
func parseGitStat(output string, s *orchestration.ChangeSummary) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return
	}
	summary := lines[len(lines)-1]
	for _, part := range strings.Split(summary, ",") {
		part = strings.TrimSpace(part)
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		if strings.Contains(part, "insertion") {
			n, _ := strconv.Atoi(fields[0])
			s.LinesAdded = n
		} else if strings.Contains(part, "deletion") {
			n, _ := strconv.Atoi(fields[0])
			s.LinesRemoved = n
		}
	}
}

// uniqueModules returns deduplicated top-level directory names from file paths.
func uniqueModules(files []string) []string {
	seen := map[string]bool{}
	var mods []string
	for _, f := range files {
		parts := strings.SplitN(f, "/", 2)
		mod := parts[0]
		if mod == "" || seen[mod] {
			continue
		}
		seen[mod] = true
		mods = append(mods, mod)
	}
	return mods
}

// indexSummaryFile writes the change summary to docs/change-summaries/{id}.md
// and marks it dirty so Vexor picks it up. This makes past summaries discoverable
// in future similarity searches ("similar past changes and their consequences").
func (s *Server) indexSummaryFile(state *orchestration.WorkflowState) {
	if state.ChangeSummary == nil {
		return
	}
	if !s.vexor.Available() {
		return
	}

	dir := filepath.Join(s.projectRoot, "docs", "change-summaries")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("change summary: mkdir %s: %v", dir, err)
		return
	}

	filePath := filepath.Join(dir, state.ID+".md")
	if err := os.WriteFile(filePath, []byte(renderSummaryMD(state)), 0o644); err != nil {
		log.Printf("change summary: write file %s: %v", filePath, err)
		return
	}

	s.markDirty([]string{filePath})
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// handleGetSummary returns the change summary for a completed workflow.
func (s *Server) handleGetSummary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, err := s.coordinator.Get(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	if state.ChangeSummary == nil {
		json200(w, map[string]string{"status": "pending"})
		return
	}
	json200(w, state.ChangeSummary)
}

// handleGetSummaryMD returns the change summary as a Markdown document.
func (s *Server) handleGetSummaryMD(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	state, err := s.coordinator.Get(id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	if state.ChangeSummary == nil {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		fmt.Fprintf(w, "# Change Summary — %s\n\n_Analysis pending._\n", state.Title)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	fmt.Fprint(w, renderSummaryMD(state))
}

// handleUpdateSummary stores semantic fields submitted by an agent.
func (s *Server) handleUpdateSummary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var incoming orchestration.ChangeSummary
	if err := decodeBody(r, &incoming); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	state, err := s.coordinator.SetChangeSummary(id, &incoming)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, orchestration.ErrWorkflowNotFound) {
			status = http.StatusNotFound
		}
		jsonErr(w, status, err.Error())
		return
	}
	s.hub.BroadcastJSON("workflow_updated", state)
	go s.indexSummaryFile(state)
	json200(w, state.ChangeSummary)
}

// renderSummaryMD converts a ChangeSummary to a Markdown string.
func renderSummaryMD(state *orchestration.WorkflowState) string {
	cs := state.ChangeSummary
	var b strings.Builder

	fmt.Fprintf(&b, "# Change Summary — %s\n\n", state.Title)
	fmt.Fprintf(&b, "**%d files changed**, +%d / -%d lines\n\n",
		cs.FilesChanged, cs.LinesAdded, cs.LinesRemoved)

	if cs.TestCoverageDelta != "" {
		fmt.Fprintf(&b, "**Test coverage delta:** %s\n\n", cs.TestCoverageDelta)
	}

	if len(cs.CapabilitiesAdded) > 0 {
		b.WriteString("## Capabilities Added\n\n")
		for _, c := range cs.CapabilitiesAdded {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteByte('\n')
	}
	if len(cs.CapabilitiesModified) > 0 {
		b.WriteString("## Capabilities Modified\n\n")
		for _, c := range cs.CapabilitiesModified {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteByte('\n')
	}
	if len(cs.CapabilitiesRemoved) > 0 {
		b.WriteString("## Capabilities Removed\n\n")
		for _, c := range cs.CapabilitiesRemoved {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteByte('\n')
	}
	if len(cs.DownstreamRisks) > 0 {
		b.WriteString("## Downstream Risks\n\n")
		for _, r := range cs.DownstreamRisks {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteByte('\n')
	}
	if len(cs.GovernanceCompliance) > 0 {
		b.WriteString("## Governance\n\n")
		for _, g := range cs.GovernanceCompliance {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		b.WriteByte('\n')
	}

	if cs.GeneratedAt != "" {
		fmt.Fprintf(&b, "_Generated at %s_\n", cs.GeneratedAt)
	}

	return b.String()
}
