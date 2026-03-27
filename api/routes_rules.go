package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/agents"
)

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	rulesDir := filepath.Join(s.projectRoot, ".claude", "rules")
	rules, err := agents.ListRuleFiles(rulesDir)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "list rules: "+err.Error())
		return
	}

	json200(w, map[string]interface{}{
		"rules": rules,
	})
}

func (s *Server) handleGetRule(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
		return
	}
	if err := agents.ValidateRuleName(name); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	rulesDir := filepath.Join(s.projectRoot, ".claude", "rules")
	rule, err := agents.ParseRuleFile(filepath.Join(rulesDir, name+".md"))
	if err != nil {
		jsonErr(w, http.StatusNotFound, "rule not found")
		return
	}

	json200(w, rule)
}

type createRuleRequest struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req createRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.Name == "" {
		jsonErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := agents.ValidateRuleName(req.Name); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	rulesDir := filepath.Join(s.projectRoot, ".claude", "rules")
	rulePath := filepath.Join(rulesDir, req.Name+".md")
	if pathExists(rulePath) {
		jsonErr(w, http.StatusConflict, "rule already exists: "+req.Name)
		return
	}

	rule := &agents.RuleDef{
		Name:  req.Name,
		Title: req.Title,
		Body:  req.Body,
	}
	if rule.Title == "" {
		rule.Title = rule.Name
	}

	if err := agents.WriteRule(rulesDir, rule); err != nil {
		jsonErr(w, http.StatusInternalServerError, "write rule: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "created",
		"name":   req.Name,
	})
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
		return
	}
	if err := agents.ValidateRuleName(name); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var req createRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	rulesDir := filepath.Join(s.projectRoot, ".claude", "rules")
	rulePath := filepath.Join(rulesDir, name+".md")
	if !pathExists(rulePath) {
		jsonErr(w, http.StatusNotFound, "rule not found")
		return
	}

	rule := &agents.RuleDef{
		Name:  name,
		Title: req.Title,
		Body:  req.Body,
	}
	if rule.Title == "" {
		rule.Title = rule.Name
	}

	if err := agents.WriteRule(rulesDir, rule); err != nil {
		jsonErr(w, http.StatusInternalServerError, "update rule: "+err.Error())
		return
	}

	json200(w, map[string]string{"status": "updated", "name": name})
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "name")
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "missing name")
		return
	}
	if err := agents.ValidateRuleName(name); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	rulesDir := filepath.Join(s.projectRoot, ".claude", "rules")
	if err := agents.DeleteRule(rulesDir, name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			jsonErr(w, http.StatusNotFound, err.Error())
		} else {
			jsonErr(w, http.StatusInternalServerError, "delete rule: "+err.Error())
		}
		return
	}

	json200(w, map[string]string{"status": "deleted", "name": name})
}
