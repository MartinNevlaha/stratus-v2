package baseline

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
	"github.com/MartinNevlaha/stratus-v2/db"
)

// VexorClient is the interface for semantic code search.
type VexorClient interface {
	Search(ctx context.Context, root, query string, topK int) ([]VexorHit, error)
}

// Dependencies holds the optional collaborators for the baseline Builder.
// nil fields cause the corresponding data source to be silently skipped.
type Dependencies struct {
	Vexor       VexorClient // may be nil → skip vexor hits
	DB          *db.DB      // may be nil → skip wiki titles & governance refs
	CommitQuery string      // query forwarded to Vexor, e.g. "recently changed code hotspots"
}

// Builder gathers a Bundle of grounded project context.
type Builder interface {
	Build(ctx context.Context, root string, limits config.BaselineLimits) (Bundle, error)
}

// New returns a Builder backed by the given Dependencies.
func New(deps Dependencies) Builder {
	return &builder{deps: deps}
}

// ---------------------------------------------------------------------------
// builder implementation
// ---------------------------------------------------------------------------

type builder struct {
	deps Dependencies
}

// Build gathers all baseline sources and returns a Bundle.
// Individual sources are resilient: errors are logged and skipped.
// The only error path is when root is empty.
func (bld *builder) Build(ctx context.Context, root string, limits config.BaselineLimits) (Bundle, error) {
	if root == "" {
		return Bundle{}, fmt.Errorf("baseline: build: root must not be empty")
	}

	bundle := Bundle{
		ProjectRoot: root,
		GeneratedAt: time.Now().UTC(),
	}

	// --- Vexor hits ---
	if bld.deps.Vexor != nil {
		hits, err := bld.deps.Vexor.Search(ctx, root, bld.deps.CommitQuery, limits.VexorTopK)
		if err != nil {
			log.Printf("baseline: vexor search failed (skipping): %v", err)
		} else {
			bundle.VexorHits = hits
		}
	}

	// --- Git log ---
	bundle.GitCommits = bld.gatherGitCommits(ctx, root, limits.GitLogCommits)

	// --- File tree ---
	bundle.FileTree = bld.gatherFileTree(root)

	// --- TODOs ---
	bundle.TODOs = bld.gatherTODOs(root, limits.TODOMax)

	// --- Wiki titles ---
	if bld.deps.DB != nil {
		bundle.WikiTitles = bld.gatherWikiTitles(bld.deps.DB)
	}

	// --- Governance refs ---
	if bld.deps.DB != nil {
		bundle.GovernanceRefs = bld.gatherGovernanceRefs(bld.deps.DB)
	}

	// --- Test ratios ---
	bundle.TestRatios = bld.gatherTestRatios(root)

	return bundle, nil
}

// ---------------------------------------------------------------------------
// Git log
// ---------------------------------------------------------------------------

// gitLogSepRE matches the custom format line: HASH\tSUBJECT\tUNIX_TS
var gitLogSepRE = regexp.MustCompile(`^([0-9a-f]{7,40})\t(.+)\t(\d+)$`)

func (bld *builder) gatherGitCommits(_ context.Context, root string, maxN int) []GitCommit {
	if maxN <= 0 {
		maxN = 200
	}
	args := []string{
		"-C", root,
		"log",
		"--since=30 days ago",
		"--name-only",
		fmt.Sprintf("--pretty=format:%%H\t%%s\t%%ct"),
		fmt.Sprintf("-n%d", maxN),
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("baseline: git log failed (skipping): %v", err)
		return nil
	}

	var commits []GitCommit
	var current *GitCommit

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// blank separator between commit blocks
			continue
		}
		if m := gitLogSepRE.FindStringSubmatch(line); m != nil {
			if current != nil {
				commits = append(commits, *current)
				if len(commits) >= maxN {
					break
				}
			}
			ts, _ := strconv.ParseInt(m[3], 10, 64)
			current = &GitCommit{
				Hash:    m[1],
				Subject: m[2],
				At:      time.Unix(ts, 0).UTC(),
			}
		} else if current != nil && strings.TrimSpace(line) != "" {
			current.Files = append(current.Files, strings.TrimSpace(line))
		}
	}
	if current != nil && len(commits) < maxN {
		commits = append(commits, *current)
	}
	return commits
}

