// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import Team from './Team.svelte'
import type { ManagedTeam, TeamPreflight } from '$lib/teams'

vi.mock('$lib/ws', () => ({ wsClient: { on: () => () => {} } }))
let app: ReturnType<typeof mount> | undefined
let team: ManagedTeam
let requests: { path: string; body: unknown }[]
beforeEach(() => {
 requests = []
 team = {
  id: 'team-test', project_id: 'project', project_root: '/project', state: 'running',
  limits: { max_concurrent: 2, max_auto_rounds: 4, max_turns: 40, max_tokens: 0 },
  turns: 0, auto_wakeups: 0, input_tokens: 0, output_tokens: 0, usage_known: false,
  members: [{ id: 'chat', role: 'member', profile: { runtime: 'codex', model: '', effort: '' }, state: 'ready',
   attachment: { id: 'connection', generation: 1, state: 'connected',
    identity: { runtime: 'codex', host_id: 'local', native_id: 'original-chat', project_root: '/project' },
    capabilities: { queue: true, observe_user_turns: true, correlate_results: true, wake: true } } }],
  tasks: []
 }
 vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
  if (init?.method === 'POST') {
   requests.push({ path, body: JSON.parse(String(init.body)) })
   if (path.endsWith('/attachments')) team.members[0].attachment!.state = 'detached'
   return Response.json({ ok: true })
  }
  return Response.json({ teams: [team] })
 })
})
afterEach(async () => {
 if (app) await unmount(app)
 app = undefined
 document.body.innerHTML = ''
 vi.unstubAllGlobals()
})
async function show() {
 app = mount(Team, { target: document.body })
 flushSync()
 await vi.waitFor(() => expect(document.querySelector('.member')).not.toBeNull())
}

function pickerCatalog(): TeamPreflight {
 const profiles = Object.fromEntries(['lead', 'implementation', 'design', 'review', 'diagnosis', 'mechanical'].map(role => [role, { runtime: 'codex', model: 'gpt-6-astra', effort: 'high' }]))
 return {
  project_id: 'project', project_root: '/project', limits: team.limits, warnings: [],
  profiles: { codex: profiles },
  runtimes: [
   { runtime: 'codex', available: true, models: [
    { id: 'gpt-6-astra', efforts: ['xhigh', 'high', 'low'] },
    { id: 'gpt-6.1-sol', efforts: ['medium', 'high'] }
   ] },
   { runtime: 'opencode', available: true, models: [{ id: 'provider/compact', efforts: ['medium'] }] }
  ]
 }
}

async function showPickers(catalog = pickerCatalog()) {
 const ordinaryFetch = globalThis.fetch
 vi.stubGlobal('fetch', (path: string, init?: RequestInit) => path === '/api/teams/preflight'
  ? Promise.resolve(Response.json(catalog)) : ordinaryFetch(path, init))
 await show()
 const openSetup = [...document.querySelectorAll('button')].find(b => b.textContent === 'Nový tím')!
 openSetup.click()
 await vi.waitFor(() => expect(document.querySelector('table')).not.toBeNull())
}

function picker(label: string): HTMLSelectElement {
 const element = document.querySelector(`[aria-label="${label}"]`)
 expect(element?.tagName).toBe('SELECT')
 return element as HTMLSelectElement
}

function choose(select: HTMLSelectElement, value: string) {
 select.value = value
 select.dispatchEvent(new Event('change', { bubbles: true }))
 flushSync()
}

it('uses model and effort dropdowns and submits the chosen profiles', async () => {
 await showPickers()
 const model = picker('Model: Vedenie postupu')
 expect([...model.options].map(option => option.value)).toEqual(['gpt-6-astra', 'gpt-6.1-sol'])
 expect(picker('Uvažovanie: Vedenie postupu').value).toBe('high')
 expect(picker('Zložitá úloha: Vedenie postupu').value).toBe('xhigh')
 choose(model, 'gpt-6.1-sol')
 const effort = picker('Uvažovanie: Vedenie postupu')
 const complex = picker('Zložitá úloha: Vedenie postupu')
 expect([...effort.options].filter(option => !option.disabled).map(option => option.value)).toEqual(['medium', 'high'])
 expect(complex.value).toBe('')
 choose(effort, 'medium')
 choose(complex, 'high')
 const start = [...document.querySelectorAll('button')].find(b => b.textContent === 'Spustiť tím')!
 start.click()
 await vi.waitFor(() => expect(requests.some(request => request.path === '/api/teams')).toBe(true))
 const body = requests.find(request => request.path === '/api/teams')!.body as { members: { profile: unknown; complex_profile: unknown }[] }
 expect(body.members[0].profile).toEqual({ runtime: 'codex', model: 'gpt-6.1-sol', effort: 'medium' })
 expect(body.members[0].complex_profile).toEqual({ runtime: 'codex', model: 'gpt-6.1-sol', effort: 'high' })
})

