package product_intelligence

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MartinNevlaha/stratus-v2/internal/insight/llm"
)

type FeatureExtractor struct {
	config FeatureExtractorConfig
	llm    llm.Client
}

type FeatureExtractorConfig struct {
	MinConfidence float64 `json:"min_confidence"`
	MaxFeatures   int     `json:"max_features"`
}

func DefaultFeatureExtractorConfig() FeatureExtractorConfig {
	return FeatureExtractorConfig{
		MinConfidence: 0.4,
		MaxFeatures:   100,
	}
}

func NewFeatureExtractor(config FeatureExtractorConfig, llmClient llm.Client) *FeatureExtractor {
	return &FeatureExtractor{config: config, llm: llmClient}
}

type featureSignal struct {
	name        string
	featureType FeatureType
	description string
	confidence  float64
	source      string
	evidence    map[string]any
}

func (e *FeatureExtractor) Extract(ctx context.Context, projectPath, projectID string) ([]ProjectFeature, error) {
	if e.llm != nil {
		features, err := e.extractWithLLM(ctx, projectPath, projectID)
		if err == nil && len(features) > 0 {
			if len(features) > e.config.MaxFeatures {
				features = features[:e.config.MaxFeatures]
			}
			return features, nil
		}
		if err != nil {
			slog.Warn("feature_extractor: LLM extraction failed, falling back to rule-based", "error", err)
		}
	}

	signals := []featureSignal{}

	apiFeatures := e.extractAPIFeatures(projectPath)
	signals = append(signals, apiFeatures...)

	schemaFeatures := e.extractSchemaFeatures(projectPath)
	signals = append(signals, schemaFeatures...)

	uiFeatures := e.extractUIFeatures(projectPath)
	signals = append(signals, uiFeatures...)

	authFeatures := e.extractAuthFeatures(projectPath)
	signals = append(signals, authFeatures...)

	configFeatures := e.extractConfigFeatures(projectPath)
	signals = append(signals, configFeatures...)

	features := e.consolidateFeatures(signals, projectID)

	if len(features) > e.config.MaxFeatures {
		features = features[:e.config.MaxFeatures]
	}

	return features, nil
}

type llmFeatureResponse struct {
	FeatureName string                 `json:"feature_name"`
	FeatureType string                 `json:"feature_type"`
	Description string                 `json:"description"`
	Confidence  float64                `json:"confidence"`
	Evidence    map[string]interface{} `json:"evidence"`
}

