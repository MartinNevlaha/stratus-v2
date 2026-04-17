<script lang="ts">
  import { onMount } from 'svelte'
  import {
    getGuardianConfig,
    updateGuardianConfig,
    testGuardianLLM,
    getInsightConfig,
    updateInsightConfig,
    getLLMConfig,
    updateLLMConfig,
    getWikiConfig,
    updateWikiConfig,
    getLanguage,
    setLanguage,
    getEvolutionConfig,
    updateEvolutionConfig,
  } from '$lib/api'
  import type { GuardianConfig, InsightConfig, Language, LLMConfig, WikiConfig, EvolutionConfig } from '$lib/types'
  import { appState } from '$lib/store.svelte'

  const emptyLLM = (): LLMConfig => ({
    provider: '',
    model: '',
    api_key: '',
    base_url: '',
    timeout: 0,
    max_tokens: 0,
    temperature: 0,
    max_retries: 0,
    concurrency: 0,
  })

  let globalLLM = $state<LLMConfig>(emptyLLM())
  let globalLLMMsg = $state<string | null>(null)
  let globalLLMError = $state<string | null>(null)
  let globalLLMSaving = $state(false)
  let globalLLMApiKeyChanged = $state(false)

  let cfg = $state<GuardianConfig>({
    enabled: true,
    interval_minutes: 15,
    coverage_drift_pct: 5,
    stale_workflow_hours: 2,
    memory_threshold: 5000,
    tech_debt_threshold: 50,
    reviewer_timeout_minutes: 30,
    ticket_timeout_minutes: 30,
    llm: emptyLLM(),
  })

  let insightCfg = $state<InsightConfig>({
    enabled: false,
    interval: 1,
    max_proposals: 5,
    min_confidence: 0.7,
    llm: { provider: '', model: '', api_key: '', base_url: '', timeout: 120, max_tokens: 16384, temperature: 0.7, max_retries: 0, concurrency: 0 },
  })

  let loading = $state(true)
  let saving = $state(false)
  let testing = $state(false)
  let saveMsg = $state<string | null>(null)
  let saveError = $state<string | null>(null)
  let testMsg = $state<string | null>(null)
  let testError = $state<string | null>(null)

  let insightSaving = $state(false)
  let insightSaveMsg = $state<string | null>(null)
  let insightSaveError = $state<string | null>(null)
  let insightApiKeyChanged = $state(false)

  let guardianOverride = $state(false)
  let insightOverride = $state(false)

  let language = $state<Language>('en')
  let langSaving = $state(false)
  let langSaveMsg = $state<string | null>(null)
  let langSaveError = $state<string | null>(null)

  let wikiCfg = $state<WikiConfig>({
    enabled: false,
    ingest_on_event: true,
    max_pages_per_ingest: 20,
    staleness_threshold: 0.7,
    max_page_size_tokens: 4096,
    vault_path: '',
    vault_sync_on_save: true,
    onboarding_depth: 'standard',
    onboarding_max_pages: 20,
  })
  let wikiSaving = $state(false)
  let wikiSaveMsg = $state<string | null>(null)
  let wikiSaveError = $state<string | null>(null)

  let evolutionCfg = $state<EvolutionConfig | null>(null)
  let evolutionSaving = $state(false)
  let evolutionSaveMsg = $state<string | null>(null)
  let evolutionSaveError = $state<string | null>(null)

  onMount(async () => {
    try {
      const [guardianCfg, iCfg, llmCfg, wCfg, langRes, evoCfg] = await Promise.all([
        getGuardianConfig(),
        getInsightConfig(),
        getLLMConfig(),
        getWikiConfig(),
        getLanguage(),
        getEvolutionConfig(),
      ])
      cfg = guardianCfg
      insightCfg = iCfg
      globalLLM = llmCfg
      wikiCfg = wCfg
      language = langRes.language as Language
      evolutionCfg = evoCfg
      guardianOverride = cfg.llm.provider !== '' || cfg.llm.base_url !== ''
      insightOverride = insightCfg.llm.provider !== '' || insightCfg.llm.base_url !== ''
    } catch (e) {
      saveError = 'Failed to load config'
    } finally {
      loading = false
    }
  })

  async function saveLang() {
    langSaving = true
    langSaveMsg = null
    langSaveError = null
    try {
      const res = await setLanguage(language)
      language = res.language as Language
      appState.language = language
      langSaveMsg = 'Saved successfully'
      setTimeout(() => { langSaveMsg = null }, 3000)
    } catch (e) {
      langSaveError = e instanceof Error ? e.message : 'Save failed'
    } finally {
      langSaving = false
    }
  }

  async function saveEvolution() {
    if (!evolutionCfg) return
    evolutionSaving = true
    evolutionSaveMsg = null
    evolutionSaveError = null
    try {
      evolutionCfg = await updateEvolutionConfig(evolutionCfg)
      evolutionSaveMsg = 'Saved successfully'
      setTimeout(() => { evolutionSaveMsg = null }, 3000)
    } catch (e) {
      evolutionSaveError = e instanceof Error ? e.message : 'Save failed'
    } finally {
      evolutionSaving = false
    }
  }

  async function saveWiki() {
    wikiSaving = true
    wikiSaveMsg = null
    wikiSaveError = null
    try {
      wikiCfg = await updateWikiConfig(wikiCfg)
      wikiSaveMsg = 'Saved — vault sync ready immediately'
      setTimeout(() => { wikiSaveMsg = null }, 4000)
    } catch (e) {
      wikiSaveError = e instanceof Error ? e.message : 'Save failed'
    } finally {
      wikiSaving = false
    }
  }

  async function saveGlobalLLM() {
    globalLLMSaving = true
    globalLLMMsg = null
    globalLLMError = null
    try {
      globalLLM = await updateLLMConfig(globalLLM)
      globalLLMMsg = 'Saved successfully'
      globalLLMApiKeyChanged = false
      setTimeout(() => { globalLLMMsg = null }, 3000)
    } catch (e) {
      globalLLMError = e instanceof Error ? e.message : 'Save failed'
    } finally {
      globalLLMSaving = false
    }
  }

  async function save() {
    saving = true
    saveMsg = null
    saveError = null
    try {
      const payload: GuardianConfig = { ...cfg }
      if (!guardianOverride) {
        payload.llm = emptyLLM()
      }
      cfg = await updateGuardianConfig(payload)
      saveMsg = 'Saved successfully'
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
      await testGuardianLLM(cfg.llm)
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
      const payload: InsightConfig = { ...insightCfg }
      if (!insightOverride) {
        payload.llm = emptyLLM()
      }
      insightCfg = await updateInsightConfig(payload)
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
    <p class="muted">Loading...</p>
  {:else}
    <section class="card">
      <h3>General</h3>
      <p class="section-desc">Application-wide preferences.</p>

      <div class="form-row">
        <div class="form-group">
          <label for="output-language">Output Language / Jazyk výstupu</label>
          <select
            id="output-language"
            bind:value={language}
            onchange={saveLang}
            disabled={langSaving}
          >
            <option value="en">English</option>
            <option value="sk">Slovenčina</option>
          </select>
          <span class="hint">Controls the language of Evolution hypotheses and Guardian/Code Analysis findings.</span>
        </div>
      </div>

      {#if langSaveMsg}
        <span class="ok-msg">{langSaveMsg}</span>
      {/if}
      {#if langSaveError}
        <span class="err-msg">{langSaveError}</span>
      {/if}
    </section>

    <section class="card">
      <h3>Global LLM</h3>
      <p class="section-desc">
        Default LLM endpoint used by Insight, Guardian, and wiki/evolution subsystems.
        Subsystems can override individual fields.
      </p>

      <div class="form-row">
        <div class="form-group">
          <label for="global-provider">Provider</label>
          <select id="global-provider" bind:value={globalLLM.provider}>
            <option value="">— none —</option>
            <option value="zai">zai</option>
            <option value="anthropic">anthropic</option>
            <option value="openai">openai</option>
            <option value="ollama">ollama</option>
          </select>
        </div>
        <div class="form-group">
          <label for="global-model">Model</label>
          <input id="global-model" type="text" placeholder="claude-3-5-haiku, gpt-4o" bind:value={globalLLM.model} />
        </div>
      </div>

      <div class="form-group">
        <label for="global-base-url">Base URL</label>
        <input id="global-base-url" type="url" placeholder="https://api.anthropic.com" bind:value={globalLLM.base_url} />
      </div>

      <div class="form-group">
        <label for="global-api-key">API Key</label>
        <input
          id="global-api-key"
          type="password"
          placeholder={globalLLM.api_key === '***' ? '(saved — enter new value to change)' : ''}
          bind:value={globalLLM.api_key}
          oninput={() => { globalLLMApiKeyChanged = true }}
        />
      </div>

      <div class="form-row">
        <div class="form-group">
          <label for="global-timeout">Timeout (s)</label>
          <input id="global-timeout" type="number" min="0" max="600" bind:value={globalLLM.timeout} />
        </div>
        <div class="form-group">
          <label for="global-max-tokens">Max tokens</label>
          <input id="global-max-tokens" type="number" min="0" max="200000" bind:value={globalLLM.max_tokens} />
        </div>
        <div class="form-group">
          <label for="global-temperature">Temperature</label>
          <input id="global-temperature" type="number" min="0" max="2" step="0.05" bind:value={globalLLM.temperature} />
        </div>
        <div class="form-group">
          <label for="global-max-retries">Max retries</label>
          <input id="global-max-retries" type="number" min="0" max="10" bind:value={globalLLM.max_retries} />
        </div>
        <div class="form-group">
          <label for="global-concurrency">Concurrency</label>
          <input id="global-concurrency" type="number" min="0" max="100" bind:value={globalLLM.concurrency} />
        </div>
        <div class="form-group">
          <label for="global-min-interval">Min request interval (ms)</label>
          <input id="global-min-interval" type="number" min="0" max="60000" step="500" bind:value={globalLLM.min_request_interval_ms} />
          <small>0 = disabled. Use to stay under provider rate limits (e.g. 3000 = max ~20 req/min).</small>
        </div>
      </div>

      <div class="actions">
        <button class="btn-primary" onclick={saveGlobalLLM} disabled={globalLLMSaving}>
          {globalLLMSaving ? 'Saving...' : 'Save Global LLM'}
        </button>
        {#if globalLLMMsg}
          <span class="ok-msg">{globalLLMMsg}</span>
        {/if}
        {#if globalLLMError}
          <span class="err-msg">{globalLLMError}</span>
        {/if}
      </div>
    </section>

    <section class="card">
      <h3>Wiki — Obsidian Vault</h3>
      <p class="section-desc">
        Export published wiki pages to a local Obsidian vault directory. The path is applied
        immediately — no server restart required. Leave empty to disable vault sync.
      </p>

      <div class="form-group">
        <label for="wiki-vault-path">Vault path (absolute)</label>
        <input
          id="wiki-vault-path"
          type="text"
          placeholder="/home/you/Documents/ObsidianVault"
          bind:value={wikiCfg.vault_path}
        />
        <span class="hint">
          Absolute path. Created if it doesn't exist. Leave empty to disable.
        </span>
      </div>

      <div class="form-group row">
        <label class="checkbox-label">
          <input type="checkbox" bind:checked={wikiCfg.vault_sync_on_save} />
          Auto-sync on wiki page save
        </label>
      </div>

      <div class="form-row">
        <div class="form-group">
          <label for="wiki-max-pages">Max pages per ingest</label>
          <input id="wiki-max-pages" type="number" min="0" max="1000" bind:value={wikiCfg.max_pages_per_ingest} />
        </div>
        <div class="form-group">
          <label for="wiki-staleness">Staleness threshold (0-1)</label>
          <input id="wiki-staleness" type="number" min="0" max="1" step="0.05" bind:value={wikiCfg.staleness_threshold} />
        </div>
      </div>

      <div class="actions">
        <button class="btn-primary" onclick={saveWiki} disabled={wikiSaving}>
          {wikiSaving ? 'Saving...' : 'Save Wiki settings'}
        </button>
        {#if wikiSaveMsg}
          <span class="ok-msg">{wikiSaveMsg}</span>
        {/if}
        {#if wikiSaveError}
          <span class="err-msg">{wikiSaveError}</span>
        {/if}
      </div>
    </section>

    <section class="card">
      <h3>Evolution — Self-Improvement</h3>
      <p class="section-desc">
        Configure the autonomous evolution loop that generates improvement proposals for this project.
        Full evolution settings are available in the Evolution tab.
      </p>

      {#if evolutionCfg}
        <div class="form-group row">
          <label class="checkbox-label" for="evo-stratus-self">
            <input
              id="evo-stratus-self"
              type="checkbox"
              bind:checked={evolutionCfg.stratus_self_enabled}
            />
            Allow Stratus-self evolution (prompt_tuning)
            <span
              class="tooltip-icon"
              title="When enabled, the evolution loop may also generate prompt_tuning hypotheses that tune Stratus's own agent prompts. This is a low-priority opt-in category. Takes effect on the next evolution run."
            >ⓘ</span>
          </label>
        </div>
        <p class="hint" style="margin-bottom: 12px;">
          Enables LLM self-tuning — evolution targets Stratus's own prompts in addition to project
          code. Low-priority category. Does not require a server restart.
        </p>

        <div class="actions">
          <button class="btn-primary" onclick={saveEvolution} disabled={evolutionSaving}>
            {evolutionSaving ? 'Saving...' : 'Save Evolution settings'}
          </button>
          {#if evolutionSaveMsg}
            <span class="ok-msg">{evolutionSaveMsg}</span>
          {/if}
          {#if evolutionSaveError}
            <span class="err-msg">{evolutionSaveError}</span>
          {/if}
        </div>
      {:else}
        <p class="muted">Loading evolution config…</p>
      {/if}
    </section>

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
          <label>Min confidence (0-1)</label>
          <input type="number" min="0" max="1" step="0.05" bind:value={insightCfg.min_confidence} />
        </div>
      </div>

      <div class="insight-llm-section">
        <label class="checkbox-label">
          <input type="checkbox" bind:checked={insightOverride} />
          Override global LLM
        </label>

        {#if insightOverride}
          <h4>LLM override (for Product Intelligence)</h4>

          <div class="form-row">
            <div class="form-group">
              <label>Provider</label>
              <select bind:value={insightCfg.llm.provider}>
                <option value="">— none —</option>
                <option value="zai">zai</option>
                <option value="anthropic">anthropic</option>
                <option value="openai">openai</option>
                <option value="ollama">ollama</option>
              </select>
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
              <input type="number" min="0" max="200000" bind:value={insightCfg.llm.max_tokens} />
            </div>
            <div class="form-group">
              <label>Timeout (seconds)</label>
              <input type="number" min="0" max="600" bind:value={insightCfg.llm.timeout} />
            </div>
            <div class="form-group">
              <label>Max retries</label>
              <input type="number" min="0" max="10" bind:value={insightCfg.llm.max_retries} />
            </div>
            <div class="form-group">
              <label for="insight-concurrency">Concurrency</label>
              <input id="insight-concurrency" type="number" min="0" max="100" bind:value={insightCfg.llm.concurrency} />
            </div>
            <div class="form-group">
              <label for="insight-min-interval">Min request interval (ms)</label>
              <input id="insight-min-interval" type="number" min="0" max="60000" step="500" bind:value={insightCfg.llm.min_request_interval_ms} />
              <small>0 = disabled. Use to stay under provider rate limits (e.g. 3000 = max ~20 req/min).</small>
            </div>
          </div>
        {/if}
      </div>

      <div class="actions">
        <button class="btn-primary" onclick={saveInsight} disabled={insightSaving}>
          {insightSaving ? 'Saving...' : 'Save Insight settings'}
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
        <div class="form-group">
          <label>Reviewer timeout (minutes)</label>
          <input type="number" min="1" bind:value={cfg.reviewer_timeout_minutes} />
        </div>
        <div class="form-group">
          <label>Ticket timeout (minutes)</label>
          <input type="number" min="1" bind:value={cfg.ticket_timeout_minutes} />
        </div>
      </div>

      <div class="guardian-llm-section">
        <label class="checkbox-label">
          <input type="checkbox" bind:checked={guardianOverride} />
          Override global LLM
        </label>

        {#if guardianOverride}
          <h4>LLM override (for governance analysis)</h4>

          <div class="form-row">
            <div class="form-group">
              <label>Provider</label>
              <select bind:value={cfg.llm.provider}>
                <option value="">— none —</option>
                <option value="zai">zai</option>
                <option value="anthropic">anthropic</option>
                <option value="openai">openai</option>
                <option value="ollama">ollama</option>
              </select>
            </div>
            <div class="form-group">
              <label>Model</label>
              <input type="text" placeholder="glm-5.1, gpt-4o" bind:value={cfg.llm.model} />
              <span class="hint">Examples: glm-5.1, glm-4.7, gpt-4o, claude-3-5-haiku-20241022</span>
            </div>
          </div>

          <div class="form-group">
            <label>Base URL</label>
            <input type="url" placeholder="https://api.z.ai/api/coding/paas/v4" bind:value={cfg.llm.base_url} />
          </div>

          <div class="form-group">
            <label>API Key</label>
            <input
              type="password"
              placeholder={cfg.llm.api_key === '***' ? '(saved — enter new value to change)' : ''}
              bind:value={cfg.llm.api_key}
            />
          </div>

          <div class="form-row">
            <div class="form-group">
              <label>Temperature</label>
              <input type="number" min="0" max="2" step="0.05" bind:value={cfg.llm.temperature} />
            </div>
            <div class="form-group">
              <label>Max tokens</label>
              <input type="number" min="0" max="200000" bind:value={cfg.llm.max_tokens} />
            </div>
            <div class="form-group">
              <label>Timeout (seconds)</label>
              <input type="number" min="0" max="600" bind:value={cfg.llm.timeout} />
            </div>
            <div class="form-group">
              <label>Max retries</label>
              <input type="number" min="0" max="10" bind:value={cfg.llm.max_retries} />
            </div>
            <div class="form-group">
              <label for="guardian-concurrency">Concurrency</label>
              <input id="guardian-concurrency" type="number" min="0" max="100" bind:value={cfg.llm.concurrency} />
            </div>
            <div class="form-group">
              <label for="guardian-min-interval">Min request interval (ms)</label>
              <input id="guardian-min-interval" type="number" min="0" max="60000" step="500" bind:value={cfg.llm.min_request_interval_ms} />
              <small>0 = disabled. Use to stay under provider rate limits (e.g. 3000 = max ~20 req/min).</small>
            </div>
          </div>

          <div class="test-row">
            <button class="btn-secondary" onclick={testLLM} disabled={testing || !cfg.llm.base_url || !cfg.llm.model}>
              {testing ? 'Testing...' : 'Test connection'}
            </button>
            {#if testMsg}
              <span class="ok-msg">{testMsg}</span>
            {/if}
            {#if testError}
              <span class="err-msg">{testError}</span>
            {/if}
          </div>
        {/if}
      </div>

      <div class="actions">
        <button class="btn-primary" onclick={save} disabled={saving}>
          {saving ? 'Saving...' : 'Save Guardian settings'}
        </button>
        {#if saveMsg}
          <span class="ok-msg">{saveMsg}</span>
        {/if}
        {#if saveError}
          <span class="err-msg">{saveError}</span>
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
  input[type="number"],
  select {
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

  input:focus,
  select:focus {
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

  .insight-llm-section,
  .guardian-llm-section {
    border-top: 1px solid #21262d;
    margin-top: 8px;
    padding-top: 12px;
  }

  .tooltip-icon {
    font-size: 0.75rem;
    color: #8b949e;
    cursor: help;
    margin-left: 4px;
  }
</style>
