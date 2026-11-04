// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import Team from './Team.svelte'
import type { ManagedTeam } from '$lib/teams'

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
