package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/db"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/prompts"
	"github.com/MartinNevlaha/stratus-v2/internal/insight/wiki_engine"
	"github.com/google/uuid"
)

// OnboardingOpts configures a RunOnboarding call.
type OnboardingOpts struct {
	// Depth controls how many pages are planned: "shallow" | "standard" | "deep"
	Depth string
	// MaxPages is the upper bound on module pages added beyond the core set.
	// Zero means use a sensible default (10).
	MaxPages int
	// IngestTokenBudget is the cumulative LLM token budget for the entire run.
	// When cumulative tokens exceed this value after a page is written, no
	// further pages are generated and a budget-exceeded warning is appended to
	// the result's Errors slice. The partial result (pages written so far) is
	// returned without an error — callers can check result.Errors for the warning.
	// Zero means unlimited (no budget guard applied).
	IngestTokenBudget int
	// OutputDir, if non-empty, causes each page to be written as <slug>.md.
	OutputDir string
	// ProgressFn is called with status updates. Nil-safe.
	ProgressFn func(OnboardingProgress)
	// SaveAssetProposals is called after page generation with proposed assets.
	// Nil-safe — if nil, asset proposals are skipped.
	SaveAssetProposals func([]AssetProposal) error
}

// OnboardingResult summarises the outcome of a RunOnboarding call.
type OnboardingResult struct {
	PagesGenerated int           `json:"pages_generated"`
	PagesFailed    int           `json:"pages_failed"`
	PagesSkipped   int           `json:"pages_skipped"`
	LinksCreated   int           `json:"links_created"`
	VaultSynced    bool          `json:"vault_synced"`
	OutputDir      string        `json:"output_dir,omitempty"`
	Duration       time.Duration `json:"duration"`
	TokensUsed     int           `json:"tokens_used"`
	Errors         []string      `json:"errors"`
	PageIDs        []string      `json:"page_ids"`
	AssetProposals int           `json:"asset_proposals"`
}

// OnboardingProgress is delivered to OnboardingOpts.ProgressFn.
type OnboardingProgress struct {
	JobID       string   `json:"job_id"`
	Status      string   `json:"status"`       // scanning|generating|linking|syncing|complete|failed|idle
	CurrentPage string   `json:"current_page"`
	Generated   int      `json:"generated"`
	Total       int      `json:"total"`
	Errors      []string `json:"errors"`
}

// pageSpec describes a single wiki page to be generated.
type pageSpec struct {
	title        string
	pageType     string
	systemPrompt string
	sourceData   string
}

