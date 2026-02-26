import type { DashboardState, Event, SearchResult, WorkflowState, Candidate, Proposal, VersionInfo, SwarmMission, SwarmMissionDetail } from './types'

const BASE = '/api'

async function get<T>(path: string, params?: Record<string, string>): Promise<T> {
  const url = new URL(BASE + path, window.location.origin)
  if (params) {
    Object.entries(params).forEach(([k, v]) => url.searchParams.set(k, v))
  }
  const res = await fetch(url)
  if (!res.ok) throw new Error(`${res.status} ${await res.text()}`)
  return res.json()
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

export const startWorkflow = (id: string, type: 'spec' | 'bug', title: string, complexity = 'simple') =>
  post<WorkflowState>('/workflows', { id, type, title, complexity })

export const getWorkflow = (id: string) => get<WorkflowState>(`/workflows/${id}`)

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

// Swarm
export const listMissions = () => get<SwarmMission[]>('/swarm/missions')
export const getMission = (id: string) => get<SwarmMissionDetail>(`/swarm/missions/${id}`)
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
