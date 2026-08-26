// MarketWebSocket - subscribes to /ws/markets and notifies on events.
//
// Used by Markets page to auto-refresh when admin toggles pairs.
// Used by AdminPairs page to sync state across tabs.
//
// Usage:
//   const ws = new MarketWebSocket()
//   ws.onChange = (event) => { ... }
//   ws.connect()

export interface MarketWSEvent {
  type: 'pair.toggled' | 'pair.added' | 'pair.removed' | 'pairs.reloaded' | 'ticker.update'
  base?: string
  quote?: string
  enabled?: boolean
  bid?: string
  ask?: string
  last?: string
  time: string
}

export class MarketWebSocket {
  private ws: WebSocket | null = null
  private reconnectTimer: number | null = null
  private pingTimer: number | null = null
  private url: string
  private shouldReconnect = true

  public onEvent: ((e: MarketWSEvent) => void) | null = null
  public onConnected: (() => void) | null = null
  public onDisconnected: (() => void) | null = null

  constructor() {
    // Same-origin WS URL (proxied via nginx in prod, direct in dev)
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    this.url = `${protocol}//${window.location.host}/api/v1/ws/markets`
  }

  connect() {
    if (this.ws) return // already connected
    this.shouldReconnect = true
    this.open()
  }

  private open() {
    try {
      this.ws = new WebSocket(this.url)

      this.ws.onopen = () => {
        // eslint-disable-next-line no-console
        console.log('[market-ws] connected')
        this.onConnected?.()
        // Start ping to keep connection alive
        this.pingTimer = window.setInterval(() => {
          this.ws?.send('ping')
        }, 25000)
      }

      this.ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as MarketWSEvent
          this.onEvent?.(data)
        } catch (e) {
          // eslint-disable-next-line no-console
          console.warn('[market-ws] invalid message', event.data)
        }
      }

      this.ws.onclose = () => {
        // eslint-disable-next-line no-console
        console.log('[market-ws] disconnected')
        this.onDisconnected?.()
        this.cleanup()
        if (this.shouldReconnect) {
          this.reconnectTimer = window.setTimeout(() => this.open(), 3000)
        }
      }

      this.ws.onerror = (e) => {
        // eslint-disable-next-line no-console
        console.warn('[market-ws] error', e)
      }
    } catch (e) {
      // eslint-disable-next-line no-console
      console.error('[market-ws] connect failed', e)
      if (this.shouldReconnect) {
        this.reconnectTimer = window.setTimeout(() => this.open(), 5000)
      }
    }
  }

  private cleanup() {
    if (this.pingTimer) {
      clearInterval(this.pingTimer)
      this.pingTimer = null
    }
    this.ws = null
  }

  disconnect() {
    this.shouldReconnect = false
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.pingTimer) {
      clearInterval(this.pingTimer)
      this.pingTimer = null
    }
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
  }
}

// Singleton instance for the app
let _instance: MarketWebSocket | null = null

export function getMarketWS(): MarketWebSocket {
  if (!_instance) {
    _instance = new MarketWebSocket()
  }
  return _instance
}
