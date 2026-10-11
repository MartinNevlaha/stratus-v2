<script lang="ts">
  import { appState, dismissToast } from '$lib/store'
</script>

{#if appState.toasts.length > 0}
  <div class="stack" aria-live="polite">
    {#each appState.toasts as t (t.id)}
      <div class="toast {t.kind}">
        {#if t.href}
          <a href={t.href} onclick={() => dismissToast(t.id)}>{t.text}</a>
        {:else}
          <span>{t.text}</span>
        {/if}
        <button onclick={() => dismissToast(t.id)} aria-label="Dismiss">✕</button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .stack { position: fixed; right: 16px; bottom: 16px; display: flex; flex-direction: column; gap: 8px; z-index: 1000; max-width: 360px; }
  .toast { display: flex; align-items: center; gap: 10px; background: #161b22; border: 1px solid #30363d; border-left-width: 3px; border-radius: 6px; padding: 8px 10px; font-size: 12px; color: #c9d1d9; box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4); }
  .toast.warn { border-left-color: #d29922; }
  .toast.error { border-left-color: #f85149; }
  .toast.success { border-left-color: #3fb950; }
  .toast.info { border-left-color: #58a6ff; }
  a { color: inherit; text-decoration: none; flex: 1; }
  a:hover { text-decoration: underline; }
  span { flex: 1; }
  button { background: none; border: none; color: #8b949e; cursor: pointer; font-size: 11px; padding: 0; }
</style>
