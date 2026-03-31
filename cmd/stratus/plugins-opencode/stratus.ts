import type { Plugin } from "@opencode-ai/plugin"
import { readFileSync } from "fs"

function getBase(): string {
  if (process.env.STRATUS_PORT) {
    return `http://localhost:${process.env.STRATUS_PORT}`
  }
  try {
    const cfg = JSON.parse(readFileSync(".stratus.json", "utf-8"))
    if (cfg.port) return `http://localhost:${cfg.port}`
  } catch {}
  return "http://localhost:41777"
}

const BASE = getBase()

const WRITE_TOOLS = ["write", "edit", "bash", "patch"]
const WATCH_TOOLS = ["write", "edit"]

const noActiveWorkflowReason = "No active workflow registered. Use /spec or /bug command first."

const phaseAgentAllowlist: Record<string, Record<string, string[]>> = {
  bug: {
    analyze: ["delivery-debugger", "delivery-strategic-architect", "delivery-system-architect", "plan", "explore"],
    fix: [
      "delivery-backend-engineer",
      "delivery-frontend-engineer",
      "delivery-database-engineer",
      "delivery-devops-engineer",
      "delivery-mobile-engineer",
      "delivery-implementation-expert",
      "delivery-ux-designer",
    ],
    review: ["delivery-code-reviewer"],
  },
  spec: {
    plan: ["delivery-strategic-architect", "delivery-system-architect", "plan", "explore"],
    discovery: ["delivery-debugger", "delivery-strategic-architect", "explore"],
    design: ["delivery-strategic-architect", "delivery-system-architect", "delivery-ux-designer"],
    governance: ["delivery-code-reviewer", "delivery-governance-checker"],
    accept: [],
    implement: [
      "delivery-backend-engineer",
      "delivery-frontend-engineer",
      "delivery-database-engineer",
      "delivery-devops-engineer",
      "delivery-mobile-engineer",
      "delivery-implementation-expert",
      "delivery-ux-designer",
    ],
    verify: ["delivery-code-reviewer"],
    learn: [],
    complete: [],
  },
  e2e: {
    setup: ["delivery-qa-engineer"],
    plan: ["delivery-strategic-architect", "plan"],
    generate: ["delivery-qa-engineer", "delivery-frontend-engineer"],
    heal: ["delivery-debugger", "delivery-qa-engineer"],
    complete: [],
  },
}

interface Workflow {
  id: string
  type: string
  phase: string
  session_id?: string
}

interface DashboardState {
  workflows: Workflow[]
}

async function fetchDashboardState(): Promise<DashboardState | null> {
  try {
    const res = await fetch(`${BASE}/api/dashboard/state`)
    if (!res.ok) return null
    return await res.json()
  } catch {
    return null
  }
}

async function fetchDashboardStateStrict(): Promise<DashboardState> {
  const res = await fetch(`${BASE}/api/dashboard/state`)
  if (!res.ok) {
    throw new Error(`Stratus API returned status ${res.status}`)
  }
  return await res.json()
}

async function getActiveWorkflow(sessionID?: string): Promise<Workflow | null> {
  const state = await fetchDashboardState()
  if (!state) return null

  let untracked: Workflow | null = null
  let first: Workflow | null = null

  for (const wf of state.workflows) {
    if (!first) first = wf
    if (!sessionID) return wf
    if (wf.session_id === sessionID) return wf
    if (!wf.session_id && !untracked) untracked = wf
  }

  return untracked ?? first
}

async function getWorkflowForSessionStrict(sessionID?: string): Promise<Workflow | null> {
  if (!sessionID) return null

  const state = await fetchDashboardStateStrict()

  for (const wf of state.workflows) {
    if (wf.session_id === sessionID) return wf
  }
  return null
}

function isDeliveryAgent(): boolean {
  const agentID = process.env["OPENCODE_AGENT_ID"] ?? process.env["CLAUDE_AGENT_ID"]
  return agentID?.startsWith("delivery-") ?? false
}

function isDeliverySubagent(subagentType: string): boolean {
  return subagentType.startsWith("delivery-")
}

function isAgentAllowedInPhase(agentID: string, wtype: string, phase: string): boolean {
  const workflowAgents = phaseAgentAllowlist[wtype]
  if (!workflowAgents) return true
  const allowedAgents = workflowAgents[phase]
  if (!allowedAgents) return true
  return allowedAgents.includes(agentID)
}

function getAllowedAgentsForPhase(wtype: string, phase: string): string[] {
  const workflowAgents = phaseAgentAllowlist[wtype]
  if (!workflowAgents) return ["(any)"]
  const agents = workflowAgents[phase]
  return agents ?? ["(any)"]
}

