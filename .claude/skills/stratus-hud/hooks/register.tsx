import { atom, read, update } from 'claude-code'
import type { ElementConstructor, EngineInterface, HttpResponse, Register, RenderElement, TextProps } from 'claude-code'

import type { Alert, Mission, Pending, Snapshot, Swarm, Tab, Workflow } from '../types'
import { agentsNow, denialsOf, freshAlerts, freshDenials, goalCondition, isOwn, nextPhases, parseClean, pickMission, pickWorkflow, steps, taskCount } from './hud'

const PANE = 'stratus'
const POLL_MS = 5_000
// While the API is down, look the port up again every this many polls: the server may come back on another.
const REDIAL_POLLS = 6
const DEFAULT_PORT = 41777

const snapshot = atom({ plugin: 'stratus-hud', key: 'snapshot' } as const, null)
const tab = atom({ plugin: 'stratus-hud', key: 'tab' } as const, 'workflow')
const pending = atom({ plugin: 'stratus-hud', key: 'pending' } as const, null)

const TABS: [Tab, string][] = [['workflow', 'Workflow'], ['plan', 'Plan'], ['swarm', 'Swarm'], ['guardian', 'Guardian'], ['denials', 'Denials']]
const MARK: Record<string, string> = { done: '✓', in_progress: '▸', active: '▸', failed: '✗', blocked: '⏸' }

// A long title leaves the phases room on the band.
const TITLE_MAX = 40
const clip = (text: string) => (text.length > TITLE_MAX ? `${text.slice(0, TITLE_MAX - 1)}…` : text)

// plan → ▸implement → verify, the current phase marked and coloured; spans of one Text, so they wrap as words do
const drawSteps = (Text: ElementConstructor<TextProps>, wf: Workflow): RenderElement[] =>
  steps(wf).flatMap((s, i) => [
    ...(i > 0 ? [<Text dimColor> → </Text>] : []),
    <Text bold={s.isCurrent} color={s.isCurrent ? 'suggestion' : undefined} dimColor={!s.isCurrent}>
      {s.isCurrent ? '▸' : ''}{s.name}
    </Text>,
  ])

let base = `http://127.0.0.1:${DEFAULT_PORT}`
let projectDir = ''
let sessionId = ''
// What the previous poll saw, so a poll toasts only what changed since.
let isPrimed = false
// This session's workflow as last shown; another session's workflow is never tracked.
let shown: { id: string; title: string; phase: string } | null = null
let lastDenial = 0
let lastAlert = 0
let lastDrawn = ''
let offlinePolls = 0
let polling: Promise<void> | null = null

async function getJSON<T>($: EngineInterface, path: string): Promise<T | null> {
  const res = await $.http.fetch(base + path).catch(() => null)
  if (!res?.ok) return null
  try {
    return parseClean<T>(res.text)
  } catch (err) {
    if (err instanceof SyntaxError) return null
    throw err
  }
}

// `stratus port` resolves the port as the server does: STRATUS_PORT, then .stratus.json
async function resolvePort($: EngineInterface) {
  const run = await $.process.run(['stratus', 'port'], { cwd: projectDir, timeoutMs: 3_000 }).catch(() => null)
  const port = run?.exitCode === 0 ? Number(run.stdout.trim()) : NaN
  if (Number.isInteger(port) && port > 0) base = `http://127.0.0.1:${port}`
}

// A shown workflow that left the active list finished or was aborted: say which.
async function toastEnd($: EngineInterface, gone: NonNullable<typeof shown>) {
  const wf = await getJSON<Workflow>($, `/api/workflows/${encodeURIComponent(gone.id)}`)
  const title = wf?.title || gone.id
  if (wf?.aborted) $.ui.toast(`◈ ${title}: aborted`)
  else if (wf?.phase === 'complete') $.ui.toast(`◈ ${title}: ${gone.phase} → complete`)
}

