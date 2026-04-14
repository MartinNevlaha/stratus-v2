package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// ErrExtractorMissing indicates the external binary required for extraction
// is not available on PATH.
var ErrExtractorMissing = errors.New("ingest: external extractor binary not found")

// extractPDF shells out to pdftotext to produce plain text. The binary path
// can be overridden via config (default "pdftotext"). Returns ErrExtractorMissing
// wrapped when the binary is not on PATH.
func extractPDF(ctx context.Context, path, binary string) (string, error) {
	if binary == "" {
		binary = "pdftotext"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return "", fmt.Errorf("%w: %s", ErrExtractorMissing, binary)
	}
	// -layout preserves reading order; "-" writes to stdout.
	cmd := exec.CommandContext(ctx, binary, "-layout", "-enc", "UTF-8", path, "-")
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ingest pdf: %s %s: %w (%s)", binary, path, err, errBuf.String())
	}
	return out.String(), nil
}
