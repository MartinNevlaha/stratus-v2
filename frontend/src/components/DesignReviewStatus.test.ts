// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import DesignReviewStatus from './DesignReviewStatus.svelte'
import type { DesignReviewStatus as Status } from '$lib/types'

const wsHandlers = vi.hoisted(() => new Map<string, Set<() => void>>())
vi.mock('$lib/ws', () => ({
  wsClient: {
    on: (type: string, fn: () => void) => {
      if (!wsHandlers.has(type)) wsHandlers.set(type, new Set())
      wsHandlers.get(type)!.add(fn)
      return () => wsHandlers.get(type)?.delete(fn)
    },
  },
}))

let app: ReturnType<typeof mount> | undefined
let calls: string[]
let posts: { path: string; body: unknown }[]

const status = (over: Partial<Status> = {}): Status => ({
  enabled: true, gated_transition: 'design->governance', state: 'reviewing', round: 2, max_rounds: 3,
  reviewer: { id: 'review-4', role: 'review' }, author: { id: 'design-3', role: 'design' },
  docs: [{ path: 'docs/plans/x-design.md', bytes: 1200 }], doc_hash: 'abc', last_verdict: '', last_review_task_id: '',
  issues_excerpt: '', waived: false, ...over,
})

function stub(route: (call: number, init?: RequestInit) => Response | Promise<Response>) {
  calls = []
  posts = []
  vi.stubGlobal('fetch', async (input: string, init?: RequestInit) => {
    if (init?.method === 'POST') posts.push({ path: String(input), body: JSON.parse(String(init.body)) })
    else calls.push(String(input))
    return route(calls.length, init)
  })
}
function show(workflowId = 'spec-a') {
  app = mount(DesignReviewStatus, { target: document.body, props: { workflowId, teamId: 't1' } })
  flushSync()
}
const chip = () => document.querySelector('.design-review summary')

