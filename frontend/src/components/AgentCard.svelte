<script lang="ts">
  import { formatDuration, formatTokens, isAgentStale, relativeTime, workerStatusColor } from '$lib/ui'
  import AgentTranscript from './AgentTranscript.svelte'
  import type { WorkflowAgent } from '$lib/types'

  interface Props {
    agent: WorkflowAgent
    now: number
    onfail?: (id: string) => void
  }

  let { agent, now, onfail }: Props = $props()

  let stale = $derived((void now, isAgentStale(agent.status, agent.last_activity)))
  let color = $derived(workerStatusColor(stale ? 'stale' : agent.status === 'running' ? 'active' : agent.status))
  let name = $derived(agent.agent_type.replace('delivery-', '') || 'agent')
  let open = $state(false)
</script>

<div class="agent" class:failed={agent.status === 'failed'} class:stale>
  <button class="open" onclick={() => (open = !open)} title="Show what this agent did">{open ? '▾' : '▸'}</button>
  <span class="dot" class:running={agent.status === 'running' && !stale} style="background: {color}"></span>
  <span class="name">{name}</span>
  <span class="status">{stale ? 'idle' : agent.status}</span>
  {#if agent.task_index !== undefined && agent.task_index !== null}
    <span class="chip task">task {agent.task_index + 1}</span>
  {/if}
  <span class="chip">{agent.tool_calls} {agent.tool_calls === 1 ? 'call' : 'calls'}</span>
  {#if agent.usage_available === false}
    <span class="chip">Usage unavailable</span>
  {:else if agent.tokens_in + agent.tokens_out > 0}
    <span class="chip" title="input incl. cache / output tokens">{formatTokens(agent.tokens_in)} in · {formatTokens(agent.tokens_out)} out</span>
  {/if}
  {#if agent.files.length > 0}
    <span class="chip" title={agent.files.join('\n')}>{agent.files.length} files</span>
  {/if}
  {#if agent.verdict}
    <span class="chip verdict" class:pass={agent.verdict === 'PASS'} class:fail={agent.verdict === 'FAIL'}>
      {agent.verdict}{agent.must_fix > 0 ? ` · ${agent.must_fix} must_fix` : ''}{agent.should_fix > 0 ? ` · ${agent.should_fix} should_fix` : ''}
    </span>
  {/if}
  <span class="time" title="started {agent.started_at}">
    {#if agent.status === 'running'}
      {stale ? `idle ${relativeTime(agent.last_activity)}` : agent.last_tool ?? ''} · {formatDuration(agent.started_at)}
    {:else}
      {formatDuration(agent.started_at, agent.ended_at)}
    {/if}
  </span>
  {#if stale && onfail}
    <button class="fail-btn" onclick={() => onfail?.(agent.id)} title="Mark this agent as failed">Mark failed</button>
  {/if}
</div>
{#if open}
  <AgentTranscript workflowId={agent.workflow_id} agentId={agent.id} live={agent.status === 'running'} />
{/if}

<style>
  .agent {
    display: flex; align-items: center; gap: 6px; flex-wrap: wrap;
    background: #21262d; border-radius: 6px; padding: 4px 10px; font-size: 12px;
  }
  .agent.stale { border: 1px solid #d29922; }
  .agent.failed { border: 1px solid #f85149; }
  .open { background: none; border: none; color: #8b949e; cursor: pointer; padding: 0; font-size: 11px; }
  .dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
  .dot.running { animation: pulse 2s ease-in-out infinite; }
  .name { color: #c9d1d9; }
  .status { color: #8b949e; font-size: 11px; }
  .chip { font-size: 10px; color: #8b949e; background: #161b22; padding: 1px 5px; border-radius: 3px; }
  .chip.task { color: #58a6ff; background: #1f3056; }
  .chip.verdict.pass { color: #3fb950; background: #1f2d1f; }
  .chip.verdict.fail { color: #f85149; background: #3d1f1f; }
  .time { font-size: 10px; color: #8b949e; margin-left: auto; }
  .fail-btn {
    background: none; border: 1px solid #d29922; color: #d29922; border-radius: 4px;
    font-size: 10px; padding: 1px 6px; cursor: pointer;
  }
  .fail-btn:hover { background: #2d2200; }
  @keyframes pulse {
    0%, 100% { box-shadow: 0 0 0 0 rgba(63, 185, 80, 0.6); }
    50% { box-shadow: 0 0 0 4px rgba(63, 185, 80, 0); }
  }
</style>
