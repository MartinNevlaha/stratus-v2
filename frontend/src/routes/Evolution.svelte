<script lang="ts">
  import { listEvolutionRuns, getEvolutionRun, triggerEvolution, getEvolutionConfig, updateEvolutionConfig } from '$lib/api'
  import type { EvolutionRun, EvolutionHypothesis, EvolutionConfig } from '$lib/types'

  let runs = $state<EvolutionRun[]>([])
  let totalCount = $state(0)
  let selectedRun = $state<EvolutionRun | null>(null)
  let hypotheses = $state<EvolutionHypothesis[]>([])
  let config = $state<EvolutionConfig | null>(null)
  let triggering = $state(false)
  let activeView = $state<'runs' | 'config'>('runs')
  let loading = $state(true)
  let error = $state<string | null>(null)
  let configSaving = $state(false)
  let configError = $state<string | null>(null)
  let configSuccess = $state(false)
  let timeoutInput = $state('')
  let statusFilter = $state('')
  let loadingRun = $state(false)
  let triggerSuccess = $state<string | null>(null)

  $effect(() => {
    loadRuns()
  })

  async function loadRuns() {
    loading = true
    error = null
    try {
      const params: { status?: string; limit?: number } = { limit: 50 }
      if (statusFilter) params.status = statusFilter
      const data = await listEvolutionRuns(params)
      runs = data.runs ?? []
      totalCount = data.count ?? 0
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load evolution runs'
    } finally {
      loading = false
    }
  }

  async function loadConfig() {
    configError = null
    try {
      config = await getEvolutionConfig()
    } catch (e) {
      configError = e instanceof Error ? e.message : 'Failed to load config'
    }
  }

  async function selectRun(run: EvolutionRun) {
    if (selectedRun?.id === run.id) {
      selectedRun = null
      hypotheses = []
      return
    }
    loadingRun = true
    try {
      const data = await getEvolutionRun(run.id)
      selectedRun = data.run
      hypotheses = data.hypotheses ?? []
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load run detail'
    } finally {
      loadingRun = false
    }
  }

  async function handleTrigger() {
    triggering = true
    error = null
    triggerSuccess = null
    try {
      const ms = timeoutInput ? parseInt(timeoutInput, 10) : undefined
      const result = await triggerEvolution(ms)
      triggerSuccess = result.message ?? 'Evolution cycle triggered'
      timeoutInput = ''
      await loadRuns()
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to trigger evolution'
    } finally {
      triggering = false
    }
  }

  async function handleSwitchToConfig() {
    activeView = 'config'
    if (!config) {
      await loadConfig()
    }
  }

  async function saveConfig() {
    if (!config) return
    configSaving = true
    configError = null
    configSuccess = false
    try {
      config = await updateEvolutionConfig(config)
      configSuccess = true
      setTimeout(() => { configSuccess = false }, 3000)
    } catch (e) {
      configError = e instanceof Error ? e.message : 'Failed to save config'
    } finally {
      configSaving = false
    }
  }

  function formatDate(dateStr: string | null) {
    if (!dateStr) return '—'
    return new Date(dateStr).toLocaleString()
  }

  function formatDuration(ms: number) {
    if (!ms || ms === 0) return '—'
    if (ms < 1000) return ms + 'ms'
    if (ms < 60000) return (ms / 1000).toFixed(1) + 's'
    return (ms / 60000).toFixed(1) + 'm'
  }

  function getStatusColor(status: string): string {
    switch (status) {
      case 'running': return '#388bfd'
      case 'completed': return '#3fb950'
      case 'failed': return '#f85149'
      case 'timeout': return '#d29922'
      default: return '#8b949e'
    }
  }

  function getDecisionColor(decision: string): string {
    switch (decision) {
      case 'auto_applied': return '#3fb950'
      case 'proposal_created': return '#388bfd'
      case 'rejected': return '#8b949e'
      case 'inconclusive': return '#d29922'
      default: return '#8b949e'
    }
  }

  function getCategoryLabel(cat: string): string {
    switch (cat) {
      case 'prompt_tuning': return 'Prompt Tuning'
      case 'workflow_routing': return 'Workflow Routing'
      case 'agent_selection': return 'Agent Selection'
      case 'threshold_adjustment': return 'Threshold Adjustment'
      default: return cat
    }
  }

  function getConfidenceColor(confidence: number): string {
    if (confidence >= 0.8) return '#3fb950'
    if (confidence >= 0.6) return '#d29922'
    return '#f85149'
  }

  function metricDelta(h: EvolutionHypothesis): string {
    const delta = h.experiment_metric - h.baseline_metric
    const sign = delta >= 0 ? '+' : ''
    return `${sign}${delta.toFixed(3)}`
  }

  function metricDeltaColor(h: EvolutionHypothesis): string {
    const delta = h.experiment_metric - h.baseline_metric
    return delta > 0 ? '#3fb950' : delta < 0 ? '#f85149' : '#8b949e'
  }