beforeEach(() => wsHandlers.clear())
afterEach(async () => {
  if (app) await unmount(app)
  app = undefined
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('DesignReviewStatus', () => {
  it.each([
    ['none', 'Review návrhu: čaká na spustenie'],
    ['reviewing', 'Review návrhu: prebieha (kolo 2/3)'],
    ['reworking', 'Review návrhu: prepracovanie (kolo 2/3)'],
    ['pass', 'Review návrhu: PASS'],
    ['fail', 'Review návrhu: FAIL – na prepracovanie'],
    ['waived', 'Review návrhu: schválené používateľom'],
    ['exhausted', 'Review návrhu: vyčerpané kolá – rozhodni'],
    ['review_failed', 'Review návrhu: review zlyhalo'],
    ['documents_missing', 'Review návrhu: chýbajú dokumenty'],
    ['documents_invalid', 'Review návrhu: neplatné dokumenty'],
  ])('should label the %s state', async (state, label) => {
    stub(() => Response.json(status({ state: state as Status['state'] })))
    show()
    await vi.waitFor(() => expect(chip()?.textContent?.trim()).toBe(label))
    expect(calls).toEqual(['/api/workflows/spec-a/design-review'])
  })

  it('should render nothing for an inactive gate, a disabled gate and an unknown workflow', async () => {
    for (const response of [() => Response.json(status({ state: 'inactive' })), () => Response.json({ enabled: false }), () => Response.json({ error: 'not found' }, { status: 404 })]) {
      stub(response)
      show()
      await vi.waitFor(() => expect(calls).toHaveLength(1))
      await new Promise(r => setTimeout(r, 0))
      flushSync()
      expect(document.querySelector('.design-review')).toBeNull()
      await unmount(app!)
      app = undefined
    }
  })

  it('should show docs, reviewer, author, issues and the message as plain text in a collapsed details', async () => {
    stub(() => Response.json(status({
      state: 'fail', issues_excerpt: '<b>[must_fix]</b> missing rollback', message: 'Prepracuj dokumenty', missing: ['docs/adr/x.md'],
    })))
    show()
    await vi.waitFor(() => expect(chip()).not.toBeNull())
    const details = document.querySelector('.design-review') as HTMLDetailsElement
    expect(details.open).toBe(false)
    expect(details.textContent).toContain('docs/plans/x-design.md')
    expect(details.textContent).toContain('review-4')
    expect(details.textContent).toContain('design-3')
    expect(details.textContent).toContain('Prepracuj dokumenty')
    expect(details.textContent).toContain('docs/adr/x.md')
    const pre = details.querySelector('pre')!
    expect(pre.textContent).toBe('<b>[must_fix]</b> missing rollback')
    expect(pre.querySelector('b')).toBeNull()
  })

  it('should never let a stale response overwrite a newer one', async () => {
    const releases: ((r: Response) => void)[] = []
    stub(() => new Promise<Response>(resolve => { releases.push(resolve) }))
    show()
    await vi.waitFor(() => expect(releases).toHaveLength(1))
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    wsHandlers.get('team_updated')!.forEach(fn => fn())
    await vi.advanceTimersByTimeAsync(200)
    vi.useRealTimers()
    await vi.waitFor(() => expect(releases).toHaveLength(2))
    releases[1](Response.json(status({ state: 'pass' })))
    await vi.waitFor(() => expect(chip()?.textContent).toContain('PASS'))
    releases[0](Response.json(status({ state: 'fail' })))
    await new Promise(r => setTimeout(r, 20))
    expect(chip()?.textContent).toContain('PASS')
  })

  it('should refresh once, debounced, on team_updated and stop listening on unmount', async () => {
    stub(() => Response.json(status()))
    show()
    await vi.waitFor(() => expect(calls).toHaveLength(1))
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    wsHandlers.get('team_updated')!.forEach(fn => { fn(); fn() })
    expect(calls).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(200)
    expect(calls).toHaveLength(2)
    await unmount(app!)
    app = undefined
    expect(wsHandlers.get('team_updated')?.size ?? 0).toBe(0)
  })

  describe('stuck review', () => {
    it('should show a stuck chip with the reason, the states and the reason in the title', async () => {
      stub(() => Response.json(status({ stuck: true, reason: 'reviewer is waiting for approval', team_state: 'paused', reviewer_state: 'needs_user' })))
      show()
      await vi.waitFor(() => expect(chip()?.textContent?.trim()).toBe('Review návrhu: zaseknuté'))
      expect(chip()?.getAttribute('title')).toBe('reviewer is waiting for approval')
      expect(chip()?.className).toContain('warn')
      const details = document.querySelector('.design-review')!.textContent!
      expect(details).toContain('reviewer is waiting for approval')
      expect(details).toContain('Pozastavený')
      expect(details).toContain('Potrebuje teba')
    })
  })

  describe('override', () => {
    const overrideButton = () => [...document.querySelectorAll('.design-review button')].find(b => b.textContent?.trim() === 'Schváliť bez review') as HTMLButtonElement | undefined
    const byText = (t: string) => [...document.querySelectorAll('.design-review button')].find(b => b.textContent?.trim() === t) as HTMLButtonElement | undefined

    it.each([
      ['fail', {}, true],
      ['exhausted', {}, true],
      ['review_failed', {}, true],
      ['reviewing', { stuck: true }, true],
      ['reworking', { stuck: true }, true],
      ['reviewing', {}, false],
      ['reworking', {}, false],
      ['none', {}, false],
      ['pass', {}, false],
      ['waived', {}, false],
      ['documents_missing', {}, false],
      ['documents_invalid', {}, false],
    ])('should offer the override in state %s %j: %s', async (state, extra, allowed) => {
      stub(() => Response.json(status({ state: state as Status['state'], ...extra })))
      show()
      await vi.waitFor(() => expect(chip()).not.toBeNull())
      expect(overrideButton() !== undefined).toBe(allowed)
    })

    async function failing() {
      stub((_call, init) => init?.method === 'POST'
        ? Response.json({ ok: true })
        : Response.json(status({ state: 'fail' })))
      show()
      await vi.waitFor(() => expect(overrideButton()).toBeDefined())
    }

    it('should ask for confirmation and send nothing until it is given', async () => {
      await failing()
      overrideButton()!.click()
      flushSync()
      expect(document.body.textContent).toContain('Schváliť návrh bez úspešného review?')
      expect(posts).toHaveLength(0)
    })

    it('should send nothing when cancelled', async () => {
      await failing()
      overrideButton()!.click()
      flushSync()
      byText('Zrušiť')!.click()
      flushSync()
      expect(byText('Áno, schváliť')).toBeUndefined()
      expect(posts).toHaveLength(0)
    })

    it('should post workflow_id and the edited reason, then reload the status', async () => {
      let waived = false
      stub((_call, init) => {
        if (init?.method === 'POST') { waived = true; return Response.json({ ok: true }) }
        return Response.json(status({ state: waived ? 'waived' : 'fail' }))
      })
      show()
      await vi.waitFor(() => expect(overrideButton()).toBeDefined())
      overrideButton()!.click()
      flushSync()
      const input = document.querySelector('input[aria-label="Dôvod schválenia"]') as HTMLInputElement
      input.value = 'Prečítal som to sám'
      input.dispatchEvent(new Event('input', { bubbles: true }))
      flushSync()
      byText('Áno, schváliť')!.click()
      await vi.waitFor(() => expect(posts).toHaveLength(1))
      expect(posts[0]).toEqual({ path: '/api/teams/t1/design-review/override', body: { workflow_id: 'spec-a', reason: 'Prečítal som to sám' } })
      await vi.waitFor(() => expect(chip()?.textContent).toContain('schválené používateľom'))
      expect(calls).toHaveLength(2)
    })

    it('should show a refused override in an alert and not reload', async () => {
      stub((_call, init) => init?.method === 'POST'
        ? Response.json({ error: 'design review gate is not blocking' }, { status: 409 })
        : Response.json(status({ state: 'fail' })))
      show()
      await vi.waitFor(() => expect(overrideButton()).toBeDefined())
      overrideButton()!.click()
      flushSync()
      byText('Áno, schváliť')!.click()
      await vi.waitFor(() => expect(document.querySelector('.design-review [role="alert"]')?.textContent).toContain('design review gate is not blocking'))
      expect(calls).toHaveLength(1)
    })
  })
})
