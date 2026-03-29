import type {
  DashboardState,
  Event,
  SearchResult,
  WorkflowState,
  ChangeSummary,
  Candidate,
  Proposal,
  VersionInfo,
  SwarmMission,
  SwarmMissionDetail,
  SwarmFileReservation,
  AgentsResponse,
  AgentDetail,
  SkillsResponse,
  SkillDef,
  RulesResponse,
  RuleDef,
  PastItemsResponse,
  AnalysisResult,
  GuardianAlert,
  GuardianConfig,
  InsightConfig,
  MetricsSummary,
  DailyMetricsResponse,
  AgentMetricsResponse,
  ProjectMetricsResponse,
} from './types'

const BASE = '/api'

async function get<T>(path: string, params?: Record<string, string>): Promise<T> {
  const url = new URL(BASE + path, window.location.origin)
  if (params) {
    Object.entries(params).forEach(([k, v]) => url.searchParams.set(k, v))
  }
  
  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), 10000)
  
  try {
    const res = await fetch(url, { signal: controller.signal })
    if (!res.ok) throw new Error(`${res.status} ${await res.text()}`)
    return res.json()
  } finally {
    clearTimeout(timeoutId)
  }
}

async function post<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`)
  return res.json()
}

async function put<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`)
  return res.json()
}

