<script lang="ts">
 import { onMount } from 'svelte'
 import { wsClient } from '$lib/ws'
 import { debounce } from '$lib/comms'
 import { appState } from '$lib/store'
 import { applySavedProfiles, defaultComplexEffort, launchMembers, saveProjectProfile, sourceLabel, teamRequest, usageLabel } from '$lib/teams'
 import TeamHandoff from '../components/TeamHandoff.svelte'
 import type { ManagedTeam, TeamChoice, TeamLimits, TeamPreflight } from '$lib/teams'
 let preflight=$state<TeamPreflight|null>(null)
 let teams=$state<ManagedTeam[]>([])
 let selected=$state('')
 let choices=$state<TeamChoice[]>([])
 let limits=$state<TeamLimits>({max_concurrent:2,max_auto_rounds:4,max_turns:40,max_tokens:0})
 let busy=$state(false),loading=$state(true),error=$state(''),notice=$state(''),showSetup=$state(false),isolated=$state(false)
 let text=$state(''),recipient=$state('')
 let pendingRequest:{id:string;text:string;to:string}|null=null
 const roles:Record<string,string>={lead:'Vedenie postupu',implementation:'Implementácia',design:'Návrh a plán',review:'Nezávislá kontrola',diagnosis:'Diagnostika',mechanical:'Jednoduché úpravy'}
 const runtimes:Record<string,string>={codex:'Codex','claude-code':'Claude Code',opencode:'OpenCode'}
 const states:Record<string,string>={running:'Pracuje',working:'Pracuje',active:'Aktívny',ready:'Pripravený',idle:'Pripravený',queued:'Vo fronte',waiting:'Čaká na kolegu',waiting_colleague:'Čaká na kolegu',needs_user:'Potrebuje teba',paused:'Pozastavený',stopped:'Zastavený',done:'Hotovo',completed:'Hotovo',complete:'Hotovo',error:'Chyba',failed:'Chyba',cancelled:'Zrušené',stale:'Zmenený podklad',connected:'Pripojený',disconnected:'Spojenie prerušené',detached:'Odpojený',expired:'Spojenie vypršalo',unknown:'Spojenie sa overuje'}
 let team=$derived(teams.find(t=>t.id===selected)??null)
 let visibleTasks=$derived(team?.tasks.filter(task=>task.origin!=='external')??[])
 const effortOrder:Record<string,number>={none:0,minimal:1,low:2,medium:3,high:4,xhigh:5,max:6}
 function modelOptions(runtime:string) {
  return preflight?.runtimes.find(rt=>rt.runtime===runtime)?.models??[]
 }
 function effortOptions(row:TeamChoice) {
  const efforts=modelOptions(row.runtime).find(model=>model.id===row.model)?.efforts??[]
  return [...efforts].sort((left,right)=>(effortOrder[left]??99)-(effortOrder[right]??99)||left.localeCompare(right))
 }
 let validChoices=$derived(choices.length>0&&choices.every(row=>{
  const available=preflight?.runtimes.some(rt=>rt.runtime===row.runtime&&rt.available)
  const efforts=effortOptions(row)
  return available&&efforts.includes(row.effort)&&efforts.includes(row.complexEffort)
 }))
 const status=(value:string)=>states[value]??value
 function chooseRuntime(index:number,runtime:string) {
  const role=choices[index].role,profile=preflight?.profiles[runtime]?.[role]
  choices[index]={role,runtime,model:profile?.model??'',effort:profile?.effort??'',complexEffort:preflight?defaultComplexEffort(preflight,runtime,role):''}
 }
 function chooseModel(index:number,model:string) {
  const row={...choices[index],model}
  const efforts=effortOptions(row)
  if(!efforts.includes(row.effort))row.effort=''
  if(!efforts.includes(row.complexEffort))row.complexEffort=''
  choices[index]=row
 }
 async function refresh() {
  try {const data=await teamRequest<{teams:ManagedTeam[]}>('teams');teams=data.teams??[];if(!selected&&teams.length)selected=teams[0].id}
  catch(e){error=e instanceof Error?e.message:String(e)}
 }
 async function readiness() {
  busy=true;error=''
  try {
   preflight=await teamRequest<TeamPreflight>('teams/preflight')
   const available=preflight.runtimes.filter(r=>r.available).map(r=>r.runtime)
   choices=Object.keys(roles).map(role=>{
    const runtime=(role==='lead'&&available.includes('claude-code')?'claude-code':available.includes('codex')?'codex':available[0])??'codex'
    const profile=preflight?.profiles[runtime]?.[role]
    return {role,runtime,model:profile?.model??'',effort:profile?.effort??'',complexEffort:defaultComplexEffort(preflight!,runtime,role)}
   })
  }catch(e){error=e instanceof Error?e.message:String(e)}finally{busy=false}
 }
 async function perform(fn:()=>Promise<void>) {busy=true;error='';try{await fn();await refresh()}catch(e){error=e instanceof Error?e.message:String(e)}finally{busy=false}}
 async function start() {await perform(async()=>{const created=await teamRequest<ManagedTeam>('teams',{members:launchMembers(choices),limits,...(isolated?{isolated_workspaces:true}:{})});selected=created.id;showSetup=false})}
 async function saveProfile() {
  busy=true;error='';notice=''
  try {const saved=await saveProjectProfile(choices);if(preflight)preflight=applySavedProfiles(preflight,saved);notice='Profil projektu je uložený. Nové tímy ho použijú, bežiace tímy sa nezmenia.'}
  catch(e){error=e instanceof Error?e.message:String(e)}finally{busy=false}
 }
 async function action(value:string) {if(team)await perform(async()=>{await teamRequest(`teams/${team!.id}/actions`,{action:value})})}
 async function send() {if(!team||!text.trim())return;const value=text;if(!pendingRequest||pendingRequest.text!==value||pendingRequest.to!==recipient)pendingRequest={id:crypto.randomUUID(),text:value,to:recipient};const id=pendingRequest.id;await perform(async()=>{await teamRequest(`teams/${team!.id}/tasks`,{id,text:value,to:recipient||undefined});text='';pendingRequest=null})}
 async function recover(member:string) {if(team)await perform(async()=>{await teamRequest(`teams/${team!.id}/recover`,{member_id:member})})}
 async function attachmentAction(member:string,action:'detach'|'lead') {if(team)await perform(async()=>{await teamRequest(`teams/${team!.id}/attachments`,{member_id:member,action})})}
 async function approval(member:string,id:string,allow:boolean) {if(team)await perform(async()=>{await teamRequest(`teams/${team!.id}/approvals`,{member_id:member,approval_id:id,allow})})}
 onMount(()=>{
  const pending=appState.pendingTeamTask
  if(pending){text=pending.text;appState.pendingTeamTask=null}
  refresh().finally(()=>{loading=false;if(!teams.length){showSetup=true;readiness()}})
  const d=debounce(refresh,150),offs=[wsClient.on('team_updated',d.call),wsClient.on('connected',d.call)]
  return()=>{offs.forEach(off=>off());d.cancel()}
 })
