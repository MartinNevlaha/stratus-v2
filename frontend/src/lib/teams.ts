export interface TeamProfile { runtime: string; model: string; effort: string }
export interface TeamChoice extends TeamProfile { role: string; complexEffort: string }
export interface TeamMemberConfig { id: string; role: string; profile: TeamProfile; complex_profile?: TeamProfile }
export interface TeamEvent {kind:string;id:string;text:string}
export interface TeamAttachment {id:string;generation:number;state:string;expires_at?:string;identity:{runtime:string;host_id:string;native_id:string;project_root:string};capabilities:{queue:boolean;observe_user_turns:boolean;correlate_results:boolean;wake:boolean}}
export interface TeamMember extends TeamMemberConfig {native_id?:string;state:string;observed_model?:string;observed_effort?:string;active_task_id?:string;error?:string;approval?:TeamEvent;approval_response_id?:string;last_output?:string;attachment?:TeamAttachment}
export interface TeamTask {id:string;from:string;to:string;role:string;text:string;workflow_id?:string;revision?:string;state:string;result?:string;error?:string;kind:string;created_at:string;origin?:string}
export interface TeamLimits {max_concurrent:number;max_auto_rounds:number;max_turns:number;max_tokens:number}
export interface ManagedTeam {id:string;project_id:string;project_root:string;state:string;members:TeamMember[];tasks:TeamTask[];limits:TeamLimits;turns:number;auto_wakeups:number;input_tokens:number;output_tokens:number;usage_known:boolean;reason?:string}
export interface TeamPreflight {project_id:string;project_root:string;runtimes:{runtime:string;available:boolean;reason?:string;models:{id:string;efforts:string[]}[]}[];profiles:Record<string,Record<string,TeamProfile>>;warnings:string[];limits:TeamLimits}
export function launchMembers(rows:TeamChoice[]):TeamMemberConfig[] {
 return rows.map((row,i)=>{
  if(!row.model.trim()||!row.effort.trim()||!row.runtime) throw new Error(`Vyber model a úroveň uvažovania pre rolu ${row.role}.`)
  const profile={runtime:row.runtime,model:row.model.trim(),effort:row.effort.trim()}
  return {id:`${row.role}-${i+1}`,role:row.role,profile,complex_profile:{...profile,effort:row.complexEffort.trim()||profile.effort}}
 })
}
export function usageLabel(known:boolean,input:number,output:number):string {
 return known?`${(input+output).toLocaleString('sk')} tokenov`:'Spotreba tokenov neznáma'
}
export async function teamRequest<T>(path:string,body?:unknown):Promise<T> {
 const controller=new AbortController();const timeout=setTimeout(()=>controller.abort(),30000)
 try {
  const res=await fetch(`/api/${path}`,{method:body===undefined?'GET':'POST',headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:controller.signal})
  const data=await res.json();if(!res.ok)throw new Error(data.error??`Požiadavka zlyhala (${res.status})`);return data
 } finally {clearTimeout(timeout)}
}
