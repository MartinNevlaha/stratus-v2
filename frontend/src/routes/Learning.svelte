<script lang="ts">
  import { listProposals, decideProposal, listCandidates, saveProposal } from '$lib/api'
  import type { Proposal, Candidate } from '$lib/types'
  import { onMount } from 'svelte'

  let proposals = $state<Proposal[]>([])
  let candidates = $state<Candidate[]>([])
  let tab = $state<'proposals' | 'candidates'>('proposals')
  let loading = $state(true)
  let decideError = $state<string | null>(null)
  let decideWarn = $state<string | null>(null)

  onMount(async () => {
    await refresh()
  })

  async function refresh() {
    loading = true
    try {
      const [p, c] = await Promise.all([listProposals('pending'), listCandidates()])
      proposals = p.proposals
      candidates = c.candidates
    } finally {
      loading = false
    }
  }

  async function decide(id: string, decision: string) {
    decideError = null
    decideWarn = null
    try {
      const res = await decideProposal(id, decision)
      proposals = proposals.filter(p => p.id !== id)
      if (decision === 'accept' && !res.applied) {
        decideWarn = 'Accepted, but no file was written — proposal was missing proposed_path or proposed_content.'
      }
    } catch (e) {
      decideError = e instanceof Error ? e.message : 'Failed to save decision'
    }
  }

  async function promote(c: Candidate) {
    decideError = null
    try {
      await saveProposal({
        candidate_id: c.id,
        type: c.detection_type,
        title: c.description.slice(0, 120),
        description: c.description,
        proposed_content: '',
        confidence: c.confidence,
      })
      await refresh()
      tab = 'proposals'
    } catch (e) {
      decideError = e instanceof Error ? e.message : 'Failed to promote candidate'
    }
  }

  const confidenceColor = (c: number) =>
    c > 0.7 ? '#3fb950' : c > 0.4 ? '#d29922' : '#f85149'
</script>

<div class="learning">
  <div class="tabs">
    <button class:active={tab === 'proposals'} onclick={() => (tab = 'proposals')}>
      Proposals ({proposals.length})
    </button>
    <button class:active={tab === 'candidates'} onclick={() => (tab = 'candidates')}>
      Candidates ({candidates.length})
    </button>
    <button class="refresh" onclick={refresh}>↺</button>
  </div>

  {#if decideError}
    <div class="error">{decideError}</div>
  {/if}
  {#if decideWarn}
    <div class="warn">{decideWarn}</div>
  {/if}

  {#if loading}
    <div class="empty">Loading…</div>
  {:else if tab === 'proposals'}
    {#if proposals.length === 0}
      <div class="empty">No pending proposals</div>
    {/if}
    {#each proposals as p}
      <div class="card">
        <div class="card-header">
          <span class="type-badge">{p.type}</span>
          <span class="title">{p.title}</span>
          <span class="confidence" style="color: {confidenceColor(p.confidence)}">
            {(p.confidence * 100).toFixed(0)}%
          </span>
        </div>
        <p class="description">{p.description}</p>
        {#if p.proposed_path}
          <code class="path">{p.proposed_path}</code>
        {/if}
        {#if p.proposed_content}
          <pre class="content">{p.proposed_content.slice(0, 300)}{p.proposed_content.length > 300 ? '…' : ''}</pre>
        {/if}
        <div class="actions">
          <button class="accept" onclick={() => decide(p.id, 'accept')}>Accept</button>
          <button class="reject" onclick={() => decide(p.id, 'reject')}>Reject</button>
          <button class="snooze" onclick={() => decide(p.id, 'snooze')}>Snooze</button>
          <button class="ignore" onclick={() => decide(p.id, 'ignore')}>Ignore</button>
        </div>
      </div>
    {/each}
  {:else}
    {#if candidates.length === 0}
      <div class="empty">No pattern candidates detected</div>
    {/if}
    {#each candidates as c}
      <div class="card">
        <div class="card-header">
          <span class="type-badge">{c.detection_type}</span>
          <span class="title">{c.description.slice(0, 80)}</span>
          <span class="count">×{c.count}</span>
          <span class="confidence" style="color: {confidenceColor(c.confidence)}">
            {(c.confidence * 100).toFixed(0)}%
          </span>
          <span class="status-badge">{c.status}</span>
        </div>
        {#if c.files.length > 0}
          <div class="files">
            {#each c.files.slice(0, 3) as f}
              <code>{f}</code>
            {/each}
            {#if c.files.length > 3}
              <span>+{c.files.length - 3} more</span>
            {/if}
          </div>
        {/if}
        {#if c.status === 'pending'}
          <div class="actions">
            <button class="accept" onclick={() => promote(c)}>Promote to Proposal</button>
          </div>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .learning { display: flex; flex-direction: column; gap: 12px; }

  .tabs { display: flex; gap: 4px; border-bottom: 1px solid #30363d; padding-bottom: 8px; }
  .tabs button { padding: 6px 12px; background: transparent; border: 1px solid transparent; border-radius: 6px; color: #8b949e; cursor: pointer; }
  .tabs button.active { border-color: #30363d; color: #c9d1d9; background: #21262d; }
  .tabs button:hover:not(.active) { color: #c9d1d9; }
  .refresh { margin-left: auto; }

  .error { color: #f85149; font-size: 13px; padding: 8px 12px; background: #2d1117; border-radius: 4px; }
  .warn  { color: #d29922; font-size: 13px; padding: 8px 12px; background: #272115; border-radius: 4px; }
  .empty { color: #8b949e; text-align: center; padding: 32px; }

  .card { background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 14px; display: flex; flex-direction: column; gap: 8px; }
  .card-header { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .type-badge { font-size: 11px; background: #2d1f3d; color: #bc8cff; padding: 2px 6px; border-radius: 4px; }
  .title { flex: 1; font-weight: 600; color: #c9d1d9; font-size: 14px; }
  .confidence { font-size: 12px; font-weight: 700; }
  .count { font-size: 12px; color: #8b949e; }
  .status-badge { font-size: 11px; background: #21262d; color: #8b949e; padding: 1px 6px; border-radius: 4px; }

  .description { font-size: 13px; color: #8b949e; margin: 0; line-height: 1.5; }
  .path { font-size: 12px; color: #8b949e; background: #0d1117; padding: 3px 6px; border-radius: 4px; }
  pre.content { font-size: 12px; color: #8b949e; background: #0d1117; padding: 8px; border-radius: 4px; margin: 0; white-space: pre-wrap; max-height: 150px; overflow-y: auto; }

  .files { display: flex; gap: 6px; flex-wrap: wrap; }
  .files code { font-size: 11px; background: #0d1117; color: #58a6ff; padding: 2px 6px; border-radius: 4px; }
  .files span { font-size: 11px; color: #8b949e; }

  .actions { display: flex; gap: 6px; flex-wrap: wrap; }
  .actions button { padding: 4px 12px; border: none; border-radius: 4px; cursor: pointer; font-size: 13px; }
  .accept { background: #238636; color: white; }
  .accept:hover { background: #2ea043; }
  .reject { background: #da3633; color: white; }
  .reject:hover { background: #f85149; }
  .snooze, .ignore { background: #21262d; border: 1px solid #30363d; color: #8b949e; }
  .snooze:hover, .ignore:hover { color: #c9d1d9; }
</style>
