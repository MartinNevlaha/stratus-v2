package onboarding

import (
	"os"
	"path/filepath"
)

// AssetProposal represents a suggested asset (rule, skill, agent, command) to be
// created in the project.
type AssetProposal struct {
	// Type is one of: "asset.rule" | "asset.skill.cc" | "asset.agent.cc" | "asset.agent.oc" | "asset.command.oc"
	Type string `json:"type"`
	// Title is a human-readable name for the proposed asset.
	Title string `json:"title"`
	// Description explains what the asset does.
	Description string `json:"description"`
	// ProposedPath is the relative path from the project root where the file would be written.
	ProposedPath string `json:"proposed_path"`
	// ProposedContent is the full file content to write.
	ProposedContent string `json:"proposed_content"`
	// Confidence is a score between 0.0 and 1.0 indicating how confident the engine is
	// that this asset is relevant.
	Confidence float64 `json:"confidence"`
	// Target indicates the tooling target: "claude-code" | "opencode"
	Target string `json:"target"`
	// Signals lists which profile signals triggered this proposal.
	Signals []string `json:"signals"`
}

// DeduplicateProposals removes proposals where the target file already exists
// on disk or an equivalent proposal already exists in the DB.
//
// For each proposal:
//  1. If filepath.Join(projectRoot, proposal.ProposedPath) exists on disk, the proposal is skipped.
//  2. If existingPaths[proposal.ProposedPath] is true, the proposal is skipped.
//  3. Otherwise the proposal is kept.
//
// existingPaths may be nil, in which case only the disk check is performed.
func DeduplicateProposals(proposals []AssetProposal, projectRoot string, existingPaths map[string]bool) []AssetProposal {
	result := make([]AssetProposal, 0, len(proposals))

	for _, p := range proposals {
		if existsOnDisk(filepath.Join(projectRoot, p.ProposedPath)) {
			continue
		}
		if existingPaths[p.ProposedPath] {
			continue
		}
		result = append(result, p)
	}

	return result
}

// existsOnDisk returns true when os.Stat succeeds for the given path.
func existsOnDisk(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
