package wiki_engine

import (
	"strings"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func fixturePage() *db.WikiPage {
	return &db.WikiPage{
		ID:             "wp-abc123",
		PageType:       "entity",
		Title:          "Error Handling",
		Content:        "This page describes error handling and go error handling patterns.",
		Status:         "published",
		StalenessScore: 0.15,
		Tags:           []string{"go", "error-handling"},
		GeneratedBy:    "ingest",
		Version:        3,
		CreatedAt:      "2026-04-06T12:00:00Z",
		UpdatedAt:      "2026-04-06T14:30:00Z",
	}
}

func fixtureRefs() []db.WikiPageRef {
	return []db.WikiPageRef{
		{ID: "ref-1", PageID: "wp-abc123", SourceType: "solution_pattern", SourceID: "sp-456"},
		{ID: "ref-2", PageID: "wp-abc123", SourceType: "event", SourceID: "evt-789"},
	}
}

func fixtureLinkedPages() []db.WikiPage {
	return []db.WikiPage{
		{ID: "wp-other", Title: "Other Page Title"},
	}
}

// TestPageToObsidian_FullOutput verifies complete output with frontmatter + content + wikilinks.
func TestPageToObsidian_FullOutput(t *testing.T) {
	page := fixturePage()
	page.Content = "Content here with Other Page Title wikilinks."
	refs := fixtureRefs()
	linkedPages := fixtureLinkedPages()

	got := PageToObsidian(page, refs, linkedPages)

	// Must start with frontmatter delimiter
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected output to start with ---\\n, got: %q", got[:20])
	}

	// Must contain the page heading
	if !strings.Contains(got, "\n# Error Handling\n") {
		t.Errorf("expected heading '# Error Handling', got:\n%s", got)
	}

	// Content must have wikilink applied
	if !strings.Contains(got, "[[Other Page Title]]") {
		t.Errorf("expected [[Other Page Title]] wikilink in output, got:\n%s", got)
	}

	// Must contain frontmatter fields
	for _, field := range []string{"id: wp-abc123", "page_type: entity", "status: published", "version: 3"} {
		if !strings.Contains(got, field) {
			t.Errorf("expected frontmatter field %q in output, got:\n%s", field, got)
		}
	}
}

// TestObsidianFrontmatter_IncludesAllFields verifies YAML has all required fields.
func TestObsidianFrontmatter_IncludesAllFields(t *testing.T) {
	page := fixturePage()
	refs := fixtureRefs()

	fm := ObsidianFrontmatter(page, refs)

	requiredFields := []string{
		"id: wp-abc123",
		"page_type: entity",
		"status: published",
		"staleness_score: 0.15",
		"generated_by: ingest",
		"version: 3",
		"created_at: 2026-04-06T12:00:00Z",
		"updated_at: 2026-04-06T14:30:00Z",
		"  - go",
		"  - error-handling",
	}

	for _, field := range requiredFields {
		if !strings.Contains(fm, field) {
			t.Errorf("expected field %q in frontmatter, got:\n%s", field, fm)
		}
	}

	// Must open and close with ---
	if !strings.HasPrefix(fm, "---\n") {
		t.Errorf("frontmatter must start with ---\\n")
	}
	if !strings.HasSuffix(fm, "---\n") {
		t.Errorf("frontmatter must end with ---\\n")
	}
}

// TestObsidianFrontmatter_HandlesEmptyTags verifies tags renders as empty list.
func TestObsidianFrontmatter_HandlesEmptyTags(t *testing.T) {
	page := fixturePage()
	page.Tags = []string{}
	refs := []db.WikiPageRef{}

	fm := ObsidianFrontmatter(page, refs)

	if !strings.Contains(fm, "tags:\n  []\n") {
		t.Errorf("expected 'tags:\\n  []\\n' for empty tags, got:\n%s", fm)
	}
}

// TestObsidianFrontmatter_HandlesRefs verifies sources listed correctly.
func TestObsidianFrontmatter_HandlesRefs(t *testing.T) {
	page := fixturePage()
	refs := fixtureRefs()

	fm := ObsidianFrontmatter(page, refs)

	if !strings.Contains(fm, "sources:\n") {
		t.Errorf("expected 'sources:' section in frontmatter, got:\n%s", fm)
	}
	if !strings.Contains(fm, "  - type: solution_pattern\n    id: sp-456") {
		t.Errorf("expected solution_pattern ref in sources, got:\n%s", fm)
	}
	if !strings.Contains(fm, "  - type: event\n    id: evt-789") {
		t.Errorf("expected event ref in sources, got:\n%s", fm)
	}
}

