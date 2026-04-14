package wiki_engine

import (
	"context"
	"testing"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func TestClusterSynthesizer_CreatesTopicWhenMinMet(t *testing.T) {
	store := &memStore{}
	// 5 raw pages with tag "ml"
	for i := 0; i < 5; i++ {
		store.pages = append(store.pages, db.WikiPage{
			ID: idOf(i), Title: "raw " + idOf(i), PageType: "raw", Status: "published",
			Tags: []string{"ml"}, Content: "content " + idOf(i),
		})
	}
	cs := NewClusterSynthesizer(store, &canned{body: "# Topic ML\n\nSynthesis."}, ClusterConfig{MinSources: 5})

	res, err := cs.SynthesizeClusters(context.Background())
	if err != nil {
		t.Fatalf("SynthesizeClusters: %v", err)
	}
	if res.TopicsCreated != 1 {
		t.Errorf("TopicsCreated = %d, want 1", res.TopicsCreated)
	}
	var topicCount int
	for _, p := range store.pages {
		if p.PageType == "topic" {
			topicCount++
			if p.GeneratedBy != db.GeneratedByCluster {
				t.Errorf("GeneratedBy = %q", p.GeneratedBy)
			}
			if len(p.Tags) != 1 || p.Tags[0] != "ml" {
				t.Errorf("tags = %v", p.Tags)
			}
		}
	}
	if topicCount != 1 {
		t.Errorf("topic pages = %d, want 1", topicCount)
	}
	// 5 cites links from topic → raw
	cites := 0
	for _, l := range store.links {
		if l.LinkType == "cites" {
			cites++
		}
	}
	if cites != 5 {
		t.Errorf("cites = %d, want 5", cites)
	}
}

func TestClusterSynthesizer_SkipsBelowMin(t *testing.T) {
	store := &memStore{}
	for i := 0; i < 3; i++ {
		store.pages = append(store.pages, db.WikiPage{
			ID: idOf(i), Title: "raw " + idOf(i), PageType: "raw", Status: "published", Tags: []string{"ml"},
		})
	}
	cs := NewClusterSynthesizer(store, &canned{body: "x"}, ClusterConfig{MinSources: 5})
	res, err := cs.SynthesizeClusters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.TopicsCreated != 0 {
		t.Errorf("TopicsCreated = %d, want 0", res.TopicsCreated)
	}
	if res.TopicsSkipped != 1 {
		t.Errorf("TopicsSkipped = %d, want 1", res.TopicsSkipped)
	}
}

func TestClusterSynthesizer_SkipsExistingTopic(t *testing.T) {
	store := &memStore{}
	for i := 0; i < 5; i++ {
		store.pages = append(store.pages, db.WikiPage{
			ID: idOf(i), Title: "raw " + idOf(i), PageType: "raw", Status: "published", Tags: []string{"ml"},
		})
	}
	store.pages = append(store.pages, db.WikiPage{
		ID: "topic-ml", PageType: "topic", Tags: []string{"ml"}, Status: "published",
	})
	cs := NewClusterSynthesizer(store, &canned{body: "x"}, ClusterConfig{MinSources: 5})
	res, err := cs.SynthesizeClusters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.TopicsCreated != 0 || res.TopicsRegenerated != 0 {
		t.Errorf("unexpected creation: %+v", res)
	}
}

func TestGroupByTag(t *testing.T) {
	pages := []db.WikiPage{
		{ID: "a", Tags: []string{"x", "y"}},
		{ID: "b", Tags: []string{"x"}},
		{ID: "c", Tags: []string{"y"}},
	}
	g := groupByTag(pages)
	if len(g["x"]) != 2 {
		t.Errorf("x bucket = %d, want 2", len(g["x"]))
	}
	if len(g["y"]) != 2 {
		t.Errorf("y bucket = %d, want 2", len(g["y"]))
	}
}

func idOf(i int) string {
	return string(rune('a' + i))
}
