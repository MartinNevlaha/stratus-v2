package ingest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// extractURL fetches a URL and returns (title, plainText). HTML is stripped
// of script/style blocks and tags via a regex pipeline — good enough for
// readable articles without pulling in a full parser.
func extractURL(ctx context.Context, rawURL string, timeout time.Duration) (title, text string, err error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("ingest url: build request: %w", err)
	}
	req.Header.Set("User-Agent", "Stratus-Ingest/1.0 (+https://github.com/MartinNevlaha/stratus-v2)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("ingest url: fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("ingest url: fetch %s: HTTP %d", rawURL, resp.StatusCode)
	}

	// Cap at ~4 MiB of body.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", "", fmt.Errorf("ingest url: read body: %w", err)
	}
	html := string(body)
	title = ExtractHTMLTitle(html)
	text = HTMLToText(html)
	return title, text, nil
}

var (
	scriptRe  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	commentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)
	titleRe   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	wsRe      = regexp.MustCompile(`[ \t]+`)
	nlRe      = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText strips HTML down to plain text. Not a true reader-mode implementation
// but sufficient for feeding text to an LLM.
func HTMLToText(html string) string {
	s := scriptRe.ReplaceAllString(html, " ")
	s = styleRe.ReplaceAllString(s, " ")
	s = commentRe.ReplaceAllString(s, " ")
	// Insert newlines at block boundaries so text doesn't collapse.
	s = regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|br|tr|article|section)>`).ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, "")
	s = htmlUnescape(s)
	s = wsRe.ReplaceAllString(s, " ")
	s = nlRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// ExtractHTMLTitle returns the contents of the first <title>...</title>.
func ExtractHTMLTitle(html string) string {
	m := titleRe.FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(htmlUnescape(m[1]))
}

func htmlUnescape(s string) string {
	repl := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&apos;", "'",
		"&nbsp;", " ",
	)
	return repl.Replace(s)
}
