import { useEffect, useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import * as api from '../lib/api'

type Filter = 'all' | 'unread' | 'read'

export function Notifications() {
  const { t } = useTranslation()
  const [notifs, setNotifs] = useState<api.Notification[]>([])
  const [filter, setFilter] = useState<Filter>('all')
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [page, setPage] = useState(1)
  const perPage = 20

  useEffect(() => {
    load()
    // Refresh every 10s for new notifications
    const t = setInterval(load, 10000)
    return () => clearInterval(t)
  }, [])

  // Visual feedback for keyboard shortcut
  const [justMarked, setJustMarked] = useState(false)

  // WebSocket for real-time
  useEffect(() => {
    const token = localStorage.getItem('goexchange_token')
    if (!token) return
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(
      `${proto}//${window.location.host}/api/v1/ws/notifications?token=${encodeURIComponent(token)}`
    )
    ws.onmessage = e => {
      try {
        const env = JSON.parse(e.data)
        if (env.type === 'notification' && env.payload) {
          setNotifs(prev => [env.payload, ...prev])
        }
      } catch (err) {}
    }
    return () => ws.close()
  }, [])

  // Keyboard shortcut: Ctrl/Cmd + Shift + M to mark all as read
  useEffect(() => {
    function handleKeydown(e: KeyboardEvent) {
      // Check for Ctrl/Cmd + Shift + M
      if (e.shiftKey && (e.key === 'M' || e.key === 'm') && (e.ctrlKey || e.metaKey)) {
        // Only if not in an input field
        const tag = (e.target as HTMLElement)?.tagName
        if (tag === 'INPUT' || tag === 'TEXTAREA') return
        e.preventDefault()
        // Always trigger - the function itself checks if there are unread
        markAllRead()
        setJustMarked(true)
        setTimeout(() => setJustMarked(false), 1500)
      }
    }
    window.addEventListener('keydown', handleKeydown)
    return () => window.removeEventListener('keydown', handleKeydown)
  }, [])

  async function load() {
    setLoading(true)
    try {
      const data = await api.listNotifications()
      setNotifs(data)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  async function markRead(id: string) {
    try {
      await api.markNotificationRead(id)
      setNotifs(prev =>
        prev.map(n => (n.id === id ? { ...n, read_at: new Date().toISOString() } : n))
      )
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function markAllRead() {
    try {
      // Optimistic update
      setNotifs(prev => prev.map(n => ({ ...n, read_at: n.read_at || new Date().toISOString() })))
      // Call backend bulk endpoint
      const token = localStorage.getItem('goexchange_token')
      const res = await fetch('/api/v1/users/me/notifications/read-all', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!res.ok) throw new Error('Failed to mark all as read')
    } catch (e: any) {
      setErr(e.message)
      // Reload on error
      load()
    }
  }

  // Filter and paginate
  const filtered = useMemo(() => {
    let r = notifs
    if (filter === 'unread') r = r.filter(n => !n.read_at)
    if (filter === 'read') r = r.filter(n => n.read_at)
    return r
  }, [notifs, filter])

  const unreadCount = useMemo(() => notifs.filter(n => !n.read_at).length, [notifs])
  const totalPages = Math.ceil(filtered.length / perPage)
  const paginated = filtered.slice((page - 1) * perPage, page * perPage)

  // Notification type icon/color
  function notifStyle(type: string) {
    if (type.includes('KYC')) return { icon: '👤', color: 'text-blue-400', bg: 'bg-blue-900' }
    if (type.includes('WITHDRAWAL_DONE')) return { icon: '✅', color: 'text-green-400', bg: 'bg-green-900' }
    if (type.includes('WITHDRAWAL_HELD')) return { icon: '⏸', color: 'text-yellow-400', bg: 'bg-yellow-900' }
    if (type.includes('WITHDRAW')) return { icon: '💸', color: 'text-blue-400', bg: 'bg-blue-900' }
    if (type.includes('LOGIN_RISK')) return { icon: '🔐', color: 'text-red-400', bg: 'bg-red-900' }
    if (type.includes('DEPOSIT')) return { icon: '⬇', color: 'text-green-400', bg: 'bg-green-900' }
    return { icon: 'ℹ', color: 'text-blue-400', bg: 'bg-blue-900' }
  }

  function timeAgo(iso: string): string {
    const ms = Date.now() - new Date(iso).getTime()
    const s = Math.floor(ms / 1000)
    if (s < 60) return `${s}s ago`
    if (s < 3600) return `${Math.floor(s / 60)}m ago`
    if (s < 86400) return `${Math.floor(s / 3600)}h ago`
    return `${Math.floor(s / 86400)}d ago`
  }

  return (
    <div className="max-w-4xl mx-auto space-y-4">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-bold">{t('notifications.title')}</h1>
          <p className="text-sm text-gray-400">
            {t('notifications.total', { count: notifs.length })} · {t('notifications.unread', { count: unreadCount })}
            {unreadCount > 0 && (
              <span className="ml-2 text-xs text-gray-500">
                · {t('notifications.pressShortcut')}{' '}
                <kbd className="bg-gray-800 border border-gray-700 px-1.5 py-0.5 rounded text-xs font-mono">
                  {t('notifications.shortcut')}
                </kbd>{' '}
                {t('notifications.toMarkAllRead')}
              </span>
            )}
          </p>
        </div>
        <div className="flex gap-2 items-center">
          {unreadCount > 0 && (
            <>
              <button
                onClick={markAllRead}
                className={`px-4 py-2 rounded text-sm flex items-center gap-2 transition-all ${
                  justMarked
                    ? 'bg-green-600 text-white'
                    : 'bg-blue-600 hover:bg-blue-700 text-white'
                }`}
              >
                {justMarked ? (
                  <>✓ {t('notifications.marked', { count: unreadCount })}</>
                ) : (
                  <>✓ {t('notifications.markAllRead', { count: unreadCount })}</>
                )}
              </button>
              <kbd className="hidden md:inline-block text-xs text-gray-500 bg-gray-800 border border-gray-700 px-2 py-1 rounded font-mono">
                {t('notifications.shortcut')}
              </kbd>
            </>
          )}
        </div>
      </div>

      {err && <div className="bg-red-900 text-red-200 p-3 rounded text-sm">{err}</div>}

      {/* Filter tabs */}
      <div className="flex gap-2 border-b border-gray-700">
        {(['all', 'unread', 'read'] as const).map(f => (
          <button
            key={f}
            onClick={() => {
              setFilter(f)
              setPage(1)
            }}
            className={`px-4 py-2 text-sm border-b-2 ${
              filter === f
                ? 'border-blue-500 text-blue-400'
                : 'border-transparent text-gray-400 hover:text-white'
            }`}
          >
            {f.charAt(0).toUpperCase() + f.slice(1)}
            {f === 'unread' && unreadCount > 0 && (
              <span className="ml-2 bg-blue-600 text-white text-xs rounded-full px-2">
                {unreadCount}
              </span>
            )}
          </button>
        ))}
      </div>

      {/* Notifications list */}
      <div className="space-y-2">
        {loading && notifs.length === 0 ? (
          <div className="card text-center text-gray-500">{t('notifications.loading')}</div>
        ) : paginated.length === 0 ? (
          <div className="card text-center text-gray-500 py-12">
            {filter === 'unread' ? t('notifications.noUnread') : t('notifications.noNotifications')}
          </div>
        ) : (
          paginated.map(n => {
            const style = notifStyle(n.type)
            const isUnread = !n.read_at
            return (
              <div
                key={n.id}
                className={`card flex gap-3 transition-colors ${
                  isUnread ? 'border-l-4 border-blue-500' : ''
                }`}
              >
                <div className={`text-2xl ${style.bg} w-12 h-12 rounded-full flex items-center justify-center flex-shrink-0`}>
                  {style.icon}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex-1 min-w-0">
                      <div className={`font-semibold ${isUnread ? 'text-white' : 'text-gray-400'}`}>
                        {n.title}
                      </div>
                      <div className={`text-sm mt-1 ${isUnread ? 'text-gray-300' : 'text-gray-500'}`}>
                        {n.body}
                      </div>
                      <div className="flex items-center gap-3 mt-2 text-xs text-gray-500">
                        <span>{n.type}</span>
                        <span>·</span>
                        <span>{timeAgo(n.created_at)}</span>
                      </div>
                    </div>
                    {isUnread && (
                      <button
                        onClick={() => markRead(n.id)}
                        className="text-xs bg-gray-700 hover:bg-gray-600 text-gray-300 px-2 py-1 rounded flex-shrink-0"
                        title={t('notificationBell.markAsRead')}
                      >
                        {t('notifications.markedRead')}
                      </button>
                    )}
                  </div>
                </div>
              </div>
            )
          })
        )}
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex justify-center gap-2 mt-4">
          <button
            onClick={() => setPage(p => Math.max(1, p - 1))}
            disabled={page === 1}
            className="bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 disabled:text-gray-600 px-3 py-1 rounded"
          >
            {t('notifications.prev')}
          </button>
          <span className="px-3 py-1 text-gray-400">
            {t('notifications.page', { page, total: totalPages })}
          </span>
          <button
            onClick={() => setPage(p => Math.min(totalPages, p + 1))}
            disabled={page === totalPages}
            className="bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 disabled:text-gray-600 px-3 py-1 rounded"
          >
            {t('notifications.next')}
          </button>
        </div>
      )}

      {/* Quick links */}
      <div className="card bg-gray-800">
        <h3 className="text-sm font-semibold mb-2">{t('notifications.notificationTypes')}</h3>
        <div className="grid grid-cols-2 md:grid-cols-3 gap-2 text-xs text-gray-400">
          <div>{t('notifications.types.kyc')}</div>
          <div>{t('notifications.types.withdrawDone')}</div>
          <div>{t('notifications.types.withdrawHold')}</div>
          <div>{t('notifications.types.deposit')}</div>
          <div>{t('notifications.types.login')}</div>
          <div>{t('notifications.types.large')}</div>
        </div>
      </div>
    </div>
  )
}