async function del<T>(path: string): Promise<T> {
  const res = await fetch(BASE + path, { method: 'DELETE' })
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`)
  return res.json()
}

// Dashboard
export const getDashboardState = () => get<DashboardState>('/dashboard/state')

// Events / Memory
export const searchEvents = (q: string, opts?: Record<string, string>) =>
  get<{ results: Event[]; count: number }>('/events/search', { q, ...opts })

export const saveEvent = (body: Partial<Event>) => post<{ id: number }>('/events', body)

export const getTimeline = (id: number, before = 10, after = 10) =>
  get<{ events: Event[]; anchor_id: number }>(`/events/${id}/timeline`, {
    before: String(before),
    after: String(after),
  })

// Retrieval
export const retrieve = (q: string, corpus?: string, topK = 10) =>
  get<{ results: SearchResult[]; count: number; query: string }>('/retrieve', {
    q,
    ...(corpus ? { corpus } : {}),
    top_k: String(topK),
  })

export const getRetrieveStatus = () =>
  get<{ vexor_available: boolean; governance_available: boolean; governance_stats: unknown }>('/retrieve/status')

export const triggerReIndex = () => post<{ status: string }>('/retrieve/index')

// Workflows
export const listWorkflows = () => get<WorkflowState[]>('/workflows')
export const deleteWorkflow = (id: string) => del<{ deleted: boolean }>(`/workflows/${id}`)
export const listPastItems = (limit = 20, offset = 0) =>
  get<PastItemsResponse>('/past', { limit: String(limit), offset: String(offset) })
export const analyzeWorkflow = (description: string, filesHint?: string[]) =>
  post<AnalysisResult>('/workflows/analyze', { description, files_hint: filesHint ?? [] })

export const startWorkflow = (id: string, type: 'spec' | 'bug', title: string, complexity = 'simple') =>
  post<WorkflowState>('/workflows', { id, type, title, complexity })

export const getWorkflow = (id: string) => get<WorkflowState>(`/workflows/${id}`)

export const getWorkflowSummary = (id: string) =>
  get<ChangeSummary | { status: string }>(`/workflows/${id}/summary`)

export const updateWorkflowSummary = (id: string, summary: Partial<ChangeSummary>) =>
  put<ChangeSummary>(`/workflows/${id}/summary`, summary)

export const transitionPhase = (id: string, phase: string) =>
  put<WorkflowState>(`/workflows/${id}/phase`, { phase })

export const recordDelegation = (id: string, agentId: string) =>
  post<WorkflowState>(`/workflows/${id}/delegate`, { agent_id: agentId })

export const setTasks = (id: string, tasks: string[]) =>
  post<WorkflowState>(`/workflows/${id}/tasks`, { tasks })

export const startTask = (id: string, index: number) =>
  post<WorkflowState>(`/workflows/${id}/tasks/${index}/start`)

export const completeTask = (id: string, index: number) =>
  post<WorkflowState>(`/workflows/${id}/tasks/${index}/complete`)

// Learning
export const listCandidates = (status?: string) =>
  get<{ candidates: Candidate[]; count: number }>('/learning/candidates', status ? { status } : {})

export const listProposals = (status?: string) =>
  get<{ proposals: Proposal[]; count: number }>('/learning/proposals', status ? { status } : {})

export const decideProposal = (id: string, decision: string) =>
  post<{ status: string; applied: boolean }>(`/learning/proposals/${id}/decide`, { decision })

export const saveProposal = (proposal: {
  candidate_id: string
  type: string
  title: string
  description: string
  proposed_content: string
  proposed_path?: string
  confidence: number
}) => post<{ id: string }>('/learning/proposals', proposal)

// Swarm
export const listMissions = () => get<SwarmMission[]>('/swarm/missions')
export const getMission = (id: string) => get<SwarmMissionDetail>(`/swarm/missions/${id}`)
export const getMissionFiles = (id: string) => get<SwarmFileReservation[]>(`/swarm/missions/${id}/files`)
export const deleteMission = (id: string) => del<{ deleted: boolean }>(`/swarm/missions/${id}`)

// System
export const getVersion = () => get<VersionInfo>('/system/version')
export const triggerUpdate = () => post<{ accepted: boolean }>('/system/update', {})

// STT
export const getSttStatus = () => get<{ available: boolean; endpoint: string }>('/stt/status')

export async function transcribeAudio(blob: Blob): Promise<{ text: string }> {
  const form = new FormData()
  form.append('file', blob, 'recording.webm')
  form.append('model', 'whisper-1')
  const res = await fetch(BASE + '/stt/transcribe', { method: 'POST', body: form })
  if (!res.ok) throw new Error(`STT error: ${res.status}`)
  return res.json()
}

// Terminal image upload
export async function uploadTerminalImage(blob: Blob, filename: string): Promise<{ path: string; filename: string }> {
  const form = new FormData()
  form.append('image', blob, filename)
  const res = await fetch(BASE + '/terminal/upload-image', { method: 'POST', body: form })
  if (!res.ok) throw new Error(`Upload error: ${res.status}`)
  return res.json()
}

// Agents
export const listAgents = () => get<AgentsResponse>('/agents')
export const getAgent = (name: string) => get<AgentDetail>(`/agents/${name}`)
export const createAgent = (data: { name: string; description: string; tools?: string[]; model?: string; skills?: string[]; body?: string }) =>
  post<{ status: string; name: string }>('/agents', data)
export const updateAgent = (name: string, data: { description: string; tools?: string[]; model?: string; skills?: string[]; body?: string }) =>
  put<{ status: string; name: string }>(`/agents/${name}`, data)
export const deleteAgent = (name: string) => del<{ status: string; name: string }>(`/agents/${name}`)
export const assignSkills = (name: string, skills: string[]) =>
  put<{ status: string; name: string; skills: string[] }>(`/agents/${name}/skills`, { skills })

// Skills
export const listSkills = () => get<SkillsResponse>('/skills')
export const getSkill = (name: string) => get<SkillDef>(`/skills/${name}`)
export const createSkill = (data: { name: string; description: string; disable_model_invocation?: boolean; argument_hint?: string; body?: string }) =>
  post<{ status: string; name: string }>('/skills', data)
export const updateSkill = (name: string, data: { description: string; disable_model_invocation?: boolean; argument_hint?: string; body?: string }) =>
  put<{ status: string; name: string }>(`/skills/${name}`, data)
export const deleteSkill = (name: string) => del<{ status: string; name: string }>(`/skills/${name}`)

export const triggerAggregation = () =>
  post<{ status: string }>('/metrics/aggregate', {})

export const getMetricsSummary = (days = 30) =>
  get<{ summary: MetricsSummary }>('/metrics/summary', { days: String(days) })

export const getWorkflowMetrics = (days = 30) =>
  get<{ metrics: MetricsSummary }>('/metrics/workflows', { days: String(days) })

export const getDailyMetrics = (limit = 30) =>
  get<DailyMetricsResponse>('/metrics/daily', { limit: String(limit) })

export const getAgentMetrics = (agentId?: string, days = 30) =>
  get<AgentMetricsResponse>('/metrics/agents', {
    ...(agentId ? { agent_id: agentId } : {}),
    days: String(days)
  })

export const getProjectMetrics = (project?: string, days = 30) =>
  get<ProjectMetricsResponse>('/metrics/projects', {
    ...(project ? { project } : {}),
    days: String(days)
  })

// Insight
export const getInsightStatus = () =>
  get<{ enabled: boolean; state: any; metrics: any; recent_patterns: any[]; recent_analyses: any[] }>('/insight/status')

export const triggerInsightAnalysis = () =>
  post<{ status: string; message: string }>('/insight/trigger', {})

export const getInsightPatterns = (type?: string, minConfidence?: number, limit?: number) => {
  const params: Record<string, string> = {}
  if (type) params.type = type
  if (minConfidence) params.min_confidence = String(minConfidence)
  if (limit) params.limit = String(limit)
  return get<{ patterns: any[]; count: number }>('/insight/patterns', params)
}

export const getInsightAnalyses = (type?: string, limit?: number) => {
  const params: Record<string, string> = {}
  if (type) params.type = type
  if (limit) params.limit = String(limit)
  return get<{ analyses: any[]; count: number }>('/insight/analyses', params)
}

// Rules
export const listRules = () => get<RulesResponse>('/rules')
export const getRule = (name: string) => get<RuleDef>(`/rules/${name}`)
export const createRule = (data: { name: string; title?: string; body?: string }) =>
  post<{ status: string; name: string }>('/rules', data)
export const updateRule = (name: string, data: { title?: string; body?: string }) =>
  put<{ status: string; name: string }>(`/rules/${name}`, data)
export const deleteRule = (name: string) => del<{ status: string; name: string }>(`/rules/${name}`)

// Guardian
export const listGuardianAlerts = (type?: string) =>
  get<GuardianAlert[]>('/guardian/alerts' + (type ? `?type=${type}` : ''))
export const dismissGuardianAlert = (id: number) =>
  put<{ ok: boolean }>(`/guardian/alerts/${id}/dismiss`, {})
export const deleteGuardianAlert = (id: number) =>
  del<{ ok: boolean }>(`/guardian/alerts/${id}`)
export const getGuardianConfig = () => get<GuardianConfig>('/guardian/config')
export const updateGuardianConfig = (cfg: GuardianConfig) =>
  put<GuardianConfig>('/guardian/config', cfg)
export const runGuardianScan = () => post<{ ok: boolean }>('/guardian/run', {})
export const testGuardianLLM = (cfg: Partial<GuardianConfig>) =>
  post<{ ok: boolean }>('/guardian/test-llm', cfg)

export const getInsightConfig = () => get<InsightConfig>('/insight/config')
export const updateInsightConfig = (cfg: InsightConfig) =>
  put<InsightConfig>('/insight/config', cfg)
