<script lang="ts">
  import { onMount } from 'svelte'
  import { appState, initStore, startUpdate } from '$lib/store'
  import Overview from './routes/Overview.svelte'
  import Memory from './routes/Memory.svelte'
  import Retrieval from './routes/Retrieval.svelte'
  import Learning from './routes/Learning.svelte'
  import Terminal from './components/Terminal.svelte'

  let activeTab = $state<'overview' | 'memory' | 'retrieval' | 'learning' | 'terminal'>('overview')

  onMount(() => {
    initStore()
  })

  const tabs = [
    { id: 'overview' as const, label: 'Overview' },
    { id: 'memory' as const, label: 'Memory' },
    { id: 'retrieval' as const, label: 'Retrieve' },
    { id: 'learning' as const, label: 'Learning' },
  ]

  let pendingProposals = $derived(appState.dashboard?.pending_proposals?.length ?? 0)
</script>

<div class="app">
  <header>
    <div class="logo">
      <img src="/logo.png" alt="Stratus" class="logo-img" />
      <span class="version">v2</span>
    </div>

    <nav>
      {#each tabs as t}
        <button
          class:active={activeTab === t.id}
          onclick={() => (activeTab = t.id)}
        >
          {t.label}
          {#if t.id === 'learning' && pendingProposals > 0}
            <span class="badge">{pendingProposals}</span>
          {/if}
        </button>
      {/each}
    </nav>

    {#if appState.version?.update_available}
      <button class="update-btn" onclick={startUpdate} disabled={appState.updateInProgress}>
        {appState.updateInProgress ? 'Updating…' : `↑ v${appState.version.latest}`}
      </button>
    {/if}

    <div class="status-dot" class:connected={appState.connected} title={appState.connected ? 'Live' : 'Reconnecting…'}></div>
  </header>

  <main>
    {#if appState.loading && !appState.dashboard}
      <div class="loading">Connecting to stratus…</div>
    {:else if activeTab === 'overview'}
      <div class="split-view">
        <div class="split-pane">
          <Overview />
        </div>
        <div class="split-divider"></div>
        <div class="split-pane terminal-pane">
          <Terminal />
        </div>
      </div>
    {:else if activeTab === 'memory'}
      <Memory />
    {:else if activeTab === 'retrieval'}
      <Retrieval />
    {:else if activeTab === 'learning'}
      <Learning />
    {/if}
  </main>
</div>

<style>
  :global(*) {
    box-sizing: border-box;
    margin: 0;
    padding: 0;
  }

  :global(body) {
    background: #0d1117;
    color: #c9d1d9;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    font-size: 14px;
    line-height: 1.5;
  }

  .app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 0 16px;
    height: 48px;
    background: #161b22;
    border-bottom: 1px solid #30363d;
    flex-shrink: 0;
  }

  .logo { display: flex; align-items: center; gap: 6px; }
  .logo-img { height: 28px; width: auto; }
  .version { font-size: 11px; color: #8b949e; }

  nav { display: flex; gap: 2px; flex: 1; }
  nav button {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    background: transparent;
    border: none;
    border-radius: 6px;
    color: #8b949e;
    cursor: pointer;
    font-size: 14px;
    transition: color 0.15s, background 0.15s;
  }
  nav button:hover { color: #c9d1d9; background: #21262d; }
  nav button.active { color: #c9d1d9; background: #21262d; }

  .badge {
    font-size: 11px;
    background: #da3633;
    color: white;
    border-radius: 10px;
    padding: 0 5px;
    min-width: 16px;
    text-align: center;
  }

  .update-btn {
    padding: 4px 10px;
    background: #9e6a03;
    color: #ffa657;
    border: 1px solid #d29922;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s, opacity 0.15s;
    white-space: nowrap;
  }
  .update-btn:hover:not(:disabled) { background: #bb8009; }
  .update-btn:disabled { opacity: 0.6; cursor: default; }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #8b949e;
    transition: background 0.3s;
  }
  .status-dot.connected { background: #3fb950; }

  main {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  main :global(> *:not(.split-view)) {
    padding: 20px;
    overflow-y: auto;
    flex: 1;
  }

  .split-view {
    display: flex;
    flex-direction: row;
    flex: 1;
    overflow: hidden;
    height: 100%;
  }

  .split-pane {
    flex: 1;
    overflow-y: auto;
    padding: 20px;
    min-width: 0;
    min-height: 0;
  }

  .split-divider {
    width: 1px;
    background: #30363d;
    flex-shrink: 0;
  }

  .terminal-pane {
    padding: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  .loading {
    text-align: center;
    padding: 64px;
    color: #8b949e;
  }
</style>
