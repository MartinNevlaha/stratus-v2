// Package ingest turns external raw sources (PDF, URL, YouTube, markdown, text)
// into wiki pages in the Stratus knowledge base. It implements the "Raw Sources"
// layer of the Karpathy-style LLM wiki pattern.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
	"github.com/google/uuid"
)

// ErrEmptySource is returned for blank inputs.
var ErrEmptySource = errors.New("ingest: empty source")

// ErrNoContent is returned when extraction produced no usable text.
var ErrNoContent = errors.New("ingest: no content extracted")

// MaxContentChars caps raw text length to protect DB and LLM contexts.
const MaxContentChars = 2_000_000

// Options controls a single ingest call.
type Options struct {
	Tags            []string
	Title           string // overrides inferred title
	AutoSynthesize  bool   // create a cleaned wiki page in addition to raw
	SkipLinkSuggest bool   // suppress link_suggester even when config enables it
}

// Result reports what ingest produced.
type Result struct {
	Kind        Kind   `json:"kind"`
	RawPageID   string `json:"raw_page_id"`
	WikiPageID  string `json:"wiki_page_id,omitempty"`
	Title       string `json:"title"`
	Chars       int    `json:"chars"`
	SynthError  string `json:"synth_error,omitempty"`
	SuggestedLinks int `json:"suggested_links,omitempty"`
}

// LinkSuggester is the optional pluggable surface for producing stub pages
// from a just-ingested wiki page. The real implementation lives in wiki_engine.
type LinkSuggester interface {
	SuggestAndCreateStubs(ctx context.Context, page *db.WikiPage) (int, error)
}

// Ingester runs ingest pipelines.
type Ingester struct {
	store    wiki_engine.WikiStore
	engine   *wiki_engine.WikiEngine
	cfgFn    func() config.WikiConfig
	suggest  LinkSuggester // may be nil
}

// New returns an Ingester bound to the given wiki store, engine and config
// accessor. The LinkSuggester is optional.
func New(store wiki_engine.WikiStore, engine *wiki_engine.WikiEngine, cfgFn func() config.WikiConfig, suggest LinkSuggester) *Ingester {
	return &Ingester{store: store, engine: engine, cfgFn: cfgFn, suggest: suggest}
}

// IngestFile is a narrow adapter used by watchers/MCP clients that only need to
// feed a filesystem path and tags. It always runs AutoSynthesize.
func (i *Ingester) IngestFile(ctx context.Context, path string, tags []string) (string, error) {
	res, err := i.Ingest(ctx, path, Options{Tags: tags, AutoSynthesize: true})
	if err != nil {
		return "", err
	}
	return res.RawPageID, nil
}

// Ingest runs the full pipeline: detect → extract → save raw page → (optional)
// synthesize clean wiki page → (optional) link suggester. All extractor failures
// are returned wrapped; partial success is possible when AutoSynthesize fails
// (the raw page persists and SynthError is populated in the Result).
func (i *Ingester) Ingest(ctx context.Context, source string, opts Options) (*Result, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, ErrEmptySource
	}

	kind := DetectKind(source)
	cfg := i.cfgFn()

	title, text, err := i.extract(ctx, kind, source, cfg)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, ErrNoContent
	}
	if len(text) > MaxContentChars {
		text = text[:MaxContentChars]
	}

	if opts.Title != "" {
		title = opts.Title
	}
	if title == "" {
		title = inferTitle(source, kind)
	}

	sourceType := sourceTypeForKind(kind)
	rawPage := &db.WikiPage{
		ID:          uuid.NewString(),
		PageType:    db.PageTypeRaw,
		Title:       title,
		Content:     text,
		Status:      "published",
		GeneratedBy: db.GeneratedByIngest,
		Tags:        dedupeTags(opts.Tags),
		Version:     1,
	}
	if err := i.store.SavePage(rawPage); err != nil {
		return nil, fmt.Errorf("ingest: save raw page: %w", err)
	}

	excerpt := text
	if len(excerpt) > 300 {
		excerpt = excerpt[:300]
	}
	if err := i.store.SaveRef(&db.WikiPageRef{
		ID:         uuid.NewString(),
		PageID:     rawPage.ID,
		SourceType: sourceType,
		SourceID:   source,
		Excerpt:    excerpt,
	}); err != nil {
		slog.Warn("ingest: save ref failed", "page_id", rawPage.ID, "err", err)
	}

	res := &Result{
		Kind:      kind,
		RawPageID: rawPage.ID,
		Title:     title,
		Chars:     len(text),
	}

	if !opts.AutoSynthesize {
		return res, nil
	}

	// Synthesize a cleaned wiki page (concept).
	cleanedRefs := []db.WikiPageRef{{
		SourceType: sourceType,
		SourceID:   source,
		Excerpt:    excerpt,
	}}
	wikiPage, synthErr := i.engine.GeneratePageFromData(ctx, title, text, db.PageTypeConcept, cleanedRefs)
	if synthErr != nil {
		res.SynthError = synthErr.Error()
		slog.Warn("ingest: synthesize failed", "title", title, "err", synthErr)
		return res, nil
	}
	res.WikiPageID = wikiPage.ID

	// Link raw → wiki via "cites".
	if err := i.store.SaveLink(&db.WikiLink{
		ID:         uuid.NewString(),
		FromPageID: wikiPage.ID,
		ToPageID:   rawPage.ID,
		LinkType:   "cites",
		Strength:   1.0,
	}); err != nil {
		slog.Warn("ingest: save cites link failed", "err", err)
	}

	if !opts.SkipLinkSuggest && cfg.LinkSuggesterEnabled && i.suggest != nil {
		n, err := i.suggest.SuggestAndCreateStubs(ctx, wikiPage)
		if err != nil {
			slog.Warn("ingest: link suggester failed", "page_id", wikiPage.ID, "err", err)
		}
		res.SuggestedLinks = n
	}

	return res, nil
}

// extract dispatches by kind.
func (i *Ingester) extract(ctx context.Context, kind Kind, source string, cfg config.WikiConfig) (title, text string, err error) {
	switch kind {
	case KindPDF:
		t, err := extractPDF(ctx, source, cfg.IngestPdfBinary)
		return "", t, err
	case KindYouTube:
		t, err := extractYouTube(ctx, source, cfg.IngestYoutubeBinary)
		return "", t, err
	case KindURL:
		to := time.Duration(cfg.IngestHTTPTimeoutSec) * time.Second
		return extractURL(ctx, source, to)
	case KindMarkdown, KindText:
		b, err := os.ReadFile(source)
		if err != nil {
			return "", "", fmt.Errorf("ingest: read %s: %w", source, err)
		}
		return "", string(b), nil
	case KindUnknown:
		return "", "", fmt.Errorf("ingest: unknown source kind for %q", source)
	}
	return "", "", fmt.Errorf("ingest: unsupported kind %q", kind)
}

func sourceTypeForKind(k Kind) string {
	switch k {
	case KindPDF, KindMarkdown, KindText:
		return "file"
	case KindYouTube:
		return "youtube"
	case KindURL:
		return "url"
	}
	return "unknown"
}

func inferTitle(source string, kind Kind) string {
	switch kind {
	case KindURL, KindYouTube:
		return source
	}
	base := filepath.Base(source)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

func dedupeTags(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
