export interface TeamProfile { runtime: string; model: string; effort: string }
export interface TeamChoice extends TeamProfile { role: string; complexEffort: string }
export interface TeamMemberConfig { id: string; role: string; profile: TeamProfile; complex_profile?: TeamProfile }
export interface TeamEvent {kind:string;id:string;text:string}
export interface TeamAttachment {id:string;generation:number;state:string;expires_at?:string;identity:{runtime:string;host_id:string;native_id:string;project_root:string};capabilities:{queue:boolean;observe_user_turns:boolean;correlate_results:boolean;wake:boolean}}
export interface TeamMember extends TeamMemberConfig {native_id?:string;state:string;observed_model?:string;observed_effort?:string;active_task_id?:string;error?:string;approval?:TeamEvent;approval_response_id?:string;last_output?:string;attachment?:TeamAttachment}
export interface TeamTask {id:string;from:string;to:string;role:string;text:string;workflow_id?:string;revision?:string;state:string;result?:string;error?:string;kind:string;created_at:string;origin?:string;profile?:TeamProfile;profile_reason?:string}
export interface TeamLimits {max_concurrent:number;max_auto_rounds:number;max_turns:number;max_tokens:number}
export interface ManagedTeam {id:string;project_id:string;project_root:string;state:string;members:TeamMember[];tasks:TeamTask[];limits:TeamLimits;turns:number;auto_wakeups:number;input_tokens:number;output_tokens:number;usage_known:boolean;reason?:string}
export interface TeamPreflight {project_id:string;project_root:string;runtimes:{runtime:string;available:boolean;reason?:string;models:{id:string;efforts:string[]}[]}[];profiles:Record<string,Record<string,TeamProfile>>;profile_sources?:Record<string,Record<string,string>>;complex_efforts?:Record<string,Record<string,string>>;warnings:string[];limits:TeamLimits}
export function launchMembers(rows:TeamChoice[]):TeamMemberConfig[] {
 return rows.map((row,i)=>{
  if(!row.model.trim()||!row.effort.trim()||!row.runtime) throw new Error(`Vyber model a úroveň uvažovania pre rolu ${row.role}.`)
  const profile={runtime:row.runtime,model:row.model.trim(),effort:row.effort.trim()}
  return {id:`${row.role}-${i+1}`,role:row.role,profile,complex_profile:{...profile,effort:row.complexEffort.trim()||profile.effort}}
 })
}
export type SavedProfiles=Record<string,Record<string,{model:string;effort:string;complex_effort?:string}>>
export function profileSaveBody(rows:TeamChoice[]):SavedProfiles {
 const body:SavedProfiles={}
 for(const row of rows){
  if(!row.model.trim()||!row.effort.trim()||!row.runtime) throw new Error(`Vyber model a úroveň uvažovania pre rolu ${row.role}.`)
  const complex=row.complexEffort.trim()
  ;(body[row.runtime]??={})[row.role]={model:row.model.trim(),effort:row.effort.trim(),...(complex?{complex_effort:complex}:{})}
 }
 return body
}
export async function saveProjectProfile(rows:TeamChoice[]):Promise<SavedProfiles> {
 const data=await teamRequest<{profiles:SavedProfiles}>('teams/profiles',profileSaveBody(rows),'PUT')
 return data.profiles
}
export function applySavedProfiles(preflight:TeamPreflight,body:SavedProfiles):TeamPreflight {
 // JSON copy: preflight may be a Svelte state proxy, which structuredClone rejects.
 const copy=<T>(value:T):T=>JSON.parse(JSON.stringify(value))
 const profiles=copy(preflight.profiles),sources=copy(preflight.profile_sources??{}),complex=copy(preflight.complex_efforts??{})
 for(const [runtime,roles] of Object.entries(body)) for(const [role,saved] of Object.entries(roles)){
  (profiles[runtime]??={})[role]={runtime,model:saved.model,effort:saved.effort}
  ;(sources[runtime]??={})[role]='project'
  if(saved.complex_effort)(complex[runtime]??={})[role]=saved.complex_effort
 }
 return {...preflight,profiles,profile_sources:sources,complex_efforts:complex}
}
const sourceLabels:Record<string,string>={default:'Predvolené','agent-file':'Súbor agenta',opencode:'Nastavenie OpenCode',project:'Profil projektu'}
export const sourceLabel=(source?:string):string=>source?sourceLabels[source]??source:''
export function defaultComplexEffort(preflight:TeamPreflight,runtime:string,role:string):string {
 const profile=preflight.profiles[runtime]?.[role]
 return preflight.complex_efforts?.[runtime]?.[role]??(runtime==='codex'&&profile?.model==='gpt-6-astra'?'xhigh':profile?.effort??'')
}
export function usageLabel(known:boolean,input:number,output:number):string {
 return known?`${(input+output).toLocaleString('sk')} tokenov`:'Spotreba tokenov neznáma'
}
export async function teamRequest<T>(path:string,body?:unknown,method:'POST'|'PUT'='POST'):Promise<T> {
 const controller=new AbortController();const timeout=setTimeout(()=>controller.abort(),30000)
 try {
  const res=await fetch(`/api/${path}`,{method:body===undefined?'GET':method,headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:controller.signal})
  const data=await res.json();if(!res.ok)throw new Error(data.error??`Požiadavka zlyhala (${res.status})`);return data
 } finally {clearTimeout(timeout)}
}
