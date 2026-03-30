<script lang="ts">
  import { onMount } from 'svelte'
  import { listKBSolutions, listKBProblems, getKBRecommendation, getKBStats } from '$lib/api'
  import type { SolutionPattern, ProblemStats, KBRecommendation, KBStats } from '$lib/types'

  let query = $state('')
  let solutions = $state<SolutionPattern[]>([])
  let problems = $state<ProblemStats[]>([])
  let recommendation = $state<KBRecommendation | null>(null)
  let stats = $state<KBStats | null>(null)
  let loading = $state(false)
  let activeTab = $state<'solutions' | 'problems'>('solutions')
  let debounceTimer: ReturnType<typeof setTimeout> | null = null

  onMount(async () => {
    try {
      stats = await getKBStats()
      await load()
    } catch { /* ignore */ }
  })

  async function load() {
    loading = true
    try {
      const [s, p] = await Promise.all([
        listKBSolutions({ problem_class: query.trim() || undefined, limit: 30 }),
        listKBProblems({ problem_class: query.trim() || undefined, limit: 20 }),
      ])
      solutions = s
      problems = p
      if (query.trim()) {
        recommendation = await getKBRecommendation(query.trim()).catch(() => null)
      } else {
        recommendation = null
      }
    } catch { /* ignore */ }
    loading = false
  }

  function onInput() {
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(load, 400)
  }

  function formatCycleTime(ms: number): string {
    if (!ms) return '—'
    if (ms < 60000) return `${(ms / 1000).toFixed(0)}s`
    if (ms < 3600000) return `${(ms / 60000).toFixed(1)}m`
    return `${(ms / 3600000).toFixed(1)}h`
  }
</script>

