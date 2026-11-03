<script lang="ts">
  import { sendCommsMessage } from '$lib/api'
  import { MESSAGE_KINDS, kindClass, toOptions, describeCommsError } from '$lib/comms'
  import { relativeTime } from '$lib/ui'
  import type { CommsMessage, Peer } from '$lib/types'

  interface Props {
    messages: CommsMessage[]
    peers: Peer[]
    filterLabel: string | null
    onclearfilter: () => void
    onsent: () => void
  }
  let { messages, peers, filterLabel, onclearfilter, onsent }: Props = $props()

  let to = $state('*')
  let kind = $state('text')
  let body = $state('')
  let sending = $state(false)
  let error = $state<string | null>(null)
  let options = $derived(toOptions(peers))

  async function send() {
    if (!body.trim() || sending) return
    sending = true
    try {
      await sendCommsMessage(to, kind, body)
      body = ''
      error = null
      onsent()
    } catch (e) {
      error = describeCommsError(e)
    } finally {
      sending = false
    }
  }
</script>

<section class="col" aria-label="Messages">
  <header>
    <h2>Messages</h2>
    {#if filterLabel}<button class="chip" onclick={onclearfilter}>{filterLabel} ✕</button>{/if}
  </header>
  <ul class="feed">
    {#each messages as m (m.id)}
      <li>
        <div class="head">
          <span class="from">{m.from_handle}</span>
          <span class="arrow">→</span>
          <span class="to">{m.to}</span>
          <span class="kind {kindClass(m.kind)}">{m.kind}</span>
          <time class="time" datetime={m.created_at}>{relativeTime(m.created_at)}</time>
        </div>
        <p class="body">{m.body}</p>
        {#if m.recipients?.length}<div class="rcpt">delivered to {m.recipients.join(', ')}</div>{/if}
      </li>
    {:else}
      <li class="empty">No messages</li>
    {/each}
  </ul>
  {#if error}<div class="err" role="alert">{error}</div>{/if}
  <form onsubmit={(e) => { e.preventDefault(); send() }}>
    <div class="line">
      <select aria-label="To" bind:value={to}>{#each options as o}<option value={o}>{o}</option>{/each}</select>
      <select aria-label="Kind" bind:value={kind}>{#each MESSAGE_KINDS as k}<option value={k}>{k}</option>{/each}</select>
      <button class="send" type="submit" disabled={sending || !body.trim()}>{sending ? 'Sending…' : 'Send'}</button>
    </div>
    <textarea aria-label="Message body" rows="3" placeholder="Message as user…" bind:value={body}
      onkeydown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) send() }}></textarea>
  </form>
</section>

<style>
  .col { display: flex; flex-direction: column; min-height: 0; background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 12px; }
  header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
  h2 { font-size: 13px; text-transform: uppercase; letter-spacing: 0.04em; color: #8b949e; }
  .chip { font-size: 11px; padding: 1px 8px; border-radius: 10px; background: #0c2d6b; border: 1px solid #1f6feb; color: #58a6ff; cursor: pointer; }
  .feed { list-style: none; flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 6px; min-height: 120px; }
  li { padding: 8px; border: 1px solid #30363d; border-radius: 6px; background: #0d1117; }
  li.empty { color: #8b949e; text-align: center; border-style: dashed; }
  .head { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; font-size: 12px; }
  .from { font-weight: 600; color: #c9d1d9; }
  .arrow, .to { color: #8b949e; }
  .time { margin-left: auto; color: #8b949e; font-size: 11px; }
  .kind { font-size: 10px; padding: 0 6px; border-radius: 10px; border: 1px solid #30363d; color: #8b949e; }
  .kind-done { color: #3fb950; border-color: #238636; background: #0f2a17; }
  .kind-question { color: #d29922; border-color: #9e6a03; background: #2d2208; }
  .kind-task { color: #bc8cff; border-color: #6e40c9; background: #271a4a; }
  .body { white-space: pre-wrap; overflow-wrap: anywhere; margin-top: 4px; font-size: 13px; }
  .rcpt { font-size: 11px; color: #8b949e; margin-top: 4px; }
  form { margin-top: 8px; display: flex; flex-direction: column; gap: 6px; }
  .line { display: flex; gap: 6px; }
  select, textarea { background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; padding: 5px 8px; font: inherit; font-size: 13px; }
  select { min-width: 0; flex: 1; }
  textarea { resize: vertical; }
  .send { padding: 5px 14px; background: #238636; color: white; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; }
  .send:hover:not(:disabled) { background: #2ea043; }
  .send:disabled { opacity: 0.5; cursor: default; }
  .err { margin-top: 8px; padding: 6px 10px; background: rgba(248,81,73,0.1); border: 1px solid #f85149; border-radius: 6px; color: #f85149; font-size: 12px; }
</style>
