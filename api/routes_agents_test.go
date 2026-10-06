package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The dashboard edits name, description, tools, model, skills and body; fields it does not
// show, such as effort and color, must survive an update.
func TestHandleUpdateAgent_KeepsEffortAndColor(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := "---\nname: delivery-x\ndescription: \"Old\"\ntools: Read\nmodel: opus\neffort: high\ncolor: red\n---\n\n# X\n"
	if err := os.WriteFile(filepath.Join(dir, "delivery-x.md"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &Server{projectRoot: root}

	body := `{"name":"delivery-x","description":"New","tools":["Read","Grep"],"model":"opus","skills":[],"body":"# X\n"}`
	req := httptest.NewRequest(http.MethodPut, "/api/agents/delivery-x", strings.NewReader(body))
	req.SetPathValue("name", "delivery-x")
	w := httptest.NewRecorder()
	server.handleUpdateAgent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "delivery-x.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`description: "New"`, "effort: high", "color: red"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("updated file lacks %q:\n%s", want, data)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// .claude/skills also holds the stratus, mdview and stratus-hud plugins: a skill named like
// one must not reach it, and DELETE /api/skills/stratus must not remove the guards' hooks.
func TestSkillHandlers_NeverTouchPlugins(t *testing.T) {
	root := t.TempDir()
	hooks := filepath.Join(root, ".claude/skills/stratus/hooks/hooks.json")
	writeFile(t, hooks, `{"hooks": {}}`)
	writeFile(t, filepath.Join(root, ".claude/skills/stratus/.claude-plugin/plugin.json"), `{"name": "stratus"}`)
	server := &Server{projectRoot: root}

	del := httptest.NewRequest(http.MethodDelete, "/api/skills/stratus", nil)
	del.SetPathValue("name", "stratus")
	w := httptest.NewRecorder()
	server.handleDeleteSkill(w, del)
	if w.Code != http.StatusNotFound {
		t.Errorf("DELETE stratus: status %d, want 404", w.Code)
	}

	create := httptest.NewRequest(http.MethodPost, "/api/skills", strings.NewReader(`{"name":"stratus","description":"x"}`))
	w = httptest.NewRecorder()
	server.handleCreateSkill(w, create)
	if w.Code != http.StatusConflict {
		t.Errorf("POST skill named stratus: status %d, want 409", w.Code)
	}

	if _, err := os.Stat(hooks); err != nil {
		t.Fatalf("the plugin's hooks.json is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/skills/stratus/SKILL.md")); err == nil {
		t.Fatalf("a SKILL.md was written into the plugin")
	}
}

func TestAgentAndSkillHandlers_RejectPathNames(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude/keep.md"), "keep")
	server := &Server{projectRoot: root}

	for _, name := range []string{"..", "../x", "a/b", ""} {
		req := httptest.NewRequest(http.MethodDelete, "/api/skills/x", nil)
		req.SetPathValue("name", name)
		w := httptest.NewRecorder()
		server.handleDeleteSkill(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("DELETE skill %q: status %d, want 400", name, w.Code)
		}
		req = httptest.NewRequest(http.MethodDelete, "/api/agents/x", nil)
		req.SetPathValue("name", name)
		w = httptest.NewRecorder()
		server.handleDeleteAgent(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("DELETE agent %q: status %d, want 400", name, w.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/keep.md")); err != nil {
		t.Fatalf(".claude was touched: %v", err)
	}
}

func TestHandleUpdateSkill_KeepsUnmanagedFrontmatter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".claude/skills/code-review/SKILL.md")
	writeFile(t, path, "---\nname: code-review\ndescription: \"Old\"\ncontext: fork\nagent: delivery-code-reviewer\n---\n\nbody\n")
	server := &Server{projectRoot: root}

	req := httptest.NewRequest(http.MethodPut, "/api/skills/code-review", strings.NewReader(`{"description":"New","body":"body"}`))
	req.SetPathValue("name", "code-review")
	w := httptest.NewRecorder()
	server.handleUpdateSkill(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{`description: "New"`, "context: fork", "agent: delivery-code-reviewer"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("updated skill lacks %q:\n%s", want, data)
		}
	}
}
