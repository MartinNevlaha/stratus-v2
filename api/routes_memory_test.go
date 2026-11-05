package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MartinNevlaha/stratus-v2/db"
)

func searchEventsReq(t *testing.T, s *Server, query string) []map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/events/search?"+query, nil)
	w := httptest.NewRecorder()
	s.handleSearchEvents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d %s", w.Code, w.Body.String())
	}
	var out struct {
		Results []map[string]any `json:"results"`
		Count   int              `json:"count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Count != len(out.Results) {
		t.Fatalf("count=%d results=%d", out.Count, len(out.Results))
	}
	return out.Results
}

func memoryTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	database := setupTestDB(t)
	t.Cleanup(func() { database.Close() })
	s := &Server{db: database, hub: NewHub()}
	long := "needle " + strings.Repeat("ünïcode body text ", 200)
	if _, err := s.db.SaveEvent(db.SaveEventInput{
		Actor: "agent", Scope: "repo", Type: "decision", Title: "Pick SQLite", Text: long, Tags: []string{"db"}, Importance: 0.7,
		Refs: map[string]any{"big": strings.Repeat("r", 500)},
	}); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}
	return s, long
}

func TestHandleSearchEvents_DefaultReturnsFullEvents(t *testing.T) {
	s, long := memoryTestServer(t)
	res := searchEventsReq(t, s, "q=needle")
	if len(res) != 1 {
		t.Fatalf("results = %d", len(res))
	}
	if res[0]["text"] != long {
		t.Errorf("default response must keep the full text, got %d chars", len(res[0]["text"].(string)))
	}
	if _, ok := res[0]["refs"]; !ok {
		t.Errorf("default response must keep refs")
	}
}

func TestHandleSearchEvents_CompactReturnsIndexOnly(t *testing.T) {
	s, long := memoryTestServer(t)
	res := searchEventsReq(t, s, "q=needle&compact=true")
	if len(res) != 1 {
		t.Fatalf("results = %d", len(res))
	}
	r := res[0]
	for _, k := range []string{"text", "refs"} {
		if _, ok := r[k]; ok {
			t.Errorf("compact result must not carry %q", k)
		}
	}
	for _, k := range []string{"id", "ts", "type", "title", "snippet", "tags", "importance"} {
		if _, ok := r[k]; !ok {
			t.Errorf("compact result missing %q: %v", k, r)
		}
	}
	snippet, _ := r["snippet"].(string)
	if !strings.HasPrefix(snippet, "needle") || !utf8.ValidString(snippet) {
		t.Errorf("snippet = %q", snippet)
	}
	if n := utf8.RuneCountInString(snippet); n > compactSnippetRunes+1 || n >= utf8.RuneCountInString(long) {
		t.Errorf("snippet not truncated: %d runes", n)
	}
	if r["title"] != "Pick SQLite" || r["type"] != "decision" {
		t.Errorf("title/type = %v / %v", r["title"], r["type"])
	}
}

func TestHandleSearchEvents_CompactShortTextKeptWhole(t *testing.T) {
	s, _ := memoryTestServer(t)
	if _, err := s.db.SaveEvent(db.SaveEventInput{Actor: "agent", Scope: "repo", Type: "note", Title: "t", Text: "short haystack"}); err != nil {
		t.Fatal(err)
	}
	res := searchEventsReq(t, s, "q=haystack&compact=true")
	if len(res) != 1 || res[0]["snippet"] != "short haystack" {
		t.Errorf("res = %v", res)
	}
}
