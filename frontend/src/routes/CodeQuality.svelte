<script lang="ts">
  import {
    getCodeAnalysisRuns,
    getCodeAnalysisRun,
    getCodeFindings,
    triggerCodeAnalysis,
    getCodeAnalysisConfig,
    updateCodeAnalysisConfig,
    updateFindingStatus,
  } from '$lib/api'
  import type { CodeAnalysisRun, CodeFinding, CodeAnalysisConfig } from '$lib/types'
  import { flowForFinding, buildSlashCommand } from '$lib/findingFlow'
  import { setActiveTab, requestTerminalPrefill } from '$lib/store'

  // View state
  let activeView = $state<'findings' | 'runs' | 'config'>('findings')

  // Runs
  let runs = $state<CodeAnalysisRun[]>([])
  let totalRunCount = $state(0)
  let selectedRun = $state<CodeAnalysisRun | null>(null)
  let runFindings = $state<CodeFinding[]>([])
  let loadingRun = $state(false)
  let loadingRuns = $state(true)

  // Findings
  let findings = $state<CodeFinding[]>([])
  let findingsCount = $state(0)
  let loadingFindings = $state(true)
  let severityFilter = $state('')
  let categoryFilter = $state('')
  let statusFilter = $state<'all' | 'pending' | 'rejected' | 'applied'>('all')
  let searchQuery = $state('')
  let expandedFinding = $state<string | null>(null)

  // Per-finding mutation state
  let rejectingId = $state<string | null>(null)
  let rejectError = $state<Record<string, string>>({})

  // Trigger
  let triggering = $state(false)
  let triggerSuccess = $state<string | null>(null)

  // Config
  let config = $state<CodeAnalysisConfig | null>(null)
  let configSaving = $state(false)
  let configSuccess = $state(false)
  let configError = $state<string | null>(null)

  // Errors
  let error = $state<string | null>(null)

  // Per-finding flow override: 'auto' | 'bug' | 'spec'
  let overrideFlow = $state<Record<string, 'bug' | 'spec' | 'auto'>>({})

  // All unique categories from findings for filter dropdown
  let allCategories = $derived(
    [...new Set(findings.map(f => f.category))].sort()
  )

  // Filtered findings
  let filteredFindings = $derived(
    findings.filter(f => {
      if (severityFilter && f.severity !== severityFilter) return false
      if (categoryFilter && f.category !== categoryFilter) return false
      if (statusFilter !== 'all' && f.status !== statusFilter) return false
      if (searchQuery) {
        const q = searchQuery.toLowerCase()
        return (
          f.file_path.toLowerCase().includes(q) ||
          f.title.toLowerCase().includes(q) ||
          f.description.toLowerCase().includes(q)
        )
      }
      return true
    })
  )

  // Stats from most recent run
  let latestRun = $derived(runs.length > 0 ? runs[0] : null)

  $effect(() => {
    loadFindings()
    loadRuns()
  })

  async function loadRuns() {
    loadingRuns = true
    try {
      const data = await getCodeAnalysisRuns(20)
      runs = data.runs ?? []
      totalRunCount = data.count ?? 0
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load runs'
    } finally {
      loadingRuns = false
    }
  }

  async function loadFindings() {
    loadingFindings = true
    try {
      const params: Record<string, string> = { limit: '200' }
      if (statusFilter !== 'all') params.status = statusFilter
      const data = await getCodeFindings(params)
      findings = data.findings ?? []
      findingsCount = data.count ?? 0
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load findings'
    } finally {
      loadingFindings = false
    }
  }

  async function handleReject(id: string) {
    rejectingId = id
    rejectError = { ...rejectError, [id]: '' }
    try {
      await updateFindingStatus(id, 'rejected')
      await loadFindings()
    } catch (e) {
      rejectError = { ...rejectError, [id]: e instanceof Error ? e.message : 'Failed to reject finding' }
    } finally {
      rejectingId = null
    }
  }

  async function selectRun(run: CodeAnalysisRun) {
    if (selectedRun?.id === run.id) {
      selectedRun = null
      runFindings = []
      return
    }
    loadingRun = true
    try {
      const data = await getCodeAnalysisRun(run.id)
      selectedRun = data.run
      runFindings = data.findings ?? []
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
      await triggerCodeAnalysis()
      triggerSuccess = 'Analysis triggered successfully'
      await loadRuns()
      await loadFindings()
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to trigger analysis'
    } finally {
      triggering = false
    }
  }

  async function handleSwitchToConfig() {
    activeView = 'config'
    if (!config) await loadConfig()
  }

  async function loadConfig() {
    configError = null
    try {
      config = await getCodeAnalysisConfig()
    } catch (e) {
      configError = e instanceof Error ? e.message : 'Failed to load config'
    }
  }

  async function saveConfig() {
    if (!config) return
    configSaving = true
    configError = null
    configSuccess = false
    try {
      config = await updateCodeAnalysisConfig(config)
      configSuccess = true
      setTimeout(() => { configSuccess = false }, 3000)
    } catch (e) {
      configError = e instanceof Error ? e.message : 'Failed to save config'
    } finally {
      configSaving = false
    }
  }

  function toggleFinding(id: string) {
    expandedFinding = expandedFinding === id ? null : id
  }

  function formatDate(dateStr: string | null | undefined) {
    if (!dateStr) return '—'
    return new Date(dateStr).toLocaleString()
  }

  function formatDuration(ms: number) {
    if (!ms || ms === 0) return '—'
    if (ms < 1000) return ms + 'ms'
    if (ms < 60000) return (ms / 1000).toFixed(1) + 's'
    return (ms / 60000).toFixed(1) + 'm'
  }

  function getSeverityColor(severity: string): string {
    switch (severity.toLowerCase()) {
      case 'critical': return '#f85149'
      case 'warning': return '#d29922'
      case 'info': return '#388bfd'
      default: return '#8b949e'
    }
  }

  function getSeverityBg(severity: string): string {
    switch (severity.toLowerCase()) {
      case 'critical': return 'rgba(248,81,73,0.12)'
      case 'warning': return 'rgba(210,153,34,0.12)'
      case 'info': return 'rgba(56,139,253,0.12)'
      default: return 'rgba(139,148,158,0.12)'
    }
  }

  function getStatusColor(status: string): string {
    switch (status) {
      case 'running': return '#388bfd'
      case 'completed': return '#3fb950'
      case 'failed': return '#f85149'
      default: return '#8b949e'
    }
  }

  function getConfidenceColor(confidence: number): string {
    if (confidence >= 0.8) return '#3fb950'
    if (confidence >= 0.6) return '#d29922'
    return '#f85149'
  }

  function getFindingStatusColor(status: string): string {
    switch (status) {
      case 'applied': return '#3fb950'
      case 'rejected': return '#6e7681'
      default: return '#8b949e'
    }
  }

  function getFindingStatusBg(status: string): string {
    switch (status) {
      case 'applied': return 'rgba(63,185,80,0.12)'
      case 'rejected': return 'rgba(110,118,129,0.12)'
      default: return 'rgba(139,148,158,0.12)'
    }
  }

  function countBySeverity(sev: string): number {
    return findings.filter(f => f.severity.toLowerCase() === sev).length
  }

  function toggleCategory(cat: string) {
    if (!config) return
    const idx = config.categories.indexOf(cat)
    if (idx >= 0) {
      config.categories = config.categories.filter(c => c !== cat)
    } else {
      config.categories = [...config.categories, cat]
    }
  }

  const ALL_CATEGORIES = [
    'anti_pattern',
    'duplication',
    'coverage_gap',
    'error_handling',
    'complexity',
    'dead_code',
    'security',
  ]
