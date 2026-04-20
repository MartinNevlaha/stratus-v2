package wiki_engine

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/prompts"
	"github.com/google/uuid"
)

// ClusterConfig tunes ClusterSynthesizer.
type ClusterConfig struct {
	MinSources         int // minimum raw pages per tag bucket
	MaxContextChars    int // chars of raw content fed to LLM per cluster (default 16000)
	RegenerateHalfStep bool // if true, bump topic page when #new raw >= MinSources/2
}

// ClusterResult reports counts from a SynthesizeClusters call.
type ClusterResult struct {
	ClustersScanned  int `json:"clusters_scanned"`
	TopicsCreated    int `json:"topics_created"`
	TopicsRegenerated int `json:"topics_regenerated"`
	TopicsSkipped    int `json:"topics_skipped"`
}

// ClusterSynthesizer groups raw pages by tag and synthesizes a single "topic"
// page per bucket that reaches MinSources.
type ClusterSynthesizer struct {
	store     WikiStore
	llmClient LLMClient
	cfg       ClusterConfig

	mu      sync.Mutex
	running bool
}

// NewClusterSynthesizer constructs a ClusterSynthesizer.
func NewClusterSynthesizer(store WikiStore, llmClient LLMClient, cfg ClusterConfig) *ClusterSynthesizer {
	if cfg.MinSources <= 0 {
		cfg.MinSources = 5
	}
	if cfg.MaxContextChars <= 0 {
		cfg.MaxContextChars = 16000
	}
	return &ClusterSynthesizer{store: store, llmClient: llmClient, cfg: cfg}
}

// SynthesizeClusters scans raw pages, groups by tag and creates/updates
// topic pages. Re-entrant-safe: concurrent callers are rejected with a running
// error so the scheduler and ad-hoc HTTP trigger don't double up.
func (c *ClusterSynthesizer) SynthesizeClusters(ctx context.Context) (*ClusterResult, error) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil, fmt.Errorf("cluster synthesis: already running")
	}
	c.running = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()

	if c.llmClient == nil {
		return nil, fmt.Errorf("cluster synthesis: no LLM client")
	}

	rawPages, _, err := c.store.ListPages(db.WikiPageFilters{
		PageType: db.PageTypeRaw,
		Status:   "published",
		Limit:    10000,
	})
	if err != nil {
		return nil, fmt.Errorf("cluster synthesis: list raw: %w", err)
	}

	topicPages, _, err := c.store.ListPages(db.WikiPageFilters{
		PageType: db.PageTypeTopic,
		Limit:    10000,
	})
	if err != nil {
		return nil, fmt.Errorf("cluster synthesis: list topics: %w", err)
	}
	topicByTag := make(map[string]*db.WikiPage, len(topicPages))
	for i := range topicPages {
		for _, t := range topicPages[i].Tags {
			topicByTag[strings.ToLower(t)] = &topicPages[i]
		}
	}

	buckets := groupByTag(rawPages)
	result := &ClusterResult{}

	// Stable order for deterministic runs.
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, tag := range keys {
		result.ClustersScanned++
		sources := buckets[tag]
		if len(sources) < c.cfg.MinSources {
			result.TopicsSkipped++
			continue
		}
		existing := topicByTag[strings.ToLower(tag)]
		if existing != nil && !c.shouldRegenerate(existing, sources) {
			result.TopicsSkipped++
			continue
		}

		if err := ctx.Err(); err != nil {
			return result, err
		}

		topic, regenerated, err := c.synthesize(ctx, tag, sources, existing)
		if err != nil {
			slog.Warn("cluster synthesis: bucket failed", "tag", tag, "err", err)
			continue
		}

		if regenerated {
			result.TopicsRegenerated++
		} else {
			result.TopicsCreated++
		}
		if err := c.linkTopicToSources(topic, sources); err != nil {
			slog.Warn("cluster synthesis: link topic failed", "topic_id", topic.ID, "err", err)
		}
	}

	return result, nil
}

func (c *ClusterSynthesizer) shouldRegenerate(existing *db.WikiPage, sources []db.WikiPage) bool {
	if !c.cfg.RegenerateHalfStep {
		return false
	}
	updated, err := time.Parse(time.RFC3339Nano, existing.UpdatedAt)
	if err != nil {
		return false
	}
	newer := 0
	for _, s := range sources {
		t, err := time.Parse(time.RFC3339Nano, s.CreatedAt)
		if err != nil {
			continue
		}
		if t.After(updated) {
			newer++
		}
	}
	return newer >= c.cfg.MinSources/2
}

func (c *ClusterSynthesizer) synthesize(
	ctx context.Context, tag string, sources []db.WikiPage, existing *db.WikiPage,
) (*db.WikiPage, bool, error) {
	user := buildClusterContext(tag, sources, c.cfg.MaxContextChars)
	resp, err := c.llmClient.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: prompts.Compose(prompts.WikiTopicSynthesis, prompts.ObsidianMarkdown),
		Messages:     []llm.Message{{Role: "user", Content: user}},
		MaxTokens:    100000,
		Temperature:  0.4,
	})
	if err != nil {
		return nil, false, fmt.Errorf("cluster synthesis: llm: %w", err)
	}

	if existing != nil {
		existing.Content = resp.Content
		existing.Version++
		if err := c.store.UpdatePage(existing); err != nil {
			return nil, false, fmt.Errorf("cluster synthesis: update: %w", err)
		}
		return existing, true, nil
	}

	topic := &db.WikiPage{
		ID:          uuid.NewString(),
		PageType:    db.PageTypeTopic,
		Title:       tagToTitle(tag),
		Content:     resp.Content,
		Status:      "published",
		GeneratedBy: db.GeneratedByCluster,
		Tags:        []string{tag},
		Version:     1,
	}
	if err := c.store.SavePage(topic); err != nil {
		return nil, false, fmt.Errorf("cluster synthesis: save: %w", err)
	}
	return topic, false, nil
}

func (c *ClusterSynthesizer) linkTopicToSources(topic *db.WikiPage, sources []db.WikiPage) error {
	for _, s := range sources {
		if err := c.store.SaveLink(&db.WikiLink{
			ID:         uuid.NewString(),
			FromPageID: topic.ID,
			ToPageID:   s.ID,
			LinkType:   "cites",
			Strength:   1.0,
		}); err != nil {
			return err
		}
	}
	return nil
}

// groupByTag buckets raw pages by each of their tags. A page with N tags ends
// up in N buckets — deliberate so tags like "ml" and "transformers" each
// contribute to the right topic.
func groupByTag(pages []db.WikiPage) map[string][]db.WikiPage {
	out := make(map[string][]db.WikiPage)
	for _, p := range pages {
		for _, t := range p.Tags {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			out[t] = append(out[t], p)
		}
	}
	return out
}

func buildClusterContext(tag string, sources []db.WikiPage, max int) string {
	// Prefer newest first (sources already may be ordered by DB).
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].CreatedAt > sources[j].CreatedAt
	})
	var sb strings.Builder
	fmt.Fprintf(&sb, "Topic tag: %s\n\nRaw sources:\n", tag)
	remaining := max
	for _, s := range sources {
		block := fmt.Sprintf("\n---\n### %s (id:%s)\n%s\n", s.Title, s.ID, s.Content)
		if len(block) > remaining {
			if remaining <= 200 {
				break
			}
			block = block[:remaining]
		}
		sb.WriteString(block)
		remaining -= len(block)
		if remaining <= 0 {
			break
		}
	}
	return sb.String()
}

func tagToTitle(tag string) string {
	if tag == "" {
		return "Topic"
	}
	return "Topic: " + tag
}
