<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { Terminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'
  import SttButton from './SttButton.svelte'

  let container: HTMLDivElement
  let term: Terminal
  let fitAddon: FitAddon
  let ws: WebSocket | null = null
  let sessionId = `terminal-${Date.now()}`
  let connected = $state(false)
  let error = $state<string | null>(null)

  // Auto-scroll: follow output unless the user has manually scrolled up.
  let autoScroll = true

  onMount(() => {
    term = new Terminal({
      theme: {
        background: '#0d1117',
        foreground: '#c9d1d9',
        cursor: '#58a6ff',
        selectionBackground: '#264f78',
      },
      fontFamily: '"JetBrains Mono", "Fira Code", monospace',
      fontSize: 14,
      cursorBlink: true,
      scrollback: 5000,
    })

    fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(container)
    // Delay initial fit so the flex layout has settled and the container
    // has its final pixel dimensions before xterm calculates rows/cols.
    requestAnimationFrame(() => fitAddon.fit())

    // Track user scroll position: disable auto-scroll when they scroll up,
    // re-enable when they reach the bottom again.
    const viewport = container.querySelector('.xterm-viewport') as HTMLElement | null
    const onViewportScroll = () => {
      if (!viewport) return
      const distanceFromBottom = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight
      autoScroll = distanceFromBottom < 32
    }
    viewport?.addEventListener('scroll', onViewportScroll, { passive: true })

    connectWS()

    // Resize handler
    const observer = new ResizeObserver(() => {
      fitAddon.fit()
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({
          type: 'resize',
          data: { id: sessionId, rows: term.rows, cols: term.cols }
        }))
      }
    })
    observer.observe(container)

    // Send input to server. Re-enable auto-scroll on user input so they
    // always see the response to what they typed.
    term.onData((data) => {
      if (ws?.readyState === WebSocket.OPEN) {
        autoScroll = true
        ws.send(JSON.stringify({ type: 'input', data: { id: sessionId, data } }))
      }
    })

    return () => {
      observer.disconnect()
      viewport?.removeEventListener('scroll', onViewportScroll)
    }
  })

  onDestroy(() => {
    ws?.close()
    term?.dispose()
  })

  function handleTranscript(text: string) {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'input', data: { id: sessionId, data: text } }))
    }
  }

  function connectWS() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    ws = new WebSocket(`${proto}//${window.location.host}/api/terminal/ws`)

    ws.onopen = () => {
      connected = true
      error = null
      // Create terminal session
      ws!.send(JSON.stringify({ type: 'create', data: { id: sessionId } }))
    }

    ws.onmessage = (e) => {
      const msg = JSON.parse(e.data)
      switch (msg.type) {
        case 'output':
          term.write(msg.data as string, () => { if (autoScroll) term.scrollToBottom() })
          break
        case 'exit':
          term.writeln('\r\n\x1b[33m[session ended]\x1b[0m')
          connected = false
          break
        case 'error':
          term.writeln(`\r\n\x1b[31m[error: ${msg.data}]\x1b[0m`)
          break
        case 'create':
          term.writeln('\x1b[32mConnected\x1b[0m\r\n')
          break
      }
    }

    ws.onclose = () => {
      connected = false
      setTimeout(() => connectWS(), 3000)
    }

    ws.onerror = () => {
      error = 'WebSocket connection failed'
    }
  }
</script>

<div class="terminal-wrapper">
  <div class="terminal-header">
    <span class="title">Terminal</span>
    <span class="status" class:connected>
      {connected ? '● Connected' : '○ Disconnected'}
    </span>
    {#if error}
      <span class="error">{error}</span>
    {/if}
    <div class="stt-slot">
      <SttButton onTranscript={handleTranscript} />
    </div>
  </div>
  <div class="terminal-container" bind:this={container}></div>
</div>

<style>
  .terminal-wrapper {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    background: #0d1117;
    border-radius: 6px;
    overflow: hidden;
  }

  .terminal-header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 12px;
    background: #161b22;
    border-bottom: 1px solid #30363d;
    font-size: 12px;
    color: #8b949e;
  }

  .title {
    font-weight: 600;
    color: #c9d1d9;
  }

  .status { color: #8b949e; }
  .status.connected { color: #3fb950; }
  .error { color: #f85149; }

  .stt-slot {
    margin-left: auto;
    display: flex;
    align-items: center;
  }

  .terminal-container {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    padding: 4px;
  }

  :global(.terminal-container .xterm) {
    height: 100%;
  }

  :global(.terminal-container .xterm-viewport) {
    overflow-y: scroll !important;
  }
</style>
