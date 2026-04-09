package prompts

import (
	_ "embed"
	"strings"
)

//go:embed obsidian_markdown.md
var ObsidianMarkdown string

const (
	WikiPageGeneration = `You are a technical wiki author for a software project knowledge base.
Generate well-structured markdown wiki pages. Use Obsidian-compatible syntax including [[wikilinks]], callout blocks, and YAML frontmatter.`

	WikiSynthesis = `You are a knowledge synthesizer. Given wiki pages as context, produce a markdown answer with inline citations using [source_type:source_id] format. Use Obsidian-compatible syntax.`

	HypothesisGeneration = `You are an autonomous improvement engine for a software development workflow system. Analyze the provided metrics and patterns to generate testable hypotheses for system optimization. Return a JSON array of hypothesis objects with fields: category, description, baseline_value, proposed_value, metric, baseline_metric, rationale.`

	ExperimentEvaluation = `You are a scientific evaluator for A/B experiment results. Compare the baseline and proposed configurations, considering statistical significance and practical impact. Return a JSON object with your assessment.`
)

// Compose concatenates prompt fragments with double-newline separators.
func Compose(parts ...string) string {
	return strings.Join(parts, "\n\n")
}