function isWriteBashCommand(cmd: string): boolean {
  const normalizedCmd = cmd.replace(/\t/g, " ")
  const lowerCmd = normalizedCmd.toLowerCase()

  const writePatterns = [
    " > ",
    " >> ",
    ">|",
    " 1>",
    " 2>",
    " &>",
    "2>&1",
    "sed -i",
    "awk -i",
    "tee ",
    "install ",
    "git add",
    "git commit",
    "git push",
    "git merge",
    "git rebase",
    "git cherry-pick",
    "git reset",
    "rm ",
    "rmdir ",
    "mv ",
    "mkdir ",
    "touch ",
    "chmod ",
    "chown ",
    "cp ",
    "dd ",
    "truncate ",
  ]

  for (const p of writePatterns) {
    if (lowerCmd.includes(p)) return true
  }

  const readOnlyPatterns = [
    "git status",
    "git log",
    "git diff",
    "git show",
    "git branch",
    "git remote",
    "cat ",
    "head ",
    "tail ",
    "less ",
    "more ",
    "ls ",
    "find ",
    "which ",
    "whereis ",
    "grep ",
    "rg ",
    "ag ",
    "ack ",
    "go test",
    "npm test",
    "npm run test",
    "pytest",
    "jest",
    "cargo test",
    "curl ",
    "wget ",
  ]

  for (const p of readOnlyPatterns) {
    if (lowerCmd.includes(p)) return false
  }

  const gtIdx = lowerCmd.indexOf(">")
  if (gtIdx >= 0) {
    const precededByURLContext = gtIdx > 0 && ["/", ":", "="].includes(lowerCmd[gtIdx - 1])
    if (!precededByURLContext) return true
  }

  return false
}

export const Stratus: Plugin = async () => {
  return {
    tool: {
      execute: {
        before: async (input: { tool: string; sessionID?: string }, output: { args: Record<string, unknown> }) => {
          const toolName = input.tool.toLowerCase()

          // workflow_existence_guard: block Task delegation without workflow
          if (toolName === "task") {
            const subagentType = output.args["subagent_type"] as string | undefined
            const isDelivery = subagentType && isDeliverySubagent(subagentType)

            if (isDelivery) {
              let wf: Workflow | null = null
              try {
                wf = await getWorkflowForSessionStrict(input.sessionID)
              } catch (err) {
                throw new Error(
                  `Cannot verify workflow: ${err}. Ensure Stratus server is running (stratus serve).`,
                )
              }

              if (!wf) {
                throw new Error(noActiveWorkflowReason)
              }

              // delegation_guard: check phase-agent matching
              const phase = wf.phase
              const wtype = wf.type

              if (subagentType && !isAgentAllowedInPhase(subagentType, wtype, phase)) {
                const allowed = getAllowedAgentsForPhase(wtype, phase)
                throw new Error(
                  `Agent "${subagentType}" is not allowed in phase "${phase}" (workflow type: ${wtype}). Allowed agents: ${allowed.join(", ")}`,
                )
              }
            }
          }

          // phase_guard: block write/edit/bash during verify/review phases
          if (WRITE_TOOLS.includes(toolName)) {
            // bash_write_guard: check for workflow when delivery agent uses bash write
            if (toolName === "bash" && isDeliveryAgent()) {
              const command = output.args["command"] as string | undefined
              if (command && isWriteBashCommand(command)) {
                let wf: Workflow | null = null
                try {
                  wf = await getWorkflowForSessionStrict(input.sessionID)
                } catch (err) {
                  throw new Error(
                    `Cannot verify workflow: ${err}. Ensure Stratus server is running (stratus serve).`,
                  )
                }

                if (!wf) {
                  throw new Error(noActiveWorkflowReason + " Delivery agents must have an active workflow to execute write commands.")
                }
              }
            }

            const wf = await getActiveWorkflow(input.sessionID)
            if (wf && ["verify", "review"].includes(wf.phase)) {
              throw new Error(
                `[Stratus] Phase guard: '${input.tool}' is blocked during the '${wf.phase}' phase. ` +
                  `Only read-only tools (read, grep, glob) are allowed. ` +
                  `Complete the ${wf.phase} phase before making changes.`,
              )
            }
          }
        },

        after: async (input: { tool: string }, output: { args: Record<string, unknown> }) => {
          if (!WATCH_TOOLS.includes(input.tool.toLowerCase())) return

          const filePath = (output.args["filePath"] ?? output.args["path"]) as string | undefined
          if (!filePath) return

          fetch(`${BASE}/api/retrieve/dirty`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ paths: [filePath] }),
          }).catch(() => {})
        },
      },
    },
  }
}
