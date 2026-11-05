// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushSync, mount, unmount } from 'svelte'
import TeamHandoff from './TeamHandoff.svelte'
import type { TeamHandoff as Handoff } from '$lib/types'

const ws = vi.hoisted(() => ({ handlers: {} as Record<string, Array<() => void>> }))
vi.mock('$lib/ws', () => ({
  wsClient: {
    on: (type: string, fn: () => void) => {
      ;(ws.handlers[type] ??= []).push(fn)
      return () => { ws.handlers[type] = (ws.handlers[type] ?? []).filter(h => h !== fn) }
    },
  },
}))

const REV = 'abcdef1234567890'

function handoff(over: Partial<Handoff> = {}): Handoff {
  return {
    isolated: true,
    integration: { dir_kind: 'integration', branch: 'stratus/team-t1', revision: REV, dirty: false },
    members: [
      { id: 'implementation-2', role: 'implementation', branch: 'stratus/t1/implementation-2', ahead: 2, uncommitted: 3 },
      { id: 'mechanical-6', role: 'mechanical', branch: 'stratus/t1/mechanical-6', ahead: 0, uncommitted: 0 },
    ],
    merged: ['mechanical-6'],
    conflicts: [{ member: 'implementation-2', branch: 'stratus/t1/implementation-2', files: ['api/a.go', 'api/b.go'] }],
    checks: {
      expected_gates: ['build', 'vet', 'placeholder_scan'],
      gates: [
        { name: 'build', verdict: 'pass', content: 'ok', revision: REV, current: true },
        { name: 'vet', verdict: 'fail', content: 'vet failed', revision: REV, current: true },
      ],
      checks_ok: false, revision: REV, current: true,
    },
    review: { verdict: 'pass', revision: REV, current: true, valid: true },
    diff_stat: { files_changed: 4, lines_added: 50, lines_removed: 6 },
    warnings: ['member implementation-2 has 3 uncommitted change(s)'],
    ...over,
  }
}

const noIntegration = (): Handoff => handoff({
  integration: undefined, merged: [], conflicts: [], review: undefined, diff_stat: undefined,
  checks: { gates: [], checks_ok: false, revision: '', current: false },
})

let calls: { path: string; method: string }[]
let app: ReturnType<typeof mount> | undefined
let target: HTMLElement

function stub(route: (path: string, method: string, signal?: AbortSignal | null) => Response | Promise<Response>) {
  calls = []
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ path, method })
    return route(path, method, init?.signal)
  })
}
const count = (path: string, method: string) => calls.filter(c => c.path === path && c.method === method).length

function show(props: { teamId?: string; onremoved?: () => void } = {}) {
  target = document.createElement('div')
  document.body.appendChild(target)
  app = mount(TeamHandoff, { target, props: { teamId: 't1', ...props } })
  flushSync()
}
function button(label: string): HTMLButtonElement {
  const found = [...target.querySelectorAll('button')].find(b => b.textContent?.trim() === label)
  expect(found, `button ${label}`).toBeDefined()
  return found as HTMLButtonElement
}
const maybeButton = (label: string) => [...target.querySelectorAll('button')].find(b => b.textContent?.trim() === label)
const text = () => target.textContent ?? ''

