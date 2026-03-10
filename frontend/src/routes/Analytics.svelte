<script lang="ts">
  import { onMount } from 'svelte'
  import Chart from '$lib/Chart.svelte'
  import { appState } from '$lib/store'
  import { 
    getMetricsSummary, 
    getDailyMetrics,
    getAgentMetrics,
    exportMetricsCSV
  } from '$lib/api'
  import type { MetricsSummary, DailyMetric, AgentMetric } from '$lib/types'
  
  let loading = $state(true)
  let error = $state<string | null>(null)
  let timeRange = $state<'7d' | '30d' | '90d'>('7d')
  let summary = $state<MetricsSummary | null>(null)
  let dailyMetrics = $state<DailyMetric[]>([])
  let agentMetrics = $state<AgentMetric[]>([])
  
  let lastUpdateCounter = 0
  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let pendingRequest = false
  let metricsLiveTimeout: ReturnType<typeof setTimeout> | null = null
  let isLive = $state(false)
  
  // Chart data
  let workflowTrendData = $state<any>(null)
  let agentPerformanceData = $state<any>(null)
  let phaseDistributionData = $state<any>(null)
  let taskCompletionData = $state<any>(null)
  
  onMount(() => {
    loadMetrics()
    return () => {
      if (debounceTimer) clearTimeout(debounceTimer)
      if (metricsLiveTimeout) clearTimeout(metricsLiveTimeout)
    }
  })
  
  function setLive() {
    isLive = true
    if (metricsLiveTimeout) clearTimeout(metricsLiveTimeout)
    metricsLiveTimeout = setTimeout(() => {
      isLive = false
    }, 30000)
  }
  
  // Watch for real-time updates with debounce
  $effect(() => {
    const counter = appState.analyticsUpdateCounter
    if (counter > lastUpdateCounter) {
      lastUpdateCounter = counter
      setLive()
      if (debounceTimer) clearTimeout(debounceTimer)
      debounceTimer = setTimeout(() => {
        loadMetrics(true)
      }, 2000)
    }
  })
  
  async function loadMetrics(background = false) {
    if (pendingRequest) return
    pendingRequest = true
    if (!background) loading = true
    error = null
    try {
      const days = timeRange === '7d' ? 7 : timeRange === '30d' ? 30 : 90
      
      const [summaryData, dailyData, agentData] = await Promise.all([
        getMetricsSummary(days),
        getDailyMetrics(days),
        getAgentMetrics(days)
      ])
      
      summary = summaryData.summary
      dailyMetrics = dailyData.metrics
      agentMetrics = agentData.agents
      
      generateChartData()
    } catch (e) {
      if (!background) {
        error = e instanceof Error ? e.message : 'Failed to load metrics'
        console.error('Failed to load metrics:', e)
      }
    } finally {
      loading = false
      pendingRequest = false
    }
  }
  
  function dismissAnomaly(index: number) {
    appState.activeAnomalies.splice(index, 1)
    appState.activeAnomalies = [...appState.activeAnomalies] // trigger reactivity
  }
  
  function dismissAlert() {
    appState.lastMetricsAlert = null
  }
  
  function generateChartData() {
    const sortedMetrics = [...dailyMetrics].reverse()
    
    const labels = sortedMetrics.map(m => {
      const date = new Date(m.date)
      return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
    })
    
    workflowTrendData = {
      labels,
      datasets: [{
        label: 'Completed Workflows',
        data: sortedMetrics.map(m => m.completed_workflows),
        borderColor: '#3b82f6',
        backgroundColor: 'rgba(59, 130, 246, 0.1)',
        tension: 0.4,
        fill: true,
      }]
    }
    
    if (agentMetrics.length > 0) {
      const colors = [
        '#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6',
        '#ec4899', '#14b8a6', '#f97316', '#6366f1', '#84cc16'
      ]
      agentPerformanceData = {
        labels: agentMetrics.map(a => a.agent_id),
        datasets: [{
          label: 'Tasks Completed',
          data: agentMetrics.map(a => a.tasks_completed),
          backgroundColor: colors.slice(0, agentMetrics.length)
        }]
      }
    } else {
      agentPerformanceData = {
        labels: ['No Data'],
        datasets: [{
          label: 'Tasks Completed',
          data: [0],
          backgroundColor: ['#30363d']
        }]
      }
    }
    
    const successBuckets = { high: 0, medium: 0, low: 0, failed: 0 }
    let hasData = false
    dailyMetrics.forEach(m => {
      if (m.total_tasks === 0) return
      hasData = true
      const rate = m.success_rate
      if (rate >= 0.8) successBuckets.high++
      else if (rate >= 0.5) successBuckets.medium++
      else if (rate > 0) successBuckets.low++
      else successBuckets.failed++
    })
    
    if (!hasData) {
      phaseDistributionData = {
        labels: ['No Data'],
        datasets: [{
          data: [1],
          backgroundColor: ['#30363d']
        }]
      }
    } else {
      phaseDistributionData = {
        labels: ['High (≥80%)', 'Medium (50-80%)', 'Low (<50%)', 'Failed (0%)'],
        datasets: [{
          data: [
            successBuckets.high,
            successBuckets.medium,
            successBuckets.low,
            successBuckets.failed
          ],
          backgroundColor: ['#3fb950', '#f59e0b', '#f85149', '#6e7681']
        }]
      }
    }
    
    const successByDomain: Record<string, { success: number; failed: number }> = {}
    agentMetrics.forEach(a => {
      const parts = a.agent_id.split('-')
      const domain = parts.length > 1 ? parts[0] : 'default'
      if (!successByDomain[domain]) successByDomain[domain] = { success: 0, failed: 0 }
      const completed = a.tasks_completed
      const failed = Math.round(completed * (1 - a.success_rate))
      successByDomain[domain].success += completed - failed
      successByDomain[domain].failed += failed
    })
    
    const domains = Object.keys(successByDomain).sort((a, b) => {
      const aTotal = successByDomain[a].success + successByDomain[a].failed
      const bTotal = successByDomain[b].success + successByDomain[b].failed
      return bTotal - aTotal
    }).slice(0, 5)
    taskCompletionData = {
      labels: domains.length > 0 ? domains : ['No Data'],
      datasets: [
        {
          label: 'Successful',
          data: domains.length > 0 ? domains.map(d => successByDomain[d].success) : [0],
          backgroundColor: '#10b981'
        },
        {
          label: 'Failed',
          data: domains.length > 0 ? domains.map(d => successByDomain[d].failed) : [0],
          backgroundColor: '#ef4444'
        }
      ]
    }
  }
  
  function formatDuration(ms: number): string {
    if (!Number.isFinite(ms) || ms <= 0) return '0s'
    const seconds = Math.floor(ms / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    const days = Math.floor(hours / 24)
    
    if (days > 0) return `${days}d ${hours % 24}h`
    if (hours > 0) return `${hours}h ${minutes % 60}m`
    if (minutes > 0) return `${minutes}m`
    return `${seconds}s`
  }
  
  function formatPercent(value: number): string {
    if (!Number.isFinite(value)) return '0.0%'
    return `${(value * 100).toFixed(1)}%`
  }
  
  function formatDate(dateStr: string): string {
    const date = new Date(dateStr)
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
  }
  
  function handleExport() {
    const days = timeRange === '7d' ? 7 : timeRange === '30d' ? 30 : 90
    exportMetricsCSV(days)
  }
</script>

<div class="analytics">
  <header>
    <div class="header-left">
      <h1>Analytics Dashboard</h1>
      <p class="subtitle">
        Real-time insights into workflow performance
        {#if isLive}
          <span class="live-indicator" title="Receiving live updates">
            <span class="live-dot"></span>
            LIVE
          </span>
        {/if}
      </p>
    </div>
    
    <div class="controls">
      <div class="time-range">
        <button 
          class:active={timeRange === '7d'}
          onclick={() => { if (timeRange !== '7d') { timeRange = '7d'; loadMetrics() } }}
        >
          7 Days
        </button>
        <button 
          class:active={timeRange === '30d'}
          onclick={() => { if (timeRange !== '30d') { timeRange = '30d'; loadMetrics() } }}
        >
          30 Days
        </button>
        <button 
          class:active={timeRange === '90d'}
          onclick={() => { if (timeRange !== '90d') { timeRange = '90d'; loadMetrics() } }}
        >
          90 Days
        </button>
      </div>
      
      <button 
        class="refresh-btn"
        onclick={() => loadMetrics()}
        disabled={loading}
      >
        {loading ? 'Loading...' : '↻ Refresh'}
      </button>
      
      <button 
        class="export-btn"
        onclick={handleExport}
        disabled={loading}
      >
        ↓ Export CSV
      </button>
    </div>
  </header>
  
  <!-- Anomaly Alerts Banner -->
  {#if appState.activeAnomalies.length > 0}
    <div class="anomaly-banner">
      <div class="anomaly-header">
        <span class="anomaly-icon">⚠</span>
        <strong>{appState.activeAnomalies.length} Anomal{appState.activeAnomalies.length === 1 ? 'y' : 'ies'} Detected</strong>
        <button class="dismiss-all-btn" onclick={() => { appState.activeAnomalies = [] }}>
          Dismiss All
        </button>
      </div>
      <div class="anomaly-list">
        {#each appState.activeAnomalies as anomaly, index}
          <div class="anomaly-item" class:critical={anomaly.severity === 'critical'} class:high={anomaly.severity === 'high'}>
            <span class="anomaly-type">{anomaly.severity.toUpperCase()}</span>
            <span class="anomaly-desc">{anomaly.description}</span>
            <button class="dismiss-btn" onclick={() => dismissAnomaly(index)}>×</button>
          </div>
        {/each}
      </div>
    </div>
  {/if}
  
  <!-- Metrics Alert Banner -->
  {#if appState.lastMetricsAlert}
    <div class="alert-banner" class:critical={appState.lastMetricsAlert.severity === 'high'}>
      <span class="alert-icon">🚨</span>
      <span class="alert-message">{appState.lastMetricsAlert.message}</span>
      <button class="dismiss-btn" onclick={dismissAlert}>×</button>
    </div>
  {/if}
  
  {#if error}
    <div class="error-banner">
      <span>⚠ {error}</span>
      <button onclick={() => { error = null; loadMetrics() }}>Retry</button>
    </div>
  {/if}
  
  {#if loading && !summary}
    <div class="loading">Loading metrics...</div>
  {:else if summary}
    <!-- Summary Cards -->
    <div class="summary-cards">
      <div class="card">
        <div class="card-label">Total Workflows</div>
        <div class="card-value">{summary.total_workflows}</div>
      </div>
      
      <div class="card">
        <div class="card-label">Success Rate</div>
        <div class="card-value success">{formatPercent(summary.success_rate)}</div>
      </div>
      
      <div class="card">
        <div class="card-label">Avg Duration</div>
        <div class="card-value">{formatDuration(summary.avg_workflow_duration_ms)}</div>
      </div>
      
      <div class="card">
        <div class="card-label">Tasks Completed</div>
        <div class="card-value">{summary.completed_tasks}/{summary.total_tasks}</div>
      </div>
    </div>
    
    <!-- Charts Grid -->
    <div class="charts-grid">
      <!-- Workflow Performance Trend -->
      <div class="chart-container full-width">
        <h3>Workflow Performance Trend</h3>
        {#if workflowTrendData}
          <Chart 
            type="line"
            data={workflowTrendData}
            options={{
              responsive: true,
              maintainAspectRatio: false,
              plugins: {
                legend: {
                  display: false
                }
              },
              scales: {
                y: {
                  beginAtZero: true,
                  grid: {
                    color: 'rgba(255, 255, 255, 0.1)'
                  }
                },
                x: {
                  grid: {
                    color: 'rgba(255, 255, 255, 0.1)'
                  }
                }
              }
            }}
          />
        {/if}
      </div>
      
      <!-- Agent Performance -->
      <div class="chart-container">
        <h3>Agent Performance</h3>
        {#if agentPerformanceData}
          <Chart 
            type="bar"
            data={agentPerformanceData}
            options={{
              responsive: true,
              maintainAspectRatio: false,
              plugins: {
                legend: {
                  display: false
                }
              },
              scales: {
                y: {
                  beginAtZero: true,
                  grid: {
                    color: 'rgba(255, 255, 255, 0.1)'
                  }
                },
                x: {
                  grid: {
                    display: false
                  }
                }
              }
            }}
          />
        {/if}
      </div>
      
      <!-- Success Rate Distribution -->
      <div class="chart-container">
        <h3>Success Rate Distribution</h3>
        {#if phaseDistributionData}
          <Chart 
            type="doughnut"
            data={phaseDistributionData}
            options={{
              responsive: true,
              maintainAspectRatio: false,
              plugins: {
                legend: {
                  position: 'bottom',
                  labels: {
                    color: '#8b949e',
                    padding: 15
                  }
                }
              }
            }}
          />
        {/if}
      </div>
      
      <!-- Task Completion by Domain -->
      <div class="chart-container full-width">
        <h3>Task Completion by Domain</h3>
        {#if taskCompletionData}
          <Chart 
            type="bar"
            data={taskCompletionData}
            options={{
              responsive: true,
              maintainAspectRatio: false,
              plugins: {
                legend: {
                  position: 'top',
                  labels: {
                    color: '#8b949e'
                  }
                }
              },
              scales: {
                x: {
                  stacked: true,
                  grid: {
                    display: false
                  }
                },
                y: {
                  stacked: true,
                  beginAtZero: true,
                  grid: {
                    color: 'rgba(255, 255, 255, 0.1)'
                  }
                }
              }
            }}
          />
        {/if}
      </div>
    </div>
    
    <!-- Historical Data Table -->
    {#if dailyMetrics.length > 0}
      <div class="historical-section">
        <h3>Daily Metrics History</h3>
        <div class="table-container">
          <table>
            <thead>
              <tr>
                <th>Date</th>
                <th>Workflows</th>
                <th>Completed</th>
                <th>Avg Duration</th>
                <th>Tasks</th>
                <th>Success Rate</th>
              </tr>
            </thead>
            <tbody>
              {#each [...dailyMetrics].reverse() as day}
                <tr>
                  <td>{formatDate(day.date)}</td>
                  <td>{day.total_workflows}</td>
                  <td>{day.completed_workflows}</td>
                  <td>{formatDuration(day.avg_workflow_duration_ms)}</td>
                  <td>{day.completed_tasks}/{day.total_tasks}</td>
                  <td class:success={day.success_rate >= 0.8} class:warning={day.success_rate < 0.8 && day.success_rate > 0} class:error={day.success_rate === 0 && day.total_tasks > 0}>
                    {formatPercent(day.success_rate)}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      </div>
    {/if}
    
    <!-- Agent Performance Dashboard -->
    {#if agentMetrics.length > 0}
      <div class="agent-section">
        <h3>Agent Performance</h3>
        <div class="agent-cards">
          {#each agentMetrics as agent}
            <div class="agent-card">
              <div class="agent-id">{agent.agent_id}</div>
              <div class="agent-stats">
                <div class="stat">
                  <span class="label">Tasks</span>
                  <span class="value">{agent.tasks_completed}</span>
                </div>
                <div class="stat">
                  <span class="label">Success</span>
                  <span class="value success">{formatPercent(agent.success_rate)}</span>
                </div>
                <div class="stat">
                  <span class="label">Avg Time</span>
                  <span class="value">{formatDuration(agent.avg_task_duration_ms)}</span>
                </div>
              </div>
              <div class="last-active">Last active: {formatDate(agent.last_active)}</div>
            </div>
          {/each}
        </div>
      </div>
    {/if}
  {:else}
    <div class="no-data">No metrics available yet. Complete some workflows to see analytics.</div>
  {/if}
</div>

<style>
  .analytics {
    padding: 20px;
    max-width: 1400px;
    margin: 0 auto;
  }
  
  header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 24px;
    flex-wrap: wrap;
    gap: 16px;
  }
  
  .header-left h1 {
    font-size: 28px;
    font-weight: 600;
    margin: 0 0 4px 0;
  }
  
  .subtitle {
    color: #8b949e;
    margin: 0;
    font-size: 14px;
  }
  
  .controls {
    display: flex;
    gap: 12px;
    align-items: center;
    flex-wrap: wrap;
  }
  
  .time-range {
    display: flex;
    gap: 4px;
    background: #161b22;
    padding: 4px;
    border-radius: 8px;
    border: 1px solid #30363d;
  }
  
  .time-range button {
    padding: 6px 16px;
    background: transparent;
    border: none;
    color: #8b949e;
    border-radius: 6px;
    cursor: pointer;
    transition: all 0.15s;
    font-size: 14px;
  }
  
  .time-range button:hover {
    color: #c9d1d9;
  }
  
  .time-range button.active {
    background: #21262d;
    color: #58a6ff;
  }
  
  .refresh-btn {
    padding: 6px 16px;
    background: #21262d;
    border: 1px solid #30363d;
    color: #c9d1d9;
    border-radius: 6px;
    cursor: pointer;
    font-size: 14px;
    transition: all 0.15s;
  }
  
  .refresh-btn:hover:not(:disabled) {
    background: #30363d;
  }
  
  .refresh-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  
  .export-btn {
    padding: 6px 16px;
    background: #238636;
    border: 1px solid #2ea043;
    color: #fff;
    border-radius: 6px;
    cursor: pointer;
    font-size: 14px;
    transition: all 0.15s;
  }
  
  .export-btn:hover:not(:disabled) {
    background: #2ea043;
  }
  
  .export-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  
  .error-banner {
    background: #2d1f1f;
    border: 1px solid #f85149;
    border-radius: 8px;
    padding: 12px 16px;
    margin-bottom: 20px;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  
  .error-banner button {
    background: transparent;
    border: 1px solid #f85149;
    color: #f85149;
    padding: 4px 12px;
    border-radius: 4px;
    cursor: pointer;
  }
  
  .loading {
    text-align: center;
    padding: 60px;
    color: #8b949e;
  }
  
  .no-data {
    text-align: center;
    padding: 60px;
    color: #8b949e;
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 8px;
  }
  
  .summary-cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 16px;
    margin-bottom: 24px;
  }
  
  .card {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 8px;
    padding: 20px;
    transition: border-color 0.15s;
  }
  
  .card:hover {
    border-color: #58a6ff;
  }
  
  .card-label {
    font-size: 12px;
    color: #8b949e;
    margin-bottom: 8px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  
  .card-value {
    font-size: 32px;
    font-weight: 600;
    color: #58a6ff;
  }
  
  .card-value.success {
    color: #3fb950;
  }
  
  .charts-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 20px;
  }
  
  .chart-container {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 8px;
    padding: 20px;
    min-height: 300px;
  }
  
  .chart-container.full-width {
    grid-column: 1 / -1;
  }
  
  .chart-container h3 {
    margin: 0 0 16px 0;
    font-size: 16px;
    font-weight: 600;
    color: #c9d1d9;
  }
  
  @media (max-width: 768px) {
    .charts-grid {
      grid-template-columns: 1fr;
    }
    
    .chart-container.full-width {
      grid-column: 1;
    }
    
    header {
      flex-direction: column;
      align-items: stretch;
    }
    
    .controls {
      justify-content: space-between;
    }
  }
  
  .historical-section {
    margin-top: 24px;
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 8px;
    padding: 20px;
  }
  
  .historical-section h3 {
    margin: 0 0 16px 0;
    font-size: 16px;
    font-weight: 600;
    color: #c9d1d9;
  }
  
  .table-container {
    overflow-x: auto;
  }
  
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 14px;
  }
  
  thead {
    background: #21262d;
  }
  
  th {
    padding: 12px;
    text-align: left;
    font-weight: 600;
    color: #8b949e;
    text-transform: uppercase;
    font-size: 12px;
    letter-spacing: 0.5px;
  }
  
  td {
    padding: 12px;
    border-top: 1px solid #30363d;
    color: #c9d1d9;
  }
  
  td.success {
    color: #3fb950;
  }
  
  td.warning {
    color: #f59e0b;
  }
  
  td.error {
    color: #f85149;
  }
  
  .agent-section {
    margin-top: 24px;
  }
  
  .agent-section h3 {
    margin: 0 0 16px 0;
    font-size: 16px;
    font-weight: 600;
    color: #c9d1d9;
  }
  
  .agent-cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 16px;
  }
  
  .agent-card {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 8px;
    padding: 20px;
    transition: border-color 0.15s;
  }
  
  .agent-card:hover {
    border-color: #58a6ff;
  }
  
  .agent-id {
    font-size: 16px;
    font-weight: 600;
    color: #58a6ff;
    margin-bottom: 16px;
  }
  
  .agent-stats {
    display: flex;
    gap: 16px;
    margin-bottom: 12px;
  }
  
  .stat {
    flex: 1;
  }
  
  .stat .label {
    display: block;
    font-size: 12px;
    color: #8b949e;
    margin-bottom: 4px;
  }
  
  .stat .value {
    display: block;
    font-size: 18px;
    font-weight: 600;
    color: #58a6ff;
  }
  
  .stat .value.success {
    color: #3fb950;
  }
  
  .last-active {
    font-size: 12px;
    color: #8b949e;
  }
  
  .live-indicator {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-left: 12px;
    padding: 4px 10px;
    background: #238636;
    color: #fff;
    font-size: 11px;
    font-weight: 600;
    border-radius: 12px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  
  .live-dot {
    width: 8px;
    height: 8px;
    background: #fff;
    border-radius: 50%;
    animation: pulse 2s ease-in-out infinite;
  }
  
  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }
  
  .anomaly-banner {
    background: #2d1f1f;
    border: 1px solid #f85149;
    border-radius: 8px;
    padding: 16px;
    margin-bottom: 20px;
  }
  
  .anomaly-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 12px;
    color: #f85149;
  }
  
  .anomaly-icon {
    font-size: 20px;
  }
  
  .dismiss-all-btn {
    margin-left: auto;
    background: transparent;
    border: 1px solid #f85149;
    color: #f85149;
    padding: 4px 12px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 12px;
    transition: all 0.15s;
  }
  
  .dismiss-all-btn:hover {
    background: #f85149;
    color: #fff;
  }
  
  .anomaly-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  
  .anomaly-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    background: #161b22;
    border-radius: 6px;
    border-left: 3px solid #f85149;
  }
  
  .anomaly-item.critical {
    border-left-color: #f85149;
    background: #2d1f1f;
  }
  
  .anomaly-item.high {
    border-left-color: #f59e0b;
  }
  
  .anomaly-type {
    padding: 2px 8px;
    background: #f85149;
    color: #fff;
    font-size: 10px;
    font-weight: 600;
    border-radius: 3px;
    text-transform: uppercase;
  }
  
  .anomaly-item.high .anomaly-type {
    background: #f59e0b;
  }
  
  .anomaly-desc {
    flex: 1;
    color: #c9d1d9;
    font-size: 13px;
  }
  
  .dismiss-btn {
    background: transparent;
    border: none;
    color: #8b949e;
    font-size: 20px;
    cursor: pointer;
    padding: 0 4px;
    line-height: 1;
    transition: color 0.15s;
  }
  
  .dismiss-btn:hover {
    color: #f85149;
  }
  
  .alert-banner {
    display: flex;
    align-items: center;
    gap: 12px;
    background: #2d1f1f;
    border: 1px solid #f85149;
    border-radius: 8px;
    padding: 12px 16px;
    margin-bottom: 20px;
    color: #f85149;
  }
  
  .alert-banner.critical {
    background: #f85149;
    color: #fff;
  }
  
  .alert-icon {
    font-size: 18px;
  }
  
  .alert-message {
    flex: 1;
    font-size: 14px;
    font-weight: 500;
  }
</style>
