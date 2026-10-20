// @vitest-environment jsdom
import { it, expect, vi } from 'vitest'
import { mount, unmount } from 'svelte'
import ReviewPanel from './ReviewPanel.svelte'
import WorkflowActivityHarness from '../test/fixtures/WorkflowActivityHarness.svelte'
import WorkflowActivity from './WorkflowActivity.svelte'
import type { WorkflowAgent, WorkflowState } from '$lib/types'
const old = { id:'old',workflow_id:'spec-codex',session_id:'codex:s1',runtime:'codex',agent_type:'default',phase:'implement',task_index:0,status:'done',started_at:'2026-10-20T10:00:00Z',ended_at:'2026-10-20T10:01:00Z',last_activity:'2026-10-20T10:01:00Z',tool_calls:0,files:[],verdict:'PASS',must_fix:0,should_fix:0,suggestion:0,tokens_in:0,tokens_out:0 } as WorkflowAgent
for (const [status,label] of [['unverified','Unverified'],['outdated','Outdated'],['running','Running'],['awaiting','Awaiting review'],['FAIL','FAIL']]) {
 it(`current ${status} never inherits an old PASS`,async()=>{
 const target=document.createElement('div');const app=mount(ReviewPanel,{target,props:{agents:[old],review:{status,agent_id:'new',must_fix:0,should_fix:0,suggestion:0,reviews:1,loops:0}} as never})
 try{await vi.waitFor(()=>expect(target.textContent).toContain(label));expect(target.textContent).not.toContain('PASS')}finally{await unmount(app)}
 })
}
it('legacy review keeps its existing PASS',async()=>{const target=document.createElement('div');const app=mount(ReviewPanel,{target,props:{agents:[old]}});try{await vi.waitFor(()=>expect(target.textContent).toContain('PASS'))}finally{await unmount(app)}})
it('workflow activity uses the API current review projection',async()=>{
 vi.stubGlobal('fetch',async(input:string)=>Response.json(String(input).endsWith('/agents')?{agents:[old],conflicts:[],review:{status:'unverified',must_fix:0,should_fix:0,suggestion:0,reviews:0,loops:0}}:String(input).endsWith('/denials')?{denials:[]}:[]))
 const target=document.createElement('div');const wf={id:'spec-codex',runtime:'codex',phase:'implement',type:'spec',tasks:[],delegated_agents:{}} as unknown as WorkflowState;const app=mount(WorkflowActivity,{target,props:{wf}})
 try{await vi.waitFor(()=>expect(target.textContent).toContain('Unverified'))}finally{await unmount(app);vi.unstubAllGlobals()}
})

it('scope changes clear cached PASS and reject an older response',async()=>{
 const pending: Array<(value:Response)=>void>=[]
 let calls=0
 vi.stubGlobal('fetch',async(input:string)=>{
  if(String(input).endsWith('/agents')) {
   calls++
   if(calls===1) return Response.json({agents:[old],conflicts:[],review:{status:'PASS',reviews:1,loops:0,must_fix:0,should_fix:0,suggestion:0}})
   return new Promise<Response>(resolve=>pending.push(resolve))
  }
  return Response.json(String(input).endsWith('/denials')?{denials:[]}:[])
 })
 const target=document.createElement('div')
 const initial={id:'spec-codex',runtime:'codex',phase:'verify',current_task:0,tasks:[],delegated_agents:{}} as unknown as WorkflowState
 const app=mount(WorkflowActivityHarness,{target,props:{initial}})
 try {
  await vi.waitFor(()=>expect(target.querySelector('.review .badge')?.textContent).toBe('PASS'))
  app.setWorkflow({...initial,phase:'implement'})
  await vi.waitFor(()=>expect(calls).toBe(2))
  expect(target.querySelector('.review .badge')?.textContent).not.toBe('PASS')
  app.setWorkflow({...initial,phase:'implement',current_task:1})
  await vi.waitFor(()=>expect(calls).toBe(3))
  pending[1](Response.json({agents:[old],conflicts:[],review:{status:'unverified',reviews:1,loops:0,must_fix:0,should_fix:0,suggestion:0}}))
  await vi.waitFor(()=>expect(target.querySelector('.review .badge')?.textContent).toBe('Unverified'))
  pending[0](Response.json({agents:[old],conflicts:[],review:{status:'PASS',reviews:1,loops:0,must_fix:0,should_fix:0,suggestion:0}}))
  await new Promise(resolve=>setTimeout(resolve,20))
  expect(target.querySelector('.review .badge')?.textContent).toBe('Unverified')
 } finally {await unmount(app);vi.unstubAllGlobals()}
})
