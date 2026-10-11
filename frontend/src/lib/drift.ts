// Plan-vs-actual: which files did agents touch that the plan never mentioned?

const URL_RE = /https?:\/\/\S+/g
// Directory-qualified paths: api/routes.go, frontend/src/App.svelte
const PATH_RE = /(?:[\w.-]+\/)+[\w.-]+\.[A-Za-z0-9]{1,8}/g
// Bare file names only count with an extension that is really a file ("e.g." or "v1.2" are not).
const FILE_EXT = 'go|ts|tsx|js|jsx|mjs|cjs|svelte|vue|py|rb|rs|java|kt|swift|c|h|cpp|hpp|cs|php|sql|json|yaml|yml|toml|md|txt|html|css|scss|sh|mod|sum|lock|env|xml|csv'
const BARE_RE = new RegExp(`(?<![\\w./-])[\\w-]+\\.(?:${FILE_EXT})\\b`, 'g')

/** File-like paths and file names mentioned in plan markdown (URLs excluded). */
export function plannedPaths(plan: string | undefined | null): string[] {
  if (!plan) return []
  const text = plan.replace(URL_RE, ' ')
  const found = new Set<string>()
  for (const m of text.matchAll(PATH_RE)) found.add(m[0])
  for (const m of text.matchAll(BARE_RE)) found.add(m[0])
  return [...found]
}

/** Touched files that match none of the planned paths. Empty if the plan names no files. */
export function outOfScopeFiles(touched: string[], planned: string[]): string[] {
  if (planned.length === 0) return []
  return touched.filter(f => {
    if (f.includes('/docs/plans/')) return false // the workflow's own plan/design docs
    return !planned.some(p => (p.includes('/') ? f.endsWith('/' + p) || f === p : f.split('/').pop() === p))
  })
}
