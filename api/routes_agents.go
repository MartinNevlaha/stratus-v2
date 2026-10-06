package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"github.com/MartinNevlaha/stratus-v2/agents"
)

type agentsResponse struct {
	ClaudeCode []*agents.AgentDef `json:"claude_code"`
	OpenCode   []*agents.AgentDef `json:"opencode"`
}

// validName matches the agent and skill names the dashboard may create, change or delete:
// a plain file or directory name, never a path ("..", "a/b").
var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ccAgentDir returns .claude/agents when it holds the named agent, or "".
func (s *Server) ccAgentDir(name string) string {
	dir := s.projectRoot + "/.claude/agents"
	if validName.MatchString(name) && pathExists(dir+"/"+name+".md") {
		return dir
	}
	return ""
}

// ccSkillDir returns .claude/skills when it holds the named skill, or "". A skill is a
// directory with a SKILL.md: .claude/skills also holds the stratus, mdview and stratus-hud
// plugins, which the skill handlers must never read, rewrite or delete.
func (s *Server) ccSkillDir(name string) string {
	dir := s.projectRoot + "/.claude/skills"
	if validName.MatchString(name) && pathExists(dir+"/"+name+"/SKILL.md") {
		return dir
	}
	return ""
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	ccDir := s.projectRoot + "/.claude/agents"
	ocDir := s.projectRoot + "/.opencode/agents"

	ccAgents, _ := agents.ListAgentFiles(ccDir)
	ocAgents, _ := agents.ListAgentFiles(ocDir)

	models := agents.ReadOpenCodeConfig(s.projectRoot)
	agents.EnrichOpenCodeAgents(ocAgents, models)

	json200(w, agentsResponse{
		ClaudeCode: ccAgents,
		OpenCode:   ocAgents,
	})
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if !validName.MatchString(name) {
		jsonErr(w, http.StatusBadRequest, "invalid name")
		return
	}

	type agentDetail struct {
		Name       string           `json:"name"`
		ClaudeCode *agents.AgentDef `json:"claude_code,omitempty"`
		OpenCode   *agents.AgentDef `json:"opencode,omitempty"`
	}

	detail := agentDetail{Name: name}

	if dir := s.ccAgentDir(name); dir != "" {
		if a, err := agents.ParseAgentFile(dir + "/" + name + ".md"); err == nil {
			detail.ClaudeCode = a
		}
	}

	ocPath := s.projectRoot + "/.opencode/agents/" + name + ".md"
	if a, err := agents.ParseAgentFile(ocPath); err == nil {
		models := agents.ReadOpenCodeConfig(s.projectRoot)
		if models != nil {
			if m, ok := models[name]; ok && a.Model == "" {
				a.Model = m
			}
		}
		detail.OpenCode = a
	}

	if detail.ClaudeCode == nil && detail.OpenCode == nil {
		jsonErr(w, http.StatusNotFound, "agent not found")
		return
	}

	json200(w, detail)
}

type createAgentRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	Model       string   `json:"model"`
	Skills      []string `json:"skills"`
	Body        string   `json:"body"`
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if !validName.MatchString(req.Name) {
		jsonErr(w, http.StatusBadRequest, "name must be letters, digits, - or _")
		return
	}
	if req.Description == "" {
		jsonErr(w, http.StatusBadRequest, "description is required")
		return
	}

	agent := &agents.AgentDef{
		Name:        req.Name,
		Description: req.Description,
		Tools:       req.Tools,
		Model:       req.Model,
		Skills:      req.Skills,
		Body:        req.Body,
	}

	if len(req.Tools) == 0 {
		agent.Tools = []string{"Read", "Grep", "Glob", "Edit", "Write", "Bash"}
	}
	if agent.Model == "" {
		agent.Model = "sonnet"
	}
	if agent.Body == "" {
		agent.Body = fmt.Sprintf("# %s\n\n%s", req.Name, req.Description)
	}

	if err := agents.WriteAgentClaudeCode(s.projectRoot+"/.claude/agents", agent); err != nil {
		jsonErr(w, http.StatusInternalServerError, "write claude code agent: "+err.Error())
		return
	}
	if err := agents.WriteAgentOpenCode(s.projectRoot+"/.opencode/agents", agent, s.cfg.Port); err != nil {
		jsonErr(w, http.StatusInternalServerError, "write opencode agent: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	json200(w, map[string]string{
		"status":  "created",
		"name":    req.Name,
		"message": "Agent created in both .claude/agents/ and .opencode/agents/",
	})
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if !validName.MatchString(name) {
		jsonErr(w, http.StatusBadRequest, "invalid name")
		return
	}

	var req createAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	agent := &agents.AgentDef{
		Name:        name,
		Description: req.Description,
		Tools:       req.Tools,
		Model:       req.Model,
		Skills:      req.Skills,
		Body:        req.Body,
	}

	ccDir := s.ccAgentDir(name)
	ocDir := s.projectRoot + "/.opencode/agents"

	ccExists := ccDir != ""
	ocExists := pathExists(ocDir + "/" + name + ".md")

	if !ccExists && !ocExists {
		jsonErr(w, http.StatusNotFound, "agent not found")
		return
	}

	if ccExists {
		// The dashboard does not edit effort or color; keep what the file has.
		if existing, err := agents.ParseAgentFile(ccDir + "/" + name + ".md"); err == nil {
			agent.Effort, agent.Color = existing.Effort, existing.Color
		}
		if err := agents.WriteAgentClaudeCode(ccDir, agent); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update claude code: "+err.Error())
			return
		}
	}
	if ocExists {
		if err := agents.WriteAgentOpenCode(ocDir, agent, s.cfg.Port); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update opencode: "+err.Error())
			return
		}
	}

	json200(w, map[string]string{"status": "updated", "name": name})
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if !validName.MatchString(name) {
		jsonErr(w, http.StatusBadRequest, "invalid name")
		return
	}

	var deleted []string
	ocDir := s.projectRoot + "/.opencode/agents"

	if ccDir := s.ccAgentDir(name); ccDir != "" {
		if err := agents.DeleteAgent(ccDir, name); err == nil {
			deleted = append(deleted, "claude-code")
		}
	}
	if err := agents.DeleteAgent(ocDir, name); err == nil {
		deleted = append(deleted, "opencode")
	}

	if len(deleted) == 0 {
		jsonErr(w, http.StatusNotFound, "agent not found")
		return
	}

	json200(w, map[string]interface{}{
		"status":  "deleted",
		"name":    name,
		"formats": deleted,
	})
}

