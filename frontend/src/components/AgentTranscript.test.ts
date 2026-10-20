// @vitest-environment jsdom
import { it, expect, vi } from 'vitest'
import { mount, unmount } from 'svelte'
import AgentTranscript from './AgentTranscript.svelte'
it('unknown usage renders unavailable with null usage',async()=>{vi.stubGlobal('fetch',async()=>Response.json({steps:[],usage:null,usage_available:false,runtime:'codex'}));const target=document.createElement('div');document.body.append(target);const app=mount(AgentTranscript,{target,props:{workflowId:'spec-codex',agentId:'a1',live:false}});try{await vi.waitFor(()=>expect(target.textContent).toContain('Usage unavailable'))}finally{await unmount(app);target.remove();vi.unstubAllGlobals()}})