async function poll($: EngineInterface) {
  // /clear starts a new session id in the same process
  sessionId = await $.session.id()
  const state = await getJSON<{ workflows: Workflow[] | null }>($, '/api/dashboard/state')
  if (state === null && ++offlinePolls % REDIAL_POLLS === 0) await resolvePort($)
  if (state !== null) offlinePolls = 0
  const workflow = state ? pickWorkflow(state.workflows ?? [], sessionId) : null
  const isMine = workflow !== null && isOwn(workflow, sessionId)
  const events = state && (await getJSON<{ results: Parameters<typeof denialsOf>[0] }>($, '/api/events/search?type=hook_denial&limit=50'))
  const denials = denialsOf(events?.results ?? [])
  const mission = state && pickMission((await getJSON<Mission[]>($, '/api/swarm/missions')) ?? [], workflow?.id ?? '')
  const swarm = mission ? await getJSON<Swarm>($, `/api/swarm/missions/${encodeURIComponent(mission.id)}`) : null
  const alerts = (state && (await getJSON<Alert[]>($, '/api/guardian/alerts'))) ?? []

  if (isPrimed && state) {
    if (shown && workflow?.id !== shown.id) await toastEnd($, shown)
    else if (isMine && shown && shown.phase !== workflow.phase) {
      $.ui.toast(`◈ ${workflow.title || workflow.id}: ${shown.phase} → ${workflow.phase}`)
    }
    for (const d of freshDenials(denials, lastDenial, sessionId)) $.ui.toast(`⛔ ${d.hook} blocked ${d.tool}: ${d.reason}`)
    for (const a of freshAlerts(alerts, lastAlert)) $.ui.toast(`⚠ Guardian: ${a.message}`)
  }
  if (state) {
    isPrimed = true
    shown = isMine ? { id: workflow.id, title: workflow.title, phase: workflow.phase } : null
    lastDenial = Math.max(lastDenial, denials[0]?.id ?? 0)
    lastAlert = Math.max(lastAlert, ...alerts.map(a => a.id))
  }

  const snap: Snapshot = { isOnline: state !== null, workflow, isOwn: isMine, denials, swarm, alerts }
  const drawn = JSON.stringify(snap)
  if (drawn === lastDrawn) return
  lastDrawn = drawn
  await update($, snapshot, () => snap)
}

// One poll at a time: a slow API must not stack polls up behind each other.
function refresh($: EngineInterface): Promise<void> {
  polling ??= poll($).finally(() => {
    polling = null
  })
  return polling
}

async function connect($: EngineInterface) {
  await resolvePort($)
  // The timer comes first, so a failed first poll does not end the polling.
  $.clock.every(POLL_MS, () => void refresh($))
  await refresh($)
}

// Null for a JSON success, else why the call failed. A server older than the abort route answers an unknown
// API path with the dashboard's HTML page and a 200.
function apiError(res: HttpResponse): string | null {
  if (!(res.headers['content-type'] ?? '').includes('application/json')) return 'no API answer; restart `stratus serve` with this Stratus version'
  if (res.ok) return null
  try {
    return (JSON.parse(res.text) as { error?: string }).error ?? res.text
  } catch (err) {
    if (err instanceof SyntaxError) return res.text
    throw err
  }
}

// Runs the change the person confirmed: a phase transition or an abort, both validated by the server.
async function confirm($: EngineInterface, p: Pending) {
  await update($, pending, () => null)
  const path = `/api/workflows/${encodeURIComponent(p.workflowId)}`
  const res = await (p.kind === 'abort'
    ? $.http.fetch(`${base}${path}/abort`, { method: 'POST' })
    : $.http.fetch(`${base}${path}/phase`, { method: 'PUT', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ phase: p.phase }) })
  ).catch((err: unknown) => (err instanceof Error ? err.message : String(err)))
  const failure = typeof res === 'string' ? res : apiError(res)
  if (failure !== null) {
    $.ui.toast(`◈ ${p.kind === 'abort' ? 'abort' : `move to ${p.phase}`} failed: ${failure}`)
  }
  // A poll that started before the change cannot show it, so wait it out and poll again.
  await polling
  await refresh($)
}

