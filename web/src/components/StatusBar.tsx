import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

/**
 * StatusBar - fixed bottom status indicator (like nonkyc.io).
 *
 * Shows:
 * - WS connection status (green/yellow/red dot + text)
 * - Server time (synced with our backend, falls back to local clock)
 * - Online users count (from WS heartbeat)
 *
 * Fixed at the bottom of every page via App.tsx.
 */
export function StatusBar() {
  const { t } = useTranslation()

  const [wsStatus, setWsStatus] = useState<'connected' | 'connecting' | 'disconnected'>('connecting')
  const [serverTime, setServerTime] = useState<Date>(new Date())
  const [onlineUsers, setOnlineUsers] = useState<number | null>(null)

  // Open a WS connection for status tracking (uses notifications WS)
  useEffect(() => {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const token = localStorage.getItem('goexchange_token')
    const url = token
      ? `${proto}//${window.location.host}/api/v1/ws/notifications?token=${encodeURIComponent(token)}`
      : `${proto}//${window.location.host}/api/v1/ws/notifications`
    let ws: WebSocket | null = null
    try {
      ws = new WebSocket(url)
      setWsStatus('connecting')
      ws.onopen = () => {
        setWsStatus('connected')
        window.dispatchEvent(new CustomEvent('ws-status', { detail: 'open' }))
      }
      ws.onclose = () => {
        setWsStatus('disconnected')
        window.dispatchEvent(new CustomEvent('ws-status', { detail: 'closed' }))
      }
      ws.onerror = () => {
        setWsStatus('disconnected')
        window.dispatchEvent(new CustomEvent('ws-status', { detail: 'closed' }))
      }
      ws.onmessage = (e) => {
        try {
          const env = JSON.parse(e.data)
          // Server sends ping - respond with pong to keep connection alive
          if (env.action === 'ping') {
            ws!.send(JSON.stringify({ action: 'pong', ts: env.ts }))
            return
          }
          // Server stats message: ts + online_users
          if (env.type === 'stats' && env.payload) {
            const p = env.payload
            if (p.ts) {
              const serverNow = new Date(p.ts).getTime()
              const localNow = Date.now()
              const skew = serverNow - localNow
              ;(window as any).__serverSkew = skew
              setServerTime(new Date(localNow + skew))
            }
            if (typeof p.online_users === 'number') {
              setOnlineUsers(p.online_users)
              window.dispatchEvent(new CustomEvent('ws-online-users', { detail: p.online_users }))
            }
            return
          }
        } catch {}
      }
    } catch {
      setWsStatus('disconnected')
    }
    return () => {
      if (ws && ws.readyState === WebSocket.OPEN) ws.close()
    }
  }, [])

  // Listen for WS status changes dispatched by ws client
  useEffect(() => {
    const handler = (e: Event) => {
      const status = (e as CustomEvent).detail
      if (status === 'open' || status === 'closed' || status === 'connecting') {
        setWsStatus(status === 'open' ? 'connected' : status === 'closed' ? 'disconnected' : 'connecting')
      }
    }
    window.addEventListener('ws-status', handler)
    return () => window.removeEventListener('ws-status', handler)
  }, [])

  // Listen for online user count updates (heartbeat events)
  useEffect(() => {
    const handler = (e: Event) => {
      const count = (e as CustomEvent).detail
      if (typeof count === 'number') {
        setOnlineUsers(count)
      }
    }
    window.addEventListener('ws-online-users', handler)
    return () => window.removeEventListener('ws-online-users', handler)
  }, [])

  // Update clock every second
  useEffect(() => {
    const interval = setInterval(() => {
      const skew = (window as any).__serverSkew || 0
      setServerTime(new Date(Date.now() + skew))
    }, 1000)
    return () => clearInterval(interval)
  }, [])

  const dotColor =
    wsStatus === 'connected'
      ? 'bg-green-500'
      : wsStatus === 'connecting'
      ? 'bg-yellow-500 animate-pulse'
      : 'bg-red-500'

  const statusText =
    wsStatus === 'connected'
      ? t('statusBar.stableConnection')
      : wsStatus === 'connecting'
      ? t('statusBar.connecting')
      : t('statusBar.disconnected')

  const formattedTime = serverTime.toLocaleString('en-GB', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
    timeZone: 'UTC',
  }) + ' UTC'

  return (
    <div className="fixed bottom-0 left-0 right-0 z-40 bg-gray-900/95 border-t border-gray-700 backdrop-blur">
      <div className="container mx-auto px-4 py-1.5 flex items-center text-xs">
        {/* Left: connection status + server time */}
        <div className="flex items-center gap-4 flex-1">
          <div className="flex items-center gap-2">
            <span className={`inline-block w-2 h-2 rounded-full ${dotColor}`} />
            <span className="text-gray-300">{statusText}</span>
          </div>
          <div className="hidden sm:flex items-center gap-2 text-gray-400 border-l border-gray-700 pl-4">
            <span>🕐</span>
            <span className="font-mono">{formattedTime}</span>
          </div>
        </div>

        {/* Center: copyright */}
        <div className="hidden md:block text-gray-500 text-center">
          © {new Date().getFullYear()} {t('header.brand', 'goexchange')}. {t('statusBar.allRights')}
        </div>

        {/* Right: online users */}
        <div className="flex items-center gap-2 text-gray-400 flex-1 justify-end">
          <span>👥</span>
          {onlineUsers !== null ? (
            <span>
              <span className="text-white-">{onlineUsers.toLocaleString()}</span> {t('statusBar.usersOnline')}
            </span>
          ) : (
            <span>— {t('statusBar.usersOnline')}</span>
          )}
        </div>
      </div>
    </div>
  )
}
