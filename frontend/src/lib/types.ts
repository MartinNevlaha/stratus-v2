export interface Event {
  id: number
  ts: string
  actor: string
  scope: string
  type: string
  text: string
  title: string
  tags: string[]
  refs: Record<string, unknown>
  importance: number
  dedupe_key?: string
  project?: string
  session_id?: string
  created_ms: number
}

export interface ChangeSummary {
  capabilities_added: string[]
  capabilities_modified: string[]
  capabilities_removed: string[]
  downstream_risks: string[]
  governance_compliance: string[]
  test_coverage_delta: string
  files_changed: number
  lines_added: number
  lines_removed: number
  governance_docs_matched?: string[]
  vexor_excerpts?: string[]
  generated_at: string
}

export interface WorkflowState {
  review_generation?: number
  runtime?: string
  runtime_session_id?: string
  id: string
  type: 'spec' | 'bug' | 'e2e'
  phase: string
  complexity: 'simple' | 'complex'
  delegated_agents: Record<string, string[]>
  tasks: Task[]
  current_task?: number
  total_tasks: number
  aborted: boolean
  title: string
  session_id?: string
  plan_content?: string
  design_content?: string
  base_commit?: string
  change_summary?: ChangeSummary
  created_at: string
  updated_at: string
}

export interface Task {
  index: number
  title: string
  status: 'pending' | 'in_progress' | 'done' | 'skipped'
  started_at?: string
  completed_at?: string
  depends_on?: number[]
}

export interface WorkflowAgent {
  native_agent_id?: string
  turn_id?: string
  runtime?: string
  usage_available?: boolean
  id: string
  workflow_id: string
  session_id: string
  agent_type: string
  phase: string
  task_index?: number
  status: 'running' | 'done' | 'failed'
  started_at: string
  ended_at?: string
  last_activity: string
  tool_calls: number
  last_tool?: string
  files: string[]
  verdict?: 'PASS' | 'FAIL' | ''
  must_fix: number
  should_fix: number
  suggestion: number
  tokens_in: number
  tokens_out: number
}

export interface TranscriptStep {
  kind: 'tool' | 'text'
  name?: string
  summary?: string
  text?: string
}

export interface AgentTranscript {
  runtime?: string
  usage_available?: boolean
  steps: TranscriptStep[]
  usage: { input_tokens: number; output_tokens: number; cache_read_tokens: number; cache_creation_tokens: number } | null
}

export interface MissionAgent extends WorkflowAgent {
  workflow_title: string
  idle: boolean
}

export interface ReviewState {
  status: 'awaiting' | 'running' | 'unverified' | 'outdated' | 'PASS' | 'FAIL'
  agent_id?: string
  must_fix: number
  should_fix: number
  suggestion: number
  reviews: number
  loops: number
}

export interface WorkflowAgentsResponse {
  review?: ReviewState
  agents: WorkflowAgent[]
  conflicts: string[]
}

export interface WorkflowDenial {
  ts: string
  hook?: string
  tool_name?: string
  agent_type?: string
  reason?: string
}

export interface AttentionItem {
  workflow_id: string
  title: string
  phase: string
  running_agents: number
  stale_agents: number
  review_failed: boolean
  conflicts: string[]
}

export interface RunMetrics {
  id: string
  duration_sec: number
  tasks: number
  agents: number
  review_loops: number
  tokens: number
}

export interface RunComparison {
  current: RunMetrics
  previous: RunMetrics | null
}

export interface WorkflowLog {
  id: number
  workflow_id: string
  session_id: string
  ts: string
  tool_name: string
  summary: string
}

export interface SearchResult {
  source: 'code' | 'governance'
  file_path: string
  title: string
  excerpt: string
  score: number
  doc_type?: string
}

export interface DashboardState {
  workflows: WorkflowState[]
  recent_events: Event[]
  governance: { total_chunks: number; by_type: Array<{ type: string; count: number }> }
  vexor_available: boolean
  ws_clients: number
  ts: string
}

export interface WsMessage {
  type: string
  payload?: unknown
}

export interface VersionInfo {
  current: string
  latest: string
  update_available: boolean
  release_url: string
  release_notes: string
  sync_required: boolean
  skipped_files: string[]
}

// Swarm types
export interface SwarmMission {
  id: string
  workflow_id: string
  title: string
  status: 'planning' | 'active' | 'merging' | 'verifying' | 'complete' | 'failed' | 'aborted'
  base_branch: string
  merge_branch: string
  strategy?: string
  strategy_outcome?: string
  created_at: string
  updated_at: string
}

export interface SwarmWorker {
  id: string
  mission_id: string
  agent_type: string
  worktree_path: string
  branch_name: string
  status: 'pending' | 'active' | 'stale' | 'done' | 'failed' | 'killed'
  session_id?: string
  last_heartbeat: string
  created_at: string
  updated_at: string
}

export interface SwarmTicket {
  id: string
  mission_id: string
  title: string
  description: string
  domain: string
  priority: number
  status: 'pending' | 'assigned' | 'in_progress' | 'done' | 'failed' | 'blocked'
  worker_id?: string
  depends_on: string
  result: string
  created_at: string
  updated_at: string
}

export interface SwarmSignal {
  id: string
  mission_id: string
  from_worker: string
  to_worker: string
  type: string
  payload: string
  read: boolean
  created_at: string
}

export interface SwarmForgeEntry {
  id: string
  mission_id: string
  worker_id: string
  branch_name: string
  status: 'pending' | 'merging' | 'merged' | 'conflict' | 'failed'
  conflict_files: string
  merged_at?: string
  created_at: string
}

export interface SwarmFileReservation {
  id: string
  mission_id: string
  worker_id: string
  patterns: string
  reason: string
  created_at: string
}

