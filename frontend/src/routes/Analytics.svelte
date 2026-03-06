<script lang="ts">
  import { onMount } from 'svelte'
  import { Line, Bar, Doughnut, Pie } from 'svelte-chartjs'
  import { appState } from '$lib/store'
  import { 
    getMetricsSummary, 
    getWorkflowMetrics, 
    triggerAggregation
  } from '$lib/api'
  import type { MetricsSummary, DailyMetric, AgentMetric, ProjectMetric } from '$lib/types'
  
  let loading = $state(true)
  let error = $state<string | null>(null)
  let timeRange = $state<'7d' | '30d' | '90d'>('7d')
  let summary = $state<MetricsSummary | null>(null)
  let dailyMetrics = $state<DailyMetric[]>([])
  let agentMetrics = $state<AgentMetric[]>([])
  let projectMetrics = $state<ProjectMetric[]>([])
  
  // Chart data
  let workflowTrendData = $state<any>(null)
  let agentPerformanceData = $state<any>(null)
  let phaseDistributionData = $state<any>(null)
  let taskCompletionData = $state<any>(null)
  
  onMount(async () => {
    await loadMetrics()
  })
  
  // Watch for real-time updates
  $effect(() => {
    if (appState.analyticsUpdateCounter > 0) {
      loadMetrics()
    }
  })
  
  async function loadMetrics() {
    loading = true
    error = null
    try {
      const days = timeRange === '7d' ? 7 : timeRange === '30d' ? 30 : 90
      const data = await getMetricsSummary(days)
      summary = data.summary
      
      // Generate mock chart data (will be replaced with real data from backend)
      generateChartData()
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load metrics'
      console.error('Failed to load metrics:', e)
    } finally {
      loading = false
    }
  }
  
  function generateChartData() {
    // Workflow Performance Trend (Line Chart)
    const labels = Array.from({ length: 7 }, (_, i) => {
      const date = new Date()
      date.setDate(date.getDate() - (6 - i))
      return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
    })
    
    workflowTrendData = {
      labels,
      datasets: [{
        label: 'Completed Workflows',
        data: [12, 19, 15, 25, 22, 30, summary?.completed_workflows || 0],
        borderColor: '#3b82f6',
        backgroundColor: 'rgba(59, 130, 246, 0.1)',
        tension: 0.4,
        fill: true,
      }]
    }
    
    // Agent Performance (Bar Chart)
    agentPerformanceData = {
      labels: ['Backend', 'Frontend', 'QA', 'Database', 'DevOps'],
      datasets: [{
        label: 'Tasks Completed',
        data: [45, 38, 32, 12, 18],
        backgroundColor: [
          '#3b82f6',
          '#10b981',
          '#f59e0b',
          '#ef4444',
          '#8b5cf6'
        ]
      }]
    }
    
    // Phase Distribution (Doughnut Chart)
    phaseDistributionData = {
      labels: ['Plan', 'Implement', 'Verify', 'Learn'],
      datasets: [{
        data: [20, 45, 25, 10],
        backgroundColor: [
          '#3b82f6',
          '#10b981',
          '#f59e0b',
          '#ef4444'
        ]
      }]
    }
    
    // Task Completion by Domain (Stacked Bar)
    taskCompletionData = {
      labels: ['Backend', 'Frontend', 'Database', 'Tests', 'Infra'],
      datasets: [
        {
          label: 'Successful',
          data: [42, 35, 10, 28, 15],
          backgroundColor: '#10b981'
        },
        {
          label: 'Failed',
          data: [3, 2, 2, 4, 1],
          backgroundColor: '#ef4444'
        }
      ]
    }
  }
  
  function formatDuration(ms: number): string {
    if (!ms) return '0s'
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
    return `${(value * 100).toFixed(1)}%`
  }
</script>

<div class="analytics">
  <header>
    <div class="header-left">
      <h1>Analytics Dashboard</h1>
      <p class="subtitle">Real-time insights into workflow performance</p>
    </div>
    
    <div class="controls">
      <div class="time-range">
        <button 
          class:active={timeRange === '7d'}
          onclick={() => { timeRange = '7d'; loadMetrics() }}
        >
          7 Days
        </button>
        <button 
          class:active={timeRange === '30d'}
          onclick={() => { timeRange = '30d'; loadMetrics() }}
        >
          30 Days
        </button>
        <button 
          class:active={timeRange === '90d'}
          onclick={() => { timeRange = '90d'; loadMetrics() }}
        >
          90 Days
        </button>
      </div>
      
      <button 
        class="refresh-btn"
        onclick={loadMetrics}
        disabled={loading}
      >
        {loading ? 'Loading...' : '↻ Refresh'}
      </button>
    </div>
  </header>
  
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
          <Line 
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
          <Bar 
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
      
      <!-- Phase Distribution -->
      <div class="chart-container">
        <h3>Phase Distribution</h3>
        {#if phaseDistributionData}
          <Doughnut 
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
          <Bar 
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
</style>
