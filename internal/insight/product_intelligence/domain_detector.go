package product_intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type DomainDetector struct {
	config DomainDetectorConfig
	llm    llm.Client
}

type DomainDetectorConfig struct {
	ReadmeWeight       float64 `json:"readme_weight"`
	APIRoutesWeight    float64 `json:"api_routes_weight"`
	SchemaWeight       float64 `json:"schema_weight"`
	DependenciesWeight float64 `json:"dependencies_weight"`
	MinConfidence      float64 `json:"min_confidence"`
	MaxFilesToScan     int     `json:"max_files_to_scan"`
	MaxFileSizeKB      int64   `json:"max_file_size_kb"`
}

func DefaultDomainDetectorConfig() DomainDetectorConfig {
	return DomainDetectorConfig{
		ReadmeWeight:       0.35,
		APIRoutesWeight:    0.25,
		SchemaWeight:       0.20,
		DependenciesWeight: 0.20,
		MinConfidence:      0.4,
		MaxFilesToScan:     1000,
		MaxFileSizeKB:      1024,
	}
}

func NewDomainDetector(config DomainDetectorConfig, llmClient llm.Client) *DomainDetector {
	return &DomainDetector{config: config, llm: llmClient}
}

type domainSignal struct {
	domain     string
	confidence float64
	source     string
	keywords   []string
}

func (d *DomainDetector) Detect(ctx context.Context, projectPath string) (*DomainResult, error) {
	if d.llm != nil {
		result, err := d.detectWithLLM(ctx, projectPath)
		if err == nil && result != nil && result.Confidence >= d.config.MinConfidence {
			return result, nil
		}
		if err != nil {
			slog.Warn("domain_detector: LLM detection failed, falling back to rule-based", "error", err)
		}
	}

	signals := []domainSignal{}

	readmeSignal := d.analyzeReadme(projectPath)
	if readmeSignal != nil {
		signals = append(signals, *readmeSignal)
	}

	apiSignal := d.analyzeAPIRoutes(projectPath)
	if apiSignal != nil {
		signals = append(signals, *apiSignal)
	}

	schemaSignal := d.analyzeDatabaseSchema(projectPath)
	if schemaSignal != nil {
		signals = append(signals, *schemaSignal)
	}

	depsSignal := d.analyzeDependencies(projectPath)
	if depsSignal != nil {
		signals = append(signals, *depsSignal)
	}

	if len(signals) == 0 {
		return &DomainResult{
			Domain:     "unknown",
			Confidence: 0,
			Keywords:   []string{},
		}, nil
	}

	return d.consolidateSignals(signals), nil
}

type llmDomainResponse struct {
	Domain     string   `json:"domain"`
	Confidence float64  `json:"confidence"`
	Keywords   []string `json:"keywords"`
	Industry   string   `json:"industry,omitempty"`
	Subdomain  string   `json:"subdomain,omitempty"`
}

func (d *DomainDetector) detectWithLLM(ctx context.Context, projectPath string) (*DomainResult, error) {
	readmeContent := d.readReadme(projectPath)
	apiRoutes := d.extractAPIRoutes(projectPath)
	deps := d.extractDependencies(projectPath)

	prompt := fmt.Sprintf(`Analyze this software project and determine its primary domain.

Project information:
- README content (first 2000 chars):
%s

- API routes (sample): %v
- Dependencies (sample): %v

Respond with JSON in this exact format:
{
  "domain": "one of: ecommerce, crm, project_management, helpdesk, communication, analytics, dev_tools, audit_management, ai_assistant, healthcare, fintech, education_lms, or unknown",
  "confidence": 0.0-1.0,
  "keywords": ["list", "of", "relevant", "keywords"],
  "industry": "optional industry classification",
  "subdomain": "optional subdomain if applicable"
}

Only respond with the JSON object, no additional text.`,
		truncate(readmeContent, 2000),
		takeFirst(apiRoutes, 20),
		takeFirst(deps, 20))

	resp, err := d.llm.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: "You are an expert software architect. Analyze codebases and identify their domain with high accuracy. Always respond with valid JSON.",
		Messages:     []llm.Message{llm.UserMessage(prompt)},
		MaxTokens:    500,
		Temperature:  0.3,
	})
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}

	var llmResp llmDomainResponse
	if err := llm.ParseJSONResponse(resp.Content, &llmResp); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	return &DomainResult{
		Domain:     llmResp.Domain,
		Confidence: llmResp.Confidence,
		Keywords:   llmResp.Keywords,
		Industry:   llmResp.Industry,
		Subdomain:  llmResp.Subdomain,
	}, nil
}

