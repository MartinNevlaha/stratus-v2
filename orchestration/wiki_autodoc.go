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

	// Status
	sb.WriteString("## Status\n\n")
	fmt.Fprintf(&sb, "**complete** — documented at %s\n", time.Now().UTC().Format(time.RFC3339))

	return sb.String()
}

// AutodocWorkflow writes a wiki page summarising a completed workflow.
// It returns an error if the store call fails; callers that want fail-open
// behaviour must recover or ignore the returned error.
func AutodocWorkflow(ctx context.Context, store WikiAutodocStore, w *WorkflowState) error {
	slug := featureSlugFromID(w.ID)

	page := &db.WikiPage{
		PageType:    "summary",
		Title:       w.Title,
		Content:     buildAutodocContent(w),
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
