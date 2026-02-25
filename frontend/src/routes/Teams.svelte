<script lang="ts">
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { listWorkflows } from '$lib/api'
  import PhaseTimeline from '../components/PhaseTimeline.svelte'
  import type { WorkflowState } from '$lib/types'

  let allWorkflows = $state<WorkflowState[]>([])
  let copiedId = $state<string | null>(null)

  let teamWfs = $derived(allWorkflows.filter(w => w.title?.startsWith('[TEAM]')))
  let activeTeamWfs = $derived(teamWfs.filter(w => !w.aborted && w.phase !== 'complete'))
  let pastTeamWfs = $derived(teamWfs.filter(w => w.aborted || w.phase === 'complete'))

  async function load() {
    try {
      allWorkflows = await listWorkflows()
    } catch { /* ignore */ }
  }

  function copyToClipboard(text: string, key: string) {
    navigator.clipboard.writeText(text)
    copiedId = key
    setTimeout(() => { if (copiedId === key) copiedId = null }, 2000)
  }

  onMount(load)

  $effect(() => {
    const _ = appState.dashboard
    load()
  })
</script>

<div class="teams">
  <div class="header-row">
    <h2>Agent Teams</h2>
    <span class="experimental-badge">experimental</span>
  </div>

  <div class="info-banner">
    <strong>/team</strong> runs delivery agents in parallel — all domains at once instead of one by one.
    Requires <code>CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1</code> (set automatically by <code>stratus init</code>).
  </div>

  {#if activeTeamWfs.length === 0 && pastTeamWfs.length === 0}
    <div class="empty">
      No team workflows yet. Use <code>/team &lt;feature description&gt;</code> to start one.
    </div>
  {/if}

  {#if activeTeamWfs.length > 0}
    <div class="section-title">Active</div>
    {#each activeTeamWfs as wf}
      <div class="workflow-card">
        <div class="workflow-header">
          <span class="wf-id">{wf.id}</span>
          <span class="wf-phase">{wf.phase}</span>
        </div>
        {#if wf.title}
          <div class="wf-title">{wf.title.replace(/^\[TEAM\]\s*/, '')}</div>
        {/if}
        <PhaseTimeline type={wf.type} complexity={wf.complexity} currentPhase={wf.phase} />

        <div class="resume-row">
          <button
            class="btn-resume skill"
            onclick={() => copyToClipboard(`/resume ${wf.id}`, `skill-${wf.id}`)}
            title="Copy /resume command to clipboard"
          >
            {copiedId === `skill-${wf.id}` ? 'Copied!' : `/resume ${wf.id}`}
          </button>
          {#if wf.session_id}
            <button
              class="btn-resume session"
              onclick={() => copyToClipboard(`claude --resume ${wf.session_id}`, `sess-${wf.id}`)}
              title="Copy claude --resume command to clipboard"
            >
              {copiedId === `sess-${wf.id}` ? 'Copied!' : 'claude --resume'}
            </button>
          {/if}
        </div>

        {#if wf.tasks.length > 0}
          <div class="tasks">
            <div class="tasks-header">
              Tasks ({wf.tasks.filter(t => t.status === 'done').length}/{wf.total_tasks})
              <div class="progress-bar">
                <div
                  class="progress-fill"
                  style="width: {wf.total_tasks > 0 ? (wf.tasks.filter(t => t.status === 'done').length / wf.total_tasks) * 100 : 0}%"
                ></div>
              </div>
            </div>
            {#each wf.tasks as task}
              <div class="task" class:done={task.status === 'done'} class:active={task.status === 'in_progress'}>
                <span class="task-status">
                  {task.status === 'done' ? '✓' : task.status === 'in_progress' ? '▶' : '○'}
                </span>
                {task.title}
              </div>
            {/each}
          </div>
        {/if}

        {#if Object.keys(wf.delegated_agents).length > 0}
          <div class="delegated">
            {#each Object.entries(wf.delegated_agents) as [phase, agents]}
              {#if agents.length > 0}
                <div class="phase-agents">
                  <span class="phase-label">{phase}:</span>
                  {#each agents as agent}
                    <span class="agent-chip">{agent}</span>
                  {/each}
                </div>
              {/if}
            {/each}
          </div>
        {/if}
      </div>
    {/each}
  {/if}

  {#if pastTeamWfs.length > 0}
    <div class="section-title">Past</div>
    {#each pastTeamWfs as wf}
      <div class="workflow-card past">
        <div class="workflow-header">
          <span class="wf-id">{wf.id}</span>
          <span class="wf-phase" class:aborted={wf.aborted}>{wf.aborted ? 'aborted' : wf.phase}</span>
          <span class="wf-ts">{new Date(wf.updated_at).toLocaleDateString()}</span>
        </div>
        {#if wf.title}
          <div class="wf-title">{wf.title.replace(/^\[TEAM\]\s*/, '')}</div>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .teams { display: flex; flex-direction: column; gap: 16px; }

  .header-row { display: flex; align-items: center; gap: 10px; }
  h2 { font-size: 16px; font-weight: 600; color: #c9d1d9; }
  .experimental-badge {
    font-size: 11px; background: #2d1b69; color: #a371f7;
    border: 1px solid #6e40c9; border-radius: 12px; padding: 2px 8px;
  }

  .info-banner {
    background: #161b22; border: 1px solid #30363d; border-radius: 6px;
    padding: 10px 14px; font-size: 13px; color: #8b949e; line-height: 1.5;
  }
  .info-banner strong { color: #3fb950; }
  .info-banner code { background: #21262d; padding: 1px 4px; border-radius: 4px; font-size: 12px; }

  .empty { color: #8b949e; font-size: 14px; padding: 24px; text-align: center; background: #161b22; border-radius: 6px; }
  .empty code { background: #21262d; padding: 1px 4px; border-radius: 4px; }

  .section-title { font-size: 13px; font-weight: 600; color: #8b949e; text-transform: uppercase; letter-spacing: 0.05em; }

  .workflow-card {
    background: #161b22; border: 1px solid #30363d; border-radius: 8px;
    padding: 16px; display: flex; flex-direction: column; gap: 12px;
  }
  .workflow-card.past { opacity: 0.6; }
  .workflow-card.past:hover { opacity: 1; }

  .workflow-header { display: flex; align-items: center; gap: 8px; }
  .wf-id { font-size: 13px; font-weight: 600; color: #c9d1d9; flex: 1; }
  .wf-phase { font-size: 12px; color: #8b949e; }
  .wf-phase.aborted { color: #f85149; }
  .wf-ts { font-size: 11px; color: #8b949e; }
  .wf-title { font-size: 14px; color: #c9d1d9; }

  .resume-row { display: flex; gap: 6px; align-items: center; }
  .btn-resume { font-size: 11px; padding: 3px 10px; border-radius: 4px; cursor: pointer; border: 1px solid #30363d; font-family: monospace; white-space: nowrap; transition: background 0.1s; }
  .btn-resume.skill { background: #1f2d1f; color: #3fb950; border-color: #238636; }
  .btn-resume.skill:hover { background: #2d3f2d; }
  .btn-resume.session { background: #1a2433; color: #58a6ff; border-color: #1f6feb; }
  .btn-resume.session:hover { background: #1f3056; }

  .tasks { display: flex; flex-direction: column; gap: 4px; }
  .tasks-header { font-size: 12px; color: #8b949e; display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
  .progress-bar { flex: 1; height: 4px; background: #30363d; border-radius: 2px; }
  .progress-fill { height: 100%; background: #a371f7; border-radius: 2px; transition: width 0.3s; }

  .task { display: flex; gap: 8px; align-items: center; font-size: 13px; color: #8b949e; padding: 2px 0; }
  .task.done { color: #3fb950; }
  .task.active { color: #a371f7; }
  .task-status { width: 16px; text-align: center; }

  .delegated { display: flex; flex-direction: column; gap: 4px; }
  .phase-agents { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .phase-label { font-size: 12px; color: #8b949e; }
  .agent-chip { font-size: 11px; background: #21262d; color: #c9d1d9; padding: 2px 8px; border-radius: 12px; }
</style>