</script>

<div class="code-quality">
  <header>
    <div class="header-left">
      <h1>Code Quality</h1>
      <p class="subtitle">Static analysis, findings &amp; LLM-assisted code review</p>
    </div>
    <div class="controls">
      <button
        class="tab-btn"
        class:active={activeView === 'findings'}
        onclick={() => { activeView = 'findings' }}
      >
        Findings
      </button>
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

      {#if activeView === 'findings' || activeView === 'runs'}
        <button class="refresh-btn" onclick={() => { loadRuns(); loadFindings() }} disabled={loadingRuns || loadingFindings}>
          {(loadingRuns || loadingFindings) ? 'Loading...' : '↻ Refresh'}
        </button>
      {/if}

      <button class="trigger-btn" onclick={handleTrigger} disabled={triggering}>
        {triggering ? 'Running…' : '▶ Run Analysis'}
      </button>
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

  <!-- Stats cards -->
  {#if activeView !== 'config'}
    <section class="stats-row">
      {#if loadingFindings && findings.length === 0}
        <div class="stat-card loading-card">Loading…</div>
      {:else if findings.length === 0 && !loadingFindings}
        <div class="empty-stats">No analysis runs yet. Click "Run Analysis" to start.</div>
      {:else}
        <div class="stat-card">
          <span class="stat-value">{findingsCount}</span>
          <span class="stat-label">Total Findings</span>
        </div>
        <div class="stat-card critical">
          <span class="stat-value">{countBySeverity('critical')}</span>
          <span class="stat-label">Critical</span>
        </div>
        <div class="stat-card warning">
          <span class="stat-value">{countBySeverity('warning')}</span>
          <span class="stat-label">Warning</span>
        </div>
        <div class="stat-card info">
          <span class="stat-value">{countBySeverity('info')}</span>
          <span class="stat-label">Info</span>
        </div>
        {#if latestRun}
          <div class="stat-card">
            <span class="stat-value">{latestRun.files_scanned}</span>
            <span class="stat-label">Files Scanned</span>
          </div>
          <div class="stat-card">
            <span class="stat-value muted-val">{formatDate(latestRun.started_at)}</span>
            <span class="stat-label">Last Run</span>
          </div>
        {/if}
      {/if}
    </section>
  {/if}

  <!-- Findings view -->
  {#if activeView === 'findings'}
    <section class="findings-section">
      <div class="filter-bar">
        <input
          class="search-input"
          type="search"
          bind:value={searchQuery}
          placeholder="Search findings…"
          aria-label="Search findings"
        />
        <select bind:value={severityFilter} class="filter-select" aria-label="Filter by severity">
          <option value="">All severities</option>
          <option value="critical">Critical</option>
          <option value="warning">Warning</option>
          <option value="info">Info</option>
        </select>
        <select bind:value={categoryFilter} class="filter-select" aria-label="Filter by category">
          <option value="">All categories</option>
          {#each allCategories as cat}
            <option value={cat}>{cat}</option>
          {/each}
        </select>
        <select bind:value={statusFilter} class="filter-select" aria-label="Filter by status" onchange={() => loadFindings()}>
          <option value="all">All statuses</option>
          <option value="pending">Pending</option>
          <option value="rejected">Rejected</option>
          <option value="applied">Applied</option>
        </select>
        {#if severityFilter || categoryFilter || statusFilter !== 'all' || searchQuery}
          <button
            class="clear-btn"
            onclick={() => { severityFilter = ''; categoryFilter = ''; statusFilter = 'all'; searchQuery = ''; loadFindings() }}
          >
            Clear filters
          </button>
        {/if}
        <span class="findings-count">{filteredFindings.length} finding{filteredFindings.length !== 1 ? 's' : ''}</span>
      </div>

      {#if loadingFindings && findings.length === 0}
        <div class="loading">Loading findings…</div>
      {:else if filteredFindings.length === 0}
        <div class="empty">
          {findings.length === 0
            ? 'No findings yet. Trigger an analysis run to scan your codebase.'
            : 'No findings match the current filters.'}
        </div>
      {:else}
        <div class="findings-list">
          {#each filteredFindings as finding}
            <article
              class="finding-card"
              class:expanded={expandedFinding === finding.id}
            >
              <button
                class="finding-summary"
                onclick={() => toggleFinding(finding.id)}
                aria-expanded={expandedFinding === finding.id}
              >
                <div class="finding-left">
                  <span
                    class="sev-badge"
                    style="color: {getSeverityColor(finding.severity)}; background: {getSeverityBg(finding.severity)};"
                  >
                    {finding.severity}
                  </span>
                  <span class="cat-badge">{finding.category}</span>
                  <span
                    class="status-pill"
                    style="color: {getFindingStatusColor(finding.status)}; background: {getFindingStatusBg(finding.status)};"
                  >
                    {finding.status}
                  </span>
                  <span class="finding-title">{finding.title}</span>
                </div>
                <div class="finding-right">
                  <span class="file-path">{finding.file_path}</span>
                  {#if finding.line_start}
                    <span class="line-range">:{finding.line_start}{finding.line_end && finding.line_end !== finding.line_start ? `–${finding.line_end}` : ''}</span>
                  {/if}
                  <div class="confidence-wrap">
                    <div class="confidence-bar">
                      <div
                        class="confidence-fill"
                        style="width: {finding.confidence * 100}%; background: {getConfidenceColor(finding.confidence)}"
                      ></div>
                    </div>
                    <span class="confidence-pct" style="color: {getConfidenceColor(finding.confidence)}">
                      {(finding.confidence * 100).toFixed(0)}%
                    </span>
                  </div>
                  <span class="chevron" class:open={expandedFinding === finding.id}>›</span>
                </div>
              </button>

              {#if expandedFinding === finding.id}
                <div class="finding-detail">
                  <div class="detail-row">
                    <span class="detail-label">File</span>
                    <span class="detail-value mono">{finding.file_path}{finding.line_start ? `:${finding.line_start}` : ''}{finding.line_end && finding.line_end !== finding.line_start ? `–${finding.line_end}` : ''}</span>
                  </div>
                  <div class="detail-row">
                    <span class="detail-label">Description</span>
                    <span class="detail-value">{finding.description}</span>
                  </div>
                  {#if finding.suggestion}
                    <div class="detail-row">
                      <span class="detail-label">Suggestion</span>
                      <span class="detail-value suggestion">{finding.suggestion}</span>
                    </div>
                  {/if}
                  {#if finding.wiki_page_id}
                    <div class="detail-row">
                      <span class="detail-label">Wiki</span>
                      <span class="detail-value muted">Page: {finding.wiki_page_id}</span>
                    </div>
                  {/if}
                  <div class="detail-actions">
                    <label class="flow-label" for="flow-select-{finding.id}">Flow</label>
                    <select
                      id="flow-select-{finding.id}"
                      class="flow-select"
                      bind:value={overrideFlow[finding.id]}
                      aria-label="Select workflow type"
                    >
                      <option value="auto">Auto ({flowForFinding(finding.severity, finding.category)})</option>
                      <option value="bug">bug</option>
                      <option value="spec">spec</option>
                    </select>
                    <button
                      class="open-terminal-btn"
                      disabled={finding.status !== 'pending'}
                      onclick={() => {
                        const override = overrideFlow[finding.id] ?? 'auto'
                        const flow = override === 'auto' ? flowForFinding(finding.severity, finding.category) : override
                        requestTerminalPrefill(buildSlashCommand(flow, finding))
                        setActiveTab('overview')
                      }}
                    >
                      Open in Terminal
                    </button>
                    {#if finding.status === 'pending'}
                      <button
                        class="reject-btn"
                        disabled={rejectingId === finding.id}
                        onclick={() => handleReject(finding.id)}
                        aria-label="Reject finding"
                      >
                        {rejectingId === finding.id ? 'Rejecting…' : 'Reject'}
                      </button>
                    {/if}
                    {#if rejectError[finding.id]}
                      <span class="reject-error">{rejectError[finding.id]}</span>
                    {/if}
                  </div>
                </div>
              {/if}
            </article>
          {/each}
        </div>
      {/if}
    </section>

  <!-- Runs view -->
  {:else if activeView === 'runs'}
    <section class="runs-section">
      {#if loadingRuns && runs.length === 0}
        <div class="loading">Loading runs…</div>
      {:else if runs.length === 0}
        <div class="empty">No analysis runs yet. Click "Run Analysis" to start.</div>
      {:else}
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Status</th>
                <th>Files Scanned</th>
                <th>Analyzed</th>
                <th>Findings</th>
                <th>Wiki Created</th>
                <th>Wiki Updated</th>
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
                  <td>{run.files_scanned}</td>
                  <td>{run.files_analyzed}</td>
                  <td>{run.findings_count}</td>
                  <td>{run.wiki_pages_created}</td>
                  <td>{run.wiki_pages_updated}</td>
                  <td class="muted">{formatDuration(run.duration_ms)}</td>
                  <td class="muted">{formatDate(run.started_at)}</td>
                </tr>

                {#if selectedRun?.id === run.id}
                  <tr class="detail-row">
                    <td colspan="8">
                      {#if loadingRun}
                        <div class="loading-inline">Loading findings…</div>
                      {:else}
                        <div class="run-detail">
                          <div class="run-meta">
                            <div class="meta-pill">
                              <span class="label">Tokens Used</span>
                              <span class="value">{selectedRun.tokens_used.toLocaleString()}</span>
                            </div>
                            {#if selectedRun.git_commit_hash}
                              <div class="meta-pill">
                                <span class="label">Commit</span>
                                <span class="value mono">{selectedRun.git_commit_hash.slice(0, 8)}</span>
                              </div>
                            {/if}
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

                          {#if runFindings.length === 0}
                            <p class="empty-sub">No findings recorded for this run.</p>
                          {:else}
                            <table class="findings-sub-table">
                              <thead>
                                <tr>
                                  <th>Severity</th>
                                  <th>Category</th>
                                  <th>Title</th>
                                  <th>File</th>
                                  <th>Lines</th>
                                  <th>Confidence</th>
                                </tr>
                              </thead>
                              <tbody>
                                {#each runFindings as f}
                                  <tr>
                                    <td>
                                      <span
                                        class="sev-badge"
                                        style="color: {getSeverityColor(f.severity)}; background: {getSeverityBg(f.severity)};"
                                      >
                                        {f.severity}
                                      </span>
                                    </td>
                                    <td><span class="cat-badge">{f.category}</span></td>
                                    <td class="title-cell">{f.title}</td>
                                    <td class="muted mono file-cell">{f.file_path}</td>
                                    <td class="muted mono">
                                      {#if f.line_start}
                                        {f.line_start}{f.line_end && f.line_end !== f.line_start ? `–${f.line_end}` : ''}
                                      {:else}
                                        —
                                      {/if}
                                    </td>
                                    <td>
                                      <div class="confidence-wrap">
                                        <div class="confidence-bar">
                                          <div
                                            class="confidence-fill"
                                            style="width: {f.confidence * 100}%; background: {getConfidenceColor(f.confidence)}"
                                          ></div>
                                        </div>
                                        <span class="confidence-pct" style="color: {getConfidenceColor(f.confidence)}">
                                          {(f.confidence * 100).toFixed(0)}%
                                        </span>
                                      </div>
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
        <p class="count-label">Showing {runs.length} of {totalRunCount} run(s)</p>
      {/if}
    </section>

  <!-- Config view -->
  {:else}
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
            {#if config.enabled}
              <p class="config-warning">
                ⚠ Source file contents will be sent to the configured LLM provider for analysis.
              </p>
            {/if}
          </div>

          <div class="config-group">
            <label for="cfg-max-files">Max Files per Run</label>
            <input
              id="cfg-max-files"
              type="number"
              bind:value={config.max_files_per_run}
              min="1"
              max="1000"
            />
          </div>

          <div class="config-group">
            <label for="cfg-token-budget">Token Budget per Run</label>
            <input
              id="cfg-token-budget"
              type="number"
              bind:value={config.token_budget_per_run}
              min="1000"
              step="1000"
            />
          </div>

          <div class="config-group">
            <label for="cfg-scan-interval">Scan Interval (minutes)</label>
            <input
              id="cfg-scan-interval"
              type="number"
              bind:value={config.scan_interval}
              min="1"
            />
          </div>

          <div class="config-group">
            <label for="cfg-churn">
              Min Churn Score
              <span class="slider-val">{config.min_churn_score.toFixed(2)}</span>
            </label>
            <input
              id="cfg-churn"
              type="range"
              min="0"
              max="1"
              step="0.01"
              bind:value={config.min_churn_score}
            />
          </div>

          <div class="config-group">
            <label for="cfg-confidence">
              Confidence Threshold
              <span class="slider-val">{config.confidence_threshold.toFixed(2)}</span>
            </label>
            <input
              id="cfg-confidence"
              type="range"
              min="0"
              max="1"
              step="0.01"
              bind:value={config.confidence_threshold}
            />
          </div>

          <div class="config-group">
            <label class="toggle-label" for="cfg-git-history">
              <span>Include Git History</span>
              <input id="cfg-git-history" type="checkbox" bind:checked={config.include_git_history} />
            </label>
          </div>

          {#if config.include_git_history}
            <div class="config-group">
              <label for="cfg-git-depth">Git History Depth (commits)</label>
              <input
                id="cfg-git-depth"
                type="number"
                bind:value={config.git_history_depth}
                min="1"
                max="1000"
              />
            </div>
          {/if}

          <fieldset class="config-group categories-fieldset">
            <legend class="categories-legend">Categories</legend>
            <div class="category-grid">
              {#each ALL_CATEGORIES as cat}
                <label class="cat-toggle">
                  <input
                    type="checkbox"
                    checked={config.categories.includes(cat)}
                    onchange={() => toggleCategory(cat)}
                  />
                  {cat}
                </label>
              {/each}
            </div>
          </fieldset>

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
  .code-quality {
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

  /* Stats row */
  .stats-row {
    display: flex;
    align-items: stretch;
    gap: 8px;
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .stat-card {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 8px 14px;
    border-radius: 8px;
    background: #161b22;
    border: 1px solid #21262d;
    min-width: 90px;
  }

  .stat-card.loading-card {
    color: #8b949e;
    font-size: 12px;
    align-items: center;
    justify-content: center;
  }

  .stat-card.critical { border-color: rgba(248, 81, 73, 0.3); }
  .stat-card.warning  { border-color: rgba(210, 153, 34, 0.3); }
  .stat-card.info     { border-color: rgba(56, 139, 253, 0.3); }

  .stat-value {
    font-size: 20px;
    font-weight: 600;
    color: #e6edf3;
    line-height: 1;
  }

  .stat-card.critical .stat-value { color: #f85149; }
  .stat-card.warning  .stat-value { color: #d29922; }
  .stat-card.info     .stat-value { color: #388bfd; }

  .stat-value.muted-val {
    font-size: 12px;
    color: #8b949e;
    font-weight: 400;
  }

  .stat-label {
    font-size: 11px;
    color: #8b949e;
    white-space: nowrap;
  }

  .empty-stats {
    padding: 12px;
    color: #8b949e;
    font-size: 13px;
  }

  /* Findings section */
  .findings-section {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .filter-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .search-input {
    padding: 6px 10px;
    border-radius: 6px;
    border: 1px solid #30363d;
    background: #161b22;
    color: #c9d1d9;
    font-size: 13px;
    width: 220px;
  }

  .search-input::placeholder { color: #484f58; }

  .filter-select {
    padding: 5px 8px;
    border-radius: 6px;
    border: 1px solid #30363d;
    background: #161b22;
    color: #c9d1d9;
    font-size: 13px;
  }

  .clear-btn {
    padding: 5px 10px;
    border-radius: 6px;
    border: 1px solid #30363d;
    background: transparent;
    color: #8b949e;
    font-size: 12px;
    cursor: pointer;
  }

  .clear-btn:hover { color: #c9d1d9; }

  .findings-count {
    font-size: 12px;
    color: #8b949e;
    margin-left: auto;
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

  /* Finding cards */
  .findings-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .finding-card {
    border-radius: 6px;
    border: 1px solid #21262d;
    background: #161b22;
    overflow: hidden;
    transition: border-color 0.1s;
  }

  .finding-card.expanded {
    border-color: #30363d;
  }

  .finding-summary {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 12px;
    background: transparent;
    border: none;
    cursor: pointer;
    gap: 12px;
    text-align: left;
  }

  .finding-summary:hover {
    background: rgba(255, 255, 255, 0.02);
  }

  .finding-left {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    flex: 1;
  }

  .finding-right {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }

  .sev-badge {
    display: inline-block;
    padding: 2px 7px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .cat-badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 4px;
    background: #21262d;
    color: #8b949e;
    font-size: 11px;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .finding-title {
    font-size: 13px;
    color: #c9d1d9;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .file-path {
    font-size: 12px;
    color: #8b949e;
    font-family: 'SF Mono', ui-monospace, 'Cascadia Code', monospace;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 260px;
  }

  .line-range {
    font-size: 12px;
    color: #6e7681;
    font-family: 'SF Mono', ui-monospace, monospace;
    white-space: nowrap;
  }

  .confidence-wrap {
    display: flex;
    align-items: center;
    gap: 5px;
  }

  .confidence-bar {
    width: 48px;
    height: 4px;
    background: #21262d;
    border-radius: 2px;
    overflow: hidden;
    flex-shrink: 0;
  }

  .confidence-fill {
    height: 100%;
    border-radius: 2px;
    transition: width 0.3s;
  }

  .confidence-pct {
    font-size: 11px;
    font-weight: 500;
    white-space: nowrap;
    min-width: 28px;
  }

  .chevron {
    font-size: 16px;
    color: #6e7681;
    transition: transform 0.15s;
    line-height: 1;
  }

  .chevron.open {
    transform: rotate(90deg);
  }

  /* Finding detail */
  .finding-detail {
    padding: 10px 12px 12px;
    border-top: 1px solid #21262d;
    display: flex;
    flex-direction: column;
    gap: 8px;
    background: #0d1117;
  }

  .detail-row {
    display: flex;
    gap: 10px;
    font-size: 13px;
  }

  .detail-label {
    color: #8b949e;
    font-weight: 500;
    width: 90px;
    flex-shrink: 0;
  }

  .detail-value {
    color: #c9d1d9;
    flex: 1;
    line-height: 1.5;
  }

  .detail-value.mono {
    font-family: 'SF Mono', ui-monospace, monospace;
    font-size: 12px;
  }

  .detail-value.suggestion {
    color: #3fb950;
  }

  .detail-value.muted {
    color: #8b949e;
  }

  /* Runs section */
  .runs-section {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }

  .table-wrap { overflow-x: auto; }

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

  tbody tr { border-bottom: 1px solid #161b22; }

  tbody td {
    padding: 8px 12px;
    color: #c9d1d9;
    vertical-align: middle;
  }

  .run-row {
    cursor: pointer;
    transition: background 0.1s;
  }

  .run-row:hover { background: #161b22; }
  .run-row.selected { background: #1c2230; }

  .muted { color: #8b949e; }

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

  /* Run detail */
  .detail-row td {
    padding: 0;
    background: #0d1117;
    border-bottom: 2px solid #21262d;
  }

  .run-detail { padding: 16px 20px; }

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

  .meta-pill .label { color: #8b949e; }
  .meta-pill .value { color: #e6edf3; font-weight: 500; }
  .meta-pill.error { border-color: rgba(248, 81, 73, 0.3); }
  .meta-pill.error .label { color: #f85149; }
  .meta-pill.error .value { color: #f85149; }

  .empty-sub {
    color: #8b949e;
    font-size: 13px;
    padding: 8px 0;
  }

  /* Sub-table for findings inside run detail */
  .findings-sub-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  .findings-sub-table thead th {
    padding: 6px 10px;
    color: #8b949e;
    border-bottom: 1px solid #21262d;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .findings-sub-table tbody td {
    padding: 6px 10px;
    border-bottom: 1px solid #161b22;
    vertical-align: middle;
  }

  .findings-sub-table tbody tr:last-child td { border-bottom: none; }

  .title-cell {
    max-width: 240px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: #c9d1d9;
  }

  .file-cell {
    max-width: 200px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .count-label {
    margin-top: 8px;
    font-size: 12px;
    color: #8b949e;
  }

  /* Config section */
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

  .config-warning {
    font-size: 12px;
    color: #d29922;
    background: rgba(210, 153, 34, 0.1);
    border: 1px solid rgba(210, 153, 34, 0.3);
    border-radius: 6px;
    padding: 6px 10px;
    margin-top: 4px;
  }

  .categories-fieldset {
    border: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .categories-legend {
    font-size: 13px;
    color: #c9d1d9;
    font-weight: 500;
    padding: 0;
    margin-bottom: 2px;
  }

  .category-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .cat-toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    color: #c9d1d9;
    cursor: pointer;
    user-select: none;
    font-weight: 400;
  }

  .cat-toggle input[type='checkbox'] {
    accent-color: #388bfd;
  }

  .config-actions { padding-top: 4px; }

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

  .save-btn:hover:not(:disabled) { background: rgba(63, 185, 80, 0.25); }
  .save-btn:disabled { opacity: 0.5; cursor: default; }

  .detail-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-top: 10px;
    border-top: 1px solid #21262d;
    margin-top: 4px;
  }

  .flow-label {
    font-size: 12px;
    color: #8b949e;
  }

  .flow-select {
    padding: 4px 8px;
    border-radius: 6px;
    border: 1px solid #30363d;
    background: #0d1117;
    color: #c9d1d9;
    font-size: 12px;
    cursor: pointer;
  }

  .open-terminal-btn {
    padding: 5px 14px;
    border-radius: 6px;
    border: 1px solid #388bfd;
    background: rgba(56, 139, 253, 0.15);
    color: #388bfd;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s;
  }

  .open-terminal-btn:hover:not(:disabled) {
    background: rgba(56, 139, 253, 0.25);
  }

  .open-terminal-btn:disabled {
    opacity: 0.4;
    cursor: default;
    border-color: #30363d;
    color: #484f58;
    background: transparent;
  }

  .status-pill {
    display: inline-block;
    padding: 2px 7px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    white-space: nowrap;
    flex-shrink: 0;
  }

  .reject-btn {
    padding: 5px 14px;
    border-radius: 6px;
    border: 1px solid rgba(248, 81, 73, 0.5);
    background: rgba(248, 81, 73, 0.1);
    color: #f85149;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s;
  }

  .reject-btn:hover:not(:disabled) {
    background: rgba(248, 81, 73, 0.2);
  }

  .reject-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .reject-error {
    font-size: 12px;
    color: #f85149;
  }
</style>