// RunOnboarding generates a set of onboarding wiki pages for an existing project.
//
// It requires a non-nil llmClient and a non-nil profile. All other dependencies
// (linker, vaultSync) are optional. The function is idempotent: pages already
// published with the "onboarding" tag are skipped.
func RunOnboarding(
	ctx context.Context,
	store wiki_engine.WikiStore,
	llmClient wiki_engine.LLMClient,
	linker *wiki_engine.Linker,
	vaultSync *wiki_engine.VaultSync,
	profile *ProjectProfile,
	opts OnboardingOpts,
) (*OnboardingResult, error) {
	if llmClient == nil {
		return nil, fmt.Errorf("run onboarding: llmClient must not be nil")
	}
	if profile == nil {
		return nil, fmt.Errorf("run onboarding: profile must not be nil")
	}

	start := time.Now()
	jobID := uuid.NewString()

	result := &OnboardingResult{
		OutputDir: opts.OutputDir,
	}

	maxPages := opts.MaxPages
	if maxPages <= 0 {
		maxPages = 10
	}

	// Build the page plan.
	specs, err := planPages(profile, opts.Depth, maxPages)
	if err != nil {
		return nil, fmt.Errorf("run onboarding: plan pages: %w", err)
	}

	total := len(specs)
	reportProgress(opts, OnboardingProgress{
		JobID:  jobID,
		Status: "scanning",
		Total:  total,
	})

	// Load existing onboarding pages for idempotency.
	existing, _, err := store.ListPages(db.WikiPageFilters{Tag: "onboarding"})
	if err != nil {
		slog.Warn("run onboarding: list existing pages", "err", err)
		existing = nil
	}
	existingByTitle := make(map[string]db.WikiPage, len(existing))
	for _, pg := range existing {
		existingByTitle[pg.Title] = pg
	}

	// Prepare output directory if requested.
	if opts.OutputDir != "" {
		if mkErr := os.MkdirAll(opts.OutputDir, 0o755); mkErr != nil {
			slog.Warn("run onboarding: create output dir", "dir", opts.OutputDir, "err", mkErr)
		}
	}

	var generatedPages []db.WikiPage

	for _, spec := range specs {
		// Respect context cancellation between pages.
		if ctx.Err() != nil {
			break
		}

		reportProgress(opts, OnboardingProgress{
			JobID:       jobID,
			Status:      "generating",
			CurrentPage: spec.title,
			Generated:   result.PagesGenerated,
			Total:       total,
			Errors:      result.Errors,
		})

		// Idempotency: skip published pages; delete stale ones so they regenerate.
		if existing, found := existingByTitle[spec.title]; found {
			switch existing.Status {
			case "published":
				slog.Debug("run onboarding: skip existing published page", "title", spec.title)
				result.PagesSkipped++
				generatedPages = append(generatedPages, existing)
				continue
			case "stale":
				if delErr := store.DeletePage(existing.ID); delErr != nil {
					slog.Warn("run onboarding: delete stale page", "id", existing.ID, "err", delErr)
				}
			}
		}

		page, tokensUsed, genErr := generatePage(ctx, llmClient, spec)
		if genErr != nil {
			slog.Warn("run onboarding: generate page", "title", spec.title, "err", genErr)
			result.PagesFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", spec.title, genErr))
			continue
		}

		if saveErr := store.SavePage(page); saveErr != nil {
			slog.Warn("run onboarding: save page", "title", spec.title, "err", saveErr)
			result.PagesFailed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: save: %v", spec.title, saveErr))
			continue
		}

		result.PagesGenerated++
		result.TokensUsed += tokensUsed
		result.PageIDs = append(result.PageIDs, page.ID)
		generatedPages = append(generatedPages, *page)

		// Write standalone output file.
		if opts.OutputDir != "" {
			writeStandaloneFile(opts.OutputDir, page)
		}

		// Token-budget guard: abort further generation if budget exceeded.
		// 0 means unlimited — no guard applied.
		if opts.IngestTokenBudget > 0 && result.TokensUsed > opts.IngestTokenBudget {
			slog.Warn("run onboarding: token budget exceeded, stopping early",
				"budget", opts.IngestTokenBudget,
				"tokens_used", result.TokensUsed,
				"pages_generated", result.PagesGenerated,
			)
			result.Errors = append(result.Errors,
				fmt.Sprintf("token budget exceeded: used %d tokens (budget %d); partial result with %d pages",
					result.TokensUsed, opts.IngestTokenBudget, result.PagesGenerated),
			)
			break
		}
	}

	// Asset proposals (deterministic, no LLM).
	if opts.SaveAssetProposals != nil {
		assetProposals := GenerateAssetProposals(profile, profile.RootPath, nil)
		if len(assetProposals) > 0 {
			if saveErr := opts.SaveAssetProposals(assetProposals); saveErr != nil {
				slog.Warn("run onboarding: save asset proposals", "err", saveErr)
			} else {
				result.AssetProposals = len(assetProposals)
			}
		}
	}

	// Cross-reference linking.
	if linker != nil && len(generatedPages) > 0 {
		reportProgress(opts, OnboardingProgress{
			JobID:     jobID,
			Status:    "linking",
			Generated: result.PagesGenerated,
			Total:     total,
			Errors:    result.Errors,
		})

		for i := range generatedPages {
			links := linker.DetectCrossReferences(&generatedPages[i], generatedPages)
			if len(links) > 0 {
				saved, linkErr := linker.SaveDetectedLinks(links)
				if linkErr != nil {
					slog.Warn("run onboarding: save links", "page", generatedPages[i].Title, "err", linkErr)
				}
				result.LinksCreated += saved
			}
		}
	}

	// Vault sync.
	if vaultSync != nil {
		reportProgress(opts, OnboardingProgress{
			JobID:     jobID,
			Status:    "syncing",
			Generated: result.PagesGenerated,
			Total:     total,
			Errors:    result.Errors,
		})

		if _, syncErr := vaultSync.SyncAll(ctx); syncErr != nil {
			slog.Warn("run onboarding: vault sync", "err", syncErr)
		} else {
			result.VaultSynced = true
		}
	}

	result.Duration = time.Since(start)

	reportProgress(opts, OnboardingProgress{
		JobID:     jobID,
		Status:    "complete",
		Generated: result.PagesGenerated,
		Total:     total,
		Errors:    result.Errors,
	})

	return result, nil
}

