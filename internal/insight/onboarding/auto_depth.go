package onboarding

import "fmt"

// AutoDepthResult holds the computed depth and max pages for a project.
type AutoDepthResult struct {
	Depth    string `json:"depth"`
	MaxPages int    `json:"max_pages"`
	Reason   string `json:"reason"`
}

// ResolveAutoDepth analyses a ProjectProfile and returns the recommended
// onboarding depth and page budget using the default cap of 200 pages.
// To override the ceiling (e.g. to use the 0=unlimited sentinel from config),
// call ResolveAutoDepthWithCap instead.
func ResolveAutoDepth(profile *ProjectProfile) AutoDepthResult {
	return ResolveAutoDepthWithCap(profile, 200)
}

// ResolveAutoDepthWithCap is like ResolveAutoDepth but accepts an explicit
// maxCap parameter: 0 means unlimited (no ceiling applied), positive values
// act as the maximum page ceiling. The floor of 5 is always preserved.
func ResolveAutoDepthWithCap(profile *ProjectProfile, maxCap int) AutoDepthResult {
	totalFiles := 0
	for _, ls := range profile.Languages {
		totalFiles += ls.FileCount
	}

	languages := len(profile.Languages)
	modules := len(topLevelModules(profile))

	commits := 0
	if profile.GitStats != nil {
		commits = profile.GitStats.CommitCount
	}

	// Score the project on a 0-100 scale.
	score := 0

	// File count contribution (0-30).
	switch {
	case totalFiles >= 500:
		score += 30
	case totalFiles >= 200:
		score += 25
	case totalFiles >= 100:
		score += 20
	case totalFiles >= 50:
		score += 15
	case totalFiles >= 20:
		score += 10
	default:
		score += 5
	}

	// Module count contribution (0-25).
	switch {
	case modules >= 15:
		score += 25
	case modules >= 10:
		score += 20
	case modules >= 6:
		score += 15
	case modules >= 3:
		score += 10
	default:
		score += 5
	}

	// Language diversity contribution (0-20).
	switch {
	case languages >= 5:
		score += 20
	case languages >= 3:
		score += 15
	case languages >= 2:
		score += 10
	default:
		score += 5
	}

	// Commit history contribution (0-15).
	switch {
	case commits >= 500:
		score += 15
	case commits >= 100:
		score += 10
	case commits >= 20:
		score += 5
	}

	// Monorepo / pattern bonus (0-10).
	for _, p := range profile.DetectedPatterns {
		if p == "monorepo" {
			score += 10
			break
		}
	}

	// Map score to depth and max pages.
	switch {
	case score >= 70:
		return AutoDepthResult{
			Depth:    "deep",
			MaxPages: clampPages(modules*4+5, maxCap),
			Reason:   reasonString(totalFiles, modules, languages, commits, score),
		}
	case score >= 40:
		return AutoDepthResult{
			Depth:    "standard",
			MaxPages: clampPages(modules*3+4, maxCap),
			Reason:   reasonString(totalFiles, modules, languages, commits, score),
		}
	default:
		return AutoDepthResult{
			Depth:    "shallow",
			MaxPages: clampPages(modules*2+3, maxCap),
			Reason:   reasonString(totalFiles, modules, languages, commits, score),
		}
	}
}

// clampPages enforces a floor of 5 and an optional ceiling.
// maxCap == 0 means unlimited (no ceiling is applied).
// maxCap > 0 means the returned value will not exceed maxCap.
func clampPages(n, maxCap int) int {
	if n < 5 {
		n = 5
	}
	if maxCap > 0 && n > maxCap {
		return maxCap
	}
	return n
}

func reasonString(files, modules, languages, commits, score int) string {
	return fmt.Sprintf(
		"%d files, %d modules, %d languages, %d commits → score %d",
		files, modules, languages, commits, score,
	)
}
