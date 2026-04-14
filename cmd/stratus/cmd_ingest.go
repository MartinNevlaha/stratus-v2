package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/config"
)

// cmdIngest implements `stratus ingest <path-or-url> [flags]`. It POSTs to the
// running Stratus API server at /api/ingest.
func cmdIngest() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: stratus ingest <path-or-url> [--tags a,b,c] [--title \"...\"] [--no-synth] [--skip-links]")
		os.Exit(2)
	}
	source := os.Args[2]

	var (
		tags     []string
		title    string
		autoSyn  = true
		skipLink bool
	)
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--tags":
			if i+1 >= len(os.Args) {
				fmt.Fprintln(os.Stderr, "--tags requires a comma-separated value")
				os.Exit(2)
			}
			i++
			for _, t := range strings.Split(os.Args[i], ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
		case "--title":
			if i+1 >= len(os.Args) {
				fmt.Fprintln(os.Stderr, "--title requires a value")
				os.Exit(2)
			}
			i++
			title = os.Args[i]
		case "--no-synth":
			autoSyn = false
		case "--skip-links":
			skipLink = true
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", os.Args[i])
			os.Exit(2)
		}
	}

	body := map[string]any{
		"source":            source,
		"tags":              tags,
		"title":             title,
		"auto_synthesize":   autoSyn,
		"skip_link_suggest": skipLink,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}

	cfg := config.Load()
	port := cfg.Port
	if port == 0 {
		port = 41777
	}
	url := fmt.Sprintf("http://localhost:%d/api/ingest", port)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "POST %s: %v\n(is `stratus serve` running?)\n", url, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "ingest failed: HTTP %d\n%s\n", resp.StatusCode, string(respBody))
		os.Exit(1)
	}

	// Pretty-print the response.
	var pretty bytes.Buffer
	if json.Indent(&pretty, respBody, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(respBody))
	}
}
