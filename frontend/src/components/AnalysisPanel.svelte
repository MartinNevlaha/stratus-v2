<script lang="ts">
  import { analyzeWorkflow } from '$lib/api'
  import type { AnalysisResult } from '$lib/types'

  let description = $state('')
  let result = $state<AnalysisResult | null>(null)
  let loading = $state(false)
  let error = $state<string | null>(null)
  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let llmCopied = $state(false)

  function onInput() {
    if (debounceTimer) clearTimeout(debounceTimer)
    result = null
    error = null
    if (description.trim().length < 5) return
    debounceTimer = setTimeout(runAnalysis, 600)
  }

  async function runAnalysis() {
    if (description.trim().length < 5) return
    loading = true
    error = null
    try {
      result = await analyzeWorkflow(description.trim())
    } catch (e) {
      error = e instanceof Error ? e.message : 'Analysis failed'
    } finally {
      loading = false
    }
  }

  function useRecommendations() {
    if (!result) return
    navigator.clipboard?.writeText(
      `/${result.recommended_type}${result.recommended_complexity === 'complex' ? '-complex' : ''} ${description}`
    ).catch(() => {})
  }

  function riskColor(level: string) {
    if (level === 'high') return '#f85149'
    if (level === 'medium') return '#e3b341'
    return '#3fb950'
  }

  function riskBarWidth(score: number) {
    return `${Math.round(score * 100)}%`
  }
</script>

