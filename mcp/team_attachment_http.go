package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Tokens belong to one immutable per-chat client, never global environment.
type attachmentTransport struct {
	base          http.RoundTripper
	token, origin string
}

func (t attachmentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	origin, err := url.Parse(t.origin)
	if err != nil || req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host {
		return nil, fmt.Errorf("attachment request cannot leave its project server")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("X-Stratus-Team-Token", t.token)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func attachmentHTTP(ctx context.Context, client *http.Client, base, token, method, path string, body, result any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Stratus-Team-Token", token)
	}
	// Private capabilities must never follow redirects to a different service.
	private := *client
	private.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := private.Do(req)
	if err != nil {
		return fmt.Errorf("attachment transport failed: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("attachment response was incomplete: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		if len(failure.Error) > 1000 {
			failure.Error = failure.Error[:1000]
		}
		if token != "" {
			failure.Error = strings.ReplaceAll(failure.Error, token, "[private]")
		}
		return fmt.Errorf("attachment request rejected (HTTP %d): %s", response.StatusCode, failure.Error)
	}
	if result == nil {
		return nil
	}
	if err = json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("attachment returned invalid response")
	}
	return nil
}
