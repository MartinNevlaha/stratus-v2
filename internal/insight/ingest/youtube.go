package ingest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// extractYouTube uses yt-dlp to fetch auto-generated subtitles, then parses
// the VTT file into plain text. Requires yt-dlp on PATH.
func extractYouTube(ctx context.Context, videoURL, binary string) (string, error) {
	if binary == "" {
		binary = "yt-dlp"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return "", fmt.Errorf("%w: %s", ErrExtractorMissing, binary)
	}

	tmpDir, err := os.MkdirTemp("", "stratus-yt-*")
	if err != nil {
		return "", fmt.Errorf("ingest youtube: mkdtemp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	outTpl := filepath.Join(tmpDir, "%(id)s.%(ext)s")
	cmd := exec.CommandContext(ctx, binary,
		"--write-auto-sub",
		"--sub-lang", "en",
		"--skip-download",
		"--sub-format", "vtt",
		"-o", outTpl,
		videoURL,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ingest youtube: %s: %w (%s)", binary, err, string(out))
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", fmt.Errorf("ingest youtube: read tmpdir: %w", err)
	}
	var vttPath string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".vtt") {
			vttPath = filepath.Join(tmpDir, e.Name())
			break
		}
	}
	if vttPath == "" {
		return "", errors.New("ingest youtube: no .vtt produced (video may lack subtitles)")
	}

	f, err := os.Open(vttPath)
	if err != nil {
		return "", fmt.Errorf("ingest youtube: open vtt: %w", err)
	}
	defer f.Close()

	return parseVTT(f)
}

// parseVTT strips VTT timing markers and returns deduplicated cue text.
func parseVTT(r interface{ Read(p []byte) (int, error) }) (string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<22)

	var sb strings.Builder
	var lastLine string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line == "WEBVTT" {
			continue
		}
		// Skip metadata lines.
		if strings.HasPrefix(line, "Kind:") || strings.HasPrefix(line, "Language:") ||
			strings.HasPrefix(line, "NOTE") || strings.HasPrefix(line, "STYLE") {
			continue
		}
		// Skip timing cues ("00:00:01.000 --> 00:00:04.000").
		if strings.Contains(line, "-->") {
			continue
		}
		// Skip cue sequence numbers.
		if _, err := fmtAtoi(line); err == nil {
			continue
		}
		// Strip inline VTT tags like <c> or <00:00:01.000>.
		clean := stripVTTTags(line)
		if clean == "" || clean == lastLine {
			continue
		}
		sb.WriteString(clean)
		sb.WriteString("\n")
		lastLine = clean
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("ingest youtube: scan vtt: %w", err)
	}
	return sb.String(), nil
}

func stripVTTTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// fmtAtoi is a local integer check (avoiding strconv import dance).
func fmtAtoi(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not int")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