<div class="panel">
  <div class="panel-header">
    <span class="panel-title">Risk Analyzer</span>
    <span class="hint">Type a task description to get recommendations before starting a workflow</span>
  </div>

  <div class="input-row">
    <input
      type="text"
      placeholder="e.g. Migrate auth from sessions to JWT, update database schema"
      bind:value={description}
      oninput={onInput}
      class="desc-input"
    />
    {#if loading}
      <span class="spinner"></span>
    {/if}
  </div>

  {#if error}
    <div class="error">{error}</div>
  {/if}

  {#if result}
    <div class="result">
      <!-- Risk score bar -->
      <div class="risk-row">
        <span class="risk-label">Risk</span>
        <div class="risk-bar-bg">
          <div
            class="risk-bar-fill"
            style="width: {riskBarWidth(result.risk_score)}; background: {riskColor(result.risk_level)}"
          ></div>
        </div>
        <span class="risk-badge" style="color: {riskColor(result.risk_level)}">
          {result.risk_level.toUpperCase()} ({result.risk_score.toFixed(2)})
        </span>
      </div>

      <!-- Risk factors -->
      <div class="factors">
        {#each result.risk_factors as factor}
          <div class="factor">· {factor}</div>
        {/each}
      </div>

      <!-- Recommendations -->
      <div class="recs">
        <div class="rec-row">
          <span class="rec-label">Recommended</span>
          <span class="badge type">{result.recommended_type}</span>
          <span class="badge complexity">{result.recommended_complexity}</span>
          <span class="badge strategy">{result.recommended_strategy}</span>
          <span class="rec-duration">~{result.estimated_duration_min} min</span>
        </div>
        {#if result.suggested_domains.length > 0}
          <div class="domains">
            Domains: {result.suggested_domains.join(', ')}
          </div>
        {/if}
      </div>

      <!-- Copy command button -->
      <div class="actions">
        <button class="copy-btn" onclick={useRecommendations} title="Copy slash command to clipboard">
          Copy /{result.recommended_type}{result.recommended_complexity === 'complex' ? '-complex' : ''} command
        </button>
      </div>

      <!-- LLM Analysis -->
      {#if result.llm_analysis}
        <div class="llm-analysis">
          <div class="llm-header">
            <span class="llm-title">AI Analysis</span>
            <button class="copy-llm-btn" onclick={() => { navigator.clipboard.writeText(result?.llm_analysis ?? ''); llmCopied = true; setTimeout(() => { llmCopied = false }, 2000) }}>
              {llmCopied ? 'Copied!' : 'Copy'}
            </button>
          </div>
          <div class="llm-content">{result.llm_analysis}</div>
        </div>
      {/if}

      <!-- Similar past workflows -->
      {#if result.similar_past_workflows.length > 0}
        <div class="similar">
          <div class="similar-title">Similar past</div>
          {#each result.similar_past_workflows as sw}
            <div class="similar-item">
              <span class="similar-icon" class:aborted={sw.aborted}>{sw.aborted ? '✗' : '✓'}</span>
              <span class="similar-title-text">{sw.title || sw.id}</span>
              <span class="similar-dur">{sw.duration_min} min</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .panel {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 8px;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-bottom: 16px;
  }

  .panel-header {
    display: flex;
    align-items: baseline;
    gap: 10px;
  }

  .panel-title {
    font-size: 12px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: #8b949e;
  }

  .hint {
    font-size: 11px;
    color: #484f58;
  }

  .input-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .desc-input {
    flex: 1;
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 6px;
    color: #c9d1d9;
    font-size: 13px;
    padding: 7px 10px;
    outline: none;
    transition: border-color 0.15s;
  }

  .desc-input:focus {
    border-color: #58a6ff;
  }

  .desc-input::placeholder {
    color: #484f58;
  }

  .spinner {
    width: 14px;
    height: 14px;
    border: 2px solid #30363d;
    border-top-color: #58a6ff;
    border-radius: 50%;
    animation: spin 0.7s linear infinite;
    flex-shrink: 0;
  }

  @keyframes spin { to { transform: rotate(360deg); } }

  .error {
    font-size: 12px;
    color: #f85149;
  }

  .result {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .risk-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .risk-label {
    font-size: 11px;
    color: #8b949e;
    width: 30px;
    flex-shrink: 0;
  }

  .risk-bar-bg {
    flex: 1;
    height: 6px;
    background: #21262d;
    border-radius: 3px;
    overflow: hidden;
  }

  .risk-bar-fill {
    height: 100%;
    border-radius: 3px;
    transition: width 0.3s ease, background 0.3s ease;
  }

  .risk-badge {
    font-size: 11px;
    font-weight: 700;
    white-space: nowrap;
  }

  .factors {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .factor {
    font-size: 11px;
    color: #8b949e;
  }

  .recs {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .rec-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .rec-label {
    font-size: 11px;
    color: #8b949e;
    margin-right: 2px;
  }

  .badge {
    font-size: 10px;
    font-weight: 700;
    padding: 2px 6px;
    border-radius: 4px;
    text-transform: uppercase;
  }

  .badge.type { background: #1f3056; color: #58a6ff; }
  .badge.complexity { background: #2d1f56; color: #a371f7; }
  .badge.strategy { background: #1f3a2d; color: #3fb950; }

  .rec-duration {
    font-size: 11px;
    color: #8b949e;
    margin-left: auto;
  }

  .domains {
    font-size: 11px;
    color: #8b949e;
  }

  .actions {
    display: flex;
  }

  .copy-btn {
    font-size: 11px;
    padding: 4px 10px;
    background: #21262d;
    border: 1px solid #30363d;
    border-radius: 5px;
    color: #c9d1d9;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .copy-btn:hover {
    background: #30363d;
    border-color: #58a6ff;
  }

  .llm-analysis {
    border-top: 1px solid #21262d;
    padding-top: 8px;
  }

  .llm-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 4px;
  }

  .llm-title {
    font-size: 11px;
    color: #a371f7;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-weight: 700;
  }

  .copy-llm-btn {
    font-size: 10px;
    padding: 2px 8px;
    background: #21262d;
    border: 1px solid #30363d;
    border-radius: 4px;
    color: #8b949e;
    cursor: pointer;
  }

  .copy-llm-btn:hover { background: #2a3040; color: #c9d1d9; }

  .llm-content {
    font-size: 12px;
    color: #c9d1d9;
    line-height: 1.5;
    white-space: pre-wrap;
  }

  .similar {
    display: flex;
    flex-direction: column;
    gap: 3px;
    border-top: 1px solid #21262d;
    padding-top: 8px;
  }

  .similar-title {
    font-size: 11px;
    color: #484f58;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    margin-bottom: 2px;
  }

  .similar-item {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    color: #8b949e;
  }

  .similar-icon {
    font-size: 10px;
    color: #3fb950;
    flex-shrink: 0;
  }

  .similar-icon.aborted {
    color: #f85149;
  }

  .similar-title-text {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .similar-dur {
    flex-shrink: 0;
    color: #484f58;
  }
</style>
