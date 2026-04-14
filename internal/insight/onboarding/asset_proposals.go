package onboarding

// GenerateAssetProposals maps ProjectProfile signals to asset proposals.
// projectRoot is used for filesystem deduplication.
// existingPaths contains proposed_path values from DB for dedup.
func GenerateAssetProposals(profile *ProjectProfile, projectRoot string, existingPaths map[string]bool) []AssetProposal {
	var proposals []AssetProposal

	data := buildTemplateData(profile)

	// --- Language-based rules ---
	for _, lang := range profile.Languages {
		confidence := lang.Percentage / 100.0
		if confidence > 0.95 {
			confidence = 0.95
		}

		switch lang.Language {
		case "Go":
			proposals = append(proposals, AssetProposal{
				Type:            "asset.rule",
				Title:           "Go Conventions",
				Description:     "Go error handling, naming, interface, and test conventions.",
				ProposedPath:    ".claude/rules/go-conventions.md",
				ProposedContent: goConventionsRule,
				Confidence:      confidence,
				Target:          "claude-code",
				Signals:         []string{"language:Go"},
			})

		case "TypeScript", "JavaScript":
			proposals = append(proposals, AssetProposal{
				Type:            "asset.rule",
				Title:           "TypeScript Conventions",
				Description:     "TypeScript strict mode, typing, async/await, and ESLint conventions.",
				ProposedPath:    ".claude/rules/ts-conventions.md",
				ProposedContent: tsConventionsRule,
				Confidence:      confidence,
				Target:          "claude-code",
				Signals:         []string{"language:" + lang.Language},
			})

		case "Python":
			proposals = append(proposals, AssetProposal{
				Type:            "asset.rule",
				Title:           "Python Conventions",
				Description:     "Python type hints, docstrings, naming, and error handling conventions.",
				ProposedPath:    ".claude/rules/python-conventions.md",
				ProposedContent: pythonConventionsRule,
				Confidence:      confidence,
				Target:          "claude-code",
				Signals:         []string{"language:Python"},
			})
		}
	}

	// --- Pattern-based proposals ---
	patternSet := make(map[string]bool, len(profile.DetectedPatterns))
	for _, p := range profile.DetectedPatterns {
		patternSet[p] = true
	}

	if patternSet["docker"] {
		proposals = append(proposals, AssetProposal{
			Type:            "asset.rule",
			Title:           "Docker Conventions",
			Description:     "Multi-stage builds, .dockerignore, non-root users, and layer caching conventions.",
			ProposedPath:    ".claude/rules/docker-conventions.md",
			ProposedContent: dockerConventionsRule,
			Confidence:      0.8,
			Target:          "claude-code",
			Signals:         []string{"pattern:docker"},
		})
	}

	if patternSet["monorepo"] {
		proposals = append(proposals, AssetProposal{
			Type:            "asset.rule",
			Title:           "Monorepo Conventions",
			Description:     "Workspace boundaries, shared dependencies, and package naming in a monorepo.",
			ProposedPath:    ".claude/rules/monorepo-conventions.md",
			ProposedContent: monorepoConventionsRule,
			Confidence:      0.8,
			Target:          "claude-code",
			Signals:         []string{"pattern:monorepo"},
		})
	}

	if patternSet["web-app"] {
		ccContent, ccErr := RenderTemplate(frontendSpecialistAgentCC, data)
		if ccErr == nil {
			proposals = append(proposals, AssetProposal{
				Type:            "asset.agent.cc",
				Title:           "Frontend Specialist (Claude Code)",
				Description:     "Frontend specialist agent for implementing and fixing UI features.",
				ProposedPath:    ".claude/agents/delivery-frontend-specialist.md",
				ProposedContent: ccContent,
				Confidence:      0.8,
				Target:          "claude-code",
				Signals:         []string{"pattern:web-app"},
			})
		}

		ocContent, ocErr := RenderTemplate(frontendSpecialistAgentOC, data)
		if ocErr == nil {
			proposals = append(proposals, AssetProposal{
				Type:            "asset.agent.oc",
				Title:           "Frontend Specialist (OpenCode)",
				Description:     "Frontend specialist agent for implementing and fixing UI features.",
				ProposedPath:    ".opencode/agents/delivery-frontend-specialist.md",
				ProposedContent: ocContent,
				Confidence:      0.8,
				Target:          "opencode",
				Signals:         []string{"pattern:web-app"},
			})
		}
	}

	if patternSet["cli-app"] {
		ccContent, ccErr := RenderTemplate(cliSpecialistAgentCC, data)
		if ccErr == nil {
			proposals = append(proposals, AssetProposal{
				Type:            "asset.agent.cc",
				Title:           "CLI Specialist (Claude Code)",
				Description:     "CLI specialist agent for implementing and debugging command-line tooling.",
				ProposedPath:    ".claude/agents/delivery-cli-specialist.md",
				ProposedContent: ccContent,
				Confidence:      0.8,
				Target:          "claude-code",
				Signals:         []string{"pattern:cli-app"},
			})
		}

		ocContent, ocErr := RenderTemplate(cliSpecialistAgentOC, data)
		if ocErr == nil {
			proposals = append(proposals, AssetProposal{
				Type:            "asset.agent.oc",
				Title:           "CLI Specialist (OpenCode)",
				Description:     "CLI specialist agent for implementing and debugging command-line tooling.",
				ProposedPath:    ".opencode/agents/delivery-cli-specialist.md",
				ProposedContent: ocContent,
				Confidence:      0.8,
				Target:          "opencode",
				Signals:         []string{"pattern:cli-app"},
			})
		}
	}

	// --- CI-based proposals ---
	if profile.CIProvider != "" {
		proposals = append(proposals, AssetProposal{
			Type:            "asset.rule",
			Title:           "CI Conventions",
			Description:     "Pipeline stages, fail-fast, caching, and branch protection conventions.",
			ProposedPath:    ".claude/rules/ci-conventions.md",
			ProposedContent: ciConventionsRule,
			Confidence:      0.85,
			Target:          "claude-code",
			Signals:         []string{"ci:" + profile.CIProvider},
		})

		proposals = append(proposals, AssetProposal{
			Type:            "asset.skill.cc",
			Title:           "CI Check Skill",
			Description:     "Inspect CI pipeline status and surface failing job logs.",
			ProposedPath:    ".claude/skills/ci-check/SKILL.md",
			ProposedContent: ciCheckSkill,
			Confidence:      0.85,
			Target:          "claude-code",
			Signals:         []string{"ci:" + profile.CIProvider},
		})

		// Docker + CI → devops specialist agents
		if patternSet["docker"] {
			ccContent, ccErr := RenderTemplate(devopsSpecialistAgentCC, data)
			if ccErr == nil {
				proposals = append(proposals, AssetProposal{
					Type:            "asset.agent.cc",
					Title:           "DevOps Specialist (Claude Code)",
					Description:     "Docker and CI/CD specialist agent.",
					ProposedPath:    ".claude/agents/delivery-devops-specialist.md",
					ProposedContent: ccContent,
					Confidence:      0.85,
					Target:          "claude-code",
					Signals:         []string{"pattern:docker", "ci:" + profile.CIProvider},
				})
			}

			ocContent, ocErr := RenderTemplate(devopsSpecialistAgentOC, data)
			if ocErr == nil {
				proposals = append(proposals, AssetProposal{
					Type:            "asset.agent.oc",
					Title:           "DevOps Specialist (OpenCode)",
					Description:     "Docker and CI/CD specialist agent.",
					ProposedPath:    ".opencode/agents/delivery-devops-specialist.md",
					ProposedContent: ocContent,
					Confidence:      0.85,
					Target:          "opencode",
					Signals:         []string{"pattern:docker", "ci:" + profile.CIProvider},
				})
			}
		}
	}

	// --- Test framework skill ---
	switch profile.TestStructure.Framework {
	case "jest":
		skillData := data
		skillData.Framework = "jest"
		skillData.TestCmd = "npx jest"
		if content, err := RenderTemplate(runTestsSkill, skillData); err == nil {
			proposals = append(proposals, AssetProposal{
				Type:            "asset.skill.cc",
				Title:           "Run Tests (jest)",
				Description:     "Run the jest test suite and report results.",
				ProposedPath:    ".claude/skills/run-tests/SKILL.md",
				ProposedContent: content,
				Confidence:      0.85,
				Target:          "claude-code",
				Signals:         []string{"test-framework:jest"},
			})
		}

	case "pytest":
		skillData := data
		skillData.Framework = "pytest"
		skillData.TestCmd = "pytest"
		if content, err := RenderTemplate(runTestsSkill, skillData); err == nil {
			proposals = append(proposals, AssetProposal{
				Type:            "asset.skill.cc",
				Title:           "Run Tests (pytest)",
				Description:     "Run the pytest test suite and report results.",
				ProposedPath:    ".claude/skills/run-tests/SKILL.md",
				ProposedContent: content,
				Confidence:      0.85,
				Target:          "claude-code",
				Signals:         []string{"test-framework:pytest"},
			})
		}

	case "go-test":
		skillData := data
		skillData.Framework = "go test"
		skillData.TestCmd = "go test ./..."
		if content, err := RenderTemplate(runTestsSkill, skillData); err == nil {
			proposals = append(proposals, AssetProposal{
				Type:            "asset.skill.cc",
				Title:           "Run Tests (go test)",
				Description:     "Run the Go test suite and report results.",
				ProposedPath:    ".claude/skills/run-tests/SKILL.md",
				ProposedContent: content,
				Confidence:      0.85,
				Target:          "claude-code",
				Signals:         []string{"test-framework:go-test"},
			})
		}
	}

	return DeduplicateProposals(proposals, projectRoot, existingPaths)
}

