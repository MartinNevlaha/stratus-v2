<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { Terminal } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import '@xterm/xterm/css/xterm.css'
  import SttButton from './SttButton.svelte'
  import ImageUploadToast from './ImageUploadToast.svelte'
  import { uploadTerminalImage } from '$lib/api'

  let container: HTMLDivElement
  let term: Terminal
  let fitAddon: FitAddon
  let ws: WebSocket | null = null
  let sessionId = `terminal-${Date.now()}`
  let connected = $state(false)
  let error = $state<string | null>(null)

  // Auto-scroll: follow output unless the user has manually scrolled up.
  let autoScroll = true

  // Image upload state
  let uploading = $state(false)
  let dragOver = $state(false)
  let toastMessage = $state<string | null>(null)

  let fitTimer: ReturnType<typeof setTimeout> | null = null

  function fitAndNotify() {
    if (!fitAddon || !container || container.clientHeight === 0) return
    fitAddon.fit()
    // Force xterm to re-render all visible rows — fixes garbled glyphs
    // that occur when the container is resized rapidly (e.g. popup overlay).
    term.refresh(0, term.rows - 1)
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({
        type: 'resize',
        data: { id: sessionId, rows: term.rows, cols: term.cols }
      }))
    }
  }

  // Debounced fit: absorbs rapid resize events (popups, split drag)
  // so xterm only reflows once after the layout settles.
  function debouncedFit() {
    if (fitTimer) clearTimeout(fitTimer)
    fitTimer = setTimeout(() => fitAndNotify(), 80)
  }

  async function handleImageUpload(blob: Blob, filename: string) {
    if (uploading) return
    uploading = true
    try {
      const result = await uploadTerminalImage(blob, filename)
      if (ws?.readyState === WebSocket.OPEN) {
        autoScroll = true
        ws.send(JSON.stringify({ type: 'input', data: { id: sessionId, data: result.path } }))
      }
      toastMessage = `Image: ${result.path}`
    } catch (e) {
      toastMessage = `Upload failed: ${e instanceof Error ? e.message : 'unknown error'}`
    } finally {
      uploading = false
    }
  }

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
    // Double rAF: first frame lets the browser finish the current render
    // cycle, second frame ensures the flex layout has fully settled and the
    // container has its final pixel dimensions before xterm calculates cols.
    requestAnimationFrame(() => requestAnimationFrame(() => fitAndNotify()))

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

    // Resize handler — debounced refit whenever the container size changes.
    const resizeObs = new ResizeObserver(() => debouncedFit())
    resizeObs.observe(container)

    // Visibility handler — ResizeObserver does NOT fire when an ancestor
    // toggles display:none ↔ display:flex (the split-view does this on tab
    // switch). IntersectionObserver reliably detects when the container
    // re-enters the viewport and we can re-fit at the correct dimensions.
    const visObs = new IntersectionObserver((entries) => {
      if (entries[0]?.isIntersecting) {
        requestAnimationFrame(() => fitAndNotify())
      }
    })
    visObs.observe(container)

    // Window focus handler — when a popup overlay (e.g. Claude Code permission
    // dialog) appears and disappears, the terminal may have garbled glyphs.
    // Force a clean re-render when the window regains focus.
    const onFocus = () => requestAnimationFrame(() => fitAndNotify())
    window.addEventListener('focus', onFocus)

    // Send input to server. Re-enable auto-scroll on user input so they
    // always see the response to what they typed.
    term.onData((data) => {
      if (ws?.readyState === WebSocket.OPEN) {
        autoScroll = true
        ws.send(JSON.stringify({ type: 'input', data: { id: sessionId, data } }))
      }
    })

    // Intercept clipboard paste containing images.
    // Text paste falls through to xterm's native handler.
    const onPaste = async (e: ClipboardEvent) => {
      const items = e.clipboardData?.items
      if (!items) return
      for (const item of items) {
        if (item.type.startsWith('image/')) {
          e.preventDefault()
          e.stopPropagation()
          const blob = item.getAsFile()
          if (blob) {
            const ext = item.type.split('/')[1] || 'png'
            await handleImageUpload(blob, `paste.${ext}`)
          }
          return
        }
      }
    }
    container.addEventListener('paste', onPaste)

    // Drag-and-drop image handling.
    const onDragOver = (e: DragEvent) => {
      if (e.dataTransfer?.types.includes('Files')) {
        e.preventDefault()
        dragOver = true
      }
    }
    const onDragLeave = (e: DragEvent) => {
      if (e.currentTarget === e.target || !container.contains(e.relatedTarget as Node)) {
        dragOver = false
      }
    }
    const onDrop = async (e: DragEvent) => {
      e.preventDefault()
      dragOver = false
      const files = e.dataTransfer?.files
      if (!files?.length) return
      const file = files[0]
      if (!file.type.startsWith('image/')) {
        toastMessage = 'Only image files are supported'
        return
      }
      await handleImageUpload(file, file.name)
    }
    container.addEventListener('dragover', onDragOver)
    container.addEventListener('dragleave', onDragLeave)
    container.addEventListener('drop', onDrop)

    return () => {
      resizeObs.disconnect()
      visObs.disconnect()
      viewport?.removeEventListener('scroll', onViewportScroll)
      window.removeEventListener('focus', onFocus)
      container.removeEventListener('paste', onPaste)
      container.removeEventListener('dragover', onDragOver)
      container.removeEventListener('dragleave', onDragLeave)
      container.removeEventListener('drop', onDrop)
      if (fitTimer) clearTimeout(fitTimer)
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
    {#if uploading}
      <span class="upload-status">Uploading image...</span>
    {/if}
    {#if error}
      <span class="error">{error}</span>
    {/if}
    <div class="stt-slot">
      <SttButton onTranscript={handleTranscript} />
    </div>
  </div>
  <div class="terminal-container" bind:this={container}>
    {#if dragOver}
      <div class="drag-overlay">
        <div class="drag-overlay-content">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
            <circle cx="8.5" cy="8.5" r="1.5"/>
            <polyline points="21 15 16 10 5 21"/>
          </svg>
          <span>Drop image here</span>
        </div>
      </div>
    {/if}
  </div>

  {#if toastMessage}
    <ImageUploadToast message={toastMessage} onDismiss={() => (toastMessage = null)} />
  {/if}
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
  .upload-status { color: #58a6ff; }

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
    position: relative;
  }

  .drag-overlay {
    position: absolute;
    inset: 0;
    background: rgba(56, 139, 253, 0.1);
    border: 2px dashed #58a6ff;
    border-radius: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 5;
    pointer-events: none;
  }

  .drag-overlay-content {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    color: #58a6ff;
    font-size: 14px;
  }

  .drag-overlay-content svg {
    width: 32px;
    height: 32px;
  }

  :global(.terminal-container .xterm) {
    height: 100%;
  }

  :global(.terminal-container .xterm-viewport) {
    overflow-y: scroll !important;
  }
</style>