func (d *DomainDetector) readReadme(projectPath string) string {
	readmePath := d.findReadme(projectPath)
	if readmePath == "" {
		return ""
	}
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return ""
	}
	return string(content)
}

func (d *DomainDetector) analyzeReadme(projectPath string) *domainSignal {
	readmePath := d.findReadme(projectPath)
	if readmePath == "" {
		return nil
	}

	content, err := os.ReadFile(readmePath)
	if err != nil {
		return nil
	}

	text := strings.ToLower(string(content))
	keywords := d.extractKeywords(text)
	domain := d.inferDomainFromKeywords(keywords)

	confidence := 0.5
	if len(keywords) > 10 {
		confidence = 0.7
	}
	if len(keywords) > 20 {
		confidence = 0.85
	}

	return &domainSignal{
		domain:     domain,
		confidence: confidence * d.config.ReadmeWeight,
		source:     "readme",
		keywords:   keywords,
	}
}

func (d *DomainDetector) analyzeAPIRoutes(projectPath string) *domainSignal {
	routes := d.extractAPIRoutes(projectPath)
	if len(routes) == 0 {
		return nil
	}

	keywords := []string{}
	for _, route := range routes {
		parts := strings.Split(route, "/")
		for _, part := range parts {
			if part != "" && !strings.HasPrefix(part, ":") && !strings.HasPrefix(part, "{") {
				keywords = append(keywords, strings.ToLower(part))
			}
		}
	}

	keywords = uniqueStrings(keywords)
	domain := d.inferDomainFromKeywords(keywords)

	confidence := 0.3 + float64(minInt(len(routes), 20))*0.03

	return &domainSignal{
		domain:     domain,
		confidence: min(confidence, 0.8) * d.config.APIRoutesWeight,
		source:     "api_routes",
		keywords:   keywords,
	}
}

func (d *DomainDetector) analyzeDatabaseSchema(projectPath string) *domainSignal {
	tables := d.extractDatabaseTables(projectPath)
	if len(tables) == 0 {
		return nil
	}

	keywords := []string{}
	for _, table := range tables {
		name := strings.ToLower(table)
		name = strings.TrimSuffix(name, "s")
		keywords = append(keywords, name)
	}

	keywords = uniqueStrings(keywords)
	domain := d.inferDomainFromKeywords(keywords)

	confidence := 0.4 + float64(minInt(len(tables), 15))*0.025

	return &domainSignal{
		domain:     domain,
		confidence: min(confidence, 0.75) * d.config.SchemaWeight,
		source:     "database_schema",
		keywords:   keywords,
	}
}