// buildTemplateData builds TemplateData from a ProjectProfile, detecting
// build and test commands from config files.
func buildTemplateData(profile *ProjectProfile) TemplateData {
	data := TemplateData{
		ProjectName: profile.ProjectName,
	}

	// Detect primary language for Framework field
	if len(profile.Languages) > 0 {
		data.Language = profile.Languages[0].Language
	}

	// Derive BuildCmd and TestCmd from known config files
	configTypes := make(map[string]bool)
	for _, cf := range profile.ConfigFiles {
		configTypes[cf.Type] = true
	}

	switch {
	case configTypes["go-module"]:
		data.BuildCmd = "go build ./..."
		data.TestCmd = "go test ./..."
	case configTypes["npm"]:
		data.BuildCmd = "npm run build"
		data.TestCmd = "npm test"
	case configTypes["python"]:
		data.BuildCmd = ""
		data.TestCmd = "pytest"
	}

	// Test framework overrides TestCmd
	switch profile.TestStructure.Framework {
	case "jest":
		data.Framework = "jest"
		data.TestCmd = "npx jest"
	case "pytest":
		data.Framework = "pytest"
		data.TestCmd = "pytest"
	case "go-test":
		data.Framework = "go test"
		data.TestCmd = "go test ./..."
	}

	// Detect framework label for web-app
	if data.Framework == "" {
		for _, lang := range profile.Languages {
			if lang.Language == "TypeScript" || lang.Language == "JavaScript" {
				data.Framework = "React/TypeScript"
				break
			}
		}
		if data.Framework == "" && len(profile.Languages) > 0 {
			data.Framework = profile.Languages[0].Language
		}
	}

	return data
}
