package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/MartinNevlaha/stratus-v2/agents"
)

type agentsResponse struct {
	ClaudeCode []*agents.AgentDef `json:"claude_code"`
	OpenCode   []*agents.AgentDef `json:"opencode"`
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
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
		return
	}

	type agentDetail struct {
		Name       string           `json:"name"`
		ClaudeCode *agents.AgentDef `json:"claude_code,omitempty"`
		OpenCode   *agents.AgentDef `json:"opencode,omitempty"`
	}

	detail := agentDetail{Name: name}

	ccPath := s.projectRoot + "/.claude/agents/" + name + ".md"
	if a, err := agents.ParseAgentFile(ccPath); err == nil {
		detail.ClaudeCode = a
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

	if req.Name == "" {
		jsonErr(w, http.StatusBadRequest, "name is required")
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
	if err := agents.WriteAgentOpenCode(s.projectRoot+"/.opencode/agents", agent); err != nil {
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
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
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

	ccDir := s.projectRoot + "/.claude/agents"
	ocDir := s.projectRoot + "/.opencode/agents"

	ccExists := pathExists(ccDir + "/" + name + ".md")
	ocExists := pathExists(ocDir + "/" + name + ".md")

	if !ccExists && !ocExists {
		jsonErr(w, http.StatusNotFound, "agent not found")
		return
	}

	if ccExists {
		if err := agents.WriteAgentClaudeCode(ccDir, agent); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update claude code: "+err.Error())
			return
		}
	}
	if ocExists {
		if err := agents.WriteAgentOpenCode(ocDir, agent); err != nil {
			jsonErr(w, http.StatusInternalServerError, "update opencode: "+err.Error())
			return
		}
	}

	json200(w, map[string]string{"status": "updated", "name": name})
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
		return
	}

	var deleted []string
	ccDir := s.projectRoot + "/.claude/agents"
	ocDir := s.projectRoot + "/.opencode/agents"

	if err := agents.DeleteAgent(ccDir, name); err == nil {
		deleted = append(deleted, "claude-code")
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
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
		return
	}

	var req assignSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	ccDir := s.projectRoot + "/.claude/agents"
	ocDir := s.projectRoot + "/.opencode/agents"

	ccPath := ccDir + "/" + name + ".md"
	ocPath := ocDir + "/" + name + ".md"

	var updated []string

	if pathExists(ccPath) {
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
	skillsDir := s.projectRoot + "/.claude/skills"
	skills, err := agents.ListSkillFiles(skillsDir)
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
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
		return
	}

	skillDir := s.projectRoot + "/.claude/skills/" + name
	skill, err := agents.ParseSkillFile(skillDir)
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

	if req.Name == "" {
		jsonErr(w, http.StatusBadRequest, "name is required")
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
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
		return
	}

	var req createSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	skillsDir := s.projectRoot + "/.claude/skills"
	skillDir := skillsDir + "/" + name
	if !pathExists(skillDir) {
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

	if err := agents.WriteSkill(skillsDir, skill); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update skill: "+err.Error())
		return
	}

	json200(w, map[string]string{"status": "updated", "name": name})
}

func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
		return
	}

	skillsDir := s.projectRoot + "/.claude/skills"
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