</script>

<div class="evolution">
  <header>
    <div class="header-left">
      <h1>Evolution</h1>
      <p class="subtitle">Self-improvement cycles — hypothesis testing &amp; auto-application</p>
    </div>
    <div class="controls">
      <button
        class="tab-btn"
        class:active={activeView === 'runs'}
        onclick={() => { activeView = 'runs' }}
      >
        Runs
      </button>
      <button
        class="tab-btn"
        class:active={activeView === 'config'}
        onclick={handleSwitchToConfig}
      >
        Config
      </button>

      {#if activeView === 'runs'}
        <select bind:value={statusFilter} onchange={loadRuns} class="filter-select">
          <option value="">All statuses</option>
          <option value="running">Running</option>
          <option value="completed">Completed</option>
          <option value="failed">Failed</option>
          <option value="timeout">Timeout</option>
        </select>
        <button class="refresh-btn" onclick={loadRuns} disabled={loading}>
          {loading ? 'Loading...' : '↻ Refresh'}
        </button>
      {/if}
    </div>
  </header>

  {#if error}
    <div class="error-banner">
      <span>⚠ {error}</span>
      <button onclick={() => { error = null }}>✕</button>
    </div>
  {/if}

  {#if triggerSuccess}
    <div class="success-banner">
      <span>✓ {triggerSuccess}</span>
      <button onclick={() => { triggerSuccess = null }}>✕</button>
    </div>
  {/if}

  {#if activeView === 'runs'}
    <!-- Trigger panel -->
    <section class="trigger-section">
      <div class="trigger-row">
        <input
          class="timeout-input"
          type="number"
          bind:value={timeoutInput}
          placeholder="Timeout (ms) — optional"
          min="1000"
        />
        <button
          class="trigger-btn"
          onclick={handleTrigger}
          disabled={triggering}
        >
          {triggering ? 'Triggering…' : '▶ Run Evolution'}
        </button>
      </div>
    </section>

    <!-- Runs table -->
    <section class="runs-section">
      {#if loading}
        <div class="loading">Loading evolution runs...</div>
      {:else if runs.length === 0}
        <div class="empty">No evolution runs found. Trigger one above to get started.</div>
      {:else}
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Status</th>
                <th>Trigger</th>
                <th>Hypotheses</th>
                <th>Auto-Applied</th>
                <th>Proposals</th>
                <th>Duration</th>
                <th>Started</th>
              </tr>
            </thead>
            <tbody>
              {#each runs as run}
                <tr
                  class="run-row"
                  class:selected={selectedRun?.id === run.id}
                  onclick={() => selectRun(run)}
                  role="button"
                  tabindex="0"
                  onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') selectRun(run) }}
                >
                  <td>
                    <span class="status-badge" style="color: {getStatusColor(run.status)}; border-color: {getStatusColor(run.status)};">
                      {run.status}
                    </span>
                  </td>
                  <td class="muted">{run.trigger_type}</td>
                  <td>{run.hypotheses_count}</td>
                  <td>{run.auto_applied}</td>
                  <td>{run.proposals_created}</td>
                  <td class="muted">{formatDuration(run.duration_ms)}</td>
                  <td class="muted">{formatDate(run.started_at)}</td>
                </tr>

                {#if selectedRun?.id === run.id}
                  <tr class="detail-row">
                    <td colspan="7">
                      {#if loadingRun}
                        <div class="loading-inline">Loading hypotheses…</div>
                      {:else}
                        <div class="run-detail">
                          <div class="run-meta">
                            <div class="meta-pill">
                              <span class="label">Experiments</span>
                              <span class="value">{selectedRun.experiments_run}</span>
                            </div>
                            <div class="meta-pill">
                              <span class="label">Wiki pages updated</span>
                              <span class="value">{selectedRun.wiki_pages_updated}</span>
                            </div>
                            <div class="meta-pill">
                              <span class="label">Completed</span>
                              <span class="value">{formatDate(selectedRun.completed_at)}</span>
                            </div>
                            {#if selectedRun.error_message}
                              <div class="meta-pill error">
                                <span class="label">Error</span>
                                <span class="value">{selectedRun.error_message}</span>
                              </div>
                            {/if}
                          </div>

                          {#if hypotheses.length === 0}
                            <p class="empty-sub">No hypotheses recorded for this run.</p>
                          {:else}
                            <table class="hyp-table">
                              <thead>
                                <tr>
                                  <th>Category</th>
                                  <th>Description</th>
                                  <th>Metric</th>
                                  <th>Baseline</th>
                                  <th>Experiment</th>
                                  <th>Delta</th>
                                  <th>Confidence</th>
                                  <th>Decision</th>
                                </tr>
                              </thead>
                              <tbody>
                                {#each hypotheses as h}
                                  <tr>
                                    <td>
                                      <span class="cat-badge">{getCategoryLabel(h.category)}</span>
                                    </td>
                                    <td class="desc-cell">{h.description}</td>
                                    <td class="muted mono">{h.metric}</td>
                                    <td class="mono">{h.baseline_metric.toFixed(3)}</td>
                                    <td class="mono">{h.experiment_metric.toFixed(3)}</td>
                                    <td class="mono" style="color: {metricDeltaColor(h)}">{metricDelta(h)}</td>
                                    <td>
                                      <div class="confidence-wrap">
                                        <div class="confidence-bar">
                                          <div
                                            class="confidence-fill"
                                            style="width: {h.confidence * 100}%; background: {getConfidenceColor(h.confidence)}"
                                          ></div>
                                        </div>
                                        <span class="confidence-pct" style="color: {getConfidenceColor(h.confidence)}">
                                          {(h.confidence * 100).toFixed(0)}%
                                        </span>
                                      </div>
                                    </td>
                                    <td>
                                      <span class="decision-badge" style="color: {getDecisionColor(h.decision)}; border-color: {getDecisionColor(h.decision)};">
                                        {h.decision.replace(/_/g, ' ')}
                                      </span>
                                    </td>
                                  </tr>
                                {/each}
                              </tbody>
                            </table>
                          {/if}
                        </div>
                      {/if}
                    </td>
                  </tr>
                {/if}
              {/each}
            </tbody>
          </table>
        </div>
        <p class="count-label">Showing {runs.length} of {totalCount} run(s)</p>
      {/if}
    </section>

  {:else}
    <!-- Config panel -->
    <section class="config-section">
      {#if configError}
        <div class="error-banner">
          <span>⚠ {configError}</span>
          <button onclick={() => { configError = null }}>✕</button>
        </div>
      {/if}
      {#if configSuccess}
        <div class="success-banner">
          <span>✓ Config saved</span>
        </div>
      {/if}

      {#if !config}
        <div class="loading">Loading config…</div>
      {:else}
        <form class="config-form" onsubmit={(e) => { e.preventDefault(); saveConfig() }}>
          <div class="config-group">
            <label class="toggle-label" for="cfg-enabled">
              <span>Enabled</span>
              <input id="cfg-enabled" type="checkbox" bind:checked={config.enabled} />
            </label>
          </div>

          <div class="config-group">
            <label for="cfg-timeout">Timeout (ms)</label>
            <input
              id="cfg-timeout"
              type="number"
              bind:value={config.timeout_ms}
              min="1000"
              step="1000"
            />
          </div>

          <div class="config-group">
            <label for="cfg-max-hyp">Max Hypotheses per Run</label>
            <input
              id="cfg-max-hyp"
              type="number"
              bind:value={config.max_hypotheses_per_run}
              min="1"
            />
          </div>

          <div class="config-group">
            <label for="cfg-auto-apply">
              Auto-Apply Threshold
              <span class="slider-val">{config.auto_apply_threshold.toFixed(2)}</span>
            </label>
            <input
              id="cfg-auto-apply"
              type="range"
              min="0"
              max="1"
              step="0.01"
              bind:value={config.auto_apply_threshold}
            />
          </div>

          <div class="config-group">
            <label for="cfg-proposal">
              Proposal Threshold
              <span class="slider-val">{config.proposal_threshold.toFixed(2)}</span>
            </label>
            <input
              id="cfg-proposal"
              type="range"
              min="0"
              max="1"
              step="0.01"
              bind:value={config.proposal_threshold}
            />
          </div>

          <div class="config-group">
            <label for="cfg-sample">Min Sample Size</label>
            <input
              id="cfg-sample"
              type="number"
              bind:value={config.min_sample_size}
              min="1"
            />
          </div>

          <div class="config-group">
            <label for="cfg-budget">Daily Token Budget</label>
            <input
              id="cfg-budget"
              type="number"
              bind:value={config.daily_token_budget}
              min="0"
              step="1000"
            />
          </div>

          <div class="config-actions">
            <button type="submit" class="save-btn" disabled={configSaving}>
              {configSaving ? 'Saving…' : 'Save Config'}
            </button>
          </div>
        </form>
      {/if}
    </section>
  {/if}
</div>

<style>
  .evolution {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    padding: 16px 20px;
    gap: 12px;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-shrink: 0;
  }

  header h1 {
    font-size: 18px;
    font-weight: 600;
    color: #e6edf3;
    margin: 0;
  }

  .subtitle {
    font-size: 12px;
    color: #8b949e;
    margin: 2px 0 0;
  }

  .controls {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .tab-btn {
    padding: 5px 12px;
    border-radius: 6px;
    border: 1px solid #30363d;
    background: transparent;
    color: #8b949e;
    font-size: 13px;
    cursor: pointer;
  }

  .tab-btn.active {
    background: #21262d;
    color: #e6edf3;
    border-color: #388bfd;
  }

  .tab-btn:hover:not(.active) {
    color: #c9d1d9;
    border-color: #484f58;
  }

  .filter-select {
    padding: 5px 8px;
    border-radius: 6px;
    border: 1px solid #30363d;
    background: #161b22;
    color: #c9d1d9;
    font-size: 13px;
  }

  .refresh-btn {
    padding: 5px 12px;
    border-radius: 6px;
    border: 1px solid #30363d;
    background: transparent;
    color: #8b949e;
    font-size: 13px;
    cursor: pointer;
  }

  .refresh-btn:hover:not(:disabled) {
    color: #c9d1d9;
    border-color: #484f58;
  }

  .refresh-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .error-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    background: rgba(248, 81, 73, 0.1);
    border: 1px solid rgba(248, 81, 73, 0.3);
    border-radius: 6px;
    font-size: 13px;
    color: #f85149;
    flex-shrink: 0;
  }

  .error-banner button {
    background: transparent;
    border: none;
    color: #f85149;
    cursor: pointer;
    font-size: 14px;
    padding: 0 4px;
  }

  .success-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    background: rgba(63, 185, 80, 0.1);
    border: 1px solid rgba(63, 185, 80, 0.3);
    border-radius: 6px;
    font-size: 13px;
    color: #3fb950;
    flex-shrink: 0;
  }

  .success-banner button {
    background: transparent;
    border: none;
    color: #3fb950;
    cursor: pointer;
    font-size: 14px;
    padding: 0 4px;
  }

  /* Trigger */
  .trigger-section {
    flex-shrink: 0;
  }

  .trigger-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .timeout-input {
    padding: 6px 10px;
    border-radius: 6px;
    border: 1px solid #30363d;
    background: #161b22;
    color: #c9d1d9;
    font-size: 13px;
    width: 220px;
  }

  .timeout-input::placeholder {
    color: #484f58;
  }

  .trigger-btn {
    padding: 6px 16px;
    border-radius: 6px;
    border: 1px solid #388bfd;
    background: rgba(56, 139, 253, 0.15);
    color: #388bfd;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s;
  }

  .trigger-btn:hover:not(:disabled) {
    background: rgba(56, 139, 253, 0.25);
  }

  .trigger-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }

  /* Runs section */
  .runs-section {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .loading, .empty {
    padding: 32px;
    text-align: center;
    color: #8b949e;
    font-size: 13px;
  }

  .loading-inline {
    padding: 16px;
    color: #8b949e;
    font-size: 13px;
  }

  .table-wrap {
    overflow-x: auto;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }

  thead th {
    padding: 8px 12px;
    text-align: left;
    color: #8b949e;
    font-weight: 500;
    border-bottom: 1px solid #21262d;
    white-space: nowrap;
  }

  tbody tr {
    border-bottom: 1px solid #161b22;
  }

  tbody td {
    padding: 8px 12px;
    color: #c9d1d9;
    vertical-align: middle;
  }

  .run-row {
    cursor: pointer;
    transition: background 0.1s;
  }

  .run-row:hover {
    background: #161b22;
  }

  .run-row.selected {
    background: #1c2230;
  }

  .muted {
    color: #8b949e;
  }

  .mono {
    font-family: 'SF Mono', ui-monospace, 'Cascadia Code', monospace;
    font-size: 12px;
  }

  .status-badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 20px;
    border: 1px solid;
    font-size: 11px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .decision-badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 20px;
    border: 1px solid;
    font-size: 11px;
    font-weight: 500;
    white-space: nowrap;
  }

  .cat-badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 4px;
    background: #21262d;
    color: #8b949e;
    font-size: 11px;
    white-space: nowrap;
  }

  /* Detail row */
  .detail-row td {
    padding: 0;
    background: #0d1117;
    border-bottom: 2px solid #21262d;
  }

  .run-detail {
    padding: 16px 20px;
  }

  .run-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 16px;
  }

  .meta-pill {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 10px;
    border-radius: 6px;
    background: #161b22;
    border: 1px solid #21262d;
    font-size: 12px;
  }

  .meta-pill .label {
    color: #8b949e;
  }

  .meta-pill .value {
    color: #e6edf3;
    font-weight: 500;
  }

  .meta-pill.error {
    border-color: rgba(248, 81, 73, 0.3);
  }

  .meta-pill.error .label { color: #f85149; }
  .meta-pill.error .value { color: #f85149; }

  .empty-sub {
    color: #8b949e;
    font-size: 13px;
    padding: 8px 0;
  }

  /* Hypotheses table */
  .hyp-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  .hyp-table thead th {
    padding: 6px 10px;
    color: #8b949e;
    border-bottom: 1px solid #21262d;
    white-space: nowrap;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .hyp-table tbody td {
    padding: 6px 10px;
    border-bottom: 1px solid #161b22;
    vertical-align: middle;
  }

  .hyp-table tbody tr:last-child td {
    border-bottom: none;
  }

  .desc-cell {
    max-width: 280px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: #c9d1d9;
  }

  .confidence-wrap {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .confidence-bar {
    width: 56px;
    height: 6px;
    background: #21262d;
    border-radius: 3px;
    overflow: hidden;
    flex-shrink: 0;
  }

  .confidence-fill {
    height: 100%;
    border-radius: 3px;
    transition: width 0.3s;
  }

  .confidence-pct {
    font-size: 11px;
    font-weight: 500;
    white-space: nowrap;
  }

  .count-label {
    margin-top: 8px;
    font-size: 12px;
    color: #8b949e;
  }

  /* Config */
  .config-section {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .config-form {
    display: flex;
    flex-direction: column;
    gap: 16px;
    max-width: 480px;
  }

  .config-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .config-group label {
    font-size: 13px;
    color: #c9d1d9;
    font-weight: 500;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .toggle-label {
    flex-direction: row;
    justify-content: space-between;
    align-items: center;
  }

  .config-group input[type='number'] {
    padding: 7px 10px;
    border-radius: 6px;
    border: 1px solid #30363d;
    background: #161b22;
    color: #c9d1d9;
    font-size: 13px;
  }

  .config-group input[type='range'] {
    width: 100%;
    accent-color: #388bfd;
  }

  .config-group input[type='checkbox'] {
    width: 16px;
    height: 16px;
    accent-color: #388bfd;
    cursor: pointer;
  }

  .slider-val {
    font-size: 12px;
    color: #388bfd;
    font-family: 'SF Mono', ui-monospace, monospace;
    font-weight: 600;
  }

  .config-actions {
    padding-top: 4px;
  }

  .save-btn {
    padding: 7px 20px;
    border-radius: 6px;
    border: 1px solid #3fb950;
    background: rgba(63, 185, 80, 0.15);
    color: #3fb950;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s;
  }

  .save-btn:hover:not(:disabled) {
    background: rgba(63, 185, 80, 0.25);
  }

  .save-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
