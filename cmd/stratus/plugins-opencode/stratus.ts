import type { Plugin } from "@opencode-ai/plugin"

const BASE = "http://localhost:41777"

const WRITE_TOOLS = ["write", "edit", "bash", "patch"]
const WATCH_TOOLS = ["write", "edit"]

async function getActiveWorkflow(): Promise<{ phase: string } | null> {
  try {
    const res = await fetch(`${BASE}/api/dashboard/state`)
    if (!res.ok) return null
    const state = await res.json()
    return state?.active_workflow ?? null
  } catch {
    return null // fail open — server not running
  }
}

export const Stratus: Plugin = async () => {
  return {
    tool: {
      execute: {
        // phase_guard: block write/edit/bash during verify/review phases
        before: async (input: { tool: string }, output: { args: Record<string, unknown> }) => {
          if (!WRITE_TOOLS.includes(input.tool)) return

          const wf = await getActiveWorkflow()
          if (!wf) return

          if (["verify", "review"].includes(wf.phase)) {
            throw new Error(
              `[Stratus] Phase guard: '${input.tool}' is blocked during the '${wf.phase}' phase. ` +
              `Only read-only tools (read, grep, glob) are allowed. ` +
              `Complete the ${wf.phase} phase before making changes.`,
            )
          }
        },

        // watcher: queue modified files for vexor reindex
        after: async (input: { tool: string }, output: { args: Record<string, unknown> }) => {
          if (!WATCH_TOOLS.includes(input.tool)) return

          const filePath = (output.args.filePath ?? output.args.path) as string | undefined
          if (!filePath) return

          fetch(`${BASE}/api/retrieve/dirty`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ paths: [filePath] }),
          }).catch(() => {}) // best effort
        },
      },
    },
  }
}
