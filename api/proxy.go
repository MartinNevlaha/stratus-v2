package api

import (
	"io"
	"net/http"

	"github.com/MartinNevlaha/stratus-v2/db"
)

// newProxyRequest creates an outbound request from an incoming one.
func newProxyRequest(r *http.Request, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, r.Body)
	if err != nil {
		return nil, err
	}
	// Copy relevant headers
	for _, h := range []string{"Content-Type", "Authorization"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	return req, nil
}

// copyBody copies from src to dst.
func copyBody(dst io.Writer, src io.Reader) {
	_, _ = io.Copy(dst, src)
}

// nilSlice helpers to avoid null in JSON responses.
func nilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func searchEventsDefaults() db.SearchEventsInput {
	return db.SearchEventsInput{Limit: 10}
}
