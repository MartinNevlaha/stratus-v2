// @vitest-environment jsdom
import { beforeEach, afterEach, it, expect, vi } from 'vitest'
import { mount, unmount, flushSync } from 'svelte'
const capture=vi.hoisted(()=>({terminals:0,urls:[] as string[]}))
vi.mock('@xterm/xterm',()=>({Terminal:class {rows=24;cols=80;constructor(){capture.terminals++}loadAddon(){}open(){}onData(){}onSelectionChange(){return{dispose(){}}}attachCustomKeyEventHandler(){}dispose(){}refresh(){} }}))
vi.mock('@xterm/addon-fit',()=>({FitAddon:class{fit(){}}}))
vi.mock('$lib/store',async original=>({...await original<typeof import('$lib/store')>(),initStore:vi.fn()}))
import App from './App.svelte'
import { appState } from '$lib/store'
let app: ReturnType<typeof mount>
beforeEach(()=>{capture.terminals=0;capture.urls=[];history.replaceState({},'', '/?view=dashboard');localStorage.clear();appState.loading=false;appState.dashboard={workflows:[],recent_events:[],governance:{},vexor_available:false,ws_clients:0,ts:''} as never;appState.activeTab='memory';vi.stubGlobal('fetch',async(input: string | URL | Request)=>{const path=String(input).split('?')[0];if(path.endsWith('/workflows')||path.endsWith('/swarm/missions')||path.endsWith('/guardian/alerts'))return Response.json([]);return Response.json({items:[],agents:[],results:[],total:0,count:0})});vi.stubGlobal('ResizeObserver',class{observe(){}disconnect(){}});vi.stubGlobal('IntersectionObserver',class{observe(){}disconnect(){}});vi.stubGlobal('requestAnimationFrame',()=>0);vi.stubGlobal('WebSocket',class{static OPEN=1;readyState=0;constructor(url:string){capture.urls.push(url)}close(){}send(){}})})
afterEach(async()=>{if(app)await unmount(app);vi.unstubAllGlobals()})
it('dashboard view never mounts Terminal',()=>{app=mount(App,{target:document.body});flushSync();expect(capture.terminals).toBe(0);expect(capture.urls.some(url=>url.includes('/api/terminal/ws'))).toBe(false)})
it('remembered terminal tab still has no PTY',()=>{localStorage.setItem('stratus-active-tab','terminal');appState.activeTab='terminal';app=mount(App,{target:document.body});flushSync();expect(capture.terminals).toBe(0);expect(capture.urls.some(url=>url.includes('/api/terminal/ws'))).toBe(false)})

it('ordinary URL retains the mounted terminal',()=>{history.replaceState({},'', '/');app=mount(App,{target:document.body});flushSync();expect(capture.terminals).toBe(1);expect(capture.urls.some(url=>url.includes('/api/terminal/ws'))).toBe(true)})
it('dashboard survives unavailable local storage',()=>{const spy=vi.spyOn(Storage.prototype,'getItem').mockImplementation(()=>{throw new Error('storage disabled')});try{app=mount(App,{target:document.body});flushSync();expect(capture.terminals).toBe(0)}finally{spy.mockRestore()}})
