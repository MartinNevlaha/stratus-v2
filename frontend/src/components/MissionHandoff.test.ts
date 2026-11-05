// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount, unmount } from 'svelte'
import MissionHandoff from './MissionHandoff.svelte'
import type { MissionHandoff as Handoff } from '$lib/types'

function handoff(over: Partial<Handoff> = {}): Handoff {
  return {
    mission_id: 'm1',
    status: 'verifying',
    revision: 'abcdef1234567890',
    dirty: false,
    dir_kind: 'integration',
    gates: [
      { name: 'build', verdict: 'pass', content: 'go build: OK', revision: 'abcdef1234567890', current: true },
      { name: 'vet', verdict: 'fail', content: 'go vet FAILED', revision: 'abcdef1234567890', current: true },
      { name: 'placeholder_scan', verdict: 'pass', content: 'clean', revision: '1111111aaaa', current: false },
    ],
    checks_ok: false,
    review: { verdict: 'pass', revision: 'abcdef1234567890', current: true, valid: true },
    diff_stat: { files_changed: 3, lines_added: 40, lines_removed: 5 },
    forge: { total: 2, merged: 1, pending: 1, conflicts: 0, failed: 0 },
    screenshots: [],
    warnings: ['1 forge entries still pending'],
    ...over,
  }
}

async function render(data: Handoff | Error) {
  vi.stubGlobal('fetch', async () =>
    data instanceof Error ? new Response('boom', { status: 500 }) : Response.json(data),
  )
  const target = document.createElement('div')
  document.body.appendChild(target)
  const app = mount(MissionHandoff, { target, props: { missionId: 'm1' } })
  return { target, app }
}