export interface SwarmMissionDetail {
  mission: SwarmMission
  workers: SwarmWorker[]
  tickets: SwarmTicket[]
  forge: SwarmForgeEntry[]
}

// Agent & Skill types
export interface AgentDef {
  name: string
  description: string
  tools: string[]
  model?: string
  skills: string[]
  body: string
  format: 'claude-code' | 'opencode'
  file_path: string
}

export interface SkillDef {
  name: string
  description: string
  disable_model_invocation: boolean
  argument_hint?: string
  body: string
  has_resources: boolean
  resource_dirs: string[]
  dir_path: string
}

export interface AgentsResponse {
  claude_code: AgentDef[]
  opencode: AgentDef[]
}

export interface AgentDetail {
  name: string
  claude_code?: AgentDef
  opencode?: AgentDef
}

export interface SkillsResponse {
  skills: SkillDef[]
}

export interface RuleDef {
  name: string
  title: string
  body: string
  file_path: string
}

export interface RulesResponse {
  rules: RuleDef[]
}

export type PastItem =
  | { kind: 'workflow'; data: WorkflowState }
  | { kind: 'mission'; data: SwarmMission }

export interface PastItemsResponse {
  items: PastItem[]
  total: number
  offset: number
  limit: number
}

export interface SimilarWorkflow {
  id: string
  title: string
  type: string
  complexity: string
  duration_min: number
  aborted: boolean
}

export interface AnalysisResult {
  recommended_type: string
  recommended_complexity: string
  recommended_strategy: string
  risk_score: number
  risk_level: 'low' | 'medium' | 'high'
  risk_factors: string[]
  estimated_duration_min: number
  suggested_domains: string[]
  similar_past_workflows: SimilarWorkflow[]
  llm_analysis?: string
}

export interface GuardianAlert {
  id: number
  type: string
  severity: 'info' | 'warning' | 'critical'
  message: string
  metadata: Record<string, unknown>
  dismissed_at: string | null
  created_at: string
}

export interface LLMConfig {
  provider: string
  model: string
  api_key: string
  base_url: string
  timeout: number
  max_tokens: number
  temperature: number
  max_retries: number
  concurrency: number
  min_request_interval_ms?: number
}

export interface GuardianConfig {
  enabled: boolean
  interval_minutes: number
  coverage_drift_pct: number
  stale_workflow_hours: number
  memory_threshold: number
  tech_debt_threshold: number
  reviewer_timeout_minutes: number
  ticket_timeout_minutes: number
  workflow_token_budget: number
  llm: LLMConfig
}

export interface InsightConfig {
  enabled: boolean
  interval: number
  llm: LLMConfig
}

export interface SwarmEvidence {
  id: string
  ticket_id: string
  mission_id: string
  type: string
  content: string
  agent: string
  verdict: string
  created_at: string
}

export interface SolutionPattern {
  id: string
  problem_class: string
  solution_pattern: string
  repo_type: string
  success_rate: number
  occurrence_count: number
  example_artifacts: string[]
  confidence: number
  first_seen: string
  last_seen: string
}

export interface ProblemStats {
  id: string
  problem_class: string
  repo_type: string
  best_agent: string
  best_workflow: string
  success_rate: number
  occurrence_count: number
  avg_cycle_time: number
  agents_success: Record<string, number>
}

export interface KBRecommendation {
  solution: SolutionPattern | null
  best_agent: string
  agent_success_rate: number
}

export interface KBStats {
  solution_patterns: number
  problem_classes: number
}

// Self-Evolving System

export interface WikiPage {
  id: string
  page_type: 'summary' | 'entity' | 'concept' | 'answer' | 'index'
  title: string
  content: string
  status: 'draft' | 'published' | 'stale' | 'archived'
  staleness_score: number
  source_hashes: string[]
  tags: string[]
  metadata: Record<string, unknown>
  generated_by: string
  version: number
  created_at: string
  updated_at: string
}

export interface WikiLink {
  id: string
  from_page_id: string
  to_page_id: string
  link_type: 'related' | 'parent' | 'child' | 'contradicts' | 'supersedes' | 'cites'
  strength: number
  created_at: string
}

export interface WikiPageRef {
  id: string
  page_id: string
  source_type: string
  source_id: string
  excerpt: string
  created_at: string
}

export interface WikiGraphData {
  nodes: { id: string; title: string; page_type: string; status: string; staleness_score: number }[]
  edges: { from_page_id: string; to_page_id: string; link_type: string; strength: number }[]
}

export interface WikiQueryResult {
  answer: string
  citations: { source_type: string; source_id: string; excerpt: string; relevance: number }[]
  wiki_page_id: string | null
  tokens_used: number
}

export interface VaultStatus {
  last_sync: string | null
  file_count: number
  vault_path: string
  errors: string[]
}

export interface WikiConfig {
  enabled: boolean
  ingest_on_event: boolean
  max_pages_per_ingest: number
  staleness_threshold: number
  max_page_size_tokens: number
  vault_path: string
  vault_sync_on_save: boolean
  onboarding_depth: string
  onboarding_max_pages: number
}

export interface OnboardingProgress {
  job_id: string
  status: 'idle' | 'scanning' | 'generating' | 'linking' | 'syncing' | 'complete' | 'failed'
  current_page: string
  generated: number
  total: number
  errors: string[]
  result?: OnboardingResult
}

export interface OnboardingResult {
  pages_generated: number
  pages_failed: number
  pages_skipped: number
  links_created: number
  vault_synced: boolean
  output_dir?: string
  duration: number
  tokens_used: number
  errors: string[]
  page_ids: string[]
  asset_proposals: number
}

// Language
export type Language = 'sk' | 'en'