// ---------------------------------------------------------------------------
// File tree (2 levels)
// ---------------------------------------------------------------------------

// skipDirs are directory names always excluded from the file tree scan.
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
}

// extToLang maps a handful of relevant extensions to language names.
var extToLang = map[string]string{
	".go":     "Go",
	".py":     "Python",
	".ts":     "TypeScript",
	".tsx":    "TypeScript",
	".js":     "JavaScript",
	".jsx":    "JavaScript",
	".svelte": "Svelte",
	".md":     "Markdown",
}

func (bld *builder) gatherFileTree(root string) FileTreeNode {
	node := FileTreeNode{
		Name:          filepath.Base(root),
		Kind:          "dir",
		LanguageStats: map[string]int{},
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		log.Printf("baseline: read dir %s (skipping): %v", root, err)
		return node
	}

	for _, entry := range entries {
		if skipDirs[entry.Name()] {
			continue
		}
		child := FileTreeNode{Name: entry.Name()}
		if entry.IsDir() {
			child.Kind = "dir"
			child.Children, child.LanguageStats = readDirOneLevel(filepath.Join(root, entry.Name()))
		} else {
			child.Kind = "file"
			if lang, ok := extToLang[filepath.Ext(entry.Name())]; ok {
				node.LanguageStats[lang]++
			}
		}
		// Merge child lang stats into root.
		for lang, cnt := range child.LanguageStats {
			node.LanguageStats[lang] += cnt
		}
		node.Children = append(node.Children, child)
	}
	return node
}

// readDirOneLevel reads entries one level deep (no recursion) and returns
// child nodes + aggregate language stats.
func readDirOneLevel(dir string) ([]FileTreeNode, map[string]int) {
	stats := map[string]int{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, stats
	}
	var children []FileTreeNode
	for _, entry := range entries {
		if skipDirs[entry.Name()] {
			continue
		}
		child := FileTreeNode{Name: entry.Name()}
		if entry.IsDir() {
			child.Kind = "dir"
		} else {
			child.Kind = "file"
			if lang, ok := extToLang[filepath.Ext(entry.Name())]; ok {
				stats[lang]++
			}
		}
		children = append(children, child)
	}
	return children, stats
}

// ---------------------------------------------------------------------------
// TODOs
// ---------------------------------------------------------------------------

var todoExtensions = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".tsx": true,
	".js": true, ".jsx": true, ".svelte": true, ".java": true,
	".rb": true, ".rs": true, ".c": true, ".cpp": true, ".h": true,
}

var todoRE = regexp.MustCompile(`(?i)\b(TODO|FIXME|XXX|HACK)\b[: ](.*)`)

func (bld *builder) gatherTODOs(root string, maxN int) []TODOItem {
	if maxN <= 0 {
		maxN = 50
	}
	var todos []TODOItem

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !todoExtensions[filepath.Ext(path)] {
			return nil
		}
		if len(todos) >= maxN {
			return nil
		}

		items := scanFileForTODOs(path, maxN-len(todos))
		todos = append(todos, items...)
		return nil
	})

	return todos
}

func scanFileForTODOs(path string, remaining int) []TODOItem {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var items []TODOItem
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		m := todoRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		items = append(items, TODOItem{
			Path: path,
			Line: lineNum,
			Text: strings.TrimSpace(m[0]),
			Kind: strings.ToUpper(m[1]),
		})
		if len(items) >= remaining {
			break
		}
	}
	return items
}

// ---------------------------------------------------------------------------
// Wiki titles
// ---------------------------------------------------------------------------

