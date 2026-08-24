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

const unresolvedWorkflowReason =
  "No workflow could be resolved for this delegation. Register a workflow with /spec or /bug first. " +
  "If several workflows are active in parallel, include the exact workflow ID in the Task prompt so the correct flow is selected."

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

async function fetchWorkflowByID(id: string): Promise<Workflow | null> {
  const res = await fetch(`${BASE}/api/workflows/${encodeURIComponent(id)}`)
  if (res.status === 404) return null
  if (!res.ok) {
    throw new Error(`Stratus API returned status ${res.status}`)
  }
  return await res.json()
}

function getTaskText(args: Record<string, unknown>): string {
  return [args["prompt"], args["command"], args["description"]]
    .filter((v): v is string => typeof v === "string")
    .join("\n")
}

function extractWorkflowIDFromTaskArgs(args: Record<string, unknown>): string | null {
  const match = getTaskText(args).match(/\b(?:bug|spec|e2e)-[a-z0-9][a-z0-9-]{0,120}\b/)
  return match?.[0] ?? null
}

async function getActiveWorkflow(sessionID?: string): Promise<Workflow | null> {
  const state = await fetchDashboardState()
  if (!state) return null

  const workflows = state.workflows ?? []

  // 1. Exact session match is unambiguous — always prefer it.
  if (sessionID) {
    for (const wf of workflows) {
      if (wf.session_id === sessionID) return wf
    }
  }

  // 2. No session match. Only fall back when exactly one workflow is active, so a
  //    session-tracking glitch does not disable the guard. With several parallel
  //    workflows we must NOT guess — binding this session to whichever flow happens
  //    to be first would gate its writes against the wrong phase (the flows "mix").
  if (workflows.length === 1) return workflows[0]

  // 3. Ambiguous: multiple parallel workflows, none owned by this session → don't guess.
  return null
}

async function getWorkflowForSessionStrict(sessionID?: string): Promise<Workflow | null> {
  if (!sessionID) return null

  const state = await fetchDashboardStateStrict()

  for (const wf of state.workflows) {
    if (wf.session_id === sessionID) return wf
  }
  return null
}

async function getWorkflowForTaskStrict(args: Record<string, unknown>, sessionID?: string): Promise<Workflow | null> {
  const taskText = getTaskText(args)
  const state = await fetchDashboardStateStrict()
  const workflows = state.workflows ?? []

  // 1. Explicit workflow ID embedded in the task text — the most reliable signal and
  //    the only one that disambiguates parallel workflows sharing a session. Supports
  //    workflow IDs that do not use the spec-/bug-/e2e- prefix.
  const textMatches = workflows.filter((wf) => wf.id && taskText.includes(wf.id))
  if (textMatches.length === 1) return textMatches[0]
  if (textMatches.length > 1) {
    // Multiple IDs appear in the prompt: prefer one owned by this session; otherwise
    // it is genuinely ambiguous and we must not pick by list order.
    return textMatches.find((wf) => sessionID && wf.session_id === sessionID) ?? null
  }

  // 2. Fall back to the prefix regex for explicit IDs not present in dashboard state.
  const explicitWorkflowID = extractWorkflowIDFromTaskArgs(args)
  if (explicitWorkflowID) {
    const wf = await fetchWorkflowByID(explicitWorkflowID)
    if (wf) return wf
  }

  // 3. Last resort: session ownership — but only when it resolves to a single workflow.
  //    Picking the first of several session-mates is exactly what let parallel flows mix.
  if (sessionID) {
    const sessionMatches = workflows.filter((wf) => wf.session_id === sessionID)
    if (sessionMatches.length === 1) return sessionMatches[0]
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

function delegatedAgentType(args: Record<string, unknown>): string | undefined {
  for (const key of ["agent_type", "subagent_type", "type"]) {
    const value = args[key]
    if (typeof value === "string" && value) return value
  }
  return undefined
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
    "tool.execute.before": async (input: { tool: string; sessionID?: string }, output: { args: Record<string, unknown> }) => {
          const toolName = input.tool.toLowerCase()

          // workflow_existence_guard: block Task delegation without workflow
          if (toolName === "task") {
            const subagentType = delegatedAgentType(output.args)
            const isDelivery = subagentType && isDeliverySubagent(subagentType)

            if (isDelivery) {
              let wf: Workflow | null = null
              try {
                wf = await getWorkflowForTaskStrict(output.args, input.sessionID)
              } catch (err) {
                throw new Error(
                  `Cannot verify workflow: ${err}. Ensure Stratus server is running (stratus serve).`,
                )
              }

              if (!wf) {
                throw new Error(unresolvedWorkflowReason)
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

    "tool.execute.after": async (input: { tool: string; args: Record<string, unknown> }) => {
      if (!WATCH_TOOLS.includes(input.tool.toLowerCase())) return

      const filePath = (input.args["filePath"] ?? input.args["path"]) as string | undefined
      if (!filePath) return

      fetch(`${BASE}/api/retrieve/dirty`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ paths: [filePath] }),
      }).catch(() => {})
    },
  }
}
