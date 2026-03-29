<script lang="ts">
  import { onMount } from 'svelte'
  import { getGuardianConfig, updateGuardianConfig, testGuardianLLM, getInsightConfig, updateInsightConfig } from '$lib/api'
  import type { GuardianConfig, InsightConfig } from '$lib/types'

  let cfg = $state<GuardianConfig>({
    enabled: true,
    interval_minutes: 15,
    coverage_drift_pct: 5,
    stale_workflow_hours: 2,
    memory_threshold: 5000,
    tech_debt_threshold: 50,
    llm_endpoint: '',
    llm_api_key: '',
    llm_model: '',
    llm_temperature: 0.3,
    llm_max_tokens: 1024,
  })

  let insightCfg = $state<InsightConfig>({
    enabled: false,
    interval: 1,
    max_proposals: 5,
    min_confidence: 0.7,
    llm: { provider: '', model: '', api_key: '', base_url: '', timeout: 120, max_tokens: 16384, temperature: 0.7 },
  })

  let loading = $state(true)
  let saving = $state(false)
  let testing = $state(false)
  let saveMsg = $state<string | null>(null)
  let saveError = $state<string | null>(null)
  let testMsg = $state<string | null>(null)
  let testError = $state<string | null>(null)
  let apiKeyChanged = $state(false)

  let insightSaving = $state(false)
  let insightSaveMsg = $state<string | null>(null)
  let insightSaveError = $state<string | null>(null)
  let insightApiKeyChanged = $state(false)

  onMount(async () => {
    try {
      const [guardianCfg, iCfg] = await Promise.all([
        getGuardianConfig(),
        getInsightConfig(),
      ])
      cfg = guardianCfg
      insightCfg = iCfg
    } catch (e) {
      saveError = 'Failed to load config'
    } finally {
      loading = false
    }
  })

  async function save() {
    saving = true
    saveMsg = null
    saveError = null
    try {
      cfg = await updateGuardianConfig(cfg)
      saveMsg = 'Saved successfully'
      apiKeyChanged = false
      setTimeout(() => { saveMsg = null }, 3000)
    } catch (e) {
      saveError = e instanceof Error ? e.message : 'Save failed'
    } finally {
      saving = false
    }
  }

  async function testLLM() {
    testing = true
    testMsg = null
    testError = null
    try {
      await testGuardianLLM({
        llm_endpoint: cfg.llm_endpoint,
        llm_api_key: apiKeyChanged ? cfg.llm_api_key : undefined,
        llm_model: cfg.llm_model,
        llm_temperature: cfg.llm_temperature,
        llm_max_tokens: cfg.llm_max_tokens,
      })
      testMsg = 'Connection successful'
      setTimeout(() => { testMsg = null }, 4000)
    } catch (e) {
      testError = e instanceof Error ? e.message : 'Test failed'
    } finally {
      testing = false
    }
  }

  async function saveInsight() {
    insightSaving = true
    insightSaveMsg = null
    insightSaveError = null
    try {
      insightCfg = await updateInsightConfig(insightCfg)
      insightSaveMsg = 'Saved — restart stratus serve to apply'
      insightApiKeyChanged = false
      setTimeout(() => { insightSaveMsg = null }, 5000)
    } catch (e) {
      insightSaveError = e instanceof Error ? e.message : 'Save failed'
    } finally {
      insightSaving = false
    }
  }
</script>