// TestObsidianFrontmatter_HandlesNoRefs verifies sources renders as empty list when no refs.
func TestObsidianFrontmatter_HandlesNoRefs(t *testing.T) {
	page := fixturePage()

	fm := ObsidianFrontmatter(page, nil)

	if !strings.Contains(fm, "sources:\n  []\n") {
		t.Errorf("expected 'sources:\\n  []\\n' for empty refs, got:\n%s", fm)
	}
}

// TestContentToWikilinks_ReplacesMatches verifies title mentions become [[wikilinks]].
func TestContentToWikilinks_ReplacesMatches(t *testing.T) {
	content := "Learn about Error Handling in Go and error handling best practices."
	linkedPages := []db.WikiPage{
		{ID: "wp-1", Title: "Error Handling"},
	}

	got := ContentToWikilinks(content, linkedPages)

	if !strings.Contains(got, "[[Error Handling]]") {
		t.Errorf("expected [[Error Handling]] wikilinks, got: %s", got)
	}
	// Original plain-text occurrences replaced
	if strings.Contains(got, "Learn about Error Handling in") {
		// the first occurrence "Error Handling" (exact case) should be replaced
		// but we do a case-insensitive replace so both should be replaced
	}
}

// TestContentToWikilinks_CaseInsensitive verifies different casing still converted.
func TestContentToWikilinks_CaseInsensitive(t *testing.T) {
	content := "About error handling and ERROR HANDLING and Error Handling."
	linkedPages := []db.WikiPage{
		{ID: "wp-1", Title: "Error Handling"},
	}

	got := ContentToWikilinks(content, linkedPages)

	// All three occurrences should become [[Error Handling]]
	count := strings.Count(got, "[[Error Handling]]")
	if count != 3 {
		t.Errorf("expected 3 wikilink replacements, got %d in: %s", count, got)
	}
}

// TestContentToWikilinks_NoMatches verifies content unchanged when no linked pages match.
func TestContentToWikilinks_NoMatches(t *testing.T) {
	content := "Some content about databases and caching."
	linkedPages := []db.WikiPage{
		{ID: "wp-1", Title: "Error Handling"},
	}

	got := ContentToWikilinks(content, linkedPages)

	if got != content {
		t.Errorf("expected content unchanged, got: %s", got)
	}
}

// TestPageToVaultPath_AllTypes verifies each page type maps to the correct directory.
func TestPageToVaultPath_AllTypes(t *testing.T) {
	cases := []struct {
		pageType string
		title    string
		want     string
	}{
		{"summary", "My Summary", "summaries/my-summary.md"},
		{"entity", "Go Error", "entities/go-error.md"},
		{"concept", "Dependency Injection", "concepts/dependency-injection.md"},
		{"answer", "Why Use Interfaces", "answers/why-use-interfaces.md"},
		{"index", "Index Page", "_index.md"},
	}

	for _, tc := range cases {
		t.Run(tc.pageType, func(t *testing.T) {
			page := &db.WikiPage{PageType: tc.pageType, Title: tc.title}
			got := PageToVaultPath(page)
			if got != tc.want {
				t.Errorf("PageToVaultPath(%q, %q) = %q, want %q", tc.pageType, tc.title, got, tc.want)
			}
		})
	}
}

// TestSlugifyTitle_BasicConversion verifies spaces, special chars, uppercase are handled.
func TestSlugifyTitle_BasicConversion(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"Error Handling in Go!", "error-handling-in-go"},
		{"UPPERCASE TITLE", "uppercase-title"},
		{"mixed CASE title", "mixed-case-title"},
		{"title/with/slashes", "title-with-slashes"},
		{"title_with_underscores", "title-with-underscores"},
		{"title.with.dots", "title-with-dots"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := SlugifyTitle(tc.input)
			if got != tc.want {
				t.Errorf("SlugifyTitle(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestSlugifyTitle_ConsecutiveHyphens verifies consecutive hyphens are cleaned up.
func TestSlugifyTitle_ConsecutiveHyphens(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello   world", "hello-world"},
		{"foo--bar", "foo-bar"},
		{"  leading and trailing  ", "leading-and-trailing"},
		{"special!!chars??here", "special-chars-here"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := SlugifyTitle(tc.input)
			if got != tc.want {
				t.Errorf("SlugifyTitle(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
