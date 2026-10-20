import { describe, it, expect } from 'vitest'
import { isDashboardOnly, continuationText, usageLabel, continuationForID } from './runtime'
import type { WorkflowState } from './types'
describe('runtime presentation',()=>{
 it('Codex continuation has no Claude syntax',()=>{const wf={id:'spec-codex',runtime:'codex'} as WorkflowState;expect(continuationText(wf)).toBe('Continue Stratus workflow spec-codex');expect(continuationText({id:'spec-old'} as WorkflowState)).toBe('/resume spec-old')})
 it('detects dashboard mode without changing ordinary URLs',()=>{expect(isDashboardOnly('?view=dashboard')).toBe(true);expect(isDashboardOnly('')).toBe(false)})
 it('unknown usage renders unavailable',()=>{expect(usageLabel(false,0,0)).toBe('Usage unavailable')})
 it('finished Codex alert resolves its runtime',async()=>{const text=await continuationForID('spec-finished',[],async()=>({id:'spec-finished',runtime:'codex'} as WorkflowState));expect(text).toBe('Continue Stratus workflow spec-finished')})
})
