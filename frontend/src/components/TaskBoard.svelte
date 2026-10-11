<script lang="ts">
  import { formatDuration, toggleSet } from '$lib/ui'
  import type { Task, WorkflowAgent } from '$lib/types'

  interface Props {
    tasks: Task[]
    agents: WorkflowAgent[]
    now: number
    limit?: number
    onretry?: (index: number) => void
    onskip?: (index: number) => void
    label?: string
  }

  let { tasks, agents, now, limit = 5, onretry, onskip, label = 'Tasks' }: Props = $props()

  let showAll = $state(false)
  let expanded = $state<Set<string>>(new Set())

  // Skipped tasks no longer block the workflow, so they count as finished.
  let done = $derived(tasks.filter(t => t.status === 'done' || t.status === 'skipped').length)
  let visible = $derived(showAll ? tasks : tasks.slice(0, limit))

  function agentsFor(task: Task): WorkflowAgent[] {
    return agents.filter(a => a.task_index === task.index)
  }

  function filesFor(task: Task): string[] {
    return [...new Set(agentsFor(task).flatMap(a => a.files))]
  }

  function duration(task: Task): string {
    void now
    if (!task.started_at) return ''
    return formatDuration(task.started_at, task.status === 'done' ? task.completed_at : undefined)
  }
</script>

<div class="tasks">
  <div class="tasks-header">
    {label} ({done}/{tasks.length})
    <div class="progress-bar">
      <div class="progress-fill" class:active={tasks.some(t => t.status === 'in_progress')}
        style="width: {tasks.length > 0 ? (done / tasks.length) * 100 : 0}%"></div>
    </div>
  </div>
  {#each visible as task (task.index)}
    {@const taskAgents = agentsFor(task)}
    {@const files = filesFor(task)}
    <div class="task" class:done={task.status === 'done'} class:active={task.status === 'in_progress'} class:skipped={task.status === 'skipped'}
      role="button" tabindex="0"
      onclick={() => { if (files.length) expanded = toggleSet(expanded, String(task.index)) }}
      onkeydown={(e) => { if ((e.key === 'Enter' || e.key === ' ') && files.length) expanded = toggleSet(expanded, String(task.index)) }}
    >
      <span class="task-status" class:spinning={task.status === 'in_progress'}>
        {task.status === 'done' ? '✓' : task.status === 'in_progress' ? '▶' : task.status === 'skipped' ? '⤼' : '○'}
      </span>
      <span class="task-title">{task.title}</span>
      {#each taskAgents as a (a.id)}
        <span class="chip agent" class:running={a.status === 'running'}>{a.agent_type.replace('delivery-', '')}</span>
      {/each}
      {#if files.length > 0}
        <span class="chip">{files.length} files {expanded.has(String(task.index)) ? '▲' : '▼'}</span>
      {/if}
      {#if duration(task)}<span class="dur">{duration(task)}</span>{/if}
      {#if onskip && (task.status === 'pending' || task.status === 'in_progress')}
        <button class="retry" title="Skip this task" onclick={(e) => { e.stopPropagation(); onskip?.(task.index) }}>Skip</button>
      {/if}
      {#if onretry && task.status !== 'pending'}
        <button class="retry" title="Reopen this task" onclick={(e) => { e.stopPropagation(); onretry?.(task.index) }}>Retry</button>
      {/if}
    </div>
    {#if expanded.has(String(task.index))}
      <ul class="files">
        {#each files as f}<li>{f}</li>{/each}
      </ul>
    {/if}
  {/each}
  {#if tasks.length > limit}
    <button class="task-toggle" onclick={() => (showAll = !showAll)}>
      {showAll ? 'Show less' : `+${tasks.length - limit} more`}
    </button>
  {/if}
</div>

<style>
  .tasks { display: flex; flex-direction: column; gap: 4px; }
  .tasks-header { font-size: 12px; color: #8b949e; display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
  .progress-bar { flex: 1; height: 4px; background: #30363d; border-radius: 2px; }
  .progress-fill { height: 100%; background: #58a6ff; border-radius: 2px; transition: width 0.3s; }
  .task { display: flex; gap: 8px; align-items: center; font-size: 13px; color: #8b949e; padding: 2px 0; }
  .task[role='button'] { cursor: default; }
  .task.done { color: #3fb950; }
  .task.active { color: #58a6ff; }
  .task.skipped { color: #6e7681; text-decoration: line-through; }
  .task-status { width: 16px; text-align: center; flex-shrink: 0; }
  .task-status.spinning { display: inline-block; animation: pulse-op 1.5s ease-in-out infinite; }
  .task-title { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .chip { font-size: 10px; background: #21262d; color: #8b949e; padding: 1px 6px; border-radius: 4px; flex-shrink: 0; }
  .chip.agent { background: #2d1f56; color: #a371f7; }
  .chip.agent.running { box-shadow: 0 0 0 1px #a371f7; }
  .dur { font-size: 10px; color: #484f58; flex-shrink: 0; }
  .retry { background: none; border: 1px solid #30363d; color: #8b949e; border-radius: 4px; font-size: 10px; padding: 0 6px; cursor: pointer; flex-shrink: 0; }
  .retry:hover { color: #c9d1d9; border-color: #484f58; }
  .files { margin: 0 0 4px 24px; padding: 0; list-style: none; font-family: monospace; font-size: 11px; color: #8b949e; }
  .task-toggle {
    align-self: flex-start; background: none; border: none; color: #58a6ff;
    font-size: 12px; cursor: pointer; padding: 0;
  }
  .task-toggle:hover { color: #79c0ff; text-decoration: underline; }
  @keyframes pulse-op { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
</style>
