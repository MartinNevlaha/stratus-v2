<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { WorkflowState } from '$lib/types'

  interface Props {
    wf: WorkflowState
    onDelete: (id: string) => void
    past?: boolean
    children?: Snippet
  }

  let { wf, onDelete, past = false, children }: Props = $props()

  let confirmDelete = $state<string | null>(null)
  let confirmTimer: ReturnType<typeof setTimeout> | null = null

  function displayType(wf: WorkflowState): string {
    if (wf.title?.startsWith('[SWARM]')) return 'swarm'
    if (wf.type === 'spec' && wf.complexity === 'complex') return 'spec-complex'
    return wf.type
  }

  function handleDelete(id: string) {
    if (confirmDelete !== id) {
      if (confirmTimer) clearTimeout(confirmTimer)
      confirmDelete = id
      confirmTimer = setTimeout(() => {
        if (confirmDelete === id) confirmDelete = null
        confirmTimer = null
      }, 10_000)
      return
    }
    if (confirmTimer) { clearTimeout(confirmTimer); confirmTimer = null }
    confirmDelete = null
    onDelete(id)
  }

  function cancelDelete() {
    if (confirmTimer) { clearTimeout(confirmTimer); confirmTimer = null }
    confirmDelete = null
  }
</script>
<div class="workflow-card" class:past={past}>
  <div class="workflow-header">
    <span class="wf-type" class:swarm={displayType(wf) === 'swarm'} class:bug={wf.type === 'bug'} class:e2e={wf.type === 'e2e'}>{displayType(wf)}</span>
    <span class="wf-id">{wf.id}</span>
    <span class="wf-phase" class:aborted={wf.aborted}>{wf.aborted ? 'aborted' : wf.phase}</span>
    {#if past}
      <span class="wf-ts">{new Date(wf.updated_at).toLocaleDateString()}</span>
    {/if}
    <div class="wf-actions">
      {#if confirmDelete === wf.id}
        <span class="confirm-label">Delete?</span>
        <button class="btn-confirm" onclick={() => handleDelete(wf.id)}>Yes</button>
        <button class="btn-cancel" onclick={cancelDelete}>No</button>
      {:else}
        <button class="btn-delete" onclick={() => handleDelete(wf.id)} title="Delete workflow">✕</button>
      {/if}
    </div>
  </div>
  {#if wf.title}
    <div class="wf-title">{wf.title}</div>
  {/if}
  {#if wf.change_summary}
    <div class="change-summary">
      <div class="cs-stats">
        <span class="cs-stat">{wf.change_summary.files_changed} files</span>
        <span class="cs-stat added">+{wf.change_summary.lines_added}</span>
        <span class="cs-stat removed">-{wf.change_summary.lines_removed}</span>
        {#if wf.change_summary.test_coverage_delta}
          <span class="cs-stat">{wf.change_summary.test_coverage_delta}</span>
        {/if}
      </div>
      {#if wf.change_summary.capabilities_added.length > 0}
        <div class="cs-section">
          <span class="cs-label">Added</span>
          {#each wf.change_summary.capabilities_added as cap}
            <span class="cs-item">{cap}</span>
          {/each}
        </div>
      {/if}
      {#if wf.change_summary.capabilities_modified.length > 0}
        <div class="cs-section">
          <span class="cs-label">Modified</span>
          {#each wf.change_summary.capabilities_modified as cap}
            <span class="cs-item">{cap}</span>
          {/each}
        </div>
      {/if}
      {#if wf.change_summary.downstream_risks.length > 0}
        <div class="cs-section">
          <span class="cs-label cs-risk">Risks</span>
          {#each wf.change_summary.downstream_risks as risk}
            <span class="cs-item risk">{risk}</span>
          {/each}
        </div>
      {/if}
      {#if wf.change_summary.governance_compliance.length > 0}
        <div class="cs-section">
          <span class="cs-label">Governance</span>
          {#each wf.change_summary.governance_compliance as g}
            <span class="cs-tag">{g}</span>
          {/each}
        </div>
      {/if}
    </div>
  {:else if wf.phase === 'complete' && wf.base_commit}
    <div class="cs-pending">Analyzing changes…</div>
  {/if}
  {#if children}
    {@render children()}
  {/if}
</div>

<style>
  .workflow-card { background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 16px; display: flex; flex-direction: column; gap: 12px; }
  .workflow-card.past { opacity: 0.6; }
  .workflow-card.past:hover { opacity: 1; }
  .workflow-header { display: flex; align-items: center; gap: 8px; }
  .wf-type { font-size: 11px; text-transform: uppercase; font-weight: 700; color: #58a6ff; background: #1f3056; padding: 2px 6px; border-radius: 4px; white-space: nowrap; }
  .wf-type.swarm { color: #a371f7; background: #2d1f56; }
  .wf-type.bug { color: #f0883e; background: #3d2200; }
  .wf-type.e2e { color: #3fb950; background: #0d3226; }
  .wf-id { font-size: 13px; font-weight: 600; color: #c9d1d9; flex: 1; }
  .wf-phase { font-size: 12px; color: #8b949e; }
  .wf-phase.aborted { color: #f85149; }
  .wf-ts { font-size: 11px; color: #8b949e; }
  .wf-title { font-size: 14px; color: #c9d1d9; }
  .wf-actions { display: flex; align-items: center; gap: 4px; margin-left: auto; }
  .btn-delete { background: none; border: none; color: #8b949e; cursor: pointer; font-size: 12px; padding: 2px 6px; border-radius: 4px; line-height: 1; }
  .btn-delete:hover { color: #f85149; background: #2d1117; }
  .confirm-label { font-size: 12px; color: #f85149; }
  .btn-confirm { background: #f85149; border: none; color: #fff; cursor: pointer; font-size: 11px; padding: 2px 8px; border-radius: 4px; }
  .btn-confirm:hover { background: #da3633; }
  .btn-cancel { background: #21262d; border: none; color: #c9d1d9; cursor: pointer; font-size: 11px; padding: 2px 8px; border-radius: 4px; }
  .btn-cancel:hover { background: #30363d; }

  .change-summary { display: flex; flex-direction: column; gap: 6px; }
  .cs-pending { font-size: 12px; color: #8b949e; font-style: italic; }
  .cs-stats { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .cs-stat { font-size: 12px; color: #8b949e; background: #21262d; padding: 2px 8px; border-radius: 4px; }
  .cs-stat.added { color: #3fb950; }
  .cs-stat.removed { color: #f85149; }
  .cs-section { display: flex; align-items: flex-start; gap: 6px; flex-wrap: wrap; font-size: 12px; }
  .cs-label { color: #8b949e; font-weight: 600; min-width: 56px; padding-top: 2px; }
  .cs-label.cs-risk { color: #d29922; }
  .cs-item { color: #c9d1d9; background: #21262d; padding: 1px 8px; border-radius: 4px; }
  .cs-item.risk { color: #d29922; background: #2d2200; }
  .cs-tag { font-size: 11px; background: #1f2d1f; color: #3fb950; border: 1px solid #238636; padding: 1px 8px; border-radius: 12px; }
</style>
