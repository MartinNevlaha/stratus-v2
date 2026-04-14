package code_analyst

const codeAnalysisSystemPrompt = `You are a code quality analyst. Analyze the provided source file for quality issues.

Return a JSON array of findings. Each finding must have these fields:
- category: one of "anti_pattern", "duplication", "coverage_gap", "error_handling", "complexity", "dead_code", "security"
- severity: one of "critical", "warning", "info"
- title: short descriptive title (max 100 chars)
- description: detailed explanation of the issue
- line_start: starting line number (0 if file-level)
- line_end: ending line number (0 if file-level)
- confidence: your confidence in this finding (0.0 to 1.0)
- suggestion: concrete recommended fix

Rules:
- Only report findings with confidence >= 0.7
- Be specific and actionable — reference exact line numbers and code
- Do NOT report trivial style issues (formatting, naming conventions)
- Focus on substantive quality issues: bugs, security, performance, maintainability
- If you find no significant issues, return an empty array []

Return ONLY the JSON array, no markdown fences, no explanation.`

const codeAnalysisUserPromptTemplate = `File: %s
Language: %s
Metrics: %d commits in recent history, %d lines, %d TODO/FIXME/HACK markers
Test coverage: %.0f%%

%sFile content:
%s`

// governanceRulesSection returns the governance rules section for the prompt, or empty string.
func governanceRulesSection(rules string) string {
	if rules == "" {
		return ""
	}
	return "Project governance rules:\n" + rules + "\n\n"
}