// generatePage calls the LLM with the given spec and returns a db.WikiPage ready to save.
func generatePage(ctx context.Context, client wiki_engine.LLMClient, spec pageSpec) (*db.WikiPage, int, error) {
	req := llm.CompletionRequest{
		SystemPrompt: spec.systemPrompt,
		Messages: []llm.Message{
			{Role: "user", Content: spec.sourceData},
		},
		MaxTokens:   2048,
		Temperature: 0.4,
	}

	resp, err := client.Complete(ctx, req)
	if err != nil {
		return nil, 0, fmt.Errorf("generate page %q: llm complete: %w", spec.title, err)
	}

	page := &db.WikiPage{
		ID:          uuid.NewString(),
		PageType:    spec.pageType,
		Title:       spec.title,
		Content:     resp.Content,
		Status:      "published",
		GeneratedBy: "onboarding",
		Tags:        []string{"onboarding"},
		Version:     1,
	}

	return page, resp.InputTokens + resp.OutputTokens, nil
}

// planPages returns the ordered list of page specs for the given depth.
func planPages(profile *ProjectProfile, depth string, maxPages int) ([]pageSpec, error) {
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("marshal profile: %w", err)
	}
	profileStr := string(profileJSON)

	// Core pages always present: arch overview, conventions, build guide.
	// Order: arch overview first, then modules, then conventions, then build guide.
	var specs []pageSpec

	// 1. Architecture overview (always first).
	specs = append(specs, pageSpec{
		title:        "Architecture Overview",
		pageType:     "summary",
		systemPrompt: prompts.Compose(prompts.OnboardingArchitecture, prompts.ObsidianMarkdown),
		sourceData:   profileStr,
	})

	// 2. Module pages for standard/deep.
	if depth == "standard" || depth == "deep" {
		modules := topLevelModules(profile)
		added := 0
		// Reserve space for conventions + build guide (2 pages) + deps for deep (1).
		reserved := 2
		if depth == "deep" {
			reserved = 3
		}
		pageLimit := maxPages - 1 - reserved // -1 for arch overview already added
		if pageLimit < 0 {
			pageLimit = 0
		}

		for _, mod := range modules {
			if added >= pageLimit {
				break
			}
			modData, modErr := moduleSourceData(profile, mod)
			if modErr != nil {
				slog.Warn("run onboarding: module source data", "module", mod, "err", modErr)
			}
			specs = append(specs, pageSpec{
				title:        fmt.Sprintf("%s Module", titleCase(mod)),
				pageType:     "entity",
				systemPrompt: prompts.Compose(prompts.OnboardingModule, prompts.ObsidianMarkdown),
				sourceData:   modData,
			})
			added++
		}
	}

	// 3. Dependencies page (deep only).
	if depth == "deep" {
		depData := dependencySourceData(profile)
		specs = append(specs, pageSpec{
			title:        "Dependencies",
			pageType:     "summary",
			systemPrompt: prompts.Compose(prompts.OnboardingArchitecture, prompts.ObsidianMarkdown),
			sourceData:   depData,
		})
	}

	// 4. Conventions (always).
	specs = append(specs, pageSpec{
		title:        "Project Conventions",
		pageType:     "concept",
		systemPrompt: prompts.Compose(prompts.OnboardingConventions, prompts.ObsidianMarkdown),
		sourceData:   profileStr,
	})

	// 5. Build guide (always).
	specs = append(specs, pageSpec{
		title:        "Build Guide",
		pageType:     "summary",
		systemPrompt: prompts.Compose(prompts.OnboardingBuildGuide, prompts.ObsidianMarkdown),
		sourceData:   profileStr,
	})

	return specs, nil
}