func (d *DomainDetector) analyzeDependencies(projectPath string) *domainSignal {
	deps := d.extractDependencies(projectPath)
	if len(deps) == 0 {
		return nil
	}

	keywords := []string{}
	techIndicators := []string{}

	for _, dep := range deps {
		depLower := strings.ToLower(dep)

		switch {
		case strings.Contains(depLower, "auth") || strings.Contains(depLower, "jwt") || strings.Contains(depLower, "oauth"):
			techIndicators = append(techIndicators, "authentication")
		case strings.Contains(depLower, "payment") || strings.Contains(depLower, "stripe") || strings.Contains(depLower, "paypal"):
			techIndicators = append(techIndicators, "payment_processing")
		case strings.Contains(depLower, "chat") || strings.Contains(depLower, "socket") || strings.Contains(depLower, "websocket"):
			techIndicators = append(techIndicators, "real_time_communication")
		case strings.Contains(depLower, "analytics") || strings.Contains(depLower, "tracking") || strings.Contains(depLower, "telemetry"):
			techIndicators = append(techIndicators, "analytics")
		case strings.Contains(depLower, "ml") || strings.Contains(depLower, "ai") || strings.Contains(depLower, "tensorflow") || strings.Contains(depLower, "pytorch"):
			techIndicators = append(techIndicators, "machine_learning")
		case strings.Contains(depLower, "email") || strings.Contains(depLower, "mail"):
			techIndicators = append(techIndicators, "email_management")
		case strings.Contains(depLower, "pdf") || strings.Contains(depLower, "document") || strings.Contains(depLower, "report"):
			techIndicators = append(techIndicators, "document_management")
		}

		parts := strings.Split(dep, "/")
		if len(parts) > 0 {
			keywords = append(keywords, strings.ToLower(parts[len(parts)-1]))
		}
	}

	keywords = append(keywords, techIndicators...)
	keywords = uniqueStrings(keywords)

	domain := "software_platform"
	if len(techIndicators) > 0 {
		domain = techIndicators[0]
	}

	confidence := 0.3 + float64(minInt(len(techIndicators), 5))*0.1

	return &domainSignal{
		domain:     domain,
		confidence: min(confidence, 0.6) * d.config.DependenciesWeight,
		source:     "dependencies",
		keywords:   keywords,
	}
}

