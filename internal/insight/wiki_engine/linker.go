package wiki_engine

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/google/uuid"
)

// Linker detects and persists relationships between wiki pages.
type Linker struct {
	store WikiStore
}

// NewLinker returns a Linker backed by the given store.
func NewLinker(store WikiStore) *Linker {
	return &Linker{store: store}
}

// DetectCrossReferences scans page's content for mentions of other pages' titles
// (case-insensitive) and returns a "related" WikiLink for each match.
// Self-references are skipped.
func (l *Linker) DetectCrossReferences(page *db.WikiPage, allPages []db.WikiPage) []db.WikiLink {
	contentLower := strings.ToLower(page.Content)
	var links []db.WikiLink

	for _, candidate := range allPages {
		if candidate.ID == page.ID {
			continue
		}
		titleLower := strings.ToLower(candidate.Title)
		if titleLower == "" {
			continue
		}
		if strings.Contains(contentLower, titleLower) {
			links = append(links, db.WikiLink{
				ID:         uuid.NewString(),
				FromPageID: page.ID,
				ToPageID:   candidate.ID,
				LinkType:   "related",
				Strength:   0.5,
			})
		}
	}

	return links
}

// DetectSharedSourceLinks checks how many source references pageA and pageB share.
// If at least two sources are shared, it returns a "related" link with
// strength = sharedCount * 0.2 (capped at 1.0). Otherwise it returns nil.
func (l *Linker) DetectSharedSourceLinks(pageA *db.WikiPage, refsA []db.WikiPageRef, pageB *db.WikiPage, refsB []db.WikiPageRef) *db.WikiLink {
	setA := make(map[string]bool, len(refsA))
	for _, r := range refsA {
		setA[r.SourceType+":"+r.SourceID] = true
	}

	shared := 0
	for _, r := range refsB {
		if setA[r.SourceType+":"+r.SourceID] {
			shared++
		}
	}

	if shared < 2 {
		return nil
	}

	strength := float64(shared) * 0.2
	if strength > 1.0 {
		strength = 1.0
	}

	return &db.WikiLink{
		ID:         uuid.NewString(),
		FromPageID: pageA.ID,
		ToPageID:   pageB.ID,
		LinkType:   "related",
		Strength:   strength,
	}
}

// FindContradictions scans a set of pages for pairs that share the same title
// prefix (first three words) but have different content. Such pairs are
// returned as "contradicts" links with strength 0.7.
func (l *Linker) FindContradictions(pages []db.WikiPage) []db.WikiLink {
	type entry struct {
		titlePrefix string
		contentHash string
		pageID      string
	}

	entries := make([]entry, 0, len(pages))
	for _, p := range pages {
		entries = append(entries, entry{
			titlePrefix: titlePrefix(p.Title),
			contentHash: contentHash(p.Content),
			pageID:      p.ID,
		})
	}

	seen := make(map[string]bool)
	var links []db.WikiLink

	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			a, b := entries[i], entries[j]
			if a.titlePrefix == "" || b.titlePrefix == "" {
				continue
			}
			if a.titlePrefix != b.titlePrefix {
				continue
			}
			if a.contentHash == b.contentHash {
				continue
			}

			pairKey := a.pageID + ":" + b.pageID
			if seen[pairKey] {
				continue
			}
			seen[pairKey] = true

			links = append(links, db.WikiLink{
				ID:         uuid.NewString(),
				FromPageID: a.pageID,
				ToPageID:   b.pageID,
				LinkType:   "contradicts",
				Strength:   0.7,
			})
		}
	}

	return links
}

// SaveDetectedLinks persists each link via the store. It returns the count of
// successfully saved links and the first error encountered (if any).
func (l *Linker) SaveDetectedLinks(links []db.WikiLink) (int, error) {
	var count int
	var firstErr error

	for i := range links {
		if err := l.store.SaveLink(&links[i]); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("save detected link: %w", err)
			}
			continue
		}
		count++
	}

	return count, firstErr
}

// --- helpers ---

// titlePrefix returns the first three words of title, lower-cased.
func titlePrefix(title string) string {
	words := strings.Fields(strings.ToLower(title))
	if len(words) > 3 {
		words = words[:3]
	}
	return strings.Join(words, " ")
}

// contentHash returns a short SHA-256 hex digest of content for equality checks.
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:8])
}
