export type Task = { index: number; title: string; status: string }

export type Workflow = {
  id: string
  type: string
  phase: string
  complexity?: string
  title: string
  session_id?: string
  aborted: boolean
  tasks: Task[] | null
  total_tasks: number
  delegated_agents: Record<string, string[]> | null
  plan_content?: string
  design_content?: string
}

export type Denial = { id: number; ts: string; hook: string; tool: string; reason: string; sessionId: string }

export type Mission = { id: string; workflow_id: string; title: string; status: string; strategy: string }
export type Worker = { id: string; agent_type: string; status: string; branch_name: string; last_heartbeat: string }
export type Ticket = { id: string; title: string; status: string; domain: string }
export type Swarm = { mission: Mission; workers: Worker[] | null; tickets: Ticket[] | null }

export type Alert = { id: number; type: string; severity: string; message: string; created_at: string }

export type Snapshot = { isOnline: boolean; workflow: Workflow | null; isOwn: boolean; denials: Denial[]; swarm: Swarm | null; alerts: Alert[] }

export type Tab = 'workflow' | 'plan' | 'swarm' | 'guardian' | 'denials'

// A workflow change waiting for the person to confirm it in the pane.
export type Pending = { workflowId: string; kind: 'phase'; phase: string } | { workflowId: string; kind: 'abort' }

declare module 'claude-code' {
  interface PluginState {
    'stratus-hud': { snapshot: Snapshot | null; tab: Tab; pending: Pending | null }
  }
}