beforeEach(() => { ws.handlers = {} })
afterEach(async () => {
  if (app) await unmount(app)
  app = undefined
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('TeamHandoff', () => {
  it('should render revision, tree state, members with ahead and uncommitted counts and the diff stat', async () => {
    stub(() => Response.json(handoff()))
    show()
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    expect(text()).not.toContain('abcdef12')
    expect(text()).toContain('čistý')
    expect(text()).toContain('implementation-2')
    expect(text()).toContain('stratus/t1/implementation-2')
    expect(text()).toContain('2 commitov pred')
    expect(text()).toContain('3 neuložených súborov')
    expect(text()).toContain('4 súborov')
    expect(text()).toContain('member implementation-2 has 3 uncommitted change(s)')
    expect(calls[0]).toEqual({ path: '/api/teams/t1/handoff', method: 'GET' })
  })

  it('should show a dirty integration tree', async () => {
    stub(() => Response.json(handoff({ integration: { dir_kind: 'integration', branch: 'b', revision: REV, dirty: true } })))
    show()
    await vi.waitFor(() => expect(text()).toContain('neuložené zmeny'))
  })

  it('should list merged members and keep conflict files collapsed in details', async () => {
    stub(() => Response.json(handoff()))
    show()
    await vi.waitFor(() => expect(text()).toContain('Zlúčené: mechanical-6'))
    const details = target.querySelector('details.conflict') as HTMLDetailsElement
    expect(details).not.toBeNull()
    expect(details.open).toBe(false)
    expect(details.textContent).toContain('implementation-2')
    expect(details.textContent).toContain('api/a.go')
    expect(details.textContent).toContain('api/b.go')
  })

  it('should show gate chips with missing and outdated states and the checks label', async () => {
    stub(() => Response.json(handoff({
      checks: {
        gates: [
          { name: 'build', verdict: 'pass', content: 'ok', revision: REV, current: true },
          { name: 'vet', verdict: 'pass', content: 'ok', revision: '1111111', current: false },
        ],
        checks_ok: false, revision: REV, current: false,
      },
    })))
    show()
    await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(3))
    const chips = [...target.querySelectorAll('.gate-chip')]
    expect(chips[0].className).toContain('pass')
    expect(chips[1].className).toContain('outdated')
    expect(chips[1].textContent).toContain('zastaralé')
    expect(chips[2].className).toContain('missing')
    expect(chips[2].textContent).toContain('placeholder chýba')
    expect(text()).toContain('Kontroly neúplné')
  })

  it('should label the checks as OK when the server says so', async () => {
    stub(() => Response.json(handoff({ checks: { gates: [], checks_ok: true, revision: REV, current: true } })))
    show()
    await vi.waitFor(() => expect(text()).toContain('Kontroly OK'))
  })

  it.each([
    ['valid pass', { verdict: 'pass', revision: REV, current: true, valid: true }, 'PASS - platné'],
    ['fail at the current revision', { verdict: 'fail', revision: REV, current: true, valid: false, reason: 'review verdict is fail' }, 'FAIL'],
    ['outdated review', { verdict: 'pass', revision: '1111111', current: false, valid: false, reason: 'reviewed an older revision' }, 'Zastaralé'],
    ['no verdict', { verdict: '', revision: REV, current: true, valid: false }, 'Bez verdiktu'],
  ])('should label a %s review', async (_name, review, label) => {
    stub(() => Response.json(handoff({ review })))
    show()
    await vi.waitFor(() => expect(target.querySelector('.review-state')?.textContent).toContain(label))
  })

  it('should show a not-integrated state and disable verification until an integration exists', async () => {
    stub(() => Response.json(noIntegration()))
    show()
    await vi.waitFor(() => expect(text()).toContain('Zatiaľ nezlúčené'))
    expect(button('Spustiť kontroly').disabled).toBe(true)
    expect(button('Zlúčiť výsledky').disabled).toBe(false)
  })

  it('should post to /integrate, report the result and reload the handoff', async () => {
    stub((path, method) => path.endsWith('/integrate') && method === 'POST'
      ? Response.json({ branch: 'b', revision: REV, dirty: false, merged: ['a', 'b'], conflicts: [{ member: 'c', branch: 'x', files: ['f'] }], nothing_to_merge: [], warnings: [] })
      : Response.json(noIntegration()))
    show()
    await vi.waitFor(() => expect(count('/api/teams/t1/handoff', 'GET')).toBe(1))
    flushSync()
    button('Zlúčiť výsledky').click()
    await vi.waitFor(() => expect(count('/api/teams/t1/integrate', 'POST')).toBe(1))
    await vi.waitFor(() => expect(count('/api/teams/t1/handoff', 'GET')).toBe(2))
    await vi.waitFor(() => expect(target.querySelector('[role="status"]')?.textContent).toContain('Zlúčené: 2'))
    expect(target.querySelector('[role="status"]')?.textContent).toContain('konflikty: 1')
  })

  it('should run the checks, show the chips from the result and say when it came from memory', async () => {
    stub((path, method) => {
      if (path.endsWith('/verify') && method === 'POST') {
        return Response.json({
          cached: false,
          checks: {
            gates: ['build', 'vet', 'placeholder_scan'].map(name => ({ name, verdict: 'pass', content: 'ok', revision: REV, current: true })),
            checks_ok: true, revision: REV, current: true,
          },
        })
      }
      return Response.json(handoff({ checks: { gates: [], checks_ok: false, revision: '', current: false } }))
    })
    show()
    await vi.waitFor(() => expect(text()).toContain('placeholder chýba'))
    button('Spustiť kontroly').click()
    await vi.waitFor(() => expect(count('/api/teams/t1/verify', 'POST')).toBe(1))
    await vi.waitFor(() => expect(text()).toContain('Kontroly OK'))
    expect(target.querySelectorAll('.gate-chip.pass')).toHaveLength(3)
    expect(count('/api/teams/t1/handoff', 'GET')).toBe(2)
  })

  it('should allow one action at a time', async () => {
    let release: (r: Response) => void = () => {}
    stub((path, method) => path.endsWith('/integrate') && method === 'POST'
      ? new Promise<Response>(resolve => { release = resolve })
      : Response.json(handoff()))
    show()
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    button('Zlúčiť výsledky').click()
    await vi.waitFor(() => expect(button('Spustiť kontroly').disabled).toBe(true))
    expect(button('Zlúčiť výsledky').disabled).toBe(true)
    expect(button('Upratať pracovné priestory').disabled).toBe(true)
    release(Response.json({ branch: 'b', revision: REV, dirty: false, merged: [], conflicts: [], nothing_to_merge: [], warnings: [] }))
    await vi.waitFor(() => expect(button('Zlúčiť výsledky').disabled).toBe(false))
  })

  it('should show server blockers on 409 and send nothing forced until confirmed', async () => {
    const onremoved = vi.fn()
    stub((path, method) => method === 'DELETE'
      ? Response.json({ code: 'workspaces_blocked', error: 'conflict: workspaces hold unmerged work', blockers: ['member a has 2 unmerged commit(s)', 'integration has uncommitted changes'] }, { status: 409 })
      : Response.json(handoff()))
    show({ onremoved })
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    button('Upratať pracovné priestory').click()
    await vi.waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toContain('Upratanie je blokované (neuložená alebo nezlúčená práca):'))
    expect(target.querySelector('[role="alert"]')?.textContent).not.toContain('conflict: workspaces hold unmerged work')
    const items = [...target.querySelectorAll('[role="alert"] ul li')].map(li => li.textContent)
    expect(items).toEqual(['member a has 2 unmerged commit(s)', 'integration has uncommitted changes'])
    expect(calls.filter(c => c.method === 'DELETE').map(c => c.path)).toEqual(['/api/teams/t1/workspaces'])
    // The force offer is only a step towards the explicit confirmation.
    button('Vynútiť upratanie').click()
    flushSync()
    expect(text()).toContain('stratí')
    expect(calls.filter(c => c.method === 'DELETE')).toHaveLength(1)
    expect(onremoved).not.toHaveBeenCalled()
  })

  it('should send the forced removal with ?force=true only after the explicit confirmation', async () => {
    const onremoved = vi.fn()
    stub((path, method) => {
      if (method === 'DELETE' && path.includes('force=true')) return Response.json({ id: 't1' })
      if (method === 'DELETE') return Response.json({ code: 'workspaces_blocked', error: 'conflict: unmerged', blockers: ['member a has 1 unmerged commit(s)'] }, { status: 409 })
      return Response.json(handoff())
    })
    show({ onremoved })
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    button('Upratať pracovné priestory').click()
    await vi.waitFor(() => expect(maybeButton('Vynútiť upratanie')).toBeDefined())
    button('Vynútiť upratanie').click()
    flushSync()
    button('Áno, zahodiť prácu').click()
    await vi.waitFor(() => expect(calls.some(c => c.method === 'DELETE' && c.path === '/api/teams/t1/workspaces?force=true')).toBe(true))
    await vi.waitFor(() => expect(onremoved).toHaveBeenCalledTimes(1))
  })

  it('should send nothing when the forced removal is cancelled', async () => {
    stub((path, method) => method === 'DELETE'
      ? Response.json({ code: 'workspaces_blocked', error: 'conflict: unmerged', blockers: ['member a has 1 unmerged commit(s)'] }, { status: 409 })
      : Response.json(handoff()))
    show()
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    button('Upratať pracovné priestory').click()
    await vi.waitFor(() => expect(maybeButton('Vynútiť upratanie')).toBeDefined())
    button('Vynútiť upratanie').click()
    flushSync()
    button('Zrušiť').click()
    flushSync()
    expect(maybeButton('Áno, zahodiť prácu')).toBeUndefined()
    expect(calls.filter(c => c.method === 'DELETE')).toHaveLength(1)
  })

  it('should not offer force for a conflict that is not about unmerged work', async () => {
    stub((path, method) => method === 'DELETE'
      ? Response.json({ code: 'conflict', error: 'conflict: member a is working; wait for it to finish' }, { status: 409 })
      : Response.json(handoff()))
    show()
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    button('Upratať pracovné priestory').click()
    await vi.waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toContain('is working'))
    expect(maybeButton('Vynútiť upratanie')).toBeUndefined()
  })

  it('should show the error text when a workspaces_blocked refusal carries no blockers', async () => {
    stub((path, method) => method === 'DELETE'
      ? Response.json({ code: 'workspaces_blocked', error: 'conflict: workspaces hold unmerged work' }, { status: 409 })
      : Response.json(handoff()))
    show()
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    button('Upratať pracovné priestory').click()
    await vi.waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toContain('conflict: workspaces hold unmerged work'))
    expect(target.querySelector('[role="alert"]')?.textContent).not.toContain('Upratanie je blokované')
  })

  it('should not offer force for a 409 without the workspaces_blocked code even when the text mentions force=true', async () => {
    stub((path, method) => method === 'DELETE'
      ? Response.json({ error: 'conflict: something (pass force=true to discard)' }, { status: 409 })
      : Response.json(handoff()))
    show()
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    button('Upratať pracovné priestory').click()
    await vi.waitFor(() => expect(target.querySelector('[role="alert"]')).not.toBeNull())
    expect(maybeButton('Vynútiť upratanie')).toBeUndefined()
  })

  it('should show the server message when the forced removal is refused with 403', async () => {
    const onremoved = vi.fn()
    stub((path, method) => {
      if (method === 'DELETE' && path.includes('force=true')) return Response.json({ error: 'forced removal needs a same-origin request' }, { status: 403 })
      if (method === 'DELETE') return Response.json({ code: 'workspaces_blocked', error: 'conflict: unmerged', blockers: ['x'] }, { status: 409 })
      return Response.json(handoff())
    })
    show({ onremoved })
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    button('Upratať pracovné priestory').click()
    await vi.waitFor(() => expect(maybeButton('Vynútiť upratanie')).toBeDefined())
    button('Vynútiť upratanie').click()
    flushSync()
    button('Áno, zahodiť prácu').click()
    await vi.waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toContain('same-origin'))
    expect(onremoved).not.toHaveBeenCalled()
    expect(maybeButton('Áno, zahodiť prácu')).toBeUndefined()
  })

  it('should show merge failures that are not conflicts, with their reason, collapsed in details', async () => {
    stub(() => Response.json(handoff({
      conflicts: [{ member: 'implementation-2', branch: 'b', files: ['api/a.go'], reason: 'both modified' }],
      failed: [{ member: 'mechanical-6', branch: 'stratus/t1/mechanical-6', files: ['x.go'], reason: 'pre-merge hook rejected' }],
    })))
    show()
    await vi.waitFor(() => expect(target.querySelector('details.failed')).not.toBeNull())
    const failed = target.querySelector('details.failed') as HTMLDetailsElement
    expect(failed.open).toBe(false)
    expect(failed.textContent).toContain('Zlúčenie zlyhalo (nie konflikt)')
    expect(failed.textContent).toContain('mechanical-6')
    expect(failed.textContent).toContain('pre-merge hook rejected')
    expect(failed.textContent).toContain('x.go')
    expect(target.querySelector('details.conflict')?.textContent).toContain('both modified')
  })

  it('should count failures in the integrate notice', async () => {
    stub((path, method) => path.endsWith('/integrate') && method === 'POST'
      ? Response.json({ branch: 'b', revision: REV, dirty: false, merged: ['a'], conflicts: [{ member: 'c', branch: 'x', files: ['f'] }],
        failed: [{ member: 'd', branch: 'y', files: [], reason: 'hook' }, { member: 'e', branch: 'z', files: [], reason: 'hook' }], nothing_to_merge: ['g'], warnings: [] })
      : Response.json(noIntegration()))
    show()
    await vi.waitFor(() => expect(count('/api/teams/t1/handoff', 'GET')).toBe(1))
    button('Zlúčiť výsledky').click()
    await vi.waitFor(() => expect(target.querySelector('[role="status"]')?.textContent).toContain('Zlúčené: 1, konflikty: 1, zlyhania: 2, bez zmien: 1'))
  })

  it('should show not-applicable build and vet gates as a neutral chip', async () => {
    stub(() => Response.json(handoff({
      checks: {
        gates: [
          { name: 'build', verdict: 'info', content: 'not applicable', revision: REV, current: true },
          { name: 'vet', verdict: 'info', content: 'not applicable', revision: REV, current: true },
          { name: 'placeholder_scan', verdict: 'pass', content: 'clean', revision: REV, current: true },
        ],
        checks_ok: false, revision: REV, current: true,
      },
    })))
    show()
    await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(3))
    const chips = [...target.querySelectorAll('.gate-chip')]
    expect(chips[0].textContent).toContain('nepoužiteľné')
    expect(chips[0].className).toContain('info')
    expect(chips[0].className).not.toMatch(/pass|fail/)
    expect(chips[1].textContent).toContain('nepoužiteľné')
    expect(chips[2].className).toContain('pass')
    expect(text()).toContain('Kontroly neúplné')
  })

  describe('configured gates', () => {
    const row = (name: string, verdict: string, content = 'out', current = true) => ({ name, verdict, content, revision: REV, current })
    const withChecks = (checks: Partial<NonNullable<Handoff['checks']>>) =>
      handoff({ checks: { gates: [], checks_ok: false, revision: REV, current: true, ...checks } })
    const chipTexts = () => [...target.querySelectorAll('.gate-chip')].map(c => c.textContent?.replace(/\s+/g, ' ').trim())

    it('should render one chip per expected gate whatever its name', async () => {
      stub(() => Response.json(withChecks({
        expected_gates: ['test', 'lint', 'placeholder_scan'],
        gates: [row('test', 'pass'), row('lint', 'fail', 'lint: 2 issues'), row('placeholder_scan', 'pass')],
      })))
      show()
      await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(3))
      expect(chipTexts()).toEqual(['test ✓', 'lint ✕', 'placeholder ✓'])
      expect(text()).not.toContain('build')
      expect(target.querySelector('details.gate-detail pre')?.textContent).toBe('lint: 2 issues')
    })

    it('should show a configured gate without a recorded row as chýba and an old row as zastaralé', async () => {
      stub(() => Response.json(withChecks({
        expected_gates: ['test', 'lint', 'build'],
        gates: [row('test', 'pass'), row('build', 'pass', 'ok', false)],
      })))
      show()
      await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(3))
      expect(chipTexts()).toEqual(['test ✓', 'lint chýba', 'build zastaralé'])
      expect(target.querySelector('.gate-chip.missing')?.textContent).toContain('lint chýba')
    })

    it('should render the none-configured checks row as a neutral chip with the hint', async () => {
      stub(() => Response.json(withChecks({
        expected_gates: ['checks'],
        gates: [row('checks', 'info', 'checks: none configured (no go.mod)')],
      })))
      show()
      await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(1))
      const chip = target.querySelector('.gate-chip')!
      expect(chip.textContent).toContain('žiadne kontroly')
      expect(chip.className).toContain('info')
      expect(chip.className).not.toMatch(/pass|fail|outdated|missing/)
      expect(text()).toContain('Pridaj "checks" do .stratus.json (pozri docs/usage/managed-teams.md).')
      expect(text()).toContain('Kontroly neúplné')
      expect(text()).not.toContain('nepoužiteľné')
    })

    it('should not show the hint when a project has configured checks', async () => {
      stub(() => Response.json(withChecks({ expected_gates: ['test'], gates: [row('test', 'pass')] })))
      show()
      await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(1))
      expect(text()).not.toContain('docs/usage/managed-teams.md')
    })

    it('should keep nepoužiteľné for another not-applicable info gate', async () => {
      stub(() => Response.json(withChecks({ expected_gates: ['typecheck'], gates: [row('typecheck', 'info', 'not applicable')] })))
      show()
      await vi.waitFor(() => expect(chipTexts()).toEqual(['typecheck nepoužiteľné']))
    })

    it('should fall back to the three fixed chips when an older server sends no expected_gates', async () => {
      stub(() => Response.json(withChecks({ gates: [row('build', 'pass')] })))
      show()
      await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(3))
      expect(chipTexts()).toEqual(['build ✓', 'vet chýba', 'placeholder chýba'])
    })

    it('should fall back to the three fixed chips when expected_gates is empty', async () => {
      stub(() => Response.json(withChecks({ expected_gates: [], gates: [] })))
      show()
      await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(3))
    })
  })

  describe('failed gate detail', () => {
    const failing = (name: string, content: string) => ({ name, verdict: 'fail', content, revision: REV, current: true })
    const COMPILER = 'go build FAILED (in /abs/path [integration] @ e7526dc):\n# example.com/p\n./dup.go:4:6: Alpha redeclared in this block\n\t./alpha.go:4:6: other declaration of Alpha\n'
    const withGates = (gates: NonNullable<Handoff['checks']>['gates']) =>
      handoff({ checks: { gates, checks_ok: false, revision: REV, current: true } })

    it('should show the compiler output of a failing gate in an open details', async () => {
      stub(() => Response.json(withGates([failing('build', COMPILER), { name: 'vet', verdict: 'pass', content: 'vet clean output', revision: REV, current: true }])))
      show()
      await vi.waitFor(() => expect(target.querySelector('details.gate-detail')).not.toBeNull())
      const details = target.querySelector('details.gate-detail') as HTMLDetailsElement
      expect(details.open).toBe(true)
      expect(details.querySelector('summary')?.textContent).toContain('Detail zlyhaných kontrol')
      expect(details.querySelector('pre')?.textContent).toBe(COMPILER)
      expect(details.textContent).toContain('build')
      expect(details.textContent).toContain('Alpha redeclared in this block')
      expect(details.textContent).not.toContain('vet clean output')
      expect(target.querySelector('.gate-chip.fail')?.getAttribute('title')).toBe(COMPILER)
    })

    it('should start collapsed when several gates failed', async () => {
      stub(() => Response.json(withGates([failing('build', 'b failed'), failing('vet', 'v failed')])))
      show()
      await vi.waitFor(() => expect(target.querySelectorAll('details.gate-detail pre')).toHaveLength(2))
      expect((target.querySelector('details.gate-detail') as HTMLDetailsElement).open).toBe(false)
    })

    it('should not render the detail when no gate failed', async () => {
      stub(() => Response.json(withGates([{ name: 'build', verdict: 'pass', content: 'ok', revision: REV, current: true }])))
      show()
      await vi.waitFor(() => expect(text()).toContain('abcdef1'))
      expect(target.querySelector('details.gate-detail')).toBeNull()
    })

    it('should truncate very long output to 4000 characters with an ellipsis', async () => {
      stub(() => Response.json(withGates([failing('build', 'x'.repeat(10000))])))
      show()
      await vi.waitFor(() => expect(target.querySelector('details.gate-detail pre')).not.toBeNull())
      const shown = target.querySelector('details.gate-detail pre')!.textContent!
      expect(shown).toBe('x'.repeat(4000) + '…')
    })
  })

  describe('long running actions', () => {
    const TIMEOUT_MESSAGE = 'Požiadavka trvá príliš dlho; server ju možno ešte spracúva. Obnovte stav o chvíľu.'
    // Answers the handoff at once; the action hangs until `answer` is called, and rejects like fetch when aborted.
    function slowAction(suffix: string) {
      let answer: (r: Response) => void = () => {}
      stub((path, method, signal) => {
        if (path.endsWith(suffix) && method === 'POST') {
          return new Promise<Response>((resolve, reject) => {
            answer = resolve
            signal?.addEventListener('abort', () => reject(new DOMException('The operation was aborted.', 'AbortError')))
          })
        }
        return Response.json(handoff())
      })
      return { answer: (r: Response) => answer(r) }
    }
    async function ready() {
      show()
      await vi.waitFor(() => expect(text()).toContain('abcdef1'))
      vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    }

    it.each([
      ['Zlúčiť výsledky', '/integrate', { branch: 'b', revision: REV, dirty: false, merged: ['a'], conflicts: [], nothing_to_merge: [], warnings: [] }, 'Zlúčené: 1'],
      ['Spustiť kontroly', '/verify', { cached: false, checks: { gates: [], checks_ok: true, revision: REV, current: true } }, 'Kontroly sú hotové'],
    ])('should keep %s pending past 30 seconds and resolve it normally at 2 minutes', async (label, suffix, body, expected) => {
      const action = slowAction(suffix)
      await ready()
      button(label).click()
      await vi.advanceTimersByTimeAsync(120_000)
      expect(target.querySelector('[role="alert"]')).toBeNull()
      expect(button(label).disabled).toBe(true)
      action.answer(Response.json(body))
      await vi.advanceTimersByTimeAsync(0)
      await vi.waitFor(() => expect(target.querySelector('[role="status"]')?.textContent).toContain(expected))
      expect(target.querySelector('[role="alert"]')).toBeNull()
    })

    it('should show the timeout message and reload the handoff once without re-sending the action', async () => {
      slowAction('/verify')
      await ready()
      const gets = count('/api/teams/t1/handoff', 'GET')
      button('Spustiť kontroly').click()
      await vi.advanceTimersByTimeAsync(6 * 60 * 1000)
      await vi.waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toContain(TIMEOUT_MESSAGE))
      expect(count('/api/teams/t1/handoff', 'GET')).toBe(gets + 1)
      expect(count('/api/teams/t1/verify', 'POST')).toBe(1)
      await vi.advanceTimersByTimeAsync(60_000)
      expect(count('/api/teams/t1/handoff', 'GET')).toBe(gets + 1)
      expect(button('Spustiť kontroly').disabled).toBe(false)
    })

    it('should show the server text when a retry meets a run that is still in progress', async () => {
      stub((path, method) => path.endsWith('/verify') && method === 'POST'
        ? Response.json({ error: 'conflict: checks are already running for this team' }, { status: 409 })
        : Response.json(handoff()))
      show()
      await vi.waitFor(() => expect(text()).toContain('abcdef1'))
      button('Spustiť kontroly').click()
      await vi.waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toContain('checks are already running for this team'))
      expect(maybeButton('Vynútiť upratanie')).toBeUndefined()
    })
  })

  it('should never let a stale handoff response overwrite a newer one', async () => {
    const releases: ((r: Response) => void)[] = []
    stub(() => new Promise<Response>(resolve => { releases.push(resolve) }))
    show()
    await vi.waitFor(() => expect(releases).toHaveLength(1))
    button('Obnoviť').click()
    await vi.waitFor(() => expect(releases).toHaveLength(2))
    releases[1](Response.json(handoff({ integration: { dir_kind: 'integration', branch: 'b', revision: 'bbbbbbb1111', dirty: false } })))
    await vi.waitFor(() => expect(text()).toContain('bbbbbbb'))
    releases[0](Response.json(handoff({ integration: { dir_kind: 'integration', branch: 'b', revision: 'aaaaaaa1111', dirty: false } })))
    await new Promise(r => setTimeout(r, 20))
    expect(text()).toContain('bbbbbbb')
    expect(text()).not.toContain('aaaaaaa')
  })

  it('should show an alert when the handoff cannot be loaded', async () => {
    stub(() => Response.json({ error: 'team not found' }, { status: 404 }))
    show()
    await vi.waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toContain('team not found'))
  })

  it('should show an alert when an action fails', async () => {
    stub((path, method) => method === 'POST'
      ? Response.json({ error: 'integration failed; see the server log' }, { status: 500 })
      : Response.json(handoff()))
    show()
    await vi.waitFor(() => expect(text()).toContain('abcdef1'))
    button('Zlúčiť výsledky').click()
    await vi.waitFor(() => expect(target.querySelector('[role="alert"]')?.textContent).toContain('integration failed'))
  })

  it('should reload on team_updated, debounced, and stop listening on unmount', async () => {
    stub(() => Response.json(handoff()))
    show()
    await vi.waitFor(() => expect(count('/api/teams/t1/handoff', 'GET')).toBe(1))
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    expect(ws.handlers.team_updated).toHaveLength(1)
    ws.handlers.team_updated[0]()
    ws.handlers.team_updated[0]()
    expect(count('/api/teams/t1/handoff', 'GET')).toBe(1)
    await vi.advanceTimersByTimeAsync(200)
    expect(count('/api/teams/t1/handoff', 'GET')).toBe(2)
    await unmount(app!)
    app = undefined
    expect(ws.handlers.team_updated ?? []).toHaveLength(0)
  })
})
