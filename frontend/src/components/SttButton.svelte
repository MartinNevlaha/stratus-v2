<script lang="ts">
  import { transcribeAudio } from '$lib/api'

  interface Props {
    onTranscript: (text: string) => void
  }

  let { onTranscript }: Props = $props()

  let recording = $state(false)
  let loading = $state(false)
  let error = $state<string | null>(null)
  let mediaRecorder: MediaRecorder | null = null
  let chunks: Blob[] = []
  let volume = $state(0)
  let analyser: AnalyserNode | null = null
  let animFrame: number | null = null

  async function toggleRecording() {
    if (recording) {
      stopRecording()
    } else {
      await startRecording()
    }
  }

  async function startRecording() {
    error = null
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      chunks = []

      // Volume visualizer
      const ctx = new AudioContext()
      const source = ctx.createMediaStreamSource(stream)
      analyser = ctx.createAnalyser()
      analyser.fftSize = 256
      source.connect(analyser)
      updateVolume()

      mediaRecorder = new MediaRecorder(stream, { mimeType: 'audio/webm' })
      mediaRecorder.ondataavailable = (e) => {
        if (e.data.size > 0) chunks.push(e.data)
      }
      mediaRecorder.onstop = async () => {
        stream.getTracks().forEach((t) => t.stop())
        ctx.close()
        if (animFrame) cancelAnimationFrame(animFrame)
        volume = 0
        loading = true
        try {
          const blob = new Blob(chunks, { type: 'audio/webm' })
          const result = await transcribeAudio(blob)
          onTranscript(result.text)
        } catch (e) {
          error = e instanceof Error ? e.message : 'Transcription failed'
        } finally {
          loading = false
        }
      }
      mediaRecorder.start(250)
      recording = true
    } catch (e) {
      error = e instanceof Error ? e.message : 'Microphone access denied'
    }
  }

  function stopRecording() {
    recording = false
    mediaRecorder?.stop()
  }

  function updateVolume() {
    if (!analyser) return
    const data = new Uint8Array(analyser.frequencyBinCount)
    analyser.getByteFrequencyData(data)
    volume = data.reduce((a, b) => a + b, 0) / data.length / 128
    animFrame = requestAnimationFrame(updateVolume)
  }
</script>

<div class="stt-wrapper">
  <button
    class="stt-btn"
    class:recording
    class:loading
    onclick={toggleRecording}
    disabled={loading}
    title={recording ? 'Stop recording' : 'Start voice input'}
  >
    {#if loading}
      <svg class="spinner" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="2" stroke-dasharray="31.4" stroke-dashoffset="10"/></svg>
    {:else if recording}
      <svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>
    {:else}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/>
        <path d="M19 10v2a7 7 0 0 1-14 0v-2M12 19v4M8 23h8"/>
      </svg>
    {/if}

    {#if recording}
      <span
        class="volume-ring"
        style="transform: scale({1 + volume * 0.4}); opacity: {0.3 + volume * 0.7}"
      ></span>
    {/if}
  </button>

  {#if error}
    <span class="error">{error}</span>
  {/if}
</div>

<style>
  .stt-wrapper {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .stt-btn {
    position: relative;
    width: 40px;
    height: 40px;
    border-radius: 50%;
    border: none;
    background: #21262d;
    color: #c9d1d9;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    transition: background 0.15s;
  }

  .stt-btn:hover { background: #30363d; }
  .stt-btn.recording { background: #da3633; color: white; }
  .stt-btn.loading { opacity: 0.6; cursor: default; }

  .stt-btn svg { width: 20px; height: 20px; }

  .volume-ring {
    position: absolute;
    inset: -4px;
    border-radius: 50%;
    border: 2px solid #da3633;
    pointer-events: none;
    transition: transform 0.05s, opacity 0.05s;
  }

  .spinner {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .error {
    font-size: 12px;
    color: #f85149;
  }
</style>
