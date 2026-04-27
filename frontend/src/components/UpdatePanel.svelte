<script lang="ts">
  import { appState, dismissUpdate } from '$lib/store'

  let showPanel = $derived(appState.updateInProgress || appState.updateLog.length > 0)
</script>

{#if showPanel}
  <div class="update-panel" class:error={!!appState.updateError}>
    <div class="update-panel-header">
      <span class="update-header">
        {#if appState.updateInProgress}
          ⟳ Updating stratus…
        {:else if appState.updateError}
          ✕ Update failed
        {:else}
          ✓ Update complete — server restarting
        {/if}
      </span>
      {#if !appState.updateInProgress}
        <button class="dismiss-btn" onclick={dismissUpdate} title="Dismiss">✕</button>
      {/if}
    </div>
    <div class="update-log">
      {#each appState.updateLog as line}
        <div class="log-line" class:error-line={line.startsWith('Error:')}>{line}</div>
      {/each}
    </div>
  </div>
{/if}

<style>
  .update-panel {
    background: #1c1700;
    border: 1px solid #9e6a03;
    border-radius: 8px;
    padding: 12px 16px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .update-panel.error {
    background: #2d1117;
    border-color: #f85149;
  }
  .update-panel-header { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .update-header { font-size: 13px; font-weight: 600; color: #ffa657; }
  .update-panel.error .update-header { color: #f85149; }
  .dismiss-btn { background: none; border: none; color: #8b949e; cursor: pointer; font-size: 12px; padding: 2px 6px; border-radius: 4px; line-height: 1; }
  .dismiss-btn:hover { color: #c9d1d9; background: #30363d; }
  .update-log { display: flex; flex-direction: column; gap: 2px; }
  .log-line { font-size: 12px; color: #d29922; font-family: monospace; }
  .log-line.error-line { color: #f85149; }
</style>
