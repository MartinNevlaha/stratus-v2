package orchestration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// autodocDefaultConfidence is the confidence score stamped on pages the
// coordinator auto-generates from completed workflows. Lower than a
// human-authored page (1.0) so dashboards can down-rank when surfacing.
const autodocDefaultConfidence = 0.8

// WikiAutodocStore is the persistence interface required by AutodocWorkflow.
type WikiAutodocStore interface {
	UpsertWikiPageByWorkflow(ctx context.Context, workflowID, featureSlug string, page *db.WikiPage) (*db.WikiPage, error)
}

// WikiEnricher optionally rewrites the base autodoc markdown into a richer,
// LLM-generated functional description. Implementations receive the same
// WorkflowState passed to AutodocWorkflow plus the template-built base
// markdown and must return the final markdown to persist. Any error, or an
// empty string, causes AutodocWorkflow to fall back to the base markdown.
type WikiEnricher interface {
	Enrich(ctx context.Context, w *WorkflowState, base string) (string, error)
}

// typePrefixes lists recognised workflow-type prefixes that should be stripped
// from the workflow ID when deriving the feature slug.
var typePrefixes = []string{"spec-", "bug-", "e2e-"}

// featureSlugFromID strips a recognised type prefix (if any) from the
// workflow ID, leaving the kebab-case feature slug.
func featureSlugFromID(id string) string {
	for _, p := range typePrefixes {
		if strings.HasPrefix(id, p) {
			return strings.TrimPrefix(id, p)
		}
	}
	return id
}

// buildAutodocContent assembles the markdown body for a completed workflow wiki page.
func buildAutodocContent(w *WorkflowState) string {
	var sb strings.Builder

	// Overview
	sb.WriteString("## Overview\n\n")
	if w.PlanContent != "" {
		sb.WriteString(w.PlanContent)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("_No plan content recorded._\n\n")
	}

	// Tasks
	sb.WriteString("## Tasks\n\n")
	if len(w.Tasks) == 0 {
		sb.WriteString("_No tasks recorded._\n\n")
	} else {
		for _, t := range w.Tasks {
			mark := "[ ]"
			if t.Status == "done" {
				mark = "[x]"
			}
			fmt.Fprintf(&sb, "- %s %s\n", mark, t.Title)
		}
		sb.WriteString("\n")
	}

	// Delegated Agents
	sb.WriteString("## Delegated Agents\n\n")
	if len(w.Delegated) == 0 {
		sb.WriteString("_No agent delegations recorded._\n\n")
	} else {
		phases := make([]string, 0, len(w.Delegated))
		for p := range w.Delegated {
			phases = append(phases, p)
		}
		sort.Strings(phases)
		for _, phase := range phases {
			agents := w.Delegated[phase]
			if len(agents) == 0 {
				continue
			}
			fmt.Fprintf(&sb, "**%s**: %s\n", phase, strings.Join(agents, ", "))
		}
		sb.WriteString("\n")
	}

	// Change Summary (populated on complete; may be nil for older workflows)
	if cs := w.ChangeSummary; cs != nil {
		sb.WriteString("## Change Summary\n\n")
		fmt.Fprintf(&sb, "**Files changed:** %d (+%d / -%d)\n", cs.FilesChanged, cs.LinesAdded, cs.LinesRemoved)
		if cs.TestCoverageDelta != "" {
			fmt.Fprintf(&sb, "**Test coverage delta:** %s\n", cs.TestCoverageDelta)
		}
		sb.WriteString("\n")

		writeBulletSection(&sb, "### Capabilities added", cs.CapabilitiesAdded)
		writeBulletSection(&sb, "### Capabilities modified", cs.CapabilitiesModified)
		writeBulletSection(&sb, "### Capabilities removed", cs.CapabilitiesRemoved)
		writeBulletSection(&sb, "### Downstream risks", cs.DownstreamRisks)
	}

	// Status
	sb.WriteString("## Status\n\n")
	fmt.Fprintf(&sb, "**complete** — documented at %s\n", time.Now().UTC().Format(time.RFC3339))

	return sb.String()
}

// writeBulletSection appends a heading followed by a bulleted list of items.
// Skips the whole section when items is empty so nil-safe workflows render cleanly.
func writeBulletSection(sb *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	sb.WriteString(heading)
	sb.WriteString("\n\n")
	for _, it := range items {
		fmt.Fprintf(sb, "- %s\n", it)
	}
	sb.WriteString("\n")
}

// AutodocWorkflow writes a wiki page summarising a completed workflow.
// When enricher is non-nil its output replaces the base markdown on success;
// any error or empty string returned by the enricher is logged-and-ignored so
// the page still lands with the template fallback. It returns an error only if
// the store call fails; callers that want fail-open behaviour must recover or
// ignore the returned error.
func AutodocWorkflow(ctx context.Context, store WikiAutodocStore, enricher WikiEnricher, w *WorkflowState) error {
	slug := featureSlugFromID(w.ID)
	base := buildAutodocContent(w)
	content := base

	if enricher != nil {
		enriched, err := enricher.Enrich(ctx, w, base)
		switch {
		case err != nil:
			// Fail-open: log is the caller's responsibility; keep base template.
		case strings.TrimSpace(enriched) == "":
			// Empty response: keep base template.
		default:
			content = enriched
		}
	}

	page := &db.WikiPage{
		PageType:    "summary",
		Title:       w.Title,
		Content:     content,
		Status:      "auto-generated",
		GeneratedBy: "workflow",
		Metadata: map[string]any{
			"source":     fmt.Sprintf("workflow:%s", w.ID),
			"confidence": autodocDefaultConfidence,
		},
	}

	if _, err := store.UpsertWikiPageByWorkflow(ctx, w.ID, slug, page); err != nil {
		return fmt.Errorf("autodoc workflow %s: %w", w.ID, err)
	}
	return nil
}