type assignSkillsRequest struct {
	Skills []string `json:"skills"`
}

func (s *Server) handleAssignSkills(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if !validName.MatchString(name) {
		jsonErr(w, http.StatusBadRequest, "invalid name")
		return
	}

	var req assignSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	ccDir := s.ccAgentDir(name)
	ocDir := s.projectRoot + "/.opencode/agents"

	ocPath := ocDir + "/" + name + ".md"

	var updated []string

	if ccDir != "" {
		if err := agents.UpdateAgentSkills(ccDir, name, req.Skills, "claude-code"); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update claude code skills: "+err.Error())
			return
		}
		updated = append(updated, "claude-code")
	}

	if pathExists(ocPath) {
		if err := agents.UpdateAgentSkills(ocDir, name, req.Skills, "opencode"); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update opencode skills: "+err.Error())
			return
		}
		updated = append(updated, "opencode")
	}

	if len(updated) == 0 {
		jsonErr(w, http.StatusNotFound, "agent not found")
		return
	}

	json200(w, map[string]interface{}{
		"status":  "skills_updated",
		"name":    name,
		"skills":  req.Skills,
		"formats": updated,
	})
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := agents.ListSkillFiles(s.projectRoot + "/.claude/skills")
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list skills: "+err.Error())
		return
	}

	json200(w, map[string]interface{}{
		"skills": skills,
	})
}

func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if !validName.MatchString(name) {
		jsonErr(w, http.StatusBadRequest, "invalid name")
		return
	}

	dir := s.ccSkillDir(name)
	if dir == "" {
		jsonErr(w, http.StatusNotFound, "skill not found")
		return
	}
	skill, err := agents.ParseSkillFile(dir + "/" + name)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "skill not found")
		return
	}

	json200(w, skill)
}

type createSkillRequest struct {
	Name                   string `json:"name"`
	Description            string `json:"description"`
	DisableModelInvocation bool   `json:"disable_model_invocation"`
	ArgumentHint           string `json:"argument_hint"`
	Body                   string `json:"body"`
}

func (s *Server) handleCreateSkill(w http.ResponseWriter, r *http.Request) {
	var req createSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if !validName.MatchString(req.Name) {
		jsonErr(w, http.StatusBadRequest, "name must be letters, digits, - or _")
		return
	}
	if req.Description == "" {
		jsonErr(w, http.StatusBadRequest, "description is required")
		return
	}

	skill := &agents.SkillDef{
		Name:                   req.Name,
		Description:            req.Description,
		DisableModelInvocation: req.DisableModelInvocation,
		ArgumentHint:           req.ArgumentHint,
		Body:                   req.Body,
	}

	if skill.Body == "" {
		skill.Body = fmt.Sprintf("# %s\n\n%s", req.Name, req.Description)
	}

	skillsDir := s.projectRoot + "/.claude/skills"
	if pathExists(skillsDir+"/"+req.Name) && s.ccSkillDir(req.Name) == "" {
		jsonErr(w, http.StatusConflict, "name is taken by a plugin or another directory in .claude/skills")
		return
	}
	if err := agents.WriteSkill(skillsDir, skill); err != nil {
		jsonErr(w, http.StatusInternalServerError, "write skill: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	json200(w, map[string]string{
		"status": "created",
		"name":   req.Name,
	})
}

func (s *Server) handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if !validName.MatchString(name) {
		jsonErr(w, http.StatusBadRequest, "invalid name")
		return
	}

	var req createSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	skillsDir := s.ccSkillDir(name)
	if skillsDir == "" {
		jsonErr(w, http.StatusNotFound, "skill not found")
		return
	}

	skill := &agents.SkillDef{
		Name:                   name,
		Description:            req.Description,
		DisableModelInvocation: req.DisableModelInvocation,
		ArgumentHint:           req.ArgumentHint,
		Body:                   req.Body,
	}
	// The dashboard does not edit context, agent, allowed-tools, paths and the like.
	if existing, err := agents.ParseSkillFile(skillsDir + "/" + name); err == nil {
		skill.Extra = existing.Extra
	}

	if err := agents.WriteSkill(skillsDir, skill); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update skill: "+err.Error())
		return
	}

	json200(w, map[string]string{"status": "updated", "name": name})
}

func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if !validName.MatchString(name) {
		jsonErr(w, http.StatusBadRequest, "invalid name")
		return
	}

	skillsDir := s.ccSkillDir(name)
	if skillsDir == "" {
		jsonErr(w, http.StatusNotFound, "skill not found")
		return
	}
	if err := agents.DeleteSkill(skillsDir, name); err != nil {
		jsonErr(w, http.StatusNotFound, err.Error())
		return
	}

	json200(w, map[string]string{"status": "deleted", "name": name})
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
