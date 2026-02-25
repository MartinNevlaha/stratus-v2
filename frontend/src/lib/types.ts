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
  type: 'spec' | 'bug'
  phase: string
  complexity: 'simple' | 'complex'
  delegated_agents: Record<string, string[]>
  tasks: Task[]
  current_task?: number
  total_tasks: number
  aborted: boolean
  title: string
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