it('updates the catalog for another runtime and requires a supported selection', async () => {
 await showPickers()
 choose(picker('Nástroj: Vedenie postupu'), 'opencode')
 const model = picker('Model: Vedenie postupu')
 expect([...model.options].filter(option => !option.disabled).map(option => option.value)).toEqual(['provider/compact'])
 const start = [...document.querySelectorAll('button')].find(b => b.textContent === 'Spustiť tím')!
 expect(start.disabled).toBe(true)
 choose(model, 'provider/compact')
 choose(picker('Uvažovanie: Vedenie postupu'), 'medium')
 choose(picker('Zložitá úloha: Vedenie postupu'), 'medium')
 expect(start.disabled).toBe(false)
})

it('shows unavailable configured values without substituting a model', async () => {
 const catalog = pickerCatalog()
 catalog.profiles.codex.lead.model = 'custom-missing'
 await showPickers(catalog)
 const model = picker('Model: Vedenie postupu')
 expect(model.value).toBe('custom-missing')
 expect(model.selectedOptions[0].disabled).toBe(true)
 expect(model.selectedOptions[0].textContent).toContain('nedostupný')
 expect([...document.querySelectorAll('button')].find(b => b.textContent === 'Spustiť tím')!.disabled).toBe(true)
})

it.each([{ models: [] }, { models: [{ id: 'no-levels', efforts: [] }] }])('blocks launch when the runtime has no selectable model or reasoning level', async ({ models }) => {
 const catalog = pickerCatalog()
 catalog.runtimes[0].models = models
 await showPickers(catalog)
 expect([...document.querySelectorAll('button')].find(b => b.textContent === 'Spustiť tím')!.disabled).toBe(true)
 expect(picker('Uvažovanie: Vedenie postupu').disabled).toBe(true)
})

it('detaches the connection without invoking owned-session recovery or stop', async () => {
 await show()
 expect(document.querySelector('.member')?.textContent).toContain('Model určuje pôvodný chat')
 const button = [...document.querySelectorAll('button')].find(b => b.textContent === 'Odpojiť od tímu')!
 button.click()
 await vi.waitFor(() => expect(requests).toEqual([
  { path: '/api/teams/team-test/attachments', body: { member_id: 'chat', action: 'detach' } }
 ]))
 await vi.waitFor(() => expect(document.querySelector('.member')?.textContent).toContain('Na obnovenie povedz v pôvodnom chate'))
})

it.each(['expired', 'disconnected'])('gives a rejoin path for %s without offering owned recovery', async state => {
 team.members[0].attachment!.state = state
 team.members[0].state = 'needs_user'
 await show()
 expect(document.querySelector('.member')?.textContent).toContain('Pripoj tento chat k tímu Stratus')
 expect([...document.querySelectorAll('button')].some(b => b.textContent === 'Obnoviť po kontrole')).toBe(false)
 expect([...document.querySelectorAll('button')].some(b => b.textContent === 'Viesť tím z tohto chatu')).toBe(false)
})

it('explains how to recover an uncertain task even while its connection remains live', async () => {
 team.members[0].state = 'needs_user'
 await show()
 expect(document.querySelector('.member')?.textContent).toContain('skontroluj výsledok a potom sa znovu pripoj')
 expect([...document.querySelectorAll('button')].some(b => b.textContent === 'Obnoviť po kontrole')).toBe(false)
})

it('does not present manual native turn bookkeeping as team-assigned work', async () => {
 team.tasks = [
  { id: 'native', from: 'user', to: 'chat', role: 'member', text: 'Private manual bookkeeping', state: 'running', kind: 'user', origin: 'external', created_at: '2026-11-04T12:00:00Z' },
  { id: 'assigned', from: 'lead', to: 'chat', role: 'review', text: 'Review requested change', state: 'queued', kind: 'task', created_at: '2026-11-04T12:00:01Z' }
 ]
 await show()
 expect(document.querySelector('.task-list')?.textContent).toContain('Review requested change')
 expect(document.querySelector('.task-list')?.textContent).not.toContain('Private manual bookkeeping')
})
