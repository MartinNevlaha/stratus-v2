import { describe, expect, it } from 'vitest'
import { afterEach, vi } from 'vitest'
import { applySavedProfiles, defaultComplexEffort, launchMembers, profileSaveBody, saveProjectProfile, sourceLabel, teamRequest, TeamTimeoutError, usageLabel } from './teams'
import type { TeamPreflight } from './teams'

describe('team launch choices', () => {
 it('keeps explicit model and complex effort without silently falling back', () => {
  const rows = [{role:'design',runtime:'codex',model:'gpt-6-astra',effort:'high',complexEffort:'xhigh'},{role:'implementation',runtime:'codex',model:'gpt-6.1-sol',effort:'high',complexEffort:'high'}]
  const members = launchMembers(rows)
  expect(members[0].complex_profile?.effort).toBe('xhigh')
  expect(members[1].profile.model).toBe('gpt-6.1-sol')
  expect(()=>launchMembers([{...rows[0],model:''}])).toThrow()
 })
 it('distinguishes unknown consumption from measured zero', () => {
  expect(usageLabel(false,0,0)).toContain('neznáma')
  expect(usageLabel(true,0,0)).toContain('0')
 })
})

const row = {role:'review',runtime:'codex',model:'gpt-6-astra',effort:'high',complexEffort:'xhigh'}
const preflight = {project_id:'p',project_root:'/p',runtimes:[],profiles:{codex:{review:{runtime:'codex',model:'gpt-6-astra',effort:'high'}}},warnings:[],limits:{max_concurrent:2,max_auto_rounds:4,max_turns:40,max_tokens:0}} as TeamPreflight

describe('project profile', () => {
 afterEach(() => vi.unstubAllGlobals())
 it('builds the save body grouped by runtime with complex effort', () => {
  expect(profileSaveBody([row,{...row,role:'lead',runtime:'claude-code',model:'sonnet',effort:'high',complexEffort:''}])).toEqual({
   codex:{review:{model:'gpt-6-astra',effort:'high',complex_effort:'xhigh'}},
   'claude-code':{lead:{model:'sonnet',effort:'high'}}
  })
  expect(()=>profileSaveBody([{...row,effort:''}])).toThrow()
 })
 it('sends a PUT to the profiles endpoint and returns the saved profiles', async () => {
  const calls:{path:string;init?:RequestInit}[] = []
  vi.stubGlobal('fetch', async (path:string,init?:RequestInit)=>{calls.push({path,init});return Response.json({profiles:{codex:{review:{model:'gpt-6-astra',effort:'high'}}}})})
  const saved = await saveProjectProfile([row])
  expect(calls[0].path).toBe('/api/teams/profiles')
  expect(calls[0].init?.method).toBe('PUT')
  expect(JSON.parse(String(calls[0].init?.body)).codex.review.complex_effort).toBe('xhigh')
  expect(saved.codex.review.model).toBe('gpt-6-astra')
 })
 it('surfaces the server validation message', async () => {
  vi.stubGlobal('fetch', async ()=>Response.json({error:'codex/review: unknown model'},{status:400}))
  await expect(saveProjectProfile([row])).rejects.toThrow('unknown model')
 })
 it('marks saved profiles as project source and reuses the saved complex effort', () => {
  const next = applySavedProfiles(preflight,{codex:{review:{model:'gpt-6.1-sol',effort:'medium',complex_effort:'high'}}})
  expect(next.profiles.codex.review).toEqual({runtime:'codex',model:'gpt-6.1-sol',effort:'medium'})
  expect(next.profile_sources?.codex.review).toBe('project')
  expect(defaultComplexEffort(next,'codex','review')).toBe('high')
  expect(defaultComplexEffort(preflight,'codex','review')).toBe('xhigh')
  expect(preflight.profile_sources).toBeUndefined()
 })
 it('labels layers', () => {
  expect(sourceLabel('project')).toBe('Profil projektu')
  expect(sourceLabel('agent-file')).toBe('Súbor agenta')
  expect(sourceLabel(undefined)).toBe('')
 })
})

describe('teamRequest timeout', () => {
 // A fetch that never answers and rejects like the browser does once its signal aborts.
 function hangingFetch() {
  vi.stubGlobal('fetch', (_path: string, init?: RequestInit) => new Promise((_resolve, reject) => {
   init?.signal?.addEventListener('abort', () => reject(new DOMException('The operation was aborted.', 'AbortError')))
  }))
 }
 afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

 it('aborts after 30 seconds by default with a readable message', async () => {
  vi.useFakeTimers()
  hangingFetch()
  const pending = teamRequest('teams').then(() => 'resolved', (e: unknown) => e)
  await vi.advanceTimersByTimeAsync(29_999)
  let settled = false
  void pending.then(() => { settled = true })
  await vi.advanceTimersByTimeAsync(0)
  expect(settled).toBe(false)
  await vi.advanceTimersByTimeAsync(1)
  const error = await pending
  expect(error).toBeInstanceOf(TeamTimeoutError)
  expect((error as Error).message).toBe('Požiadavka trvá príliš dlho; server ju možno ešte spracúva. Obnovte stav o chvíľu.')
 })

 it('honours a longer timeoutMs', async () => {
  vi.useFakeTimers()
  hangingFetch()
  const pending = teamRequest('teams/t/verify', {}, 'POST', 360_000).then(() => 'resolved', (e: unknown) => e)
  await vi.advanceTimersByTimeAsync(359_999)
  let settled = false
  void pending.then(() => { settled = true })
  await vi.advanceTimersByTimeAsync(0)
  expect(settled).toBe(false)
  await vi.advanceTimersByTimeAsync(1)
  expect(await pending).toBeInstanceOf(TeamTimeoutError)
 })
})
