import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { formatNumber, formatPrice, formatTime } from '../lib/utils'
import { useToast } from '../components/Toast'

export function Orders() {
  const { t } = useTranslation()
  const toast = useToast()
  const [orders, setOrders] = useState<api.Order[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<'ALL' | 'OPEN' | 'FILLED' | 'CANCELLED'>('ALL')
  const [cancellingId, setCancellingId] = useState<string | null>(null)
  const [cancellingAll, setCancellingAll] = useState(false)

  async function load() {
    try {
      const data = await api.listOrders()
      setOrders(data)
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to load orders', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [])

  async function onCancel(o: api.Order) {
    if (!confirm(t('orders.cancelConfirm', 'Cancel this order?'))) return
    setCancellingId(o.id)
    try {
      await api.cancelOrder(o.id, o.pair || (o.base && o.quote ? `${o.base}_${o.quote}` : undefined))
      toast.showToast(t('orders.cancelled', 'Order cancelled'), 'success')
      await load()
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to cancel order', 'error')
    } finally {
      setCancellingId(null)
    }
  }

  async function onCancelAll() {
    const openOrders = orders.filter(o => o.status === 'OPEN' || o.status === 'PARTIAL')
    if (openOrders.length === 0) {
      toast.showToast(t('orders.noOpenOrders', 'No open orders to cancel'), 'info')
      return
    }
    if (!confirm(t('orders.cancelAllConfirm', `Cancel all ${openOrders.length} open orders?`))) return
    setCancellingAll(true)
    try {
      const result = await api.cancelAllOrders()
      toast.showToast(
        t('orders.cancelledAll', `Cancelled ${result.cancelled} orders`),
        'success'
      )
      await load()
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to cancel all', 'error')
    } finally {
      setCancellingAll(false)
    }
  }

  const filtered = orders.filter((o) => {
    if (filter === 'ALL') return true
    if (filter === 'OPEN') return o.status === 'OPEN' || o.status === 'PARTIAL'
    return o.status === filter
  })

  const openCount = orders.filter(o => o.status === 'OPEN' || o.status === 'PARTIAL').length

  function statusBadge(status: string) {
    const colors: Record<string, string> = {
      OPEN: 'bg-blue-900 text-blue-200',
      PARTIAL: 'bg-yellow-900 text-yellow-200',
      FILLED: 'bg-green-900 text-green-200',
      CANCELLED: 'bg-red-900 text-red-200',
    }
    return `px-2 py-1 rounded text-xs ${colors[status] || 'bg-gray-700'}`
  }

  function sideBadge(side: string) {
    return side === 'BUY'
      ? 'px-2 py-1 rounded text-xs bg-green-900 text-green-200'
      : 'px-2 py-1 rounded text-xs bg-red-900 text-red-200'
  }

  if (loading && orders.length === 0) {
    return <div className="p-8 text-center">{t('common.loadingEllipsis')}</div>
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-6 flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold">{t('orders.title', 'Orders')}</h1>
          <p className="text-sm text-gray-400 mt-1">
            {t('orders.totalCount', '{{count}} total, {{open}} open', {
              count: orders.length,
              open: openCount,
            })}
          </p>
        </div>
        <div className="flex gap-2 flex-wrap">
          {openCount > 0 && (
            <button
              onClick={onCancelAll}
              disabled={cancellingAll}
              className="px-3 py-1 rounded text-sm bg-red-700 text-white hover:bg-red-600 disabled:opacity-50"
            >
              {cancellingAll
                ? t('common.loading')
                : t('orders.cancelAll', { count: openCount })}
            </button>
          )}
        </div>
      </div>

      <div className="flex gap-2 mb-4 flex-wrap">
        {(['ALL', 'OPEN', 'FILLED', 'CANCELLED'] as const).map((f) => {
          const count = f === 'ALL'
            ? orders.length
            : orders.filter(o => o.status === f || (f === 'OPEN' && o.status === 'PARTIAL')).length
          return (
            <button
              key={f}
              onClick={() => setFilter(f)}
              className={`px-3 py-1 rounded text-sm ${
                filter === f ? 'bg-brand text-black font-semibold' : 'bg-panel text-muted border border-border'
              }`}
            >
              {f} ({count})
            </button>
          )
        })}
      </div>

      {filtered.length === 0 ? (
        <div className="card text-center py-12">
          <div className="text-4xl mb-3">{t('common.empty', '(empty)')}</div>
          <p className="text-gray-400">
            {filter === 'ALL'
              ? t('orders.noOrders', 'No orders yet')
              : t('orders.noFilteredOrders', `No ${filter.toLowerCase()} orders`)}
          </p>
          <Link to="/markets" className="btn-primary inline-block mt-4">
            {t('orders.browseMarkets', 'Browse Markets')}
          </Link>
        </div>
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left">
                <th className="p-2">{t('orders.time', 'Time')}</th>
                <th className="p-2">{t('orders.pair', 'Pair')}</th>
                <th className="p-2">{t('orders.side', 'Side')}</th>
                <th className="p-2">{t('orders.type', 'Type')}</th>
                <th className="p-2 text-right">{t('orders.price', 'Price')}</th>
                <th className="p-2 text-right">{t('orders.quantity', 'Quantity')}</th>
                <th className="p-2 text-right">{t('orders.filled', 'Filled')}</th>
                <th className="p-2">{t('orders.status', 'Status')}</th>
                <th className="p-2 text-right">{t('orders.actions', 'Actions')}</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((o) => {
                const pair = o.pair || `${o.base}/${o.quote}`
                const canCancel = o.status === 'OPEN' || o.status === 'PARTIAL'
                const filledNum = parseFloat(o.filled_quantity)
                const filledPct = parseFloat(o.quantity) > 0 && filledNum > 0
                  ? (filledNum / parseFloat(o.quantity) * 100).toFixed(1)
                  : null
                return (
                  <tr key={o.id} className="border-b border-border/50 hover:bg-bg/30">
                    <td className="p-2 text-xs">{formatTime(o.created_at)}</td>
                    <td className="p-2 font-mono">{pair}</td>
                    <td className="p-2"><span className={sideBadge(o.side)}>{o.side}</span></td>
                    <td className="p-2 text-xs">{o.type}</td>
                    <td className="p-2 text-right font-mono">{formatPrice(o.price)}</td>
                    <td className="p-2 text-right font-mono">{formatNumber(o.quantity)}</td>
                    <td className="p-2 text-right font-mono text-xs">
                      {formatNumber(o.filled_quantity)}
                      {filledPct && <span className="text-muted ml-1">({filledPct}{t("orders.percent", "%")})</span>}
                    </td>
                    <td className="p-2"><span className={statusBadge(o.status)}>{o.status}</span></td>
                    <td className="p-2 text-right">
                      {canCancel ? (
                        <button
                          onClick={() => onCancel(o)}
                          disabled={cancellingId === o.id}
                          className="text-xs px-2 py-1 bg-red-700 text-white rounded hover:bg-red-600 disabled:opacity-50"
                        >
                          {cancellingId === o.id
                            ? t('common.loading', 'Loading...')
                            : t('orders.cancel', 'Cancel')}
                        </button>
                      ) : (
                        <span className="text-xs text-muted">-</span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}