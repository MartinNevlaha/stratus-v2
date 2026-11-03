<script lang="ts">
  import { renamePeer } from '$lib/api'
  import { describeCommsError } from '$lib/comms'
  import { relativeTime } from '$lib/ui'
  import type { Peer } from '$lib/types'

  interface Props {
    peers: Peer[]
    selected: string | null
    showEnded: boolean
    onselect: (key: string | null) => void
    ontoggleended: (v: boolean) => void
    onchanged: () => void
  }
  let { peers, selected, showEnded, onselect, ontoggleended, onchanged }: Props = $props()

  let editing = $state<string | null>(null)
  let draft = $state('')
  let error = $state<string | null>(null)
  let visible = $derived(showEnded ? peers : peers.filter((p) => p.active))

  function startRename(p: Peer) {
    editing = p.session_key
    draft = p.handle
    error = null
  }

  async function commitRename(p: Peer) {
    if (draft === p.handle) { editing = null; return }
    try {
      await renamePeer(p.session_key, draft.trim())
      editing = null
      error = null
      onchanged()
    } catch (e) {
      error = describeCommsError(e)
    }
  }
</script>

<section class="col" aria-label="Peers">
  <header>
    <h2>Peers</h2>
    <label class="toggle"><input type="checkbox" checked={showEnded} onchange={(e) => ontoggleended(e.currentTarget.checked)} /> show ended</label>
  </header>
  {#if error}<div class="err" role="alert">{error}</div>{/if}
  <ul>
    {#each visible as p (p.session_key)}
      <li class:selected={selected === p.session_key} class:ended={!p.active}>
        <div class="row">
          <span class="rt rt-{p.runtime}">{p.runtime}</span>
          <span class="dot" class:on={p.active} title={p.active ? 'active' : 'ended'}></span>
          {#if editing === p.session_key}
            <form onsubmit={(e) => { e.preventDefault(); commitRename(p) }}>
              <input aria-label="New handle" bind:value={draft} onkeydown={(e) => e.key === 'Escape' && (editing = null)} />
              <button type="submit" class="mini">save</button>
            </form>
          {:else}
            <button class="handle" onclick={() => onselect(selected === p.session_key ? null : p.session_key)} title="Filter feed">{p.handle}</button>
            <button class="mini" onclick={() => startRename(p)} aria-label="Rename {p.handle}">rename</button>
          {/if}
        </div>
        <div class="meta">{p.project || 'no project'} · {p.active ? 'seen' : 'ended'} {relativeTime(p.ended_at ?? p.last_seen_at)}</div>
      </li>
    {:else}
      <li class="empty">No {showEnded ? '' : 'active '}peers</li>
    {/each}
  </ul>
</section>

<style>
  .col { display: flex; flex-direction: column; min-height: 0; background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 12px; }
  header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
  h2 { font-size: 13px; text-transform: uppercase; letter-spacing: 0.04em; color: #8b949e; }
  .toggle { font-size: 12px; color: #8b949e; display: flex; align-items: center; gap: 4px; cursor: pointer; }
  ul { list-style: none; overflow-y: auto; display: flex; flex-direction: column; gap: 6px; }
  li { padding: 8px; border: 1px solid #30363d; border-radius: 6px; background: #0d1117; }
  li.selected { border-color: #58a6ff; }
  li.ended { opacity: 0.55; }
  li.empty { color: #8b949e; text-align: center; border-style: dashed; }
  .row { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .meta { font-size: 11px; color: #8b949e; margin-top: 4px; overflow-wrap: anywhere; }
  .rt { font-size: 10px; padding: 1px 6px; border-radius: 10px; border: 1px solid; font-weight: 600; }
  .rt-claude-code { color: #ffa657; border-color: #9e6a03; background: #2d2208; }
  .rt-opencode { color: #58a6ff; border-color: #1f6feb; background: #0c2d6b; }
  .rt-codex { color: #3fb950; border-color: #238636; background: #0f2a17; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: #484f58; }
  .dot.on { background: #3fb950; }
  .handle { background: none; border: none; color: #c9d1d9; font-weight: 600; cursor: pointer; font-size: 13px; }
  .handle:hover { color: #58a6ff; }
  .mini { padding: 1px 6px; font-size: 11px; background: transparent; border: 1px solid #30363d; border-radius: 4px; color: #8b949e; cursor: pointer; margin-left: auto; }
  .mini:hover { color: #c9d1d9; border-color: #8b949e; }
  form { display: flex; gap: 4px; flex: 1; }
  input[aria-label] { flex: 1; min-width: 0; background: #0d1117; border: 1px solid #30363d; border-radius: 4px; color: #c9d1d9; padding: 2px 6px; font-size: 12px; }
  .err { padding: 6px 10px; margin-bottom: 8px; background: rgba(248,81,73,0.1); border: 1px solid #f85149; border-radius: 6px; color: #f85149; font-size: 12px; }
</style>