func (e *FeatureExtractor) extractWithLLM(ctx context.Context, projectPath, projectID string) ([]ProjectFeature, error) {
	readmeContent := e.readReadme(projectPath)
	apiRoutes := e.extractAPIRoutes(projectPath)
	schemaInfo := e.extractSchemaInfo(projectPath)
	configFiles := e.extractConfigInfo(projectPath)

	prompt := fmt.Sprintf(`Analyze this software project and extract its features.

Project information:
- README (first 1500 chars): %s
- API routes (sample): %v
- Database schema: %s
- Config files: %s

Identify all implemented features. Respond with a JSON array:
[
  {
    "feature_name": "descriptive_snake_case_name",
    "feature_type": "one of: capability, module, api, ui, integration, auth, analytics, reporting",
    "description": "brief description of the feature",
    "confidence": 0.0-1.0,
    "evidence": {"source": "where found", "details": "relevant details"}
  }
]

Only respond with the JSON array, no additional text.`,
		truncate(readmeContent, 1500),
		takeFirst(apiRoutes, 15),
		truncate(schemaInfo, 500),
		truncate(configFiles, 500))

	resp, err := e.llm.Complete(ctx, llm.CompletionRequest{
		SystemPrompt:   "You are an expert software analyst. Extract features from codebases with high accuracy. Always respond with valid JSON.",
		Messages:       []llm.Message{llm.UserMessage(prompt)},
		MaxTokens:      8192,
		Temperature:    0.3,
		ResponseFormat: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}

	var llmFeatures []llmFeatureResponse
	if err := llm.ParseJSONResponse(resp.Content, &llmFeatures); err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	features := make([]ProjectFeature, 0, len(llmFeatures))
	now := time.Now().UTC().Format(time.RFC3339Nano)

	for _, lf := range llmFeatures {
		if lf.Confidence < e.config.MinConfidence {
			continue
		}
		features = append(features, ProjectFeature{
			ID:          generateFeatureID(projectID, lf.FeatureName),
			ProjectID:   projectID,
			FeatureName: lf.FeatureName,
			FeatureType: FeatureType(lf.FeatureType),
			Description: lf.Description,
			Evidence:    lf.Evidence,
			Confidence:  lf.Confidence,
			Source:      "llm_analysis",
			DetectedAt:  now,
		})
	}

	return features, nil
}

func (e *FeatureExtractor) readReadme(projectPath string) string {
	paths := []string{
		filepath.Join(projectPath, "README.md"),
		filepath.Join(projectPath, "readme.md"),
		filepath.Join(projectPath, "README"),
	}
	for _, p := range paths {
		content, err := os.ReadFile(p)
		if err == nil {
			return string(content)
		}
	}
	return ""
}

func (e *FeatureExtractor) extractAPIRoutes(projectPath string) []string {
	return e.getFilePaths(projectPath, []string{".go", ".ts", ".js", ".py"}, 50)
}

func (e *FeatureExtractor) extractSchemaInfo(projectPath string) string {
	schemaFiles := e.getFilePaths(projectPath, []string{".sql", ".prisma", ".graphql"}, 10)
	return strings.Join(schemaFiles, ", ")
}

func (e *FeatureExtractor) extractConfigInfo(projectPath string) string {
	configFiles := e.getFilePaths(projectPath, []string{".json", ".yaml", ".yml", ".toml"}, 10)
	return strings.Join(configFiles, ", ")
}

func (e *FeatureExtractor) getFilePaths(projectPath string, extensions []string, limit int) []string {
	var files []string
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || len(files) >= limit {
			return nil
		}
		if !info.IsDir() {
			for _, ext := range extensions {
				if strings.HasSuffix(path, ext) {
					rel, _ := filepath.Rel(projectPath, path)
					files = append(files, rel)
					break
				}
			}
		}
		return nil
	})
	return files
}

