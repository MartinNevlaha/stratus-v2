package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runtimeServer(t *testing.T, h http.HandlerFunc) *Server {
	t.Helper()
	api := httptest.NewServer(h)
	t.Cleanup(api.Close)
	s := &Server{tools: map[string]Tool{}}
	RegisterToolsForRuntime(s, api.URL, api.Client(), RuntimeOptions{Runtime: "codex"})
	return s
}
func TestRuntimeMCP_RegisterOwnership(t *testing.T) {
	var body map[string]any
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{}`))
	})
	_, e := s.tools["register_workflow"].Handler(map[string]any{"id": "spec-a", "type": "spec", "title": "x", "session_id": "s1"})
	if e != nil || body["session_id"] != "codex:s1" || body["runtime_session_id"] != "s1" || body["runtime"] != "codex" {
		t.Fatal(body, e)
	}
	if _, e = s.tools["register_workflow"].Handler(map[string]any{"id": "missing"}); e == nil {
		t.Fatal("missing session accepted")
	}
}
func TestRuntimeMCP_MutationsCheckOwnership(t *testing.T) {
	writes := 0
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			writes++
		}
		w.Write([]byte(`{"id":"spec-legacy","runtime":"","session_id":"s1"}`))
	})
	_, e := s.tools["transition_phase"].Handler(map[string]any{"workflow_id": "spec-legacy", "phase": "implement", "tasks": []any{"should not save"}, "plan_content": "should not save"})
	if e == nil || writes != 0 {
		t.Fatal(e, writes)
	}
}
func TestRuntimeMCP_BindAndDispatch(t *testing.T) {
	var body map[string]any
	s := runtimeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			json.NewDecoder(r.Body).Decode(&body)
			w.Write([]byte(`{}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "state") {
			w.Write([]byte(`{"workflows":[{"id":"spec-codex","runtime":"codex","session_id":"codex:s1"},{"id":"spec-legacy","session_id":"s1"}]}`))
			return
		}
		w.Write([]byte(`{"id":"spec-codex","runtime":"codex"}`))
	})
	if _, e := s.tools["bind_workflow_session"].Handler(map[string]any{"workflow_id": "spec-codex", "session_id": "s2"}); e != nil || body["session_id"] != "codex:s2" {
		t.Fatal(body, e)
	}
	if _, e := s.tools["delivery_dispatch"].Handler(map[string]any{"session_id": "s1"}); e != nil {
		t.Fatal(e)
	}
}
