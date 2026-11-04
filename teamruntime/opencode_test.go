package teamruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenCode_OnlyUsesOwnedServerReadyAnnouncement(t *testing.T) {
	t.Setenv("STRATUS_FIXTURE_NOISE_URL", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	s, err := fixtureAdapter("opencode").Open(ctx, fixtureSpec(t, "opencode"), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}

func TestOpenCode_IgnoresHeartbeatAndOtherSessionEvents(t *testing.T) {
	s := &openCodeSession{nativeID: "ses_ours"}
	for _, test := range []struct {
		event object
		want  bool
	}{
		{object{"type": "server.heartbeat", "properties": object{}}, false},
		{object{"type": "message.updated", "properties": object{"info": object{"sessionID": "ses_ours"}}}, false},
		{object{"type": "permission.asked", "properties": object{"sessionID": "ses_other"}}, false},
		{object{"type": "session.error", "properties": object{}}, false},
		{object{"type": "permission.v2.asked", "properties": object{"sessionID": "ses_ours"}}, true},
		{object{"type": "session.status", "properties": object{"sessionID": "ses_ours"}}, true},
	} {
		if got := s.relevantEvent(test.event); got != test.want {
			t.Fatalf("event %v: got %v want %v", test.event, got, test.want)
		}
	}
}

func runOpenCodeFixture() {
	if os.Getenv("STRATUS_FIXTURE_MCP") == "1" {
		var config object
		_ = json.Unmarshal([]byte(os.Getenv("OPENCODE_CONFIG_CONTENT")), &config)
		mcp := obj(config, "mcp")
		command := arr(obj(mcp, "stratus"), "command")
		if len(command) == 0 || command[0] != "/tmp/current-stratus" || mcp["other"] == nil || config["model"] != "keep/existing" || os.Getenv("STRATUS_TEAM_API_BASE") != "http://127.0.0.1:31415" {
			os.Exit(37)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(31)
	}
	events := make(chan object, 32)
	var mu sync.Mutex
	var messages []any
	parent := ""
	count := 0
	send := func(kind string, p object) { events <- object{"type": kind, "properties": p} }
	finish := func(text, model string, aborted bool) {
		mu.Lock()
		info := object{"id": fmt.Sprintf("msg_assistant%d", count), "parentID": parent, "sessionID": "ses_fixture", "role": "assistant", "modelID": model, "providerID": "fixture", "variant": "high", "finish": "stop", "time": object{"created": 1, "completed": 2}, "tokens": object{"input": 11, "output": 3, "reasoning": 0, "cache": object{"read": 0, "write": 0}}}
		if aborted {
			info["error"] = object{"name": "MessageAbortedError", "data": object{"message": "interrupted"}}
		}
		messages = append(messages, object{"info": info, "parts": []any{object{"type": "text", "text": text}}})
		mu.Unlock()
		send("message.updated", object{"info": info})
		send("session.status", object{"sessionID": "ses_fixture", "status": object{"type": "idle"}})
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "opencode" || p != os.Getenv("OPENCODE_SERVER_PASSWORD") {
			http.Error(w, "unauthorized", 401)
			return
		}
		if os.Getenv("STRATUS_TEAM_TOKEN") != "fixture-secret" {
			http.Error(w, "token not inherited", 500)
			return
		}
		out := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
		var body object
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		switch {
		case r.URL.Path == "/global/health":
			out(object{"healthy": true, "version": "fixture"})
		case r.URL.Path == "/provider":
			out(object{"all": []any{object{"id": "fixture", "models": object{"test": object{"id": "test", "capabilities": object{"toolcall": true}, "variants": object{"high": object{}}}}}}, "connected": []string{"fixture"}, "default": object{"fixture": "test"}})
		case r.URL.Path == "/session" && r.Method == "POST":
			m := obj(body, "model")
			if str(m, "id") != "test" || str(m, "providerID") != "fixture" || str(m, "variant") != "high" {
				http.Error(w, "wrong model", 400)
				return
			}
			out(object{"id": "ses_fixture", "directory": r.URL.Query().Get("directory"), "model": m})
		case r.URL.Path == "/session/ses_fixture" && r.Method == "GET":
			out(object{"id": "ses_fixture", "directory": r.URL.Query().Get("directory")})
		case r.URL.Path == "/event":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			for {
				select {
				case e := <-events:
					b, _ := json.Marshal(e)
					fmt.Fprintf(w, "data: %s\n\n", b)
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
			}
		case r.URL.Path == "/session/ses_fixture/prompt_async":
			m := obj(body, "model")
			if str(m, "modelID") != "test" || str(m, "providerID") != "fixture" || str(body, "variant") != "high" || str(body, "system") != "Stay in the assigned project." {
				http.Error(w, "wrong turn profile", 400)
				return
			}
			parts := arr(body, "parts")
			text := str(parts[0].(map[string]any), "text")
			mu.Lock()
			parent = str(body, "messageID")
			count++
			mu.Unlock()
			w.WriteHeader(204)
			w.(http.Flusher).Flush()
			send("session.status", object{"sessionID": "ses_fixture", "status": object{"type": "busy"}})
			switch text {
			case "permission":
				send("session.status", object{"sessionID": "ses_other", "status": object{"type": "idle"}})
				send("permission.asked", object{"id": "per_fixture", "sessionID": "ses_fixture", "permission": "edit", "patterns": []string{"output"}, "always": []string{}, "metadata": object{}})
			case "wait":
			case "crash":
				os.Exit(32)
			case "wrong-model":
				finish("wrong", "other", false)
			default:
				finish("second turn", "test", false)
			}
		case r.URL.Path == "/session/ses_fixture/message":
			mu.Lock()
			copy := append([]any{}, messages...)
			mu.Unlock()
			out(copy)
		case r.URL.Path == "/permission/per_fixture/reply":
			reply := str(body, "reply")
			if reply != "once" && reply != "reject" {
				http.Error(w, "permanent permission prohibited", 400)
				return
			}
			out(true)
			if reply == "once" {
				finish("approved", "test", false)
			} else {
				finish("denied", "test", false)
			}
		case r.URL.Path == "/session/ses_fixture/abort":
			out(true)
			finish("interrupted", "test", true)
		default:
			if strings.Contains(r.URL.Path, "permission") {
				http.Error(w, "unknown approval", 404)
			} else {
				http.Error(w, "unknown endpoint "+r.URL.Path, 404)
			}
		}
	})
	if os.Getenv("STRATUS_FIXTURE_NOISE_URL") == "1" {
		fmt.Println("Plugin reference http://127.0.0.1:1")
	}
	fmt.Printf("opencode server listening on http://%s\n", ln.Addr())
	_ = http.Serve(ln, h)
}