func (e *FeatureExtractor) extractAPIFeatures(projectPath string) []featureSignal {
	signals := []featureSignal{}

	apiPatterns := []struct {
		pattern     *regexp.Regexp
		featureName string
		featureType FeatureType
		description string
	}{
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(user|users|account|profile)`),
			featureName: "user_management",
			featureType: FeatureTypeCapability,
			description: "User management and profile handling",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(auth|login|logout|register|signup|signin)`),
			featureName: "authentication",
			featureType: FeatureTypeAuth,
			description: "User authentication and authorization",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(report|reports|analytics|dashboard|metrics)`),
			featureName: "reporting_analytics",
			featureType: FeatureTypeAnalytics,
			description: "Reporting and analytics capabilities",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(upload|file|document|attachment|media)`),
			featureName: "file_management",
			featureType: FeatureTypeCapability,
			description: "File upload and management",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(search|query|filter)`),
			featureName: "search",
			featureType: FeatureTypeCapability,
			description: "Search and filtering functionality",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(notification|notify|alert|email|sms)`),
			featureName: "notifications",
			featureType: FeatureTypeCapability,
			description: "Notification and alerting system",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(export|import|csv|excel|pdf)`),
			featureName: "data_export_import",
			featureType: FeatureTypeCapability,
			description: "Data export and import functionality",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(webhook|callback|integration)`),
			featureName: "integrations",
			featureType: FeatureTypeIntegration,
			description: "External integrations and webhooks",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(settings|config|preference)`),
			featureName: "settings",
			featureType: FeatureTypeCapability,
			description: "User and system settings",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(audit|log|history|activity)`),
			featureName: "audit_logging",
			featureType: FeatureTypeCapability,
			description: "Audit logging and activity tracking",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(role|permission|access|acl)`),
			featureName: "role_based_access",
			featureType: FeatureTypeAuth,
			description: "Role-based access control",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(workflow|process|approval)`),
			featureName: "workflow_management",
			featureType: FeatureTypeCapability,
			description: "Workflow and process management",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(task|todo|checklist)`),
			featureName: "task_management",
			featureType: FeatureTypeCapability,
			description: "Task and todo management",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(comment|message|chat|conversation)`),
			featureName: "messaging",
			featureType: FeatureTypeCapability,
			description: "Messaging and communication",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(/api/|/v1/|/v2/).*?(schedule|calendar|event|booking)`),
			featureName: "scheduling",
			featureType: FeatureTypeCapability,
			description: "Scheduling and calendar functionality",
		},
	}

	routeFiles := []string{}
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".go" || ext == ".ts" || ext == ".js" || ext == ".py" {
			if strings.Contains(strings.ToLower(path), "route") ||
				strings.Contains(strings.ToLower(path), "handler") ||
				strings.Contains(strings.ToLower(path), "controller") ||
				strings.Contains(strings.ToLower(path), "api") {
				routeFiles = append(routeFiles, path)
			}
		}
		return nil
	})

	matchedFeatures := make(map[string]int)
	for _, file := range routeFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		text := string(content)

		for _, p := range apiPatterns {
			if p.pattern.MatchString(text) {
				count := matchedFeatures[p.featureName]
				matchedFeatures[p.featureName] = count + 1
			}
		}
	}

	for featureName, count := range matchedFeatures {
		for _, p := range apiPatterns {
			if p.featureName == featureName {
				confidence := 0.5 + min(float64(count)*0.1, 0.4)
				signals = append(signals, featureSignal{
					name:        p.featureName,
					featureType: p.featureType,
					description: p.description,
					confidence:  confidence,
					source:      "api_analysis",
					evidence: map[string]any{
						"match_count": count,
						"files":       routeFiles,
					},
				})
				break
			}
		}
	}

	return signals
}

func (e *FeatureExtractor) extractSchemaFeatures(projectPath string) []featureSignal {
	signals := []featureSignal{}

	schemaPatterns := []struct {
		tablePattern *regexp.Regexp
		featureName  string
		featureType  FeatureType
		description  string
	}{
		{
			tablePattern: regexp.MustCompile(`(?i)(user|account|profile|member)`),
			featureName:  "user_accounts",
			featureType:  FeatureTypeCapability,
			description:  "User account management",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(product|item|catalog|inventory|sku)`),
			featureName:  "product_catalog",
			featureType:  FeatureTypeCapability,
			description:  "Product or item catalog",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(order|purchase|cart|checkout|transaction)`),
			featureName:  "order_management",
			featureType:  FeatureTypeCapability,
			description:  "Order and transaction management",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(payment|invoice|billing|subscription)`),
			featureName:  "billing_payments",
			featureType:  FeatureTypeCapability,
			description:  "Billing and payment processing",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(audit|log|event|activity|history)`),
			featureName:  "audit_trail",
			featureType:  FeatureTypeCapability,
			description:  "Audit trail and logging",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(role|permission|access|privilege)`),
			featureName:  "permissions",
			featureType:  FeatureTypeAuth,
			description:  "Permissions and access control",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(notification|alert|message|email)`),
			featureName:  "notifications_system",
			featureType:  FeatureTypeCapability,
			description:  "Notification system",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(tag|category|label|classification)`),
			featureName:  "tagging_categorization",
			featureType:  FeatureTypeCapability,
			description:  "Tagging and categorization",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(comment|review|rating|feedback)`),
			featureName:  "feedback_reviews",
			featureType:  FeatureTypeCapability,
			description:  "Feedback and review system",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(session|token|api_key|credential)`),
			featureName:  "session_management",
			featureType:  FeatureTypeAuth,
			description:  "Session and token management",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(tenant|organization|team|workspace)`),
			featureName:  "multi_tenant",
			featureType:  FeatureTypeCapability,
			description:  "Multi-tenant or organization support",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(workflow|process|pipeline|stage)`),
			featureName:  "workflow_engine",
			featureType:  FeatureTypeCapability,
			description:  "Workflow or process engine",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(report|dashboard|metric|statistic)`),
			featureName:  "reporting_dashboard",
			featureType:  FeatureTypeReporting,
			description:  "Reporting and dashboards",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(checklist|audit_item|inspection|compliance)`),
			featureName:  "checklist_audit",
			featureType:  FeatureTypeCapability,
			description:  "Checklist and audit management",
		},
		{
			tablePattern: regexp.MustCompile(`(?i)(supplier|vendor|partner|external)`),
			featureName:  "vendor_management",
			featureType:  FeatureTypeCapability,
			description:  "Vendor and supplier management",
		},
	}

	tables := []string{}
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".sql" || ext == ".prisma" {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			tableRegex := regexp.MustCompile(`(?i)(?:CREATE\s+TABLE|table)\s+[IF\s+NOT\s+EXISTS\s+]?["']?([a-zA-Z_][a-zA-Z0-9_]*)`)
			matches := tableRegex.FindAllStringSubmatch(string(content), -1)
			for _, match := range matches {
				if len(match) > 1 {
					tables = append(tables, match[1])
				}
			}
		}
		return nil
	})

	for _, sp := range schemaPatterns {
		matchedTables := []string{}
		for _, table := range tables {
			if sp.tablePattern.MatchString(table) {
				matchedTables = append(matchedTables, table)
			}
		}
		if len(matchedTables) > 0 {
			confidence := 0.6 + min(float64(len(matchedTables))*0.08, 0.35)
			signals = append(signals, featureSignal{
				name:        sp.featureName,
				featureType: sp.featureType,
				description: sp.description,
				confidence:  confidence,
				source:      "schema_analysis",
				evidence: map[string]any{
					"matched_tables": matchedTables,
				},
			})
		}
	}

	return signals
}