func (d *DomainDetector) findReadme(projectPath string) string {
	candidates := []string{"README.md", "readme.md", "README", "readme", "README.txt", "readme.txt"}
	for _, name := range candidates {
		path := filepath.Join(projectPath, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func (d *DomainDetector) extractKeywords(text string) []string {
	wordRegex := regexp.MustCompile(`[a-z]{3,}`)
	words := wordRegex.FindAllString(text, -1)

	stopwords := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "had": true,
		"her": true, "was": true, "one": true, "our": true, "out": true,
		"has": true, "have": true, "been": true, "will": true, "your": true,
		"from": true, "they": true, "this": true, "that": true, "with": true,
		"what": true, "when": true, "where": true, "which": true, "while": true,
		"about": true, "after": true, "before": true, "between": true, "into": true,
		"through": true, "during": true, "above": true, "below": true, "under": true,
		"again": true, "further": true, "then": true, "once": true, "here": true,
		"there": true, "should": true, "would": true, "could": true, "being": true,
		"over": true, "just": true, "more": true, "some": true, "such": true,
		"only": true, "also": true, "than": true, "too": true, "very": true,
		"use": true, "using": true, "used": true, "may": true,
	}

	filtered := []string{}
	for _, word := range words {
		if !stopwords[word] && len(word) > 3 {
			filtered = append(filtered, word)
		}
	}

	wordCount := make(map[string]int)
	for _, word := range filtered {
		wordCount[word]++
	}

	type wordFreq struct {
		word  string
		count int
	}
	var freqs []wordFreq
	for w, c := range wordCount {
		freqs = append(freqs, wordFreq{w, c})
	}

	for i := 0; i < len(freqs); i++ {
		for j := i + 1; j < len(freqs); j++ {
			if freqs[j].count > freqs[i].count {
				freqs[i], freqs[j] = freqs[j], freqs[i]
			}
		}
	}

	result := []string{}
	for i := 0; i < minInt(len(freqs), 30); i++ {
		result = append(result, freqs[i].word)
	}

	return result
}

var (
	goRouteRegex      = regexp.MustCompile(`(HandleFunc|Handle|GET|POST|PUT|DELETE|PATCH)\s*\(\s*["']([^"']+)["']`)
	jsRouteRegex      = regexp.MustCompile(`(router|app)\.(get|post|put|delete|patch)\s*\([^)]*["']([^"']+)["']`)
	quoteExtractRegex = regexp.MustCompile(`["']([^"']+)["']`)
)

func (d *DomainDetector) extractAPIRoutes(projectPath string) []string {
	routes := []string{}
	filesScanned := 0
	maxFiles := d.config.MaxFilesToScan
	if maxFiles <= 0 {
		maxFiles = 1000
	}
	maxSize := d.config.MaxFileSizeKB * 1024
	if maxSize <= 0 {
		maxSize = 1024 * 1024
	}

	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		if filesScanned >= maxFiles {
			return nil
		}

		if info.Size() > maxSize {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		text := string(content)
		var matches [][]string

		switch ext {
		case ".go":
			matches = goRouteRegex.FindAllStringSubmatch(text, -1)
		case ".ts", ".js":
			matches = jsRouteRegex.FindAllStringSubmatch(text, -1)
		default:
			return nil
		}

		for _, match := range matches {
			var route string
			if len(match) > 2 {
				route = match[2]
			} else if len(match) > 1 {
				parts := quoteExtractRegex.FindAllStringSubmatch(match[0], -1)
				if len(parts) > 0 && len(parts[0]) > 1 {
					route = parts[0][1]
				}
			}
			if route != "" && !strings.HasPrefix(route, "/static") && !strings.HasPrefix(route, "/assets") {
				routes = append(routes, route)
			}
		}

		filesScanned++
		if filesScanned%100 == 0 {
			slog.Debug("domain_detector: scanned files", "count", filesScanned)
		}
		return nil
	})

	return uniqueStrings(routes)
}

func (d *DomainDetector) extractDatabaseTables(projectPath string) []string {
	tables := []string{}

	tableRegex := regexp.MustCompile(`(?i)(CREATE\s+TABLE|table|collection)\s+[IF\s+NOT\s+EXISTS\s+]?["']?([a-zA-Z_][a-zA-Z0-9_]*)["']?`)

	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".sql" || ext == ".go" || ext == ".ts" || ext == ".js" || ext == ".prisma" {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			matches := tableRegex.FindAllStringSubmatch(string(content), -1)
			for _, match := range matches {
				if len(match) > 2 {
					tableName := strings.ToLower(match[2])
					if !isSystemTable(tableName) {
						tables = append(tables, tableName)
					}
				}
			}
		}
		return nil
	})

	return uniqueStrings(tables)
}

func (d *DomainDetector) extractDependencies(projectPath string) []string {
	deps := []string{}

	goModPath := filepath.Join(projectPath, "go.mod")
	if content, err := os.ReadFile(goModPath); err == nil {
		lines := strings.Split(string(content), "\n")
		inRequire := false
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "require (" {
				inRequire = true
				continue
			}
			if inRequire && line == ")" {
				inRequire = false
				continue
			}
			if inRequire || strings.HasPrefix(line, "require ") {
				parts := strings.Fields(line)
				if len(parts) >= 1 && !strings.HasPrefix(parts[0], "//") {
					deps = append(deps, parts[0])
				}
			}
		}
	}

	packageJSONPath := filepath.Join(projectPath, "package.json")
	if content, err := os.ReadFile(packageJSONPath); err == nil {
		var pkg struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if err := json.Unmarshal(content, &pkg); err == nil {
			for name := range pkg.Dependencies {
				deps = append(deps, name)
			}
			for name := range pkg.DevDependencies {
				deps = append(deps, name)
			}
		}
	}

	cargoTomlPath := filepath.Join(projectPath, "Cargo.toml")
	if content, err := os.ReadFile(cargoTomlPath); err == nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "name = ") {
				name := strings.Trim(strings.TrimPrefix(line, "name = "), "\"")
				deps = append(deps, name)
			}
		}
	}

	return uniqueStrings(deps)
}

