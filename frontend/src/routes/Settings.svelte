<script lang="ts">
  import { onMount } from 'svelte'
  import { getGuardianConfig, updateGuardianConfig, testGuardianLLM, getPhaseRouting, updatePhaseRouting } from '$lib/api'
  import type { GuardianConfig, PhaseRoutingConfig } from '$lib/types'

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

  const defaultRouting: PhaseRoutingConfig = {
    enabled: false,
    bug: { analyze: '', fix: '', review: '' },
    spec: { plan: '', implement: '', verify: '', learn: '' },
    spec_complex: { plan: '', discovery: '', design: '', governance: '', implement: '', verify: '', learn: '' },
    swarm: { hub: '', workers: '' },
  }

  let routing = $state<PhaseRoutingConfig>(structuredClone(defaultRouting))
  let routingSaving = $state(false)
  let routingSaveMsg = $state<string | null>(null)
  let routingSaveError = $state<string | null>(null)

  let loading = $state(true)
  let saving = $state(false)
  let testing = $state(false)
  let saveMsg = $state<string | null>(null)
  let saveError = $state<string | null>(null)
  let testMsg = $state<string | null>(null)
  let testError = $state<string | null>(null)
  let apiKeyChanged = $state(false)

  onMount(async () => {
    try {
      const [guardianCfg, routingCfg] = await Promise.all([
        getGuardianConfig(),
        getPhaseRouting(),
      ])
      cfg = guardianCfg
      // Merge fetched routing over defaults so all phase keys are present
      routing = {
        ...defaultRouting,
        ...routingCfg,
        bug: { ...defaultRouting.bug, ...(routingCfg.bug ?? {}) },
        spec: { ...defaultRouting.spec, ...(routingCfg.spec ?? {}) },
        spec_complex: { ...defaultRouting.spec_complex, ...(routingCfg.spec_complex ?? {}) },
        swarm: { ...defaultRouting.swarm, ...(routingCfg.swarm ?? {}) },
      }
    } catch (e) {
      saveError = 'Failed to load config'
    } finally {
      loading = false
    }
  })

  async function saveRouting() {
    routingSaving = true
    routingSaveMsg = null
    routingSaveError = null
    try {
      routing = await updatePhaseRouting(routing)
      routingSaveMsg = 'Saved'
      setTimeout(() => { routingSaveMsg = null }, 3000)
    } catch (e) {
      routingSaveError = e instanceof Error ? e.message : 'Save failed'
    } finally {
      routingSaving = false
    }
  }

  const WORKFLOW_PHASES: Record<string, { key: keyof PhaseRoutingConfig; label: string; phases: string[] }> = {
    Bug: { key: 'bug', label: 'Bug Workflow', phases: ['analyze', 'fix', 'review'] },
    Spec: { key: 'spec', label: 'Spec Workflow (simple)', phases: ['plan', 'implement', 'verify', 'learn'] },
    'Spec Complex': { key: 'spec_complex', label: 'Spec Workflow (complex)', phases: ['plan', 'discovery', 'design', 'governance', 'implement', 'verify', 'learn'] },
    Swarm: { key: 'swarm', label: 'Swarm', phases: ['hub', 'workers'] },
  }

  function executorLabel(val: string) {
    if (val === 'cc') return 'Claude Code'
    if (val === 'oc') return 'OpenCode'
    return 'Any'
  }

  function isHandoff(phases: string[], phaseMap: Record<string, string>, i: number): boolean {
    if (i === 0) return false
    const prev = phaseMap[phases[i - 1]] ?? ''
    const curr = phaseMap[phases[i]] ?? ''
    return prev !== curr && (prev === 'cc' || prev === 'oc') && (curr === 'cc' || curr === 'oc')
  }

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
</script>

<div class="settings-root">
  <h2>Settings</h2>

  {#if loading}
    <p class="muted">Loading…</p>
  {:else}
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
        {saving ? 'Saving…' : 'Save settings'}
      </button>
      {#if saveMsg}
        <span class="ok-msg">{saveMsg}</span>
      {/if}
      {#if saveError}
        <span class="err-msg">{saveError}</span>
      {/if}
    </div>

    <section class="card">
      <h3>Phase Routing — CC / OC Collaboration</h3>
      <p class="section-desc">
        Assign individual workflow phases to Claude Code (CC) or OpenCode (OC).
        When enabled, the wrong executor is blocked and shown a handoff message.
        Requires <code>stratus init</code> (or <code>stratus init --target both</code>) to inject executor identity.
      </p>

      <div class="form-group row">
        <label class="checkbox-label">
          <input type="checkbox" bind:checked={routing.enabled} />
          Enable phase routing
        </label>
      </div>

      <div class:routing-disabled={!routing.enabled}>
        {#each Object.entries(WORKFLOW_PHASES) as [, wf]}
          <div class="routing-section">
            <div class="routing-title">{wf.label}</div>
            {#each wf.phases as phase, i}
              {#if isHandoff(wf.phases, (routing[wf.key] as Record<string, string>) ?? {}, i)}
                <div class="handoff-arrow" title="Handoff between executors here">⇄ handoff</div>
              {/if}
              <div class="routing-row">
                <span class="phase-label">{phase}</span>
                <select
                  bind:value={(routing[wf.key] as Record<string, string>)[phase]}
                  disabled={!routing.enabled}
                >
                  <option value="">Any</option>
                  <option value="cc">Claude Code</option>
                  <option value="oc">OpenCode</option>
                </select>
                <span class="executor-badge executor-{(routing[wf.key] as Record<string, string>)[phase] || 'any'}">
                  {executorLabel((routing[wf.key] as Record<string, string>)[phase] ?? '')}
                </span>
              </div>
            {/each}
          </div>
        {/each}
      </div>

      <div class="actions">
        <button class="btn-primary" onclick={saveRouting} disabled={routingSaving}>
          {routingSaving ? 'Saving…' : 'Save routing'}
        </button>
        {#if routingSaveMsg}
          <span class="ok-msg">{routingSaveMsg}</span>
        {/if}
        {#if routingSaveError}
          <span class="err-msg">{routingSaveError}</span>
        {/if}
      </div>
    </section>
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

  .routing-section {
    margin-bottom: 16px;
  }

  .routing-title {
    font-size: 0.78rem;
    color: #8b949e;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin-bottom: 6px;
  }

  .routing-row {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 6px;
  }

  .phase-label {
    width: 90px;
    font-size: 0.82rem;
    color: #c9d1d9;
    flex-shrink: 0;
  }

  .routing-row select {
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 4px;
    color: #c9d1d9;
    padding: 4px 8px;
    font-size: 0.82rem;
    width: 140px;
  }

  .routing-row select:disabled {
    opacity: 0.4;
  }

  .executor-badge {
    font-size: 0.72rem;
    padding: 2px 8px;
    border-radius: 10px;
    font-weight: 500;
  }

  .executor-cc {
    background: #1f3d6b;
    color: #79c0ff;
  }

  .executor-oc {
    background: #3d1f6b;
    color: #d2a8ff;
  }

  .executor-any {
    background: #21262d;
    color: #6e7681;
  }

  .handoff-arrow {
    font-size: 0.75rem;
    color: #f0883e;
    padding: 2px 0 4px 0;
    letter-spacing: 0.03em;
  }

  .routing-disabled {
    opacity: 0.45;
    pointer-events: none;
  }

  code {
    background: #0d1117;
    border: 1px solid #30363d;
    border-radius: 3px;
    padding: 1px 5px;
    font-size: 0.78rem;
    color: #79c0ff;
  }
</style>
