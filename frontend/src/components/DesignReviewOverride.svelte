<script lang="ts">
  import { teamRequest } from '$lib/teams'

  interface Props {
    path: string
    body: Record<string, unknown>
    ariaLabel?: string
    ondone?: () => void
  }
  let { path, body, ariaLabel, ondone }: Props = $props()

  // An override waives a failed or stuck review, so it is only sent after an explicit inline confirmation.
  let confirming = $state(false)
  let reason = $state('')
  let busy = $state(false)
  let error = $state('')

  function start() {
    reason = String(body.reason ?? '')
    error = ''
    confirming = true
  }

  async function confirm() {
    if (busy || !reason.trim()) return
    busy = true
    error = ''
    try {
      await teamRequest(path.replace(/^\//, ''), { ...body, reason: reason.trim() })
      confirming = false
      ondone?.()
    } catch (e) {
      error = e instanceof Error ? e.message : 'Akcia zlyhala'
    } finally {
      busy = false
    }
  }
</script>

{#if confirming}
  <span class="confirm" role="alertdialog" aria-label="Potvrdenie schválenia návrhu">
    <span>Schváliť návrh bez úspešného review?</span>
    <input aria-label="Dôvod schválenia" bind:value={reason} />
    <button class="primary" disabled={busy || !reason.trim()} onclick={confirm}>Áno, schváliť</button>
    <button disabled={busy} onclick={() => (confirming = false)}>Zrušiť</button>
  </span>
{:else}
  <button onclick={start} aria-label={ariaLabel}>Schváliť bez review</button>
{/if}
{#if error}<span class="err" role="alert">{error}</span>{/if}

<style>
  .confirm { display: inline-flex; align-items: center; gap: 8px; flex-wrap: wrap; border: 1px solid #d29922; border-radius: 4px; padding: 4px 8px; color: #c9d1d9; }
  .confirm input { flex: 1; min-width: 160px; background: #0d1117; color: #c9d1d9; border: 1px solid #30363d; border-radius: 4px; padding: 2px 6px; font: inherit; }
  button { background: #0d1117; color: #c9d1d9; border: 1px solid #30363d; border-radius: 4px; padding: 2px 8px; font: inherit; cursor: pointer; white-space: nowrap; }
  button:hover:not(:disabled) { border-color: #58a6ff; }
  button:disabled { opacity: 0.45; cursor: default; }
  button.primary { background: #238636; border-color: #2ea043; color: white; }
  .err { color: #ff7b72; }
</style>