describe('MissionHandoff', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('should render the short revision, gate chips and PASS - platné for a valid review', async () => {
    const { target, app } = await render(handoff())
    try {
      await vi.waitFor(() => expect(target.textContent).toContain('abcdef1'))
      expect(target.textContent).toContain('čistý')
      const chips = Array.from(target.querySelectorAll('.gate-chip')).map((c) => c.className)
      expect(chips).toHaveLength(3)
      expect(chips[0]).toContain('pass')
      expect(chips[1]).toContain('fail')
      expect(chips[2]).toContain('outdated')
      expect(target.querySelector('.review-state')?.textContent).toContain('PASS - platné')
      expect(target.textContent).toContain('3')
      expect(target.textContent).toContain('1 forge entries still pending')
    } finally {
      await unmount(app)
    }
  })

  it('should render Zastaralé when the review is not valid', async () => {
    const { target, app } = await render(
      handoff({ review: { verdict: 'pass', revision: '1111111', current: false, valid: false, reason: 'reviewed an older revision' } }),
    )
    try {
      await vi.waitFor(() => expect(target.querySelector('.review-state')?.textContent).toContain('Zastaralé'))
      expect(target.querySelector('.review-state')?.textContent).not.toContain('PASS - platné')
      expect(target.textContent).toContain('reviewed an older revision')
    } finally {
      await unmount(app)
    }
  })

  it('should not show a failing review at HEAD as valid', async () => {
    const { target, app } = await render(
      handoff({ review: { verdict: 'fail', revision: 'abcdef1234567890', current: true, valid: false, reason: 'review verdict is fail' } }),
    )
    try {
      await vi.waitFor(() => expect(target.querySelector('.review-state')?.textContent).toContain('FAIL'))
      const el = target.querySelector('.review-state') as HTMLElement
      expect(el.textContent).not.toContain('platné')
      expect(el.textContent).not.toContain('Zastaralé')
      expect(el.className).toContain('failed')
    } finally {
      await unmount(app)
    }
  })

  it('should show Bez verdiktu for a current review without a pass or fail verdict', async () => {
    const { target, app } = await render(
      handoff({ review: { verdict: 'info', revision: 'abcdef1234567890', current: true, valid: false, reason: 'review has no pass verdict' } }),
    )
    try {
      await vi.waitFor(() => expect(target.querySelector('.review-state')?.textContent).toContain('Bez verdiktu'))
    } finally {
      await unmount(app)
    }
  })

  it('should show chýba chips for gates that were never recorded and the checks_ok state', async () => {
    const { target, app } = await render(
      handoff({ gates: [{ name: 'build', verdict: 'pass', content: 'ok', revision: 'abcdef1234567890', current: true }] }),
    )
    try {
      await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(3))
      const missing = Array.from(target.querySelectorAll('.gate-chip.missing')).map((c) => c.textContent)
      expect(missing).toHaveLength(2)
      expect(missing.every((t) => t?.includes('chýba'))).toBe(true)
      expect(target.querySelector('.checks')?.textContent).toContain('neúplné')
    } finally {
      await unmount(app)
    }
    const ok = await render(handoff({ checks_ok: true }))
    try {
      await vi.waitFor(() => expect(ok.target.querySelector('.checks')?.textContent).toContain('OK'))
    } finally {
      await unmount(ok.app)
    }
  })

  it('should show not-applicable build and vet gates as a neutral nepoužiteľné chip', async () => {
    const rev = 'abcdef1234567890'
    const { target, app } = await render(handoff({
      gates: [
        { name: 'build', verdict: 'info', content: 'not applicable', revision: rev, current: true },
        { name: 'vet', verdict: 'info', content: 'not applicable', revision: rev, current: true },
        { name: 'placeholder_scan', verdict: 'pass', content: 'clean', revision: rev, current: true },
      ],
    }))
    try {
      await vi.waitFor(() => expect(target.querySelectorAll('.gate-chip')).toHaveLength(3))
      const chips = Array.from(target.querySelectorAll('.gate-chip'))
      expect(chips[0].textContent).toContain('nepoužiteľné')
      expect(chips[1].textContent).toContain('nepoužiteľné')
      expect(chips[0].className).toContain('info')
      expect(chips[0].className).not.toMatch(/pass|fail|outdated/)
      expect(chips[2].className).toContain('pass')
      expect(target.querySelector('.checks')?.textContent).toContain('neúplné')
    } finally {
      await unmount(app)
    }
  })

  it('should show neuložené zmeny when the tree is dirty', async () => {
    const { target, app } = await render(handoff({ dirty: true, review: undefined }))
    try {
      await vi.waitFor(() => expect(target.textContent).toContain('neuložené zmeny'))
      expect(target.querySelector('.review-state')).toBeNull()
    } finally {
      await unmount(app)
    }
  })

  it('should render nothing when the handoff cannot be loaded', async () => {
    const { target, app } = await render(new Error('x'))
    try {
      await new Promise((r) => setTimeout(r, 30))
      expect(target.querySelector('.handoff')).toBeNull()
    } finally {
      await unmount(app)
    }
  })

  it('should render thumbnails linking to the screenshot endpoint with a current or outdated chip', async () => {
    const { target, app } = await render(
      handoff({
        screenshots: [
          { id: 'e2', revision: 'abcdef1234567890', current: true, created_at: '2026-01-02T00:00:00Z', agent: 'qa' },
          { id: 'e1', revision: '1111111aaaa', current: false, created_at: '2026-01-01T00:00:00Z', agent: 'qa' },
        ],
      }),
    )
    try {
      await vi.waitFor(() => expect(target.querySelectorAll('.shot img')).toHaveLength(2))
      const imgs = Array.from(target.querySelectorAll<HTMLImageElement>('.shot img'))
      expect(imgs[0].getAttribute('src')).toBe('/api/swarm/missions/m1/screenshots/e2')
      expect(imgs[0].getAttribute('alt')).toBe('Snímka obrazovky, qa @ abcdef1')
      const links = Array.from(target.querySelectorAll<HTMLAnchorElement>('.shot a'))
      expect(links[1].getAttribute('href')).toBe('/api/swarm/missions/m1/screenshots/e1')
      expect(links[1].getAttribute('target')).toBe('_blank')
      expect(links[1].getAttribute('rel')).toContain('noopener')
      const chips = Array.from(target.querySelectorAll('.shot .shot-chip')).map((c) => c.textContent?.trim())
      expect(chips).toEqual(['aktuálna', 'zastaralá'])
      expect(target.textContent).toContain('Náhľady')
    } finally {
      await unmount(app)
    }
  })

  it('should show the UI-without-screenshot warning with the other warnings', async () => {
    const { target, app } = await render(handoff({ warnings: ['UI changes without a current screenshot'] }))
    try {
      await vi.waitFor(() => expect(target.querySelector('.warning')?.textContent).toContain('UI changes without a current screenshot'))
    } finally {
      await unmount(app)
    }
  })

  it('should not render the Náhľady section when there are no screenshots', async () => {
    const { target, app } = await render(handoff())
    try {
      await vi.waitFor(() => expect(target.querySelector('.handoff')).not.toBeNull())
      expect(target.querySelector('.shots')).toBeNull()
      expect(target.textContent).not.toContain('Náhľady')
    } finally {
      await unmount(app)
    }
  })

  it('should omit missing agent or revision from the alt text', async () => {
    const { target, app } = await render(
      handoff({
        screenshots: [
          { id: 'a', revision: '', current: false, created_at: '', agent: 'qa' },
          { id: 'b', revision: 'abcdef1234567890', current: true, created_at: '', agent: '' },
          { id: 'c', revision: '', current: false, created_at: '', agent: '' },
        ],
      }),
    )
    try {
      await vi.waitFor(() => expect(target.querySelectorAll('.shot img')).toHaveLength(3))
      const alts = Array.from(target.querySelectorAll('.shot img')).map((i) => i.getAttribute('alt'))
      expect(alts).toEqual(['Snímka obrazovky, qa', 'Snímka obrazovky, @ abcdef1', 'Snímka obrazovky'])
    } finally {
      await unmount(app)
    }
  })

  it('should replace a broken image by Snímka nie je dostupná', async () => {
    const { target, app } = await render(
      handoff({ screenshots: [{ id: 'e1', revision: 'abcdef1234567890', current: true, created_at: '', agent: 'qa' }] }),
    )
    try {
      await vi.waitFor(() => expect(target.querySelector('.shot img')).not.toBeNull())
      target.querySelector('.shot img')!.dispatchEvent(new Event('error'))
      await vi.waitFor(() => expect(target.querySelector('.shot img')).toBeNull())
      expect(target.querySelector('.shot-missing')?.textContent).toContain('Snímka nie je dostupná')
    } finally {
      await unmount(app)
    }
  })
})
