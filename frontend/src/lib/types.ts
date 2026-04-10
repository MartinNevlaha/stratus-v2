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
  status: 'pending' | 'in_progress' | 'done'
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

export interface GuardianConfig {
  enabled: boolean
  interval_minutes: number
  coverage_drift_pct: number
  stale_workflow_hours: number
  memory_threshold: number
  tech_debt_threshold: number
  llm_endpoint: string
  llm_api_key: string
  llm_model: string
  llm_temperature: number
  llm_max_tokens: number
}

export interface InsightLLMConfig {
  provider: string
  model: string
  api_key: string
  base_url: string
  timeout: number
  max_tokens: number
  temperature: number
}

export interface InsightConfig {
  enabled: boolean
  interval: number
  max_proposals: number
  min_confidence: number
  llm: InsightLLMConfig
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

export interface AgentScorecard {
  id: string
  agent_name: string
  window: string
  window_start: string
  window_end: string
  total_runs: number
  success_rate: number
  failure_rate: number
  review_pass_rate: number
  rework_rate: number
  avg_cycle_time_ms: number
  regression_rate: number
  confidence_score: number
  trend: string
  created_at: string
  updated_at: string
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
  edges: { from: string; to: string; link_type: string; strength: number }[]
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

export interface EvolutionRun {
  id: string
  trigger_type: 'scheduled' | 'manual' | 'event_driven'
  status: 'running' | 'completed' | 'failed' | 'timeout'
  hypotheses_count: number
  experiments_run: number
  auto_applied: number
  proposals_created: number
  wiki_pages_updated: number
  duration_ms: number
  timeout_ms: number
  error_message: string
  metadata: Record<string, unknown>
  started_at: string
  completed_at: string | null
  created_at: string
}

export interface EvolutionHypothesis {
  id: string
  run_id: string
  category: 'prompt_tuning' | 'workflow_routing' | 'agent_selection' | 'threshold_adjustment'
  description: string
  baseline_value: string
  proposed_value: string
  metric: string
  baseline_metric: number
  experiment_metric: number
  confidence: number
  decision: 'auto_applied' | 'proposal_created' | 'rejected' | 'inconclusive'
  decision_reason: string
  wiki_page_id: string | null
  evidence: Record<string, unknown>
  created_at: string
}

export interface WikiConfig {
  enabled: boolean
  ingest_on_event: boolean
  max_pages_per_ingest: number
  staleness_threshold: number
  max_page_size_tokens: number
  vault_path: string
  vault_sync_on_save: boolean
}

export interface EvolutionConfig {
  enabled: boolean
  timeout_ms: number
  max_hypotheses_per_run: number
  auto_apply_threshold: number
  proposal_threshold: number
  min_sample_size: number
  daily_token_budget: number
  categories: string[]
}

export interface OnboardingProgress {
  job_id: string
  status: 'idle' | 'scanning' | 'generating' | 'linking' | 'syncing' | 'complete' | 'failed'
  current_page: string
  generated: number
  total: number
  errors: string[]
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
}
