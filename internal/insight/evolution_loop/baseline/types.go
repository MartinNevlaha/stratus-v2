package baseline

import "time"

// Bundle holds grounded project context gathered before an evolution cycle.
// Each source is independently resilient — failure in one source is logged and
// skipped; only invalid args cause Build to return an error.
type Bundle struct {
	ProjectRoot    string          // absolute path
	VexorHits      []VexorHit      // top-K code snippets from Vexor
	GitCommits     []GitCommit     // last N commits within 30 days
	FileTree       FileTreeNode    // 2-level directory tree + language stats
	TODOs          []TODOItem      // capped at BaselineLimits.TODOMax
	WikiTitles     []WikiTitle     // title + staleness, sorted by staleness DESC
	GovernanceRefs []GovernanceRef // active rules / ADRs from the docs table
	TestRatios     []TestRatio     // per top-level directory
	GeneratedAt    time.Time
}

// VexorHit is a single semantic code search result.
type VexorHit struct {
	Path    string
	Snippet string
	Score   float64
}

// GitCommit is a parsed entry from git log.
type GitCommit struct {
	Hash    string
	Subject string
	Files   []string
	At      time.Time
}

// FileTreeNode is one node in the 2-level file tree.
// Kind is "dir" or "file".
type FileTreeNode struct {
	Name          string
	Kind          string
	Children      []FileTreeNode
	LanguageStats map[string]int // extension → file count (populated on dir nodes)
}

// TODOItem represents a single TODO/FIXME/XXX/HACK comment found in source.
type TODOItem struct {
	Path string
	Line int
	Text string
	Kind string // TODO | FIXME | XXX | HACK
}

// WikiTitle is a lightweight reference to a wiki page.
type WikiTitle struct {
	ID        string
	Title     string
	Staleness float64
}

// GovernanceRef is a lightweight reference to a governance document (rule or ADR).
type GovernanceRef struct {
	ID    string
	Title string
	Kind  string // "rule" | "adr" | "project"
}

// TestRatio summarises test coverage by top-level directory.
type TestRatio struct {
	Dir         string
	SourceFiles int
	TestFiles   int
	Ratio       float64 // TestFiles / SourceFiles; 0 when SourceFiles == 0
}