// Puts the workflow's goal in the prompt; the person sends it, so nothing runs unasked.
async function autopilot($: EngineInterface, wf: Workflow, isSwarm: boolean) {
  if ((await $.prompt.read()).text.trim() !== '') {
    $.ui.toast('◈ Autopilot: the prompt holds a draft; send or clear it first')
    return
  }
  const filled = await $.prompt
    .fill({ text: goalCondition(wf, isSwarm) })
    .catch((err: unknown) => ({ isFilled: false, refusal: err instanceof Error ? err.message : String(err) }))
  if (!filled.isFilled) {
    // The engine names its own refusals (no prompt box, a dialog open); a hook's carries no cause.
    $.ui.toast(`◈ Autopilot: could not fill the prompt${filled.refusal ? ` (${filled.refusal})` : ''}`)
    return
  }
  $.ui.toast('◈ Autopilot: the /goal is in the prompt. Press Enter to start; it runs unattended only in auto mode.')
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    await $.command.register({ name: 'stratus', description: 'Show the active Stratus workflow, its plan, swarm, Guardian alerts and hook denials' })
    // Nothing draws outside an interactive session, so there is nothing to poll for.
    if (e.isInteractive) {
      projectDir = e.cwd
      $.clock.after(0, () => void connect($))
    }

    return next(e)
  })

  on('command.run', { command: 'stratus' }, async $ => {
    await $.ui.open({ id: PANE, title: 'Stratus' })

    return { text: 'Stratus pane opened.' }
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const snap = await read($, snapshot)
    if (e.props.hasSurvey || snap === null) return next(e)

    const { Text } = $.ui.resolve(e)
    if (!snap.isOnline) return <Text dimColor>◈ stratus offline</Text>

    const wf = snap.workflow
    if (wf === null) return next(e)
    const { done, total } = taskCount(wf)
    const agents = agentsNow(wf)

    // One line: what does not fit is cut at its end.
    return (
      <Text wrap="truncate">
        <Text color="success">◈ </Text>
        <Text bold>{clip(wf.title || wf.id)}</Text>
        {!snap.isOwn && <Text dimColor> (another session)</Text>}
        <Text dimColor> · </Text>
        {drawSteps(Text, wf)}
        {total > 0 && <Text dimColor> · {done}/{total}</Text>}
        {agents.length > 0 && <Text dimColor> · {agents.join(', ')}</Text>}
      </Text>
    )
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Button, Markdown, Text } = $.ui.resolve(e)
    const snap = await read($, snapshot)
    const current = await read($, tab)
    const asked = await read($, pending)
    const wf = snap?.workflow ?? null

    const actions = (w: Workflow): RenderElement => {
      if (asked?.workflowId === w.id) {
        const question = asked.kind === 'abort' ? `Abort ${w.title || w.id}?` : `Move ${w.title || w.id} to ${asked.phase}?`
        return (
          <Box gap={1}>
            <Text color="warning">{question}</Text>
            <Button key="confirm" label="Confirm" variant="primary" onPress={() => void confirm($, asked)} />
            <Button key="cancel" label="Cancel" onPress={() => update($, pending, () => null)} />
          </Box>
        )
      }
      const isSwarm = snap?.swarm?.mission.workflow_id === w.id
      return (
        <Box gap={1}>
          <Button key="autopilot" label="▶ Autopilot" onPress={() => void autopilot($, w, isSwarm)} />
          {nextPhases(w).map(phase => (
            <Button key={`go-${phase}`} label={`→ ${phase}`} onPress={() => update($, pending, (): Pending => ({ workflowId: w.id, kind: 'phase', phase }))} />
          ))}
          <Button key="abort" label="Abort" onPress={() => update($, pending, (): Pending => ({ workflowId: w.id, kind: 'abort' }))} />
        </Box>
      )
    }

    const body = (): RenderElement | RenderElement[] => {
      if (snap === null) return <Text dimColor>Connecting to Stratus…</Text>
      if (!snap.isOnline) return <Text dimColor>Stratus API is not reachable at {base}. Start it with `stratus serve`.</Text>

      if (current === 'denials') {
        if (snap.denials.length === 0) return <Text dimColor>No hook denials recorded.</Text>
        return snap.denials.map(d => (
          <Box key={`denial-${d.id}`} flexDirection="column">
            <Text>
              <Text color="error">{d.hook}</Text> blocked {d.tool}
              <Text dimColor> · {d.ts}</Text>
            </Text>
            <Text dimColor>{d.reason}</Text>
          </Box>
        ))
      }

      if (current === 'guardian') {
        if (snap.alerts.length === 0) return <Text dimColor>No open Guardian alerts.</Text>
        return snap.alerts.map(a => (
          <Box key={`alert-${a.id}`} flexDirection="column">
            <Text>
              <Text color={a.severity === 'warning' ? 'warning' : undefined} dimColor={a.severity !== 'warning'}>{a.severity}</Text> {a.message}
            </Text>
            <Text dimColor>{a.type} · {a.created_at}</Text>
          </Box>
        ))
      }

      if (current === 'swarm') {
        const s = snap.swarm
        if (s === null) return <Text dimColor>No live swarm mission.</Text>
        return [
          <Text bold>{s.mission.title || s.mission.id}</Text>,
          <Text dimColor>{s.mission.status} · {s.mission.strategy}</Text>,
          <Text>Workers</Text>,
          ...(s.workers ?? []).map(w => (
            <Text key={`worker-${w.id}`} dimColor={w.status !== 'active'}>
              {MARK[w.status] ?? '·'} {w.agent_type} {w.status} · {w.branch_name} · {w.last_heartbeat}
            </Text>
          )),
          <Text>Tickets</Text>,
          ...(s.tickets ?? []).map(t => (
            <Text key={`ticket-${t.id}`} dimColor={t.status === 'done'}>
              {MARK[t.status] ?? '·'} {t.title} [{t.domain}]
            </Text>
          )),
        ]
      }

      if (wf === null) return <Text dimColor>No active workflow.</Text>

      if (current === 'plan') {
        return wf.plan_content ? <Markdown key="plan" text={wf.plan_content} /> : <Text dimColor>No plan recorded yet.</Text>
      }

      const delegated = Object.entries(wf.delegated_agents ?? {}).filter(([, agents]) => agents.length > 0) as [string, string[]][]
      return [
        <Text bold>{wf.title || wf.id}</Text>,
        <Text dimColor>{wf.id} · {wf.type}{wf.complexity === 'complex' ? ' (complex)' : ''}</Text>,
        <Text>{drawSteps(Text, wf)}</Text>,
        snap.isOwn ? actions(wf) : <Text dimColor>Started in another session; `/resume {wf.id}` here takes it over.</Text>,
        ...(wf.tasks ?? []).map(t => (
          <Text key={`task-${t.index}`} dimColor={t.status === 'done'}>
            {MARK[t.status] ?? '·'} {t.title}
          </Text>
        )),
        ...delegated.map(([phase, agents]) => (
          <Text key={`agents-${phase}`} dimColor>
            {phase}: {agents.join(', ')}
          </Text>
        )),
      ]
    }

    return (
      <Box flexDirection="column" gap={1}>
        <Box gap={1}>
          {TABS.map(([id, label]) => (
            <Button
              key={`tab-${id}`}
              label={label}
              variant={id === current ? 'primary' : 'secondary'}
              onPress={() => update($, tab, () => id)}
            />
          ))}
        </Box>
        <Box flexDirection="column">{body()}</Box>
      </Box>
    )
  })
}
