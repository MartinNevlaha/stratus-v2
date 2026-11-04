<script lang="ts">
 import { onMount } from 'svelte'
 import { wsClient } from '$lib/ws'
 import { debounce } from '$lib/comms'
 import { launchMembers, teamRequest, usageLabel } from '$lib/teams'
 import type { ManagedTeam, TeamChoice, TeamLimits, TeamPreflight } from '$lib/teams'
 let preflight=$state<TeamPreflight|null>(null)
 let teams=$state<ManagedTeam[]>([])
 let selected=$state('')
 let choices=$state<TeamChoice[]>([])
 let limits=$state<TeamLimits>({max_concurrent:2,max_auto_rounds:4,max_turns:40,max_tokens:0})
 let busy=$state(false),loading=$state(true),error=$state(''),showSetup=$state(false)
 let text=$state(''),recipient=$state('')
 let pendingRequest:{id:string;text:string;to:string}|null=null
 const roles:Record<string,string>={lead:'Vedenie postupu',implementation:'Implementácia',design:'Návrh a plán',review:'Nezávislá kontrola',diagnosis:'Diagnostika',mechanical:'Jednoduché úpravy'}
 const runtimes:Record<string,string>={codex:'Codex','claude-code':'Claude Code',opencode:'OpenCode'}
 const states:Record<string,string>={running:'Pracuje',working:'Pracuje',active:'Aktívny',ready:'Pripravený',idle:'Pripravený',queued:'Vo fronte',waiting:'Čaká na kolegu',waiting_colleague:'Čaká na kolegu',needs_user:'Potrebuje teba',paused:'Pozastavený',stopped:'Zastavený',done:'Hotovo',completed:'Hotovo',complete:'Hotovo',error:'Chyba',failed:'Chyba',cancelled:'Zrušené',stale:'Zmenený podklad',connected:'Pripojený',disconnected:'Spojenie prerušené',detached:'Odpojený',expired:'Spojenie vypršalo',unknown:'Spojenie sa overuje'}
 let team=$derived(teams.find(t=>t.id===selected)??null)
 let visibleTasks=$derived(team?.tasks.filter(task=>task.origin!=='external')??[])
 const status=(value:string)=>states[value]??value
 function chooseRuntime(index:number,runtime:string) {
  const role=choices[index].role,profile=preflight?.profiles[runtime]?.[role]
  choices[index]={role,runtime,model:profile?.model??'',effort:profile?.effort??'',complexEffort:runtime==='codex'&&profile?.model==='gpt-6-astra'?'xhigh':profile?.effort??''}
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
    return {role,runtime,model:profile?.model??'',effort:profile?.effort??'',complexEffort:runtime==='codex'&&profile?.model==='gpt-6-astra'?'xhigh':profile?.effort??''}
   })
  }catch(e){error=e instanceof Error?e.message:String(e)}finally{busy=false}
 }
 async function perform(fn:()=>Promise<void>) {busy=true;error='';try{await fn();await refresh()}catch(e){error=e instanceof Error?e.message:String(e)}finally{busy=false}}
 async function start() {await perform(async()=>{const created=await teamRequest<ManagedTeam>('teams',{members:launchMembers(choices),limits});selected=created.id;showSetup=false})}
 async function action(value:string) {if(team)await perform(async()=>{await teamRequest(`teams/${team!.id}/actions`,{action:value})})}
 async function send() {if(!team||!text.trim())return;const value=text;if(!pendingRequest||pendingRequest.text!==value||pendingRequest.to!==recipient)pendingRequest={id:crypto.randomUUID(),text:value,to:recipient};const id=pendingRequest.id;await perform(async()=>{await teamRequest(`teams/${team!.id}/tasks`,{id,text:value,to:recipient||undefined});text='';pendingRequest=null})}
 async function recover(member:string) {if(team)await perform(async()=>{await teamRequest(`teams/${team!.id}/recover`,{member_id:member})})}
 async function attachmentAction(member:string,action:'detach'|'lead') {if(team)await perform(async()=>{await teamRequest(`teams/${team!.id}/attachments`,{member_id:member,action})})}
 async function approval(member:string,id:string,allow:boolean) {if(team)await perform(async()=>{await teamRequest(`teams/${team!.id}/approvals`,{member_id:member,approval_id:id,allow})})}
 onMount(()=>{
  refresh().finally(()=>{loading=false;if(!teams.length){showSetup=true;readiness()}})
  const d=debounce(refresh,150),offs=[wsClient.on('team_updated',d.call),wsClient.on('connected',d.call)]
  return()=>{offs.forEach(off=>off());d.cancel()}
 })