<div class="settings-root">
  <h2>Settings</h2>

  {#if loading}
    <p class="muted">Loading…</p>
  {:else}
    <section class="card">
      <h3>Insight — AI Coach</h3>
      <p class="section-desc">
        Autonomous pattern detection, proposal generation, scorecards, and routing recommendations.
        Requires server restart after enabling/disabling.
      </p>

      <div class="form-group row">
        <label class="checkbox-label">
          <input type="checkbox" bind:checked={insightCfg.enabled} />
          Enabled
        </label>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label>Analysis interval (hours)</label>
          <input type="number" min="1" max="168" bind:value={insightCfg.interval} />
        </div>
        <div class="form-group">
          <label>Max proposals per run</label>
          <input type="number" min="1" max="50" bind:value={insightCfg.max_proposals} />
        </div>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label>Min confidence (0–1)</label>
          <input type="number" min="0" max="1" step="0.05" bind:value={insightCfg.min_confidence} />
        </div>
      </div>

      <div class="insight-llm-section">
        <h4>LLM (optional — for Product Intelligence)</h4>

        <div class="form-row">
          <div class="form-group">
            <label>Provider</label>
            <input type="text" placeholder="anthropic, openai, zai" bind:value={insightCfg.llm.provider} />
          </div>
          <div class="form-group">
            <label>Model</label>
            <input type="text" placeholder="claude-3-5-haiku, gpt-4o" bind:value={insightCfg.llm.model} />
          </div>
        </div>

        <div class="form-group">
          <label>Base URL</label>
          <input type="url" placeholder="https://api.anthropic.com" bind:value={insightCfg.llm.base_url} />
        </div>

        <div class="form-group">
          <label>API Key</label>
          <input
            type="password"
            placeholder={insightCfg.llm.api_key === '***' ? '(saved — enter new value to change)' : ''}
            bind:value={insightCfg.llm.api_key}
            oninput={() => { insightApiKeyChanged = true }}
          />
        </div>

        <div class="form-row">
          <div class="form-group">
            <label>Temperature</label>
            <input type="number" min="0" max="2" step="0.05" bind:value={insightCfg.llm.temperature} />
          </div>
          <div class="form-group">
            <label>Max tokens</label>
            <input type="number" min="64" max="65536" bind:value={insightCfg.llm.max_tokens} />
          </div>
          <div class="form-group">
            <label>Timeout (seconds)</label>
            <input type="number" min="10" max="600" bind:value={insightCfg.llm.timeout} />
          </div>
        </div>
      </div>

      <div class="actions">
        <button class="btn-primary" onclick={saveInsight} disabled={insightSaving}>
          {insightSaving ? 'Saving…' : 'Save Insight settings'}
        </button>
        {#if insightSaveMsg}
          <span class="ok-msg">{insightSaveMsg}</span>
        {/if}
        {#if insightSaveError}
          <span class="err-msg">{insightSaveError}</span>
        {/if}
      </div>
    </section>

    <section class="card">
      <h3>Guardian — Ambient Codebase Monitor</h3>
      <p class="section-desc">Background checks that surface codebase health issues proactively.</p>

      <div class="form-group row">
        <label class="checkbox-label">
          <input type="checkbox" bind:checked={cfg.enabled} />
          Enabled
        </label>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label>Check interval (minutes)</label>
          <input type="number" min="1" max="1440" bind:value={cfg.interval_minutes} />
        </div>
        <div class="form-group">
          <label>Stale workflow threshold (hours)</label>
          <input type="number" min="1" bind:value={cfg.stale_workflow_hours} />
        </div>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label>Coverage drift alert (% drop)</label>
          <input type="number" min="0.1" step="0.1" bind:value={cfg.coverage_drift_pct} />
        </div>
        <div class="form-group">
          <label>Memory threshold (event count)</label>
          <input type="number" min="100" bind:value={cfg.memory_threshold} />
        </div>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label>Tech debt alert (new files since baseline)</label>
          <input type="number" min="1" bind:value={cfg.tech_debt_threshold} />
        </div>
      </div>
    </section>

    <section class="card">
      <h3>LLM — OpenAI-compatible endpoint</h3>
      <p class="section-desc">
        Used by the Guardian for intelligent governance violation analysis.
        Compatible with any OpenAI-compatible API (GLM, GPT-4o, Claude via proxy, etc.).
        Leave blank to disable LLM-assisted checks (FTS-only fallback).
      </p>

      <div class="form-group">
        <label>Endpoint URL</label>
        <input
          type="url"
          placeholder="https://api.z.ai/api/coding/paas/v4"
          bind:value={cfg.llm_endpoint}
        />
      </div>

      <div class="form-group">
        <label>API Key</label>
        <input
          type="password"
          placeholder={cfg.llm_api_key === '***' ? '(saved — enter new value to change)' : ''}
          bind:value={cfg.llm_api_key}
          oninput={() => { apiKeyChanged = true }}
        />
      </div>

      <div class="form-group">
        <label>Model</label>
        <input
          type="text"
          placeholder="glm-5.1"
          bind:value={cfg.llm_model}
        />
        <span class="hint">Examples: glm-5.1, glm-4.7, gpt-4o, claude-3-5-haiku-20241022</span>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label>Temperature</label>
          <input type="number" min="0" max="2" step="0.05" bind:value={cfg.llm_temperature} />
        </div>
        <div class="form-group">
          <label>Max tokens</label>
          <input type="number" min="64" max="32768" bind:value={cfg.llm_max_tokens} />
        </div>
      </div>

      <div class="test-row">
        <button class="btn-secondary" onclick={testLLM} disabled={testing || !cfg.llm_endpoint || !cfg.llm_model}>
          {testing ? 'Testing…' : 'Test connection'}
        </button>
        {#if testMsg}
          <span class="ok-msg">{testMsg}</span>
        {/if}
        {#if testError}
          <span class="err-msg">{testError}</span>
        {/if}
      </div>
    </section>

    <div class="actions">
      <button class="btn-primary" onclick={save} disabled={saving}>
        {saving ? 'Saving…' : 'Save Guardian settings'}
      </button>
      {#if saveMsg}
        <span class="ok-msg">{saveMsg}</span>
      {/if}
      {#if saveError}
        <span class="err-msg">{saveError}</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .settings-root {
    padding: 24px;
    max-width: 720px;
    color: #c9d1d9;
  }

  h2 {
    font-size: 1.2rem;
    color: #e6edf3;
    margin: 0 0 20px;
  }

  h3 {
    font-size: 0.95rem;
    color: #e6edf3;
    margin: 0 0 4px;
  }

  h4 {
    font-size: 0.85rem;
    color: #8b949e;
    margin: 12px 0 8px;
  }

  .section-desc {
    font-size: 0.78rem;
    color: #8b949e;
    margin: 0 0 16px;
    line-height: 1.5;
  }

  .card {
    background: #161b22;
    border: 1px solid #30363d;
    border-radius: 6px;
    padding: 20px;
    margin-bottom: 16px;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 14px;
    flex: 1;
  }

  .form-group.row {
    flex-direction: row;
    align-items: center;
  }

  .form-row {
    display: flex;
    gap: 16px;
  }

  label {
    font-size: 0.8rem;
    color: #8b949e;
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 0.85rem;
    color: #c9d1d9;
    cursor: pointer;
  }

  input[type="text"],
  input[type="url"],
  input[type="password"],
  input[type="number"] {
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 4px;
    color: #c9d1d9;
    padding: 6px 10px;
    font-size: 0.85rem;
    outline: none;
    width: 100%;
    box-sizing: border-box;
  }

  input:focus {
    border-color: #58a6ff;
  }

  .hint {
    font-size: 0.73rem;
    color: #6e7681;
  }

  .test-row {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 4px;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 16px;
    margin-top: 8px;
  }

  .btn-primary {
    background: #238636;
    color: #fff;
    border: none;
    border-radius: 6px;
    padding: 8px 20px;
    font-size: 0.85rem;
    cursor: pointer;
  }

  .btn-primary:hover:not(:disabled) { background: #2ea043; }
  .btn-primary:disabled { opacity: 0.5; cursor: default; }

  .btn-secondary {
    background: #21262d;
    color: #c9d1d9;
    border: 1px solid #30363d;
    border-radius: 6px;
    padding: 6px 14px;
    font-size: 0.82rem;
    cursor: pointer;
  }

  .btn-secondary:hover:not(:disabled) { background: #30363d; }
  .btn-secondary:disabled { opacity: 0.5; cursor: default; }

  .ok-msg { color: #3fb950; font-size: 0.82rem; }
  .err-msg { color: #f85149; font-size: 0.82rem; }

  .muted { color: #8b949e; font-size: 0.85rem; }

  .insight-llm-section {
    border-top: 1px solid #21262d;
    margin-top: 8px;
    padding-top: 4px;
  }
</style>