func (e *FeatureExtractor) extractUIFeatures(projectPath string) []featureSignal {
	signals := []featureSignal{}

	uiPatterns := []struct {
		componentPattern *regexp.Regexp
		featureName      string
		featureType      FeatureType
		description      string
	}{
		{
			componentPattern: regexp.MustCompile(`(?i)(Dashboard|Analytics|Chart|Graph|Metrics)`),
			featureName:      "dashboard",
			featureType:      FeatureTypeUI,
			description:      "Dashboard and visualization",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Form|Wizard|Stepper|MultiStep)`),
			featureName:      "forms",
			featureType:      FeatureTypeUI,
			description:      "Form handling and wizards",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Table|Grid|DataGrid|DataTable|List)`),
			featureName:      "data_tables",
			featureType:      FeatureTypeUI,
			description:      "Data tables and grids",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Calendar|Scheduler|DatePicker|TimePicker)`),
			featureName:      "calendar_scheduling",
			featureType:      FeatureTypeUI,
			description:      "Calendar and scheduling UI",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Map|Location|Geo|Place)`),
			featureName:      "maps_location",
			featureType:      FeatureTypeUI,
			description:      "Maps and location features",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Chat|Message|Conversation|Thread)`),
			featureName:      "chat_interface",
			featureType:      FeatureTypeUI,
			description:      "Chat and messaging interface",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Upload|Dropzone|FilePicker|Attachment)`),
			featureName:      "file_upload",
			featureType:      FeatureTypeUI,
			description:      "File upload interface",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Editor|RichText|Wysiwyg|Markdown)`),
			featureName:      "rich_text_editor",
			featureType:      FeatureTypeUI,
			description:      "Rich text editing",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Notification|Toast|Alert|Snackbar)`),
			featureName:      "notifications_ui",
			featureType:      FeatureTypeUI,
			description:      "Notification UI components",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Search|Autocomplete|Typeahead|Filter)`),
			featureName:      "search_ui",
			featureType:      FeatureTypeUI,
			description:      "Search interface",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Print|Export|Download|PDF)`),
			featureName:      "export_print",
			featureType:      FeatureTypeUI,
			description:      "Export and print functionality",
		},
		{
			componentPattern: regexp.MustCompile(`(?i)(Mobile|Responsive|Drawer|Sidebar)`),
			featureName:      "mobile_responsive",
			featureType:      FeatureTypeUI,
			description:      "Mobile responsive design",
		},
	}

	componentFiles := []string{}
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".tsx" || ext == ".jsx" || ext == ".vue" || ext == ".svelte" || ext == ".html" {
			componentFiles = append(componentFiles, path)
		}
		return nil
	})

	matchedComponents := make(map[string]int)
	for _, file := range componentFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		text := string(content)

		for _, p := range uiPatterns {
			if p.componentPattern.MatchString(text) {
				count := matchedComponents[p.featureName]
				matchedComponents[p.featureName] = count + 1
			}
		}
	}

	for featureName, count := range matchedComponents {
		for _, p := range uiPatterns {
			if p.featureName == featureName {
				confidence := 0.5 + min(float64(count)*0.05, 0.4)
				signals = append(signals, featureSignal{
					name:        p.featureName,
					featureType: p.featureType,
					description: p.description,
					confidence:  confidence,
					source:      "ui_analysis",
					evidence: map[string]any{
						"component_count": count,
					},
				})
				break
			}
		}
	}

	return signals
}

