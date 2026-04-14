package ingest

import (
	"net/url"
	"path/filepath"
	"strings"
)

// Kind identifies the detected source kind of an ingest target.
type Kind string

const (
	KindPDF      Kind = "pdf"
	KindYouTube  Kind = "youtube"
	KindURL      Kind = "url"
	KindMarkdown Kind = "markdown"
	KindText     Kind = "text"
	KindUnknown  Kind = "unknown"
)

// DetectKind classifies a source string as a file path or URL and infers its
// content kind. Detection is purely syntactic — it does not open or fetch the
// resource.
func DetectKind(source string) Kind {
	s := strings.TrimSpace(source)
	if s == "" {
		return KindUnknown
	}

	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return KindURL
		}
		host := strings.ToLower(u.Host)
		if host == "youtube.com" || host == "www.youtube.com" ||
			host == "m.youtube.com" || host == "youtu.be" {
			return KindYouTube
		}
		return KindURL
	}

	ext := strings.ToLower(filepath.Ext(s))
	switch ext {
	case ".pdf":
		return KindPDF
	case ".md", ".markdown":
		return KindMarkdown
	case ".txt", ".text", "":
		return KindText
	}
	return KindText
}
