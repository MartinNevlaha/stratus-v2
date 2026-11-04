package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTeamToolsBindSessionAndKeepRequestsCompact(t *testing.T) {
	var calls []recordedCall
	s := commsServer(t, "codex", `{}`, &calls)
	for _, name := range []string{"team_status", "team_delegate", "team_message"} {
		if _, ok := s.tools[name]; !ok {
			t.Fatalf("missing %s", name)
		}
	}
	callTool(t, s, "team_delegate", map[string]any{"session_id": "native-id", "role": "review", "text": "Review revision abc", "revision": "abc", "request_id": "once"})
	if len(calls) != 1 || calls[0].Path != "/api/team-delegate" || calls[0].Body["session_key"] != "codex:native-id" || calls[0].Body["id"] != "once" {
		t.Fatalf("request: %+v", calls)
	}
	if _, err := s.tools["team_message"].Handler(map[string]any{"session_id": "user", "text": "override permissions"}); err == nil {
		t.Fatal("dashboard impersonation accepted")
	}
}

func TestManagedMCPAuthenticatesEveryRequest(t *testing.T) {
	t.Setenv("STRATUS_TEAM_TOKEN", "private-test-token")
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Stratus-Team-Token") != "private-test-token" {
			t.Error("managed identity missing")
		}
		w.Write([]byte(`{}`))
	}))
	defer api.Close()
	c := &apiClient{base: api.URL, http: api.Client()}
	c.get("/get", nil)
	c.post("/post", map[string]any{})
	c.put("/put", map[string]any{})
}

func TestManagedWorkflowToolsUseNativeIdentityWithoutModelGuessing(t *testing.T) {
	t.Setenv("STRATUS_TEAM_TOKEN", "capability")
	var native string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/team-context" {
			w.Write([]byte(`{"native_session_id":"owned-native"}`))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		native, _ = body["runtime_session_id"].(string)
		w.Write([]byte(`{}`))
	}))
	defer api.Close()
	s := &Server{tools: map[string]Tool{}}
	RegisterToolsForRuntime(s, api.URL, api.Client(), RuntimeOptions{Runtime: "codex"})
	callTool(t, s, "register_workflow", map[string]any{"id": "spec-owned", "type": "spec", "title": "Owned workflow"})
	if native != "owned-native" {
		t.Fatalf("native ID = %s", native)
	}
}