func (e *FeatureExtractor) extractAuthFeatures(projectPath string) []featureSignal {
	signals := []featureSignal{}

	authPatterns := []struct {
		pattern     *regexp.Regexp
		featureName string
		featureType FeatureType
		description string
	}{
		{
			pattern:     regexp.MustCompile(`(?i)(jwt|jsonwebtoken|bearer)`),
			featureName: "jwt_auth",
			featureType: FeatureTypeAuth,
			description: "JWT-based authentication",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(oauth|oauth2|oidc|openid)`),
			featureName: "oauth",
			featureType: FeatureTypeAuth,
			description: "OAuth/OIDC authentication",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(saml|sso|single.?sign.?on)`),
			featureName: "saml_sso",
			featureType: FeatureTypeAuth,
			description: "SAML SSO authentication",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(mfa|2fa|two.?factor|totp|otp)`),
			featureName: "mfa",
			featureType: FeatureTypeAuth,
			description: "Multi-factor authentication",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(ldap|active.?directory|ad)`),
			featureName: "ldap_auth",
			featureType: FeatureTypeAuth,
			description: "LDAP/Active Directory authentication",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(api.?key|apikey|x-api-key)`),
			featureName: "api_key_auth",
			featureType: FeatureTypeAuth,
			description: "API key authentication",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(password.?reset|forgot.?password|recovery)`),
			featureName: "password_recovery",
			featureType: FeatureTypeAuth,
			description: "Password recovery flow",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(session|cookie.?auth|express-session)`),
			featureName: "session_auth",
			featureType: FeatureTypeAuth,
			description: "Session-based authentication",
		},
	}

	allFiles := []string{}
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".go" || ext == ".ts" || ext == ".js" || ext == ".py" {
			allFiles = append(allFiles, path)
		}
		return nil
	})

	for _, ap := range authPatterns {
		matchedFiles := []string{}
		for _, file := range allFiles {
			content, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			if ap.pattern.Match(content) {
				matchedFiles = append(matchedFiles, filepath.Base(file))
			}
		}
		if len(matchedFiles) > 0 {
			confidence := 0.6 + min(float64(len(matchedFiles))*0.1, 0.35)
			signals = append(signals, featureSignal{
				name:        ap.featureName,
				featureType: ap.featureType,
				description: ap.description,
				confidence:  confidence,
				source:      "auth_analysis",
				evidence: map[string]any{
					"matched_files": matchedFiles,
				},
			})
		}
	}

	return signals
}

func (e *FeatureExtractor) extractConfigFeatures(projectPath string) []featureSignal {
	signals := []featureSignal{}

	envPatterns := []struct {
		pattern     *regexp.Regexp
		featureName string
		featureType FeatureType
		description string
	}{
		{
			pattern:     regexp.MustCompile(`(?i)(redis|memcached|cache)`),
			featureName: "caching",
			featureType: FeatureTypeCapability,
			description: "Caching layer",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(s3|minio|gcs|azure.?blob|storage)`),
			featureName: "cloud_storage",
			featureType: FeatureTypeCapability,
			description: "Cloud storage integration",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(sendgrid|mailgun|ses|smtp|email)`),
			featureName: "email_service",
			featureType: FeatureTypeCapability,
			description: "Email service integration",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(twilio|nexmo|sms|push)`),
			featureName: "sms_notifications",
			featureType: FeatureTypeCapability,
			description: "SMS/push notifications",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(stripe|paypal|braintree|payment)`),
			featureName: "payment_processing",
			featureType: FeatureTypeCapability,
			description: "Payment processing",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(kafka|rabbitmq|sqs|pubsub|queue)`),
			featureName: "message_queue",
			featureType: FeatureTypeCapability,
			description: "Message queue integration",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(elasticsearch|opensearch|search|lucene)`),
			featureName: "search_engine",
			featureType: FeatureTypeCapability,
			description: "Search engine integration",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(sentry|bugsnag|rollbar|error.?tracking|monitoring)`),
			featureName: "error_monitoring",
			featureType: FeatureTypeCapability,
			description: "Error monitoring and tracking",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(datadog|prometheus|grafana|metrics|observability)`),
			featureName: "metrics_observability",
			featureType: FeatureTypeCapability,
			description: "Metrics and observability",
		},
		{
			pattern:     regexp.MustCompile(`(?i)(feature.?flag|launchdarkly|unleash|abtest)`),
			featureName: "feature_flags",
			featureType: FeatureTypeCapability,
			description: "Feature flag system",
		},
	}

	configFiles := []string{}
	filepath.Walk(projectPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		base := strings.ToLower(filepath.Base(path))
		if base == ".env" || base == ".env.example" || base == ".env.local" ||
			strings.HasPrefix(base, "config") ||
			strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml") ||
			strings.HasSuffix(base, ".toml") || strings.HasSuffix(base, ".ini") ||
			base == "docker-compose.yml" || base == "docker-compose.yaml" {
			configFiles = append(configFiles, path)
		}
		return nil
	})

	for _, cp := range envPatterns {
		for _, file := range configFiles {
			content, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			if cp.pattern.Match(content) {
				signals = append(signals, featureSignal{
					name:        cp.featureName,
					featureType: cp.featureType,
					description: cp.description,
					confidence:  0.7,
					source:      "config_analysis",
					evidence: map[string]any{
						"config_file": filepath.Base(file),
					},
				})
				break
			}
		}
	}

	return signals
}

func (e *FeatureExtractor) consolidateFeatures(signals []featureSignal, projectID string) []ProjectFeature {
	featureMap := make(map[string]*ProjectFeature)

	for _, sig := range signals {
		if existing, ok := featureMap[sig.name]; ok {
			if sig.confidence > existing.Confidence {
				existing.Confidence = sig.confidence
				existing.Description = sig.description
				existing.Source = sig.source
				if existing.Evidence == nil {
					existing.Evidence = make(map[string]any)
				}
				for k, v := range sig.evidence {
					existing.Evidence[k] = v
				}
			}
		} else {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			featureMap[sig.name] = &ProjectFeature{
				ID:          generateFeatureID(projectID, sig.name),
				ProjectID:   projectID,
				FeatureName: sig.name,
				FeatureType: sig.featureType,
				Description: sig.description,
				Confidence:  sig.confidence,
				Source:      sig.source,
				Evidence:    sig.evidence,
				DetectedAt:  now,
			}
		}
	}

	features := make([]ProjectFeature, 0, len(featureMap))
	for _, f := range featureMap {
		if f.Confidence >= e.config.MinConfidence {
			features = append(features, *f)
		}
	}

	for i := 0; i < len(features); i++ {
		for j := i + 1; j < len(features); j++ {
			if features[j].Confidence > features[i].Confidence {
				features[i], features[j] = features[j], features[i]
			}
		}
	}

	return features
}

func generateFeatureID(projectID, featureName string) string {
	return projectID + "_" + featureName
}
