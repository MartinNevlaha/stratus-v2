package agents

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type AgentDef struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
	Model       string   `json:"model,omitempty"`
	Skills      []string `json:"skills"`
	Body        string   `json:"body"`
	Format      string   `json:"format"`
	FilePath    string   `json:"file_path"`
}

type SkillDef struct {
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	DisableModelInvocation bool     `json:"disable_model_invocation"`
	ArgumentHint           string   `json:"argument_hint,omitempty"`
	Body                   string   `json:"body"`
	HasResources           bool     `json:"has_resources"`
	ResourceDirs           []string `json:"resource_dirs"`
	DirPath                string   `json:"dir_path"`
}

func ParseAgentFile(path string) (*AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent file: %w", err)
	}

	content := string(data)
	frontmatter, body, err := extractFrontmatter(content)
	if err != nil {
		return nil, err
	}

	agent := &AgentDef{
		Body:     body,
		FilePath: path,
	}

	baseName := strings.TrimSuffix(filepath.Base(path), ".md")

	if strings.Contains(frontmatter, "mode:") {
		agent.Format = "opencode"
		agent.Name = baseName
		parseOpenCodeFrontmatter(frontmatter, agent)
	} else {
		agent.Format = "claude-code"
		parseClaudeCodeFrontmatter(frontmatter, agent)
	}

	return agent, nil
}

func parseClaudeCodeFrontmatter(fm string, agent *AgentDef) {
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			agent.Name = strings.Trim(strings.TrimPrefix(line, "name:"), ` "`)
		} else if strings.HasPrefix(line, "description:") {
			agent.Description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), `"`)
		} else if strings.HasPrefix(line, "tools:") {
			toolsStr := strings.TrimSpace(strings.TrimPrefix(line, "tools:"))
			for _, t := range strings.Split(toolsStr, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					agent.Tools = append(agent.Tools, t)
				}
			}
		} else if strings.HasPrefix(line, "model:") {
			agent.Model = strings.TrimSpace(strings.TrimPrefix(line, "model:"))
		}
	}

	agent.Skills = extractYamlList(fm, "skills")
}

func parseOpenCodeFrontmatter(fm string, agent *AgentDef) {
	inTools := false
	for _, line := range strings.Split(fm, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "description:") {
			agent.Description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "description:")), `"`)
		} else if strings.HasPrefix(trimmed, "tools:") {
			inTools = true
			continue
		} else if inTools && strings.HasPrefix(line, "  ") {
			parts := strings.SplitN(strings.TrimSpace(trimmed), ":", 2)
			if len(parts) == 2 {
				enabled := strings.TrimSpace(parts[1])
				if enabled == "true" || enabled == "false" {
					toolName := parts[0]
					agent.Tools = append(agent.Tools, toolName+":"+enabled)
				}
			}
		} else {
			inTools = false
		}
	}

	agent.Skills = extractOpenCodeSkillsFromBody(agent.Body)
}

func extractOpenCodeSkillsFromBody(body string) []string {
	var skills []string
	inSkillsSection := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Skills") {
			inSkillsSection = true
			continue
		}
		if inSkillsSection {
			if strings.HasPrefix(trimmed, "## ") || trimmed == "" && len(skills) > 0 {
				break
			}
			if strings.HasPrefix(trimmed, "- ") {
				skillRef := strings.TrimPrefix(trimmed, "- ")
				skillRef = strings.TrimSpace(skillRef)
				if skillRef != "" {
					parts := strings.SplitN(skillRef, " ", 2)
					skills = append(skills, parts[0])
				}
			}
		}
	}
	return skills
}

func extractYamlList(fm string, key string) []string {
	var items []string
	inList := false
	for _, line := range strings.Split(fm, "\n") {
		if strings.TrimSpace(line) == key+":" {
			inList = true
			continue
		}
		if inList {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") {
				items = append(items, strings.TrimPrefix(trimmed, "- "))
			} else if trimmed != "" && !strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "\t") {
				break
			}
		}
	}
	return items
}

func extractFrontmatter(content string) (frontmatter string, body string, err error) {
	if !strings.HasPrefix(content, "---") {
		return "", content, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(content[3:]))
	scanner.Buffer(make([]byte, 0), 10*1024*1024)
	var fmLines []string
	foundEnd := false

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			foundEnd = true
			break
		}
		fmLines = append(fmLines, line)
	}

	if !foundEnd {
		return "", "", fmt.Errorf("frontmatter not closed")
	}

	var bodyLines []string
	for scanner.Scan() {
		bodyLines = append(bodyLines, scanner.Text())
	}

	return strings.Join(fmLines, "\n"), strings.TrimSpace(strings.Join(bodyLines, "\n")), nil
}

func ListAgentFiles(dir string) ([]*AgentDef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read agents dir: %w", err)
	}

	var agents []*AgentDef
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		agent, err := ParseAgentFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		agents = append(agents, agent)
	}
	return agents, nil
}

