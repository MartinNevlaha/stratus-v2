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
  created_at: string
  updated_at: string
}

export interface Task {
  index: number
  title: string
  status: 'pending' | 'in_progress' | 'done'
}

export interface Candidate {
  id: string
  detection_type: string
  count: number
  confidence: number
  files: string[]
  description: string
  status: string
  detected_at: string
}

export interface Proposal {
  id: string
  candidate_id: string
  type: string
  title: string
  description: string
  proposed_content: string
  proposed_path?: string
  confidence: number
  status: string
  decision?: string
  decided_at?: string
  created_at: string
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
  pending_candidates: Candidate[]
  pending_proposals: Proposal[]
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

export interface Anomaly {
  id: string
  type: string
  metric_name: string
  actual_value: number
  expected_value: number
  deviation: number
  severity: string
  detected_at: string
  description: string
}

export interface LiveMetricsUpdate {
  summary: MetricsSummary
  daily: DailyMetric[]
  agents: AgentMetric[]
  ts: number
}

export interface MetricsAnomalyAlert {
  anomaly: Anomaly
  ts: number
  alert_msg: string
}

export interface MetricsAlert {
  message: string
  severity: string
  count: number
  ts: number
}

export interface ProjectMetric {
  project: string
  total_workflows: number
  completed_workflows: number
  avg_duration_ms: number
  success_rate: number
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