func (d *DomainDetector) inferDomainFromKeywords(keywords []string) string {
	domainPatterns := map[string][]string{
		"audit_management":   {"audit", "compliance", "checklist", "inspection", "regulatory", "certification"},
		"ecommerce":          {"cart", "checkout", "order", "product", "inventory", "payment", "shipping"},
		"crm":                {"customer", "lead", "sales", "pipeline", "contact", "deal", "prospect"},
		"hr_management":      {"employee", "payroll", "attendance", "leave", "recruitment", "onboarding"},
		"project_management": {"task", "project", "milestone", "sprint", "kanban", "backlog", "epic"},
		"education_lms":      {"course", "lesson", "student", "enrollment", "grade", "curriculum", "learning"},
		"healthcare":         {"patient", "appointment", "diagnosis", "prescription", "medical", "health"},
		"finance":            {"transaction", "account", "balance", "invoice", "expense", "budget", "ledger"},
		"real_estate":        {"property", "listing", "rental", "lease", "tenant", "listing", "broker"},
		"restaurant_pos":     {"menu", "order", "table", "reservation", "kitchen", "restaurant"},
		"logistics":          {"shipment", "tracking", "delivery", "warehouse", "fleet", "route"},
		"social_media":       {"post", "comment", "like", "follow", "feed", "message", "notification"},
		"content_management": {"article", "page", "media", "author", "category", "tag", "publish"},
		"helpdesk":           {"ticket", "support", "issue", "agent", "response", "sla", "knowledge"},
		"dev_tools":          {"code", "repository", "commit", "branch", "merge", "pull", "pipeline"},
		"communication":      {"chat", "message", "channel", "team", "workspace", "thread", "mention"},
		"analytics":          {"dashboard", "report", "metric", "chart", "visualization", "data", "insight"},
		"iot_platform":       {"device", "sensor", "telemetry", "gateway", "firmware", "iot"},
		"booking_system":     {"booking", "reservation", "slot", "availability", "calendar", "schedule"},
		"ai_assistant":       {"assistant", "chatbot", "nlp", "conversation", "ai", "llm", "prompt"},
	}

	keywordSet := make(map[string]bool)
	for _, kw := range keywords {
		keywordSet[strings.ToLower(kw)] = true
	}

	bestDomain := "unknown"
	bestScore := 0

	for domain, patterns := range domainPatterns {
		score := 0
		for _, pattern := range patterns {
			if keywordSet[pattern] {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestDomain = domain
		}
	}

	return bestDomain
}

func (d *DomainDetector) consolidateSignals(signals []domainSignal) *DomainResult {
	domainScores := make(map[string]float64)
	allKeywords := []string{}

	for _, signal := range signals {
		domainScores[signal.domain] += signal.confidence
		allKeywords = append(allKeywords, signal.keywords...)
	}

	bestDomain := "unknown"
	bestScore := 0.0
	for domain, score := range domainScores {
		if score > bestScore {
			bestScore = score
			bestDomain = domain
		}
	}

	confidence := min(bestScore, 1.0)
	if confidence < d.config.MinConfidence {
		confidence = d.config.MinConfidence * 0.5
	}

	return &DomainResult{
		Domain:     bestDomain,
		Confidence: confidence,
		Keywords:   uniqueStrings(allKeywords),
	}
}

func (d *DomainDetector) GetReadmeHash(projectPath string) string {
	readmePath := d.findReadme(projectPath)
	if readmePath == "" {
		return ""
	}
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func isSystemTable(name string) bool {
	systemPrefixes := []string{"sqlite_", "pg_", "information_", "sys_", "_"}
	for _, prefix := range systemPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func uniqueStrings(s []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, str := range s {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}
	return result
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func takeFirst[T any](slice []T, n int) []T {
	if len(slice) <= n {
		return slice
	}
	return slice[:n]
}
