import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'

export function NotificationBell() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [notifs, setNotifs] = useState<api.Notification[]>([])
  const [loading, setLoading] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)

  // WebSocket for real-time notifications + initial load
  useEffect(() => {
    let mounted = true
    let ws: WebSocket | null = null
    let reconnectTimer: number | null = null
    let pollTimer: number | null = null

    async function load() {
      try {
        const data = await api.listNotifications()
        if (mounted) setNotifs(data)
      } catch (e) {
        console.error('load notifications failed', e)
      }
    }

    function connectWS() {
      const token = localStorage.getItem('goexchange_token')
      if (!token) return

      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const wsUrl = `${proto}//${window.location.host}/api/v1/ws/notifications?token=${encodeURIComponent(token)}`

      ws = new WebSocket(wsUrl)
      ws.onopen = () => {
        console.log('notif ws connected')
      }
      ws.onmessage = (e) => {
        try {
          const env = JSON.parse(e.data)
          if (env.type === 'notification' && env.payload) {
            // Prepend new notification
            setNotifs((prev) => [env.payload, ...prev])
          }
        } catch (err) {
          console.error('ws parse failed', err)
        }
      }
      ws.onerror = () => {
        console.warn('notif ws error')
      }
      ws.onclose = () => {
        console.log('notif ws closed, reconnecting in 5s')
        if (mounted) {
          reconnectTimer = window.setTimeout(connectWS, 5000)
        }
      }
    }

    load()
    connectWS()
    // Fallback poll every 30s (in case WS misses)
    pollTimer = window.setInterval(load, 30000)

    return () => {
      mounted = false
      if (reconnectTimer) clearTimeout(reconnectTimer)
      if (pollTimer) clearInterval(pollTimer)
      if (ws) ws.close()
    }
  }, [])

  // Close dropdown on click outside
  useEffect(() => {
    function handler(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    if (open) {
      document.addEventListener('mousedown', handler)
      return () => document.removeEventListener('mousedown', handler)
    }
  }, [open])

  const unread = notifs.filter((n) => !n.read_at).length

  async function markRead(id: string) {
    try {
      await api.markNotificationRead(id)
      setNotifs((prev) =>
        prev.map((n) => (n.id === id ? { ...n, read_at: new Date().toISOString() } : n))
      )
    } catch (e) {
      console.error('mark read failed', e)
    }
  }

  function typeBadge(type: string) {
    const colors: Record<string, string> = {
      KYC_APPROVED: 'bg-green-100 text-green-800',
      KYC_REJECTED: 'bg-red-100 text-red-800',
      WITHDRAWAL_HELD_APPROVED: 'bg-blue-100 text-blue-800',
      WITHDRAWAL_DONE: 'bg-green-100 text-green-800',
      LARGE_WITHDRAW: 'bg-yellow-100 text-yellow-800',
      LOGIN_RISK: 'bg-orange-100 text-orange-800',
    }
    return colors[type] || 'bg-gray-100 text-gray-800'
  }

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setOpen(!open)}
        className="relative p-2 hover:bg-bg rounded-full transition"
        aria-label={t('notificationBell.title')}
      >
        {/* Bell icon (SVG) */}
        <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
        </svg>
        {unread > 0 && (
          <span className="absolute -top-1 -right-1 bg-red-600 text-white text-xs rounded-full px-1.5 min-w-[18px] h-[18px] flex items-center justify-center font-bold">
            {unread > 9 ? '9+' : unread}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 mt-2 w-80 bg-panel border border-border rounded-lg shadow-xl z-50">
          <div className="px-4 py-3 border-b border-border flex justify-between items-center">
            <h3 className="font-semibold text-sm">{t('notificationBell.title')}</h3>
            <div className="flex items-center gap-2">
              {unread > 0 && (
                <span className="text-xs text-muted">
                  {t('notificationBell.unread', { count: unread })}
                </span>
              )}
              <Link
                to="/notifications"
                onClick={() => setOpen(false)}
                className="text-xs text-blue-400 hover:underline"
              >
                {t('notificationBell.viewAll')}
              </Link>
            </div>
          </div>
          <div className="max-h-96 overflow-y-auto">
            {loading && notifs.length === 0 ? (
              <div className="px-4 py-8 text-center text-muted text-sm">
                {t('notificationBell.loading')}
              </div>
            ) : notifs.length === 0 ? (
              <div className="px-4 py-8 text-center text-muted text-sm">
                {t('notificationBell.noNotifications')}
              </div>
            ) : (
              notifs.map((n) => (
                <div
                  key={n.id}
                  className={`px-4 py-3 border-b border-border last:border-b-0 hover:bg-bg cursor-pointer transition \${
                    !n.read_at ? 'bg-bg' : ''
                  }`}
                  onClick={() => !n.read_at && markRead(n.id)}
                >
                  <div className="flex items-start justify-between gap-2 mb-1">
                    <span className={`text-xs px-2 py-0.5 rounded \${typeBadge(n.type)}`}>
                      {n.type.replace(/_/g, ' ')}
                    </span>
                    {!n.read_at && (
                      <span className="w-2 h-2 bg-red-600 rounded-full mt-1.5 flex-shrink-0" />
                    )}
                  </div>
                  <div className="text-sm font-medium mb-1">{n.title}</div>
                  <div className="text-xs text-muted">{n.body}</div>
                  <div className="text-xs text-muted mt-1">
                    {new Date(n.created_at).toLocaleString()}
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}