func WriteAgentClaudeCode(dir string, agent *AgentDef) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	var toolsStr string
	if len(agent.Tools) > 0 {
		var simple []string
		for _, t := range agent.Tools {
			if !strings.Contains(t, ":") {
				simple = append(simple, t)
			}
		}
		toolsStr = strings.Join(simple, ", ")
	}

	var fm strings.Builder
	fm.WriteString("---\n")
	fm.WriteString(fmt.Sprintf("name: %s\n", agent.Name))
	fm.WriteString(fmt.Sprintf("description: %q\n", agent.Description))
	if toolsStr != "" {
		fm.WriteString(fmt.Sprintf("tools: %s\n", toolsStr))
	}
	if agent.Model != "" {
		fm.WriteString(fmt.Sprintf("model: %s\n", agent.Model))
	}
	if len(agent.Skills) > 0 {
		fm.WriteString("skills:\n")
		for _, s := range agent.Skills {
			fm.WriteString(fmt.Sprintf("  - %s\n", s))
		}
	}
	fm.WriteString("---\n")

	path := filepath.Join(dir, agent.Name+".md")
	content := fm.String()
	if agent.Body != "" {
		content += "\n" + agent.Body + "\n"
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func WriteAgentOpenCode(dir string, agent *AgentDef) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	toolMap := map[string]bool{
		"read": true, "grep": true, "glob": true,
		"edit": false, "write": false, "bash": false,
		"todo": false,
	}
	for _, t := range agent.Tools {
		if strings.Contains(t, ":") {
			parts := strings.SplitN(t, ":", 2)
			toolMap[parts[0]] = parts[1] == "true"
		} else {
			toolMap[strings.ToLower(t)] = true
		}
	}

	var fm strings.Builder
	fm.WriteString("---\n")
	fm.WriteString(fmt.Sprintf("description: %s\n", agent.Description))
	fm.WriteString("mode: subagent\n")
	fm.WriteString("tools:\n")
	for _, name := range []string{"todo", "read", "grep", "glob", "edit", "write", "bash"} {
		enabled, ok := toolMap[name]
		if ok {
			fm.WriteString(fmt.Sprintf("  %s: %v\n", name, enabled))
		}
	}
	fm.WriteString("---\n")

	body := agent.Body
	if len(agent.Skills) > 0 {
		if !strings.Contains(body, "## Skills") {
			body += "\n\n## Skills\n\n"
			for _, s := range agent.Skills {
				body += fmt.Sprintf("- Use the `%s` skill when relevant.\n", s)
			}
		}
	}

	if !strings.Contains(body, "## Workflow Guard") {
		body += `

## Workflow Guard

Before starting ANY work, verify there is an active workflow:

` + "```bash" + `
curl -sS http://localhost:41777/api/dashboard/state | jq '.workflows[0]'
` + "```" + `

If no active workflow exists (null response), **STOP** and tell the user:
> "No active workflow found. Start a /spec or /bug workflow first."

Do NOT proceed without an active workflow.
`
	}

	path := filepath.Join(dir, agent.Name+".md")
	content := fm.String() + "\n" + body + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func DeleteAgent(dir string, name string) error {
	path := filepath.Join(dir, name+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("agent not found: %s", name)
	}
	return os.Remove(path)
}

func ParseSkillFile(dirPath string) (*SkillDef, error) {
	skillPath := filepath.Join(dirPath, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("read skill file: %w", err)
	}

	content := string(data)
	fm, body, err := extractFrontmatter(content)
	if err != nil {
		return nil, err
	}

	skill := &SkillDef{
		Body:    body,
		DirPath: dirPath,
	}

	for _, line := range strings.Split(fm, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "name:") {
			skill.Name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "name:")), `"`)
		} else if strings.HasPrefix(trimmed, "description:") {
			skill.Description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "description:")), `"`)
		} else if strings.HasPrefix(trimmed, "disable-model-invocation:") {
			skill.DisableModelInvocation = strings.TrimSpace(strings.TrimPrefix(trimmed, "disable-model-invocation:")) == "true"
		} else if strings.HasPrefix(trimmed, "argument-hint:") {
			skill.ArgumentHint = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "argument-hint:")), `"`)
		}
	}

	entries, err := os.ReadDir(dirPath)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "." {
				skill.HasResources = true
				skill.ResourceDirs = append(skill.ResourceDirs, entry.Name())
			}
		}
	}

	return skill, nil
}

