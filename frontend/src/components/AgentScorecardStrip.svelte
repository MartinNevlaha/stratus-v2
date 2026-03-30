<script lang="ts">
  import type { AgentScorecard } from '$lib/types'

  let { scorecard }: { scorecard: AgentScorecard | null | undefined } = $props()

  function formatCycleTime(ms: number): string {
    if (!ms) return '—'
    if (ms < 1000) return `${ms}ms`
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
    if (ms < 3600000) return `${(ms / 60000).toFixed(1)}m`
    return `${(ms / 3600000).toFixed(1)}h`
  }

  function trendArrow(trend: string): string {
    if (trend === 'improving') return '↑'
    if (trend === 'degrading') return '↓'
    return '→'
  }

  function trendColor(trend: string): string {
    if (trend === 'improving') return '#3fb950'
    if (trend === 'degrading') return '#f85149'
    return '#8b949e'
  }
</script>

{#if scorecard && scorecard.total_runs > 0}
  <div class="scorecard-strip">
    <div class="sc-metric">
      <div class="sc-bar-bg">
        <div class="sc-bar-fill" style="width: {Math.round(scorecard.success_rate * 100)}%"></div>
      </div>
      <span class="sc-val sc-success">{Math.round(scorecard.success_rate * 100)}%</span>
    </div>
    <div class="sc-divider"></div>
    <span class="sc-label">rework</span>
    <span class="sc-val" class:sc-bad={scorecard.rework_rate > 0.2}>
      {Math.round(scorecard.rework_rate * 100)}%
    </span>
    <div class="sc-divider"></div>
    <span class="sc-label">avg</span>
    <span class="sc-val">{formatCycleTime(scorecard.avg_cycle_time_ms)}</span>
    <div class="sc-divider"></div>
    <span class="sc-trend" style="color: {trendColor(scorecard.trend)}">{trendArrow(scorecard.trend)}</span>
    <span class="sc-runs">{scorecard.total_runs} runs</span>
  </div>
{/if}

<style>
  .scorecard-strip {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 0 2px;
    border-top: 1px solid #21262d;
    margin-top: 6px;
  }

  .sc-metric {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .sc-bar-bg {
    width: 48px;
    height: 4px;
    background: #21262d;
    border-radius: 2px;
    overflow: hidden;
  }

  .sc-bar-fill {
    height: 100%;
    background: #3fb950;
    border-radius: 2px;
  }

  .sc-label {
    font-size: 10px;
    color: #484f58;
  }

  .sc-val {
    font-size: 11px;
    color: #8b949e;
    font-weight: 600;
  }

  .sc-success { color: #3fb950; }
  .sc-bad { color: #f85149 !important; }

  .sc-divider {
    width: 1px;
    height: 10px;
    background: #30363d;
    flex-shrink: 0;
  }

  .sc-trend {
    font-size: 13px;
    font-weight: 700;
    flex-shrink: 0;
  }

  .sc-runs {
    font-size: 10px;
    color: #484f58;
    margin-left: auto;
  }
</style>