</script>

<div class="team-page">
 <div class="page-heading"><div><h1>Tím</h1><p>Jedno zadanie. Agenti si rozdelia prácu a výsledky doručia automaticky.</p></div><button onclick={()=>{showSetup=!showSetup;if(showSetup&&!preflight)readiness()}}>{showSetup?'Zavrieť nastavenie':'Nový tím'}</button></div>
 <section class="setup"><h2>Pripojiť existujúci chat</h2><p>V Codexe povedz: <strong>„Pripoj tento chat k tímu Stratus.“</strong> Zadania aj výsledky potom môžeš riešiť v tom istom chate. Ak tím ešte nemáš, vytvorí sa s týmto chatom ako vedúcim.</p><p class="muted">Chat potrebuje aktuálne pripojenie Stratus a prístup ku konektoru aplikácie Codex. Pripojenie už otvorených chatov Claude a OpenCode sa ešte overuje; ich spúšťanie cez tím je dostupné.</p></section>
 {#if error}<div class="error" role="alert">{error}</div>{/if}
 {#if notice}<div class="notice" role="status">{notice}</div>{/if}
 {#if loading}<p>Načítavam tím…</p>{/if}
 {#if showSetup}
 <section class="setup">
  <div class="section-heading"><h2>Pripraviť tím</h2><button onclick={readiness} disabled={busy}>Overiť dostupnosť</button></div>
  {#if text.trim()&&!team}<pre class="pending-task">Zadanie: {text}</pre>{/if}
  {#if !preflight}<p>Overujem nástroje a načítavam nastavenia projektu…</p>{:else}
   <p class="muted">Projekt: {preflight.project_root}</p>
   <div class="runtimes">{#each preflight.runtimes as rt}<div class:unavailable={!rt.available}><strong>{runtimes[rt.runtime]??rt.runtime}</strong><span>{rt.available?'Dostupný nástroj':'Nedostupný'}</span>{#if rt.reason}<small>{rt.reason}</small>{/if}</div>{/each}</div>
   {#each preflight.warnings as warning}<p class="warning">{warning}</p>{/each}
   <p class="muted">Roly sa spustia až pri potrebnej práci. Model aj oprávnenia sa overia pri otvorení relácie.</p>
   <div class="table-scroll"><table><thead><tr><th>Rola</th><th>Nástroj</th><th>Model</th><th>Uvažovanie</th><th>Zložitá úloha / spec-complex</th></tr></thead><tbody>
    {#each choices as row,i}
     {@const models=modelOptions(row.runtime)}
     {@const efforts=effortOptions(row)}
     <tr>
      <td>{roles[row.role]}{#if preflight.profile_sources?.[row.runtime]?.[row.role]}<br/><small>Zdroj: {sourceLabel(preflight.profile_sources[row.runtime][row.role])}</small>{/if}</td>
      <td><select aria-label={`Nástroj: ${roles[row.role]}`} value={row.runtime} onchange={e=>chooseRuntime(i,e.currentTarget.value)} disabled={busy}>{#each preflight.runtimes as rt}<option value={rt.runtime} disabled={!rt.available}>{runtimes[rt.runtime]}</option>{/each}</select></td>
      <td>
       <select aria-label={`Model: ${roles[row.role]}`} value={row.model} onchange={e=>chooseModel(i,e.currentTarget.value)} disabled={busy||!models.length}>
        {#if !row.model}<option value="" disabled>{models.length?'Vyber model':'Žiadne dostupné modely'}</option>
        {:else if !models.some(model=>model.id===row.model)}<option value={row.model} disabled>{row.model} (nedostupný)</option>{/if}
        {#each models as model}<option value={model.id}>{model.id}</option>{/each}
       </select>
      </td>
      <td>
       <select aria-label={`Uvažovanie: ${roles[row.role]}`} bind:value={row.effort} disabled={busy||!efforts.length}>
        {#if !row.effort}<option value="" disabled>{efforts.length?'Vyber uvažovanie':'Uvažovanie nie je dostupné'}</option>
        {:else if !efforts.includes(row.effort)}<option value={row.effort} disabled>{row.effort} (nedostupné)</option>{/if}
        {#each efforts as effort}<option value={effort}>{effort}</option>{/each}
       </select>
      </td>
      <td>
       <select aria-label={`Zložitá úloha: ${roles[row.role]}`} bind:value={row.complexEffort} disabled={busy||!efforts.length}>
        {#if !row.complexEffort}<option value="" disabled>{efforts.length?'Vyber uvažovanie':'Uvažovanie nie je dostupné'}</option>
        {:else if !efforts.includes(row.complexEffort)}<option value={row.complexEffort} disabled>{row.complexEffort} (nedostupné)</option>{/if}
        {#each efforts as effort}<option value={effort}>{effort}</option>{/each}
       </select>
      </td>
     </tr>
    {/each}
   </tbody></table></div>
   <div class="limits"><label>Súbežní agenti<input type="number" min="1" max="8" bind:value={limits.max_concurrent}/></label><label>Automatické kolá<input type="number" min="1" max="20" bind:value={limits.max_auto_rounds}/></label><label>Najviac tímových vstupov<input type="number" min="1" max="1000" bind:value={limits.max_turns}/></label><label>Limit známych tokenov (0 = bez limitu)<input type="number" min="0" step="1000" bind:value={limits.max_tokens}/></label></div>
   <label class="isolate"><input type="checkbox" aria-label="Izolované pracovné priestory" bind:checked={isolated} disabled={busy}/> Izolované pracovné priestory (implementátori pracujú vo vlastných git worktree)</label>
   <p class="muted">Nedá sa kombinovať s pripojeným chatom; server takú žiadosť odmietne.</p>
   <p class="muted">Čakanie nevolá model. Keď nástroj neoznamuje spotrebu tokenov, platí limit tímových vstupov. Súčasné fázy a povinné kontroly workflow zostávajú zachované.</p>
   <div class="actions"><button class="primary" onclick={start} disabled={busy||!validChoices}>Spustiť tím</button><button onclick={saveProfile} disabled={busy||!validChoices}>Uložiť ako profil projektu</button></div>
  {/if}
 </section>
 {/if}
 {#if teams.length}<div class="team-picker"><label>Tím <select bind:value={selected}>{#each teams as item}<option value={item.id}>{item.id.slice(0,8)} · {status(item.state)}</option>{/each}</select></label></div>{/if}
 {#if team}
 <div class="team-toolbar"><strong>{team.state==='running'?'Aktívny':status(team.state)}</strong><span>{team.turns} / {team.limits.max_turns} tímových vstupov · {usageLabel(team.usage_known,team.input_tokens,team.output_tokens)}</span><div class="actions"><button onclick={()=>action('pause')} disabled={busy||team.state==='paused'||team.state==='stopped'}>Pozastaviť</button><button onclick={()=>action('resume')} disabled={busy||team.state==='stopped'}>Pokračovať</button><button class="danger" onclick={()=>action('stop')} disabled={busy||team.state==='stopped'}>Zastaviť</button></div></div>
 {#if team.reason}<p class="warning">{team.reason}</p>{/if}
 {#if team.isolated_workspaces}{#key team.id}<TeamHandoff teamId={team.id} onremoved={()=>{notice='Pracovné priestory sú upratané.';refresh()}}/>{/key}{/if}
 <div class="members">
  {#each team.members as member}
   <article class="member">
    <div class="section-heading"><h3>{member.role==='member'?'Člen tímu':roles[member.role]??member.role}</h3><span class="state" class:attention={member.state==='needs_user'||member.state==='error'}>{status(member.state)}</span></div>
    <small>{member.attachment?'Pripojený pôvodný chat':'Relácia spúšťaná Stratusom'}</small>
    <p>{runtimes[member.profile.runtime]??member.profile.runtime} · {member.profile.model||'Model určuje pôvodný chat'}{member.profile.effort?` / ${member.profile.effort}`:''}</p>
    <small>Potvrdený model: {member.observed_model||'nástroj ho neoznámil'}{member.observed_effort?` / ${member.observed_effort}`:''}</small>
    {#if member.attachment}
     <p>Pripojenie: {status(member.attachment.state)}</p>
     {#if member.attachment.state==='connected'&&team.state!=='stopped'}
      {#if member.state==='needs_user'}<p class="muted">Ak chat čaká na tvoje rozhodnutie, odpovedz v pôvodnej aplikácii. Pri neistom doručení skontroluj výsledok a potom sa znovu pripoj v pôvodnom chate. Úloha sa automaticky nezopakuje.</p>{/if}
      <div class="actions">
       {#if member.role!=='lead'}<button disabled={busy} onclick={()=>attachmentAction(member.id,'lead')}>Viesť tím z tohto chatu</button>{/if}
       <button disabled={busy} onclick={()=>attachmentAction(member.id,'detach')}>Odpojiť od tímu</button>
      </div>
     {:else}
      <p class="muted">Na obnovenie povedz v pôvodnom chate „Pripoj tento chat k tímu Stratus“. Nejasne doručená úloha sa automaticky nezopakuje.</p>
     {/if}
    {/if}
    {#if member.error}<p class="error">{member.error}</p>{/if}
    {#if !member.attachment&&member.state==='needs_user'&&(!member.approval||!!member.approval_response_id)}<p class="muted">Skontroluj predchádzajúci výsledok. Obnovenie dovolí nové zadanie; neistá úloha sa nezopakuje.</p><button disabled={busy} onclick={()=>recover(member.id)}>Obnoviť po kontrole</button>{/if}
    {#if member.approval}<div class="approval"><strong>Nástroj potrebuje tvoje rozhodnutie</strong><pre>{member.approval.text}</pre><button disabled={busy||!!member.approval_response_id} onclick={()=>approval(member.id,member.approval!.id,true)}>Povoliť raz</button><button disabled={busy||!!member.approval_response_id} onclick={()=>approval(member.id,member.approval!.id,false)}>Zamietnuť</button></div>{/if}
    {#if member.last_output}<details><summary>Posledný výstup</summary><pre>{member.last_output}</pre></details>{/if}
   </article>
  {/each}
 </div>
 <form class="compose" onsubmit={e=>{e.preventDefault();send()}}><label for="team-input">Zadanie alebo doplnenie</label><textarea id="team-input" bind:value={text} placeholder="Napríklad: Priprav návrh funkcie, implementuj ju a nechaj nezávisle skontrolovať." rows="3" disabled={team.state==='stopped'}></textarea><div class="compose-actions"><select aria-label="Príjemca zadania" bind:value={recipient}><option value="">Vedúci tímu</option>{#each team.members as member}<option value={member.id}>{roles[member.role]??member.role} · {runtimes[member.profile.runtime]}</option>{/each}</select><button class="primary" type="submit" disabled={busy||!text.trim()||team.state==='stopped'}>Odoslať</button></div></form>
 <section class="task-list"><h2>Práca a odovzdané výsledky</h2>{#if !visibleTasks.length}<p class="muted">Tím čaká na tvoje zadanie. Čakanie nespotrebúva modelové tokeny.</p>{/if}{#each [...visibleTasks].reverse() as task}<article><div class="section-heading"><strong>{roles[task.role]??task.role} · {status(task.state)}</strong><small>{new Date(task.created_at).toLocaleString('sk')}</small></div><pre>{task.text}</pre>{#if task.workflow_id}<small>Workflow: {task.workflow_id}</small>{/if}{#if task.profile_reason}<details><summary>Prečo tento model?</summary><p>{task.profile?.model||'Model určuje pôvodný chat'}{task.profile?.effort?` / ${task.profile.effort}`:''} · {task.profile_reason}</p></details>{/if}{#if task.result}<details open={task.from==='user'}><summary>Výsledok</summary><pre>{task.result}</pre></details>{/if}{#if task.error}<p class="error">{task.error}</p>{/if}</article>{/each}</section>
 {/if}
</div>
<style>
 .team-page{height:100%;overflow:auto;padding:4px 6px 28px;display:flex;flex-direction:column;gap:18px}.page-heading,.section-heading,.team-toolbar,.compose-actions{display:flex;justify-content:space-between;align-items:center;gap:12px}.page-heading p,.muted,small{color:#8b949e}h1{font-size:24px}h2{font-size:17px}h3{font-size:15px}button,input,select,textarea{background:#0d1117;color:#c9d1d9;border:1px solid #30363d;border-radius:6px;padding:8px 10px;font:inherit}button{cursor:pointer;white-space:nowrap}button:hover:not(:disabled){border-color:#58a6ff}button:disabled{opacity:.45;cursor:default}.primary{background:#238636;color:white;border-color:#2ea043}.danger{color:#f85149}.setup,.compose,.member,.task-list article{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:16px}.setup{display:flex;flex-direction:column;gap:14px}.setup>.actions{align-self:flex-start}.runtimes,.limits,.actions{display:flex;gap:12px;flex-wrap:wrap}.runtimes>div{display:flex;flex-direction:column;gap:4px;border-left:3px solid #3fb950;padding:8px 12px;flex:1}.runtimes>.unavailable{border-color:#8b949e}.table-scroll{overflow:auto}table{border-collapse:collapse;width:100%;text-align:left}th,td{padding:8px;border-bottom:1px solid #30363d}th{color:#8b949e;font-weight:500;font-size:12px}td select{width:100%;min-width:90px}td:nth-child(3){min-width:180px}.isolate{display:flex;align-items:center;gap:8px}.isolate input{padding:0}.limits label{display:flex;flex-direction:column;gap:5px;font-size:12px}.limits input{max-width:180px}.team-toolbar{flex-wrap:wrap}.members{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:12px}.member p{margin:9px 0}.state{font-size:12px;color:#3fb950}.attention,.warning{color:#d29922}.warning,.error{padding:10px;border-radius:6px;background:#d2992210}.notice{color:#3fb950;padding:10px;border-radius:6px;background:#3fb95010}.error{color:#ff7b72;background:#f8514910;white-space:pre-wrap}.compose{display:flex;flex-direction:column;gap:10px}textarea{resize:vertical;width:100%}.task-list{display:flex;flex-direction:column;gap:10px}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;margin:10px 0}details{margin-top:10px}summary{cursor:pointer;color:#58a6ff}.approval{margin-top:12px;border:1px solid #d29922;padding:12px;border-radius:6px}.approval button{margin-right:8px}.team-picker select{margin-left:8px}@media(max-width:650px){.page-heading{align-items:flex-start}.members{grid-template-columns:1fr}.team-toolbar{align-items:flex-start;flex-direction:column}.limits input{max-width:125px}}
</style>