func ListSkillFiles(skillsDir string) ([]*SkillDef, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills dir: %w", err)
	}

	var skills []*SkillDef
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(skillsDir, entry.Name())
		skill, err := ParseSkillFile(skillDir)
		if err != nil {
			continue
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

func WriteSkill(skillsDir string, skill *SkillDef) error {
	skillDir := filepath.Join(skillsDir, skill.Name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return err
	}

	var fm strings.Builder
	fm.WriteString("---\n")
	fm.WriteString(fmt.Sprintf("name: %s\n", skill.Name))
	fm.WriteString(fmt.Sprintf("description: %q\n", skill.Description))
	if skill.DisableModelInvocation {
		fm.WriteString("disable-model-invocation: true\n")
	}
	if skill.ArgumentHint != "" {
		fm.WriteString(fmt.Sprintf("argument-hint: %q\n", skill.ArgumentHint))
	}
	fm.WriteString("---\n")

	content := fm.String()
	if skill.Body != "" {
		content += "\n" + skill.Body + "\n"
	}

	return os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
}

func DeleteSkill(skillsDir string, name string) error {
	skillDir := filepath.Join(skillsDir, name)
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return fmt.Errorf("skill not found: %s", name)
	}
	return os.RemoveAll(skillDir)
}

func UpdateAgentSkills(dir string, agentName string, skills []string, format string) error {
	path := filepath.Join(dir, agentName+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read agent: %w", err)
	}

	content := string(data)

	if format == "claude-code" {
		content = updateClaudeCodeSkills(content, skills)
	} else {
		content = updateOpenCodeSkills(content, skills)
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func updateClaudeCodeSkills(content string, skills []string) string {
	fm, body, _ := extractFrontmatter(content)

	lines := strings.Split(fm, "\n")
	var result []string
	skipSkills := false
	skillsWritten := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "skills:" {
			skipSkills = true
			continue
		}
		if skipSkills && strings.HasPrefix(trimmed, "- ") {
			continue
		}
		skipSkills = false

		if !skillsWritten && (trimmed == "" || !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "skills:") {
			if strings.Contains(line, "model:") || strings.Contains(line, "tools:") || strings.Contains(line, "name:") || strings.Contains(line, "description:") {
				result = append(result, line)
				continue
			}
		}

		if !skillsWritten {
			result = append(result, "skills:")
			for _, s := range skills {
				result = append(result, fmt.Sprintf("  - %s", s))
			}
			skillsWritten = true
		}
		result = append(result, line)
	}

	if !skillsWritten {
		result = append(result, "skills:")
		for _, s := range skills {
			result = append(result, fmt.Sprintf("  - %s", s))
		}
	}

	return "---\n" + strings.Join(result, "\n") + "\n---\n\n" + body
}

func updateOpenCodeSkills(content string, skills []string) string {
	var bodyLines []string
	var inSkillsSection bool
	var skillsReplaced bool

	fm, body, _ := extractFrontmatter(content)

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## Skills") {
			inSkillsSection = true
			if !skillsReplaced {
				bodyLines = append(bodyLines, "## Skills", "")
				for _, s := range skills {
					bodyLines = append(bodyLines, fmt.Sprintf("- Use the `%s` skill when relevant.", s))
				}
				skillsReplaced = true
			}
			continue
		}
		if inSkillsSection {
			if strings.HasPrefix(trimmed, "## ") || (trimmed == "" && len(bodyLines) > 0 && bodyLines[len(bodyLines)-1] != "") {
				inSkillsSection = false
			} else {
				continue
			}
		}
		bodyLines = append(bodyLines, line)
	}

	if !skillsReplaced {
		bodyLines = append(bodyLines, "", "## Skills", "")
		for _, s := range skills {
			bodyLines = append(bodyLines, fmt.Sprintf("- Use the `%s` skill when relevant.", s))
		}
	}

	return "---\n" + fm + "\n---\n\n" + strings.Join(bodyLines, "\n")
}

type RuleDef struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	FilePath string `json:"file_path"`
}

func ValidateRuleName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid rule name: %q", name)
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\:*?\"<>|\x00") {
		return fmt.Errorf("invalid characters in rule name: %q", name)
	}
	if len(name) > 255 {
		return fmt.Errorf("rule name too long (max 255 chars)")
	}
	return nil
}

func ListRuleFiles(dir string) ([]*RuleDef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rules dir: %w", err)
	}

	var rules []*RuleDef
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		rule, err := ParseRuleFile(path)
		if err != nil {
			continue
		}
		rules = append(rules, rule)
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Name < rules[j].Name
	})

	return rules, nil
}

func ParseRuleFile(path string) (*RuleDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rule file: %w", err)
	}

	content := string(data)
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	title := name

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			break
		}
	}

	return &RuleDef{
		Name:     name,
		Title:    title,
		Body:     content,
		FilePath: path,
	}, nil
}

func WriteRule(dir string, rule *RuleDef) error {
	if err := ValidateRuleName(rule.Name); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create rules dir: %w", err)
	}

	body := rule.Body
	if body == "" {
		title := rule.Title
		if title == "" {
			title = rule.Name
		}
		body = fmt.Sprintf("# %s\n", title)
	}

	path := filepath.Join(dir, rule.Name+".md")
	return os.WriteFile(path, []byte(body), 0o644)
}

func DeleteRule(dir string, name string) error {
	if err := ValidateRuleName(name); err != nil {
		return err
	}
	path := filepath.Join(dir, name+".md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("rule not found: %s", name)
	}
	return os.Remove(path)
}
