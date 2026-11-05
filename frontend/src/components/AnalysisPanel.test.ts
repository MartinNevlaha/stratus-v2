// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount, unmount, flushSync } from 'svelte'
import AnalysisPanel from './AnalysisPanel.svelte'
import { appState } from '$lib/store'

function analysis(type: string, complexity = 'simple') {
  return {
    recommended_type: type,
    recommended_complexity: complexity,
    recommended_strategy: 'sequential',
    risk_score: 0.2,
    risk_level: 'low',
    risk_factors: [],
    estimated_duration_min: 10,
    suggested_domains: [],
    similar_past_workflows: [],
  }
}

function setup() {
  const target = document.createElement('div')
  document.body.appendChild(target)
  const app = mount(AnalysisPanel, { target })
  const input = target.querySelector('input') as HTMLInputElement
  const type = (value: string) => {
    input.value = value
    input.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
  }
  const analyzeButton = () =>
    Array.from(target.querySelectorAll('button')).find((b) => b.textContent?.trim() === 'Analyze') as
      | HTMLButtonElement
      | undefined
  return { target, app, input, type, analyzeButton }
}

describe('AnalysisPanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.useRealTimers()
    document.body.innerHTML = ''
    appState.pendingTeamTask = null
    appState.activeTab = 'overview'
  })

  it('should not call the API while typing', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.fn(async () => Response.json(analysis('spec')))
    vi.stubGlobal('fetch', fetchMock)
    const { app, type } = setup()
    try {
      type('Add Excel export')
      await vi.advanceTimersByTimeAsync(5000)
      expect(fetchMock).not.toHaveBeenCalled()
    } finally {
      await unmount(app)
    }
  })

  it('should call the API once when Analyze is clicked', async () => {
    const fetchMock = vi.fn(async () => Response.json(analysis('spec')))
    vi.stubGlobal('fetch', fetchMock)
    const { app, type, analyzeButton } = setup()
    try {
      type('Add Excel export')
      analyzeButton()!.click()
      await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    } finally {
      await unmount(app)
    }
  })

  it('should call the API once when Enter is pressed', async () => {
    const fetchMock = vi.fn(async () => Response.json(analysis('spec')))
    vi.stubGlobal('fetch', fetchMock)
    const { app, type, input } = setup()
    try {
      type('Add Excel export')
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
      await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    } finally {
      await unmount(app)
    }
  })

  it('should disable Analyze for descriptions shorter than 5 characters', async () => {
    vi.stubGlobal('fetch', vi.fn())
    const { app, type, analyzeButton } = setup()
    try {
      type('abc')
      expect(analyzeButton()!.disabled).toBe(true)
      type('abcde')
      expect(analyzeButton()!.disabled).toBe(false)
    } finally {
      await unmount(app)
    }
  })

  it('should ignore a stale response when the text changed after submitting', async () => {
    const pending: Array<(r: Response) => void> = []
    vi.stubGlobal('fetch', () => new Promise<Response>((resolve) => pending.push(resolve)))
    const { app, target, type, analyzeButton } = setup()
    try {
      type('first description')
      analyzeButton()!.click()
      await vi.waitFor(() => expect(pending).toHaveLength(1))
      type('second description')
      analyzeButton()!.click()
      await vi.waitFor(() => expect(pending).toHaveLength(2))

      pending[1](Response.json(analysis('bug')))
      await vi.waitFor(() => expect(target.textContent).toContain('/bug'))
      pending[0](Response.json(analysis('spec')))
      await new Promise((resolve) => setTimeout(resolve, 20))
      expect(target.textContent).toContain('/bug')
      expect(target.textContent).not.toContain('/spec')
    } finally {
      await unmount(app)
    }
  })

  it('should copy the analyzed text and clear the result when the input is edited', async () => {
    const writeText = vi.fn(async () => {})
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    vi.stubGlobal('fetch', async () => Response.json(analysis('spec', 'complex')))
    const { app, target, type, analyzeButton } = setup()
    try {
      type('  Add Excel export  ')
      analyzeButton()!.click()
      await vi.waitFor(() => expect(target.textContent).toContain('/spec-complex'))
      const copy = Array.from(target.querySelectorAll('button')).find((b) =>
        b.textContent?.includes('Copy /spec-complex'),
      ) as HTMLButtonElement
      copy.click()
      expect(writeText).toHaveBeenCalledWith('/spec-complex Add Excel export')
      type('Something else entirely')
      expect(target.textContent).not.toContain('/spec-complex')
    } finally {
      await unmount(app)
    }
  })

  it('should hand the analyzed text to the Team page when the team button is clicked', async () => {
    vi.stubGlobal('fetch', async () => Response.json(analysis('spec', 'complex')))
    const { app, target, type, analyzeButton } = setup()
    try {
      type('  Add Excel export  ')
      analyzeButton()!.click()
      await vi.waitFor(() => expect(target.textContent).toContain('/spec-complex'))
      const start = Array.from(target.querySelectorAll('button')).find(
        (b) => b.textContent?.trim() === 'Spustiť tím s týmto zadaním',
      ) as HTMLButtonElement
      start.click()
      expect(appState.pendingTeamTask).toEqual({ text: 'Add Excel export' })
      expect(appState.activeTab).toBe('team')
    } finally {
      await unmount(app)
    }
  })

  it('should ignore Enter while IME composition is active', async () => {
    const fetchMock = vi.fn(async () => Response.json(analysis('spec')))
    vi.stubGlobal('fetch', fetchMock)
    const { app, type, input } = setup()
    try {
      type('Add Excel export')
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 20))
      expect(fetchMock).not.toHaveBeenCalled()
    } finally {
      await unmount(app)
    }
  })
})
