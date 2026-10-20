<script lang="ts">
  import { onMount } from 'svelte'
  import { appState } from '$lib/store'
  import { getAgentTranscript } from '$lib/api'
  import { formatTokens } from '$lib/ui'
  import type { AgentTranscript } from '$lib/types'

  let { workflowId, agentId, live }: { workflowId: string; agentId: string; live: boolean } = $props()

  let data = $state<AgentTranscript | null>(null)
  let error = $state('')

  async function load() {
    try {
      data = await getAgentTranscript(workflowId, agentId)
      error = ''
    } catch {
      error = 'Transcript is not available for this agent.'
    }
  }

  onMount(load)

  // Follow a running agent: re-read when new tool activity arrives.
  $effect(() => {
    const _ = appState.workflowActivityCounter
    if (live && _ > 0) load()
  })
</script>

<div class="transcript">
  {#if error}
    <div class="empty">{error}</div>
  {:else if !data}
    <div class="empty">Loading…</div>
  {:else}
    {#each data.steps as step}
      {#if step.kind === 'tool'}
        <div class="step"><span class="tool">{step.name}</span><span class="sum">{step.summary}</span></div>
      {:else}
        <div class="step text">{step.text}</div>
      {/if}
    {/each}
    {#if data.steps.length === 0}<div class="empty">No activity report available.</div>{/if}
    {#if data.usage_available === false || !data.usage}
      <div class="usage">Usage unavailable</div>
    {:else}
    <div class="usage">
      {formatTokens(data.usage.input_tokens + data.usage.cache_read_tokens + data.usage.cache_creation_tokens)} in ·
      {formatTokens(data.usage.output_tokens)} out
    </div>
    {/if}
  {/if}
</div>

<style>
  .transcript { display: flex; flex-direction: column; gap: 3px; max-height: 260px; overflow-y: auto; padding: 6px 8px; background: #0d1117; border-radius: 4px; }
  .step { display: flex; gap: 8px; font-size: 11px; color: #8b949e; align-items: baseline; }
  .step.text { color: #c9d1d9; font-style: italic; }
  .tool { color: #58a6ff; flex-shrink: 0; min-width: 52px; }
  .sum { font-family: monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .usage { margin-top: 4px; font-size: 10px; color: #484f58; }
  .empty { font-size: 11px; color: #484f58; }
</style>