</script>

<div class="team-page">
 <div class="page-heading"><div><h1>Tím</h1><p>Jedno zadanie. Agenti si rozdelia prácu a výsledky doručia automaticky.</p></div><button onclick={()=>{showSetup=!showSetup;if(showSetup&&!preflight)readiness()}}>{showSetup?'Zavrieť nastavenie':'Nový tím'}</button></div>
 <section class="setup"><h2>Pripojiť existujúci chat</h2><p>V Codexe povedz: <strong>„Pripoj tento chat k tímu Stratus.“</strong> Zadania aj výsledky potom môžeš riešiť v tom istom chate. Ak tím ešte nemáš, vytvorí sa s týmto chatom ako vedúcim.</p><p class="muted">Chat potrebuje aktuálne pripojenie Stratus a prístup ku konektoru aplikácie Codex. Pripojenie už otvorených chatov Claude a OpenCode sa ešte overuje; ich spúšťanie cez tím je dostupné.</p></section>
 {#if error}<div class="error" role="alert">{error}</div>{/if}
 {#if loading}<p>Načítavam tím…</p>{/if}
 {#if showSetup}
 <section class="setup">
  <div class="section-heading"><h2>Pripraviť tím</h2><button onclick={readiness} disabled={busy}>Overiť dostupnosť</button></div>
  {#if !preflight}<p>Overujem nástroje a načítavam nastavenia projektu…</p>{:else}
   <p class="muted">Projekt: {preflight.project_root}</p>
   <div class="runtimes">{#each preflight.runtimes as rt}<div class:unavailable={!rt.available}><strong>{runtimes[rt.runtime]??rt.runtime}</strong><span>{rt.available?'Dostupný nástroj':'Nedostupný'}</span>{#if rt.reason}<small>{rt.reason}</small>{/if}</div>{/each}</div>
   {#each preflight.warnings as warning}<p class="warning">{warning}</p>{/each}
   <p class="muted">Roly sa spustia až pri potrebnej práci. Model aj oprávnenia sa overia pri otvorení relácie.</p>
   <div class="table-scroll"><table><thead><tr><th>Rola</th><th>Nástroj</th><th>Model</th><th>Uvažovanie</th><th>Zložitá úloha / spec-complex</th></tr></thead><tbody>
    {#each choices as row,i}<tr><td>{roles[row.role]}</td><td><select aria-label={`Nástroj: ${roles[row.role]}`} value={row.runtime} onchange={e=>chooseRuntime(i,e.currentTarget.value)}>{#each preflight.runtimes as rt}<option value={rt.runtime} disabled={!rt.available}>{runtimes[rt.runtime]}</option>{/each}</select></td><td><input aria-label={`Model: ${roles[row.role]}`} bind:value={row.model} list={`models-${i}`} /><datalist id={`models-${i}`}>{#each preflight.runtimes.find(rt=>rt.runtime===row.runtime)?.models??[] as model}<option value={model.id}></option>{/each}</datalist></td><td><input aria-label={`Uvažovanie: ${roles[row.role]}`} bind:value={row.effort} /></td><td><input aria-label={`Zložitá úloha: ${roles[row.role]}`} bind:value={row.complexEffort}/></td></tr>{/each}
   </tbody></table></div>
   <div class="limits"><label>Súbežní agenti<input type="number" min="1" max="8" bind:value={limits.max_concurrent}/></label><label>Automatické kolá<input type="number" min="1" max="20" bind:value={limits.max_auto_rounds}/></label><label>Najviac tímových vstupov<input type="number" min="1" max="1000" bind:value={limits.max_turns}/></label><label>Limit známych tokenov (0 = bez limitu)<input type="number" min="0" step="1000" bind:value={limits.max_tokens}/></label></div>
   <p class="muted">Čakanie nevolá model. Keď nástroj neoznamuje spotrebu tokenov, platí limit tímových vstupov. Súčasné fázy a povinné kontroly workflow zostávajú zachované.</p>
   <button class="primary" onclick={start} disabled={busy||!preflight.runtimes.some(rt=>rt.available)}>Spustiť tím</button>
  {/if}
 </section>
 {/if}
 {#if teams.length}<div class="team-picker"><label>Tím <select bind:value={selected}>{#each teams as item}<option value={item.id}>{item.id.slice(0,8)} · {status(item.state)}</option>{/each}</select></label></div>{/if}
 {#if team}
 <div class="team-toolbar"><strong>{team.state==='running'?'Aktívny':status(team.state)}</strong><span>{team.turns} / {team.limits.max_turns} tímových vstupov · {usageLabel(team.usage_known,team.input_tokens,team.output_tokens)}</span><div class="actions"><button onclick={()=>action('pause')} disabled={busy||team.state==='paused'||team.state==='stopped'}>Pozastaviť</button><button onclick={()=>action('resume')} disabled={busy||team.state==='stopped'}>Pokračovať</button><button class="danger" onclick={()=>action('stop')} disabled={busy||team.state==='stopped'}>Zastaviť</button></div></div>
 {#if team.reason}<p class="warning">{team.reason}</p>{/if}
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
 <section class="task-list"><h2>Práca a odovzdané výsledky</h2>{#if !visibleTasks.length}<p class="muted">Tím čaká na tvoje zadanie. Čakanie nespotrebúva modelové tokeny.</p>{/if}{#each [...visibleTasks].reverse() as task}<article><div class="section-heading"><strong>{roles[task.role]??task.role} · {status(task.state)}</strong><small>{new Date(task.created_at).toLocaleString('sk')}</small></div><pre>{task.text}</pre>{#if task.workflow_id}<small>Workflow: {task.workflow_id}</small>{/if}{#if task.result}<details open={task.from==='user'}><summary>Výsledok</summary><pre>{task.result}</pre></details>{/if}{#if task.error}<p class="error">{task.error}</p>{/if}</article>{/each}</section>
 {/if}
</div>
<style>
 .team-page{height:100%;overflow:auto;padding:4px 6px 28px;display:flex;flex-direction:column;gap:18px}.page-heading,.section-heading,.team-toolbar,.compose-actions{display:flex;justify-content:space-between;align-items:center;gap:12px}.page-heading p,.muted,small{color:#8b949e}h1{font-size:24px}h2{font-size:17px}h3{font-size:15px}button,input,select,textarea{background:#0d1117;color:#c9d1d9;border:1px solid #30363d;border-radius:6px;padding:8px 10px;font:inherit}button{cursor:pointer;white-space:nowrap}button:hover:not(:disabled){border-color:#58a6ff}button:disabled{opacity:.45;cursor:default}.primary{background:#238636;color:white;border-color:#2ea043}.danger{color:#f85149}.setup,.compose,.member,.task-list article{background:#161b22;border:1px solid #30363d;border-radius:8px;padding:16px}.setup{display:flex;flex-direction:column;gap:14px}.setup>.primary{align-self:flex-start}.runtimes,.limits,.actions{display:flex;gap:12px;flex-wrap:wrap}.runtimes>div{display:flex;flex-direction:column;gap:4px;border-left:3px solid #3fb950;padding:8px 12px;flex:1}.runtimes>.unavailable{border-color:#8b949e}.table-scroll{overflow:auto}table{border-collapse:collapse;width:100%;text-align:left}th,td{padding:8px;border-bottom:1px solid #30363d}th{color:#8b949e;font-weight:500;font-size:12px}td input{width:100%;min-width:70px}td:nth-child(3){min-width:180px}.limits label{display:flex;flex-direction:column;gap:5px;font-size:12px}.limits input{max-width:180px}.team-toolbar{flex-wrap:wrap}.members{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:12px}.member p{margin:9px 0}.state{font-size:12px;color:#3fb950}.attention,.warning{color:#d29922}.warning,.error{padding:10px;border-radius:6px;background:#d2992210}.error{color:#ff7b72;background:#f8514910;white-space:pre-wrap}.compose{display:flex;flex-direction:column;gap:10px}textarea{resize:vertical;width:100%}.task-list{display:flex;flex-direction:column;gap:10px}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;margin:10px 0}details{margin-top:10px}summary{cursor:pointer;color:#58a6ff}.approval{margin-top:12px;border:1px solid #d29922;padding:12px;border-radius:6px}.approval button{margin-right:8px}.team-picker select{margin-left:8px}@media(max-width:650px){.page-heading{align-items:flex-start}.members{grid-template-columns:1fr}.team-toolbar{align-items:flex-start;flex-direction:column}.limits input{max-width:125px}}
</style>
