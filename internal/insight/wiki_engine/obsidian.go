package wiki_engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// PageToObsidian converts a WikiPage to Obsidian-compatible markdown with YAML frontmatter.
func PageToObsidian(page *db.WikiPage, refs []db.WikiPageRef, linkedPages []db.WikiPage) string {
	var b strings.Builder
	b.WriteString(ObsidianFrontmatter(page, refs))
	b.WriteString("\n# ")
	b.WriteString(page.Title)
	b.WriteString("\n\n")
	b.WriteString(ContentToWikilinks(page.Content, linkedPages))
	b.WriteString("\n")
	return b.String()
}

// ObsidianFrontmatter generates YAML frontmatter for a wiki page.
func ObsidianFrontmatter(page *db.WikiPage, refs []db.WikiPageRef) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", page.ID)
	fmt.Fprintf(&b, "page_type: %s\n", page.PageType)
	fmt.Fprintf(&b, "status: %s\n", page.Status)
	fmt.Fprintf(&b, "staleness_score: %g\n", page.StalenessScore)

	b.WriteString("tags:\n")
	if len(page.Tags) == 0 {
		b.WriteString("  []\n")
	} else {
		for _, tag := range page.Tags {
			fmt.Fprintf(&b, "  - %s\n", tag)
		}
	}

	fmt.Fprintf(&b, "generated_by: %s\n", page.GeneratedBy)
	fmt.Fprintf(&b, "version: %d\n", page.Version)
	fmt.Fprintf(&b, "created_at: %s\n", page.CreatedAt)
	fmt.Fprintf(&b, "updated_at: %s\n", page.UpdatedAt)

	b.WriteString("sources:\n")
	if len(refs) == 0 {
		b.WriteString("  []\n")
	} else {
		for _, ref := range refs {
			fmt.Fprintf(&b, "  - type: %s\n", ref.SourceType)
			fmt.Fprintf(&b, "    id: %s\n", ref.SourceID)
		}
	}

	b.WriteString("---\n")
	return b.String()
}

// ContentToWikilinks replaces plain page title mentions in content with [[wikilink]] syntax.
// Matching is case-insensitive, but the wikilink uses the canonical page title.
func ContentToWikilinks(content string, linkedPages []db.WikiPage) string {
	for _, lp := range linkedPages {
		if lp.Title == "" {
			continue
		}
		pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(lp.Title) + `\b`)
		replacement := "[[" + lp.Title + "]]"
		content = pattern.ReplaceAllString(content, replacement)
	}
	return content
}

// PageToVaultPath returns the relative file path within the vault for a given page type.
func PageToVaultPath(page *db.WikiPage) string {
	slug := SlugifyTitle(page.Title)
	switch page.PageType {
	case "summary":
		return "summaries/" + slug + ".md"
	case "entity":
		return "entities/" + slug + ".md"
	case "concept":
		return "concepts/" + slug + ".md"
	case "answer":
		return "answers/" + slug + ".md"
	case "index":
		return "_index.md"
	case "raw":
		return "01-Raw-Sources/" + slug + ".md"
	case "topic":
		return "topics/" + slug + ".md"
	default:
		return slug + ".md"
	}
}

// SlugifyTitle converts a page title to a filename-safe slug.
// Lowercases the title, replaces spaces and special characters with hyphens,
// removes consecutive hyphens, and trims leading/trailing hyphens.
func SlugifyTitle(title string) string {
	// Lowercase
	s := strings.ToLower(title)

	// Replace any non-alphanumeric (except hyphen) with a hyphen
	nonAlphanumRe := regexp.MustCompile(`[^a-z0-9]+`)
	s = nonAlphanumRe.ReplaceAllString(s, "-")

	// Trim leading and trailing hyphens
	s = strings.Trim(s, "-")

	return s
}