func (bld *builder) gatherWikiTitles(database *db.DB) []WikiTitle {
	rows, err := database.SQL().Query(
		`SELECT id, title, staleness_score FROM wiki_pages ORDER BY staleness_score DESC LIMIT 50`,
	)
	if err != nil {
		log.Printf("baseline: wiki_pages query failed (skipping): %v", err)
		return nil
	}
	defer rows.Close()

	var titles []WikiTitle
	for rows.Next() {
		var wt WikiTitle
		if err := rows.Scan(&wt.ID, &wt.Title, &wt.Staleness); err != nil {
			log.Printf("baseline: scan wiki_page row: %v", err)
			continue
		}
		titles = append(titles, wt)
	}
	if err := rows.Err(); err != nil {
		log.Printf("baseline: wiki_pages rows error: %v", err)
	}
	return titles
}

// ---------------------------------------------------------------------------
// Governance refs
// ---------------------------------------------------------------------------

func (bld *builder) gatherGovernanceRefs(database *db.DB) []GovernanceRef {
	// The docs table stores governance documents with doc_type values like
	// 'rule', 'adr', 'project'. We return the distinct (file_path, title,
	// doc_type) per file (chunk_index=0 for dedup).
	rows, err := database.SQL().Query(`
		SELECT id, title, doc_type
		FROM docs
		WHERE doc_type IN ('rule', 'adr', 'project')
		  AND chunk_index = 0
		ORDER BY doc_type, title
		LIMIT 200
	`)
	if err != nil {
		log.Printf("baseline: docs query failed (skipping): %v", err)
		return nil
	}
	defer rows.Close()

	var refs []GovernanceRef
	for rows.Next() {
		var ref GovernanceRef
		if err := rows.Scan(&ref.ID, &ref.Title, &ref.Kind); err != nil {
			log.Printf("baseline: scan docs row: %v", err)
			continue
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		log.Printf("baseline: docs rows error: %v", err)
	}
	return refs
}

// ---------------------------------------------------------------------------
// Test ratios
// ---------------------------------------------------------------------------

// testSuffixes are filename patterns that identify test files.
var testSuffixes = []string{
	"_test.go",
	".test.ts", ".test.tsx", ".test.js",
	".spec.ts", ".spec.tsx", ".spec.js",
	"_test.py",
}

// testPrefixes are filename prefixes that identify test files (Python style).
var testPrefixes = []string{"test_"}

func isTestFile(name string) bool {
	lower := strings.ToLower(name)
	for _, suf := range testSuffixes {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	for _, pre := range testPrefixes {
		if strings.HasPrefix(lower, pre) {
			return true
		}
	}
	return false
}

var sourceExtensions = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".tsx": true,
	".js": true, ".jsx": true, ".svelte": true, ".java": true,
	".rb": true, ".rs": true, ".c": true, ".cpp": true, ".h": true,
}

func (bld *builder) gatherTestRatios(root string) []TestRatio {
	// Collect top-level directories only.
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Printf("baseline: read dir for test ratios (skipping): %v", err)
		return nil
	}

	var ratios []TestRatio
	for _, entry := range entries {
		if !entry.IsDir() || skipDirs[entry.Name()] {
			continue
		}
		dirPath := filepath.Join(root, entry.Name())
		src, test := countSourceAndTestFiles(dirPath)
		ratio := 0.0
		if src > 0 {
			ratio = float64(test) / float64(src)
		}
		ratios = append(ratios, TestRatio{
			Dir:         entry.Name(),
			SourceFiles: src,
			TestFiles:   test,
			Ratio:       ratio,
		})
	}
	return ratios
}

func countSourceAndTestFiles(dir string) (src, test int) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		ext := filepath.Ext(name)
		if !sourceExtensions[ext] {
			return nil
		}
		if isTestFile(name) {
			test++
		} else {
			src++
		}
		return nil
	})
	return
}