// topLevelModules returns top-level directory names that likely contain source files,
// inferred from the profile's directory tree and language stats.
func topLevelModules(profile *ProjectProfile) []string {
	seen := make(map[string]bool)
	var modules []string

	// Parse directory tree lines to find top-level dirs.
	for _, line := range strings.Split(profile.DirectoryTree, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Top-level entries have no leading spaces.
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}
		// Strip trailing slash if present.
		name := strings.TrimSuffix(trimmed, "/")
		if name == "" || scanSkipDirs[name] {
			continue
		}
		// Only include dirs (no extension = likely a dir in tree output).
		if filepath.Ext(name) != "" {
			continue
		}
		if !seen[name] {
			seen[name] = true
			modules = append(modules, name)
		}
	}

	return modules
}

// moduleSourceData returns a JSON string describing a single module for the LLM prompt.
func moduleSourceData(profile *ProjectProfile, modName string) (string, error) {
	type moduleInfo struct {
		Module      string         `json:"module"`
		ProjectName string         `json:"project_name"`
		Languages   []LanguageStat `json:"languages"`
		EntryPoints []EntryPoint   `json:"entry_points"`
		ConfigFiles []ConfigFile   `json:"relevant_configs"`
	}

	var relevantEntries []EntryPoint
	for _, ep := range profile.EntryPoints {
		if strings.HasPrefix(ep.Path, modName+"/") || strings.HasPrefix(ep.Path, modName+string(filepath.Separator)) {
			relevantEntries = append(relevantEntries, ep)
		}
	}

	var relevantConfigs []ConfigFile
	for _, cf := range profile.ConfigFiles {
		if strings.HasPrefix(cf.Path, modName+"/") {
			relevantConfigs = append(relevantConfigs, cf)
		}
	}

	info := moduleInfo{
		Module:      modName,
		ProjectName: profile.ProjectName,
		Languages:   profile.Languages,
		EntryPoints: relevantEntries,
		ConfigFiles: relevantConfigs,
	}

	data, err := json.Marshal(info)
	if err != nil {
		return "{}", fmt.Errorf("marshal module info: %w", err)
	}
	return string(data), nil
}

// dependencySourceData builds source data for the dependencies page.
func dependencySourceData(profile *ProjectProfile) string {
	type depInfo struct {
		ProjectName string       `json:"project_name"`
		ConfigFiles []ConfigFile `json:"config_files"`
	}
	info := depInfo{
		ProjectName: profile.ProjectName,
		ConfigFiles: profile.ConfigFiles,
	}
	data, _ := json.Marshal(info)
	return string(data)
}

// writeStandaloneFile writes page content to <outputDir>/<slug>.md.
func writeStandaloneFile(outputDir string, page *db.WikiPage) {
	slug := pageSlug(page.Title)
	filePath := filepath.Join(outputDir, slug+".md")
	if err := os.WriteFile(filePath, []byte(page.Content), 0o644); err != nil {
		slog.Warn("run onboarding: write standalone file", "path", filePath, "err", err)
	}
}

// pageSlug converts a page title into a filesystem-safe slug.
func pageSlug(title string) string {
	lower := strings.ToLower(title)
	replaced := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, lower)
	// Collapse repeated dashes.
	for strings.Contains(replaced, "--") {
		replaced = strings.ReplaceAll(replaced, "--", "-")
	}
	return strings.Trim(replaced, "-")
}

// titleCase capitalises the first letter of a string.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// reportProgress calls opts.ProgressFn if it is non-nil.
func reportProgress(opts OnboardingOpts, p OnboardingProgress) {
	if opts.ProgressFn != nil {
		opts.ProgressFn(p)
	}
}
