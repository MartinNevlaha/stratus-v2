import type { WsMessage } from './types'

type Listener = (msg: WsMessage) => void

class WebSocketClient {
  private ws: WebSocket | null = null
  private listeners = new Map<string, Set<Listener>>()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectDelay = 1000

  connect() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${window.location.host}/api/ws`
    this.ws = new WebSocket(url)

    this.ws.onopen = () => {
      this.reconnectDelay = 1000
      this.emit({ type: 'connected' })
    }

    this.ws.onmessage = (e) => {
      try {
        const msg: WsMessage = JSON.parse(e.data)
        this.emit(msg)
      } catch {
        // ignore parse errors
      }
    }

    this.ws.onclose = () => {
      this.emit({ type: 'disconnected' })
      this.scheduleReconnect()
    }

    this.ws.onerror = () => {
      this.ws?.close()
    }
  }

  private scheduleReconnect() {
    if (this.reconnectTimer) return
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, 30_000)
    }, this.reconnectDelay)
  }

  private emit(msg: WsMessage) {
    this.listeners.get(msg.type)?.forEach((fn) => fn(msg))
    this.listeners.get('*')?.forEach((fn) => fn(msg))
  }

  on(type: string, fn: Listener): () => void {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set())
    this.listeners.get(type)!.add(fn)
    return () => this.listeners.get(type)?.delete(fn)
  }

  send(msg: WsMessage) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
    }
  }

  ping() {
    this.send({ type: 'ping' })
  }
}

export const wsClient = new WebSocketClient()