<div class="kb">
  <div class="kb-header">
    <span class="kb-title">Knowledge Base</span>
    {#if stats}
      <span class="kb-stats">{stats.solution_patterns} patterns · {stats.problem_classes} problem classes</span>
    {/if}
  </div>

  <div class="kb-search-row">
    <input
      type="text"
      placeholder="Search by problem class (e.g. auth, database-migration, bug-fix)…"
      bind:value={query}
      oninput={onInput}
      class="kb-input"
    />
    {#if loading}<span class="kb-spinner"></span>{/if}
  </div>

  {#if recommendation}
    <div class="kb-recommendation">
      <div class="kb-rec-title">Recommendation</div>
      {#if recommendation.best_agent}
        <div class="kb-rec-row">
          <span class="kb-rec-label">Best agent:</span>
          <span class="kb-rec-agent">{recommendation.best_agent}</span>
          <span class="kb-rec-rate">{Math.round(recommendation.agent_success_rate * 100)}% success</span>
        </div>
      {/if}
      {#if recommendation.solution}
        <div class="kb-rec-row">
          <span class="kb-rec-label">Best pattern:</span>
          <span class="kb-rec-pattern">{recommendation.solution.solution_pattern}</span>
          <span class="kb-rec-rate">{Math.round(recommendation.solution.success_rate * 100)}% success</span>
        </div>
      {/if}
    </div>
  {/if}

  <div class="kb-tabs">
    <button class="kb-tab" class:active={activeTab === 'solutions'} onclick={() => (activeTab = 'solutions')}>
      Solutions ({solutions.length})
    </button>
    <button class="kb-tab" class:active={activeTab === 'problems'} onclick={() => (activeTab = 'problems')}>
      Problems ({problems.length})
    </button>
  </div>

  {#if activeTab === 'solutions'}
    {#if solutions.length === 0}
      <div class="kb-empty">No solution patterns found.</div>
    {:else}
      <div class="kb-list">
        {#each solutions as p}
          <div class="kb-solution-card">
            <div class="kb-card-header">
              <span class="kb-problem-class">{p.problem_class}</span>
              {#if p.repo_type}<span class="kb-repo-type">{p.repo_type}</span>{/if}
              <span class="kb-confidence">conf {Math.round(p.confidence * 100)}%</span>
            </div>
            <div class="kb-solution-text">{p.solution_pattern}</div>
            <div class="kb-card-footer">
              <div class="kb-bar-bg"><div class="kb-bar-fill" style="width: {Math.round(p.success_rate * 100)}%"></div></div>
              <span class="kb-rate">{Math.round(p.success_rate * 100)}% success</span>
              <span class="kb-occurrences">×{p.occurrence_count}</span>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  {:else}
    {#if problems.length === 0}
      <div class="kb-empty">No problem stats found.</div>
    {:else}
      <div class="kb-list">
        {#each problems as p}
          <div class="kb-problem-card">
            <div class="kb-card-header">
              <span class="kb-problem-class">{p.problem_class}</span>
              {#if p.repo_type}<span class="kb-repo-type">{p.repo_type}</span>{/if}
            </div>
            <div class="kb-problem-meta">
              {#if p.best_agent}<span class="kb-best-agent">agent: {p.best_agent}</span>{/if}
              {#if p.best_workflow}<span class="kb-best-wf">workflow: {p.best_workflow}</span>{/if}
            </div>
            <div class="kb-card-footer">
              <div class="kb-bar-bg"><div class="kb-bar-fill" style="width: {Math.round(p.success_rate * 100)}%"></div></div>
              <span class="kb-rate">{Math.round(p.success_rate * 100)}% success</span>
              <span class="kb-occurrences">×{p.occurrence_count}</span>
              <span class="kb-cycle">{formatCycleTime(p.avg_cycle_time)}</span>
            </div>
            {#if p.agents_success && Object.keys(p.agents_success).length > 0}
              <div class="kb-agents-success">
                {#each Object.entries(p.agents_success).sort((a, b) => b[1] - a[1]).slice(0, 3) as [agent, rate]}
                  <span class="kb-agent-chip">{agent} {Math.round(rate * 100)}%</span>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  {/if}
</div>

<style>
  .kb {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 4px 0;
  }

  .kb-header {
    display: flex;
    align-items: baseline;
    gap: 10px;
  }

  .kb-title {
    font-size: 12px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: #8b949e;
  }

  .kb-stats {
    font-size: 11px;
    color: #484f58;
  }

  .kb-search-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .kb-input {
    flex: 1;
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 6px;
    color: #c9d1d9;
    font-size: 13px;
    padding: 7px 10px;
    outline: none;
  }

  .kb-input:focus { border-color: #58a6ff; }
  .kb-input::placeholder { color: #484f58; }

  .kb-spinner {
    width: 14px;
    height: 14px;
    border: 2px solid #30363d;
    border-top-color: #58a6ff;
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    flex-shrink: 0;
  }

  @keyframes spin { to { transform: rotate(360deg); } }

  .kb-recommendation {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 6px;
    padding: 8px 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .kb-rec-title {
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: #a371f7;
    margin-bottom: 2px;
  }

  .kb-rec-row {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
  }

  .kb-rec-label { color: #8b949e; flex-shrink: 0; }
  .kb-rec-agent { color: #58a6ff; font-weight: 600; }
  .kb-rec-pattern { color: #c9d1d9; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .kb-rec-rate { color: #3fb950; font-size: 11px; flex-shrink: 0; }

  .kb-tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid #21262d;
    padding-bottom: 0;
  }

  .kb-tab {
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    padding: 5px 10px;
    font-size: 12px;
    color: #8b949e;
    cursor: pointer;
    margin-bottom: -1px;
  }

  .kb-tab.active { color: #c9d1d9; border-bottom-color: #58a6ff; }
  .kb-tab:hover { color: #c9d1d9; }

  .kb-empty { font-size: 12px; color: #484f58; padding: 8px 0; }

  .kb-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 400px;
    overflow-y: auto;
  }

  .kb-solution-card, .kb-problem-card {
    background: #161b22;
    border: 1px solid #21262d;
    border-radius: 6px;
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .kb-card-header {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .kb-problem-class {
    font-size: 12px;
    font-weight: 600;
    color: #58a6ff;
  }

  .kb-repo-type {
    font-size: 10px;
    padding: 1px 5px;
    background: #21262d;
    border-radius: 4px;
    color: #8b949e;
  }

  .kb-confidence {
    font-size: 10px;
    color: #484f58;
    margin-left: auto;
  }

  .kb-solution-text {
    font-size: 12px;
    color: #c9d1d9;
    line-height: 1.4;
  }

  .kb-problem-meta {
    display: flex;
    gap: 10px;
    font-size: 11px;
    color: #8b949e;
  }

  .kb-best-agent { color: #a371f7; }
  .kb-best-wf { color: #58a6ff; }

  .kb-card-footer {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .kb-bar-bg {
    width: 48px;
    height: 4px;
    background: #21262d;
    border-radius: 2px;
    overflow: hidden;
    flex-shrink: 0;
  }

  .kb-bar-fill {
    height: 100%;
    background: #3fb950;
    border-radius: 2px;
  }

  .kb-rate { font-size: 11px; color: #3fb950; font-weight: 600; }
  .kb-occurrences { font-size: 10px; color: #484f58; }
  .kb-cycle { font-size: 10px; color: #484f58; margin-left: auto; }

  .kb-agents-success {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }

  .kb-agent-chip {
    font-size: 10px;
    padding: 1px 6px;
    background: #21262d;
    border: 1px solid #30363d;
    border-radius: 10px;
    color: #8b949e;
  }
</style>
