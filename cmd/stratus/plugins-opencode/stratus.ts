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
  "If several workflows are active in parallel, name the one this delegation belongs to once at the top of the " +
  "Task prompt as `Workflow ID: <id>` (document paths such as <id>-design.md and slugs of inactive workflows are ignored)."

// Mirrors hooks/phase_guard.go so both runtimes resolve identically.
const WORKFLOW_ID_PATTERN = "(?:bug|spec|e2e)-[a-z0-9][a-z0-9-]{0,120}"

// The `Workflow ID: <id>` / `workflow_id: <id>` line the workflow skills put at the top of
// every brief, tolerating markdown emphasis and quotes.
const LABELED_WORKFLOW_ID_RE = new RegExp(`workflow[ _-]?id[*\`"']*\\s*[:=]\\s*[*\`"']*(${WORKFLOW_ID_PATTERN})`, "gi")

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

function isSlugChar(ch: string): boolean {
  return /[A-Za-z0-9_-]/.test(ch)
}

// Whole-token occurrence: `spec-a-design.md` and `myspec-a` contain spec-a but do not name it.
function mentionsWorkflowID(text: string, id: string): boolean {
  for (let i = text.indexOf(id); i >= 0; i = text.indexOf(id, i + 1)) {
    const end = i + id.length
    if ((i === 0 || !isSlugChar(text[i - 1])) && (end === text.length || !isSlugChar(text[end]))) return true
  }
  return false
}

// Labeled IDs come from a `Workflow ID: <id>` line, deduplicated in order; mentioned IDs are
// active workflows named as a whole token. Document paths and slugs of inactive workflows are
// context, not identification.
function promptWorkflowIDs(
  args: Record<string, unknown>,
  workflows: Workflow[],
): { labeled: string[]; mentioned: string[] } {
  const taskText = getTaskText(args)
  const labeled: string[] = []
  for (const m of taskText.matchAll(LABELED_WORKFLOW_ID_RE)) {
    if (!labeled.includes(m[1])) labeled.push(m[1])
  }
  const mentioned = workflows.filter((wf) => wf.id && mentionsWorkflowID(taskText, wf.id)).map((wf) => wf.id)
  return { labeled, mentioned }
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
  const state = await fetchDashboardStateStrict()
  const workflows = state.workflows ?? []
  const { labeled, mentioned } = promptWorkflowIDs(args, workflows)

  // 1. A labeled `Workflow ID: <id>` line is the brief's own statement: one is
  //    authoritative, two different ones are a confused brief and must not be guessed at.
  //    Without a label, a bare mention of exactly one active workflow identifies it.
  if (labeled.length > 1) return null
  const promptID = labeled[0] ?? (mentioned.length === 1 ? mentioned[0] : undefined)
  if (promptID) {
    return workflows.find((wf) => wf.id === promptID) ?? (await fetchWorkflowByID(promptID))
  }

  // 2. Last resort: session ownership — but only when it resolves to a single workflow.
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
