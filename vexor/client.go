package vexor

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Result is a single Vexor search result.
type Result struct {
	Rank       int     `json:"rank"`
	Score      float64 `json:"score"`
	FilePath   string  `json:"file_path"`
	ChunkIndex int     `json:"chunk_index"`
	LineStart  int     `json:"line_start"`
	LineEnd    int     `json:"line_end"`
	Heading    string  `json:"heading"`
	Excerpt    string  `json:"excerpt"`
}

// Client wraps the vexor binary.
type Client struct {
	binaryPath string
	model      string
	timeout    time.Duration
}

// New creates a new Vexor client.
func New(binaryPath, model string, timeoutSec int) *Client {
	if binaryPath == "" {
		binaryPath = "vexor"
	}
	if model == "" {
		model = "nomic-embed-text-v1.5"
	}
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	return &Client{
		binaryPath: binaryPath,
		model:      model,
		timeout:    time.Duration(timeoutSec) * time.Second,
	}
}

// Available checks if the vexor binary is in PATH.
func (c *Client) Available() bool {
	_, err := exec.LookPath(c.binaryPath)
	return err == nil
}

// Search runs a semantic search and returns results.
func (c *Client) Search(query string, topK int, mode string) ([]Result, error) {
	if topK <= 0 {
		topK = 10
	}
	if mode == "" {
		mode = "auto"
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.binaryPath,
		"search",
		"--format", "porcelain",
		"--top", strconv.Itoa(topK),
		"--mode", mode,
		query,
	)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("vexor search timeout after %v", c.timeout)
		}
		return nil, fmt.Errorf("vexor search: %w", err)
	}

	return parsePorcelain(string(out)), nil
}

// Index runs a full project reindex. The paths argument is accepted for API
// compatibility but ignored — vexor index does not support incremental
// file-level indexing.
func (c *Client) Index(_ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.binaryPath, "index")
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("vexor index timeout after 120s")
		}
		return fmt.Errorf("vexor index: %w\n%s", err, out)
	}
	return nil
}

// parsePorcelain parses vexor's tab-separated porcelain output.
// Format: rank \t score \t file_path \t chunk_index \t line_start \t line_end \t heading :: excerpt
func parsePorcelain(output string) []Result {
	var results []Result
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		// Split into at most 8 fields (excerpt may contain tabs)
		parts := strings.SplitN(line, "\t", 8)
		if len(parts) < 7 {
			continue
		}
		r := Result{}
		r.Rank, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
		r.Score, _ = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		r.FilePath = strings.TrimSpace(parts[2])
		r.ChunkIndex, _ = strconv.Atoi(strings.TrimSpace(parts[3]))
		r.LineStart, _ = strconv.Atoi(strings.TrimSpace(parts[4]))
		r.LineEnd, _ = strconv.Atoi(strings.TrimSpace(parts[5]))

		// parts[6] = "heading :: excerpt" or just heading if no excerpt
		rest := parts[6]
		if len(parts) == 8 {
			rest = parts[6] + "\t" + parts[7]
		}
		if idx := strings.Index(rest, " :: "); idx != -1 {
			r.Heading = rest[:idx]
			r.Excerpt = rest[idx+4:]
		} else {
			r.Heading = rest
		}
		results = append(results, r)
	}
	return results
}
