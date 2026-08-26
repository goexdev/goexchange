import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { useToast } from './Toast'
import { ActiveTriggers } from './ActiveTriggers'

interface OpenOrder {
  id: string
  user_id: string
  pair: string
  base: string
  quote: string
  side: string
  price: string
  quantity: string
  filled_quantity: string
  status: string
  created_at: string
  updated_at: string
}

interface OrdersPanelProps {
  refresh?: () => void
}

export function OrdersPanel({ refresh }: OrdersPanelProps) {
  const { t } = useTranslation()
  const toast = useToast()
  const [tab, setTab] = useState<'open' | 'history' | 'trades' | 'triggers'>('open')
  const [openOrders, setOpenOrders] = useState<OpenOrder[]>([])
  const [history, setHistory] = useState<OpenOrder[]>([])
  const [trades, setTrades] = useState<any[]>([])
  const [error, setError] = useState('')

  // Edit modal state
  const [editing, setEditing] = useState<OpenOrder | null>(null)
  const [editPrice, setEditPrice] = useState('')
  const [editQuantity, setEditQuantity] = useState('')
  const [editLoading, setEditLoading] = useState(false)
  const [editError, setEditError] = useState('')

  useEffect(() => {
    load()
  }, []) // load on mount

  // Poll every 3s to keep Open Orders in sync
  // (no WebSocket for orders yet, so polling is the simple solution)
  useEffect(() => {
    const interval = setInterval(() => {
      load()
    }, 3000)
    return () => clearInterval(interval)
  }, [])

  async function load() {
    try {
      const [all, tr] = await Promise.all([
        api.listOrders().catch(() => []),
        api.getRecentTrades('BTC', 'USDT', 50).catch(() => ({ trades: [] })),
      ])
      const orders = (Array.isArray(all) ? all : []) as OpenOrder[]
      setOpenOrders(orders.filter((o) => o.status === 'OPEN' || o.status === 'PARTIAL'))
      setHistory(orders.filter((o) => o.status !== 'OPEN' && o.status !== 'PARTIAL'))
      setTrades(tr.trades || [])
    } catch (e: any) {
      setError(e.message || String(e))
    }
  }

  async function handleCancel(id: string, pair?: string, base?: string, quote?: string) {
    if (!confirm(t('ordersPanel.cancelConfirm', { pair: pair || '' }))) return
    // Optimistic update - immediately remove from open list
    setOpenOrders(prev => prev.filter(o => o.id !== id))
    try {
      await api.cancelOrder(id, pair)
      toast.showToast(t('ordersPanel.orderCancelled', { pair: pair || '' }), 'success')
      await load()
      refresh?.()
    } catch (e: any) {
      // If cancel failed, reload to restore
      await load()
      toast.showToast(e.message || 'Cancel failed', 'error')
      setError(e.message || String(e))
    }
  }

  async function handleCancelAll() {
    if (!confirm(t('ordersPanel.cancelAllConfirm'))) return
    try {
      const result = await api.cancelAllOrders()
      toast.showToast(
        t('ordersPanel.allCancelled', { count: result.cancelled || 0 }),
        'success'
      )
      await load()
      refresh?.()
    } catch (e: any) {
      toast.showToast(e.message || 'Cancel all failed', 'error')
      setError(e.message || String(e))
    }
  }

  function openEdit(o: OpenOrder) {
    setEditing(o)
    setEditPrice(o.price)
    setEditQuantity(o.quantity)
    setEditError('')
  }

  function closeEdit() {
    setEditing(null)
    setEditPrice('')
    setEditQuantity('')
    setEditError('')
  }

  async function handleAmend() {
    if (!editing) return
    setEditError('')
    setEditLoading(true)
    try {
      await api.amendOrder(editing.id, editing.pair, editPrice, editQuantity)
      closeEdit()
      await load()
      refresh?.()
    } catch (e: any) {
      const msg = e.message || String(e)
      // Try to extract error message from JSON
      try {
        const j = JSON.parse(msg)
        setEditError(j.error || msg)
      } catch {
        setEditError(msg)
      }
    } finally {
      setEditLoading(false)
    }
  }

  return (
    <div className="card">
      {/* Tabs */}
      <div className="flex gap-2 mb-3 border-b border-gray-700">
        <button
          onClick={() => setTab('open')}
          className={`px-3 py-1 text-sm ${
            tab === 'open' ? 'border-b-2 border-blue-500 text-white' : 'text-gray-400'
          }`}>
          {t('ordersPanel.openOrders')} ({openOrders.length})
        </button>
        <button
          onClick={() => setTab('history')}
          className={`px-3 py-1 text-sm ${
            tab === 'history' ? 'border-b-2 border-blue-500 text-white' : 'text-gray-400'
          }`}>
          {t('ordersPanel.orderHistory')}
        </button>
        <button
          onClick={() => setTab('trades')}
          className={`px-3 py-1 text-sm ${
            tab === 'trades' ? 'border-b-2 border-blue-500 text-white' : 'text-gray-400'
          }`}>
          {t('ordersPanel.tradeHistory')}
        </button>
        <button
          onClick={() => setTab('triggers')}
          className={`px-3 py-1 text-sm ${
            tab === 'triggers' ? 'border-b-2 border-blue-500 text-white' : 'text-gray-400'
          }`}>
          {t('ordersPanel.triggers', 'Triggers')}
        </button>
      </div>

      {error && (
        <div className="text-red-400 text-xs mb-2 p-2 bg-red-900/20 rounded">{error}</div>
      )}

      {tab === 'open' && (
        <>
          {openOrders.length === 0 ? (
            <div className="text-center text-gray-500 text-sm py-8">
              {t('ordersPanel.noOpenOrders')}
            </div>
          ) : (
            <>
            <div className="flex justify-end mb-2">
              <button
                onClick={handleCancelAll}
                className="text-xs bg-red-800 hover:bg-red-700 text-white px-2 py-1 rounded">
                {t('ordersPanel.cancelAll')}
              </button>
            </div>
            <table className="w-full text-xs">
              <thead>
                <tr className="text-gray-500 border-b border-gray-700">
                  <th className="py-2 px-2 text-left">{t('ordersPanel.pair')}</th>
                  <th className="py-2 px-2 text-left">{t('ordersPanel.side')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.price')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.quantity')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.filled')}</th>
                  <th className="py-2 px-2 text-left">{t('ordersPanel.status')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {openOrders.map((o) => (
                  <tr key={o.id} className="border-b border-gray-800 hover:bg-gray-800/50">
                    <td className="py-2 px-2 font-mono">{o.pair}</td>
                    <td className="py-2 px-2">
                      <span
                        className={`px-1.5 py-0.5 rounded text-xs ${
                          o.side === 'BUY' ? 'bg-green-900 text-green-200' : 'bg-red-900 text-red-200'
                        }`}>
                        {o.side}
                      </span>
                    </td>
                    <td className="py-2 px-2 text-right font-mono">{parseFloat(o.price).toFixed(2)}</td>
                    <td className="py-2 px-2 text-right font-mono">{parseFloat(o.quantity).toFixed(4)}</td>
                    <td className="py-2 px-2 text-right font-mono text-gray-400">
                      {parseFloat(o.filled_quantity).toFixed(4)}
                    </td>
                    <td className="py-2 px-2">
                      <span
                        className={`px-1.5 py-0.5 rounded text-xs ${
                          o.status === 'OPEN' ? 'bg-blue-900 text-blue-200' :
                          o.status === 'PARTIAL' ? 'bg-yellow-900 text-yellow-200' :
                          'bg-gray-700 text-gray-400'
                        }`}>
                        {o.status}
                      </span>
                    </td>
                    <td className="py-2 px-2 text-right">
                      <div className="flex gap-1 justify-end">
                        <button
                          onClick={() => openEdit(o)}
                          className="text-xs bg-blue-700 hover:bg-blue-600 text-white px-2 py-1 rounded">
                          {t('ordersPanel.edit')}
                        </button>
                        <button
                          onClick={() => handleCancel(o.id, o.pair, o.base, o.quote)}
                          className="text-xs bg-red-700 hover:bg-red-600 text-white px-2 py-1 rounded">
                          {t('ordersPanel.cancel')}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            </>
          )}
        </>
      )}

      {tab === 'history' && (
        <>
          {history.length === 0 ? (
            <div className="text-center text-gray-500 text-sm py-8">
              {t('ordersPanel.noHistory')}
            </div>
          ) : (
            <table className="w-full text-xs">
              <thead>
                <tr className="text-gray-500 border-b border-gray-700">
                  <th className="py-2 px-2 text-left">{t('ordersPanel.pair')}</th>
                  <th className="py-2 px-2 text-left">{t('ordersPanel.side')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.price')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.quantity')}</th>
                  <th className="py-2 px-2 text-left">{t('ordersPanel.status')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.time')}</th>
                </tr>
              </thead>
              <tbody>
                {history.map((o) => (
                  <tr key={o.id} className="border-b border-gray-800">
                    <td className="py-2 px-2 font-mono">{o.pair}</td>
                    <td className="py-2 px-2">
                      <span
                        className={`px-1.5 py-0.5 rounded text-xs ${
                          o.side === 'BUY' ? 'bg-green-900 text-green-200' : 'bg-red-900 text-red-200'
                        }`}>
                        {o.side}
                      </span>
                    </td>
                    <td className="py-2 px-2 text-right font-mono">{parseFloat(o.price).toFixed(2)}</td>
                    <td className="py-2 px-2 text-right font-mono">{parseFloat(o.quantity).toFixed(4)}</td>
                    <td className="py-2 px-2">
                      <span
                        className={`px-1.5 py-0.5 rounded text-xs ${
                          o.status === 'FILLED' ? 'bg-green-900 text-green-200' :
                          o.status === 'CANCELLED' ? 'bg-gray-700 text-gray-400' :
                          'bg-gray-700 text-gray-400'
                        }`}>
                        {o.status}
                      </span>
                    </td>
                    <td className="py-2 px-2 text-right text-gray-500">
                      {new Date(o.updated_at).toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}

      {tab === 'trades' && (
        <>
          {trades.length === 0 ? (
            <div className="text-center text-gray-500 text-sm py-8">
              {t('ordersPanel.noTrades')}
            </div>
          ) : (
            <table className="w-full text-xs">
              <thead>
                <tr className="text-gray-500 border-b border-gray-700">
                  <th className="py-2 px-2 text-left">{t('ordersPanel.pair')}</th>
                  <th className="py-2 px-2 text-left">{t('ordersPanel.side')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.price')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.quantity')}</th>
                  <th className="py-2 px-2 text-right">{t('ordersPanel.time')}</th>
                </tr>
              </thead>
              <tbody>
                {trades.map((tr) => (
                  <tr key={tr.id} className="border-b border-gray-800">
                    <td className="py-2 px-2 font-mono">{tr.pair}</td>
                    <td className="py-2 px-2">
                      <span
                        className={`px-1.5 py-0.5 rounded text-xs ${
                          tr.side === 'BUY' ? 'bg-green-900 text-green-200' : 'bg-red-900 text-red-200'
                        }`}>
                        {tr.side}
                      </span>
                    </td>
                    <td className="py-2 px-2 text-right font-mono">{parseFloat(tr.price).toFixed(2)}</td>
                    <td className="py-2 px-2 text-right font-mono">{parseFloat(tr.quantity).toFixed(4)}</td>
                    <td className="py-2 px-2 text-right text-gray-500">
                      {new Date(tr.executed_at).toLocaleString()}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}

      {/* Edit Order Modal */}
      {editing && (
        <div
          className="fixed inset-0 bg-black/60 flex items-center justify-center z-50"
          onClick={closeEdit}>
          <div
            className="bg-gray-800 rounded-lg p-6 w-full max-w-md mx-4 shadow-xl"
            onClick={(e) => e.stopPropagation()}>
            <h3 className="text-lg font-semibold mb-4">
              {t('ordersPanel.editOrderTitle')}
            </h3>

            <div className="text-xs text-gray-400 mb-4">
              <div>
                {t('ordersPanel.pair')}: <span className="font-mono text-white">{editing.pair}</span>
              </div>
              <div>
                {t('ordersPanel.side')}:{' '}
                <span
                  className={`px-1.5 py-0.5 rounded text-xs ${
                    editing.side === 'BUY' ? 'bg-green-900 text-green-200' : 'bg-red-900 text-red-200'
                  }`}>
                  {editing.side}
                </span>
              </div>
              <div>
                {t('ordersPanel.filled')}:{' '}
                <span className="font-mono">{parseFloat(editing.filled_quantity).toFixed(4)}</span>
              </div>
              <div className="mt-2 text-gray-500">{t('ordersPanel.editHint')}</div>
            </div>

            <div className="space-y-3">
              <div>
                <label className="text-xs text-gray-400 block mb-1">
                  {t('ordersPanel.price')} ({editing.quote})
                </label>
                <input
                  type="number"
                  step="any"
                  value={editPrice}
                  onChange={(e) => setEditPrice(e.target.value)}
                  className="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm font-mono"
                  data-testid="edit-price-input"
                />
              </div>
              <div>
                <label className="text-xs text-gray-400 block mb-1">
                  {t('ordersPanel.quantity')} ({editing.base})
                </label>
                <input
                  type="number"
                  step="any"
                  value={editQuantity}
                  onChange={(e) => setEditQuantity(e.target.value)}
                  className="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2 text-sm font-mono"
                  data-testid="edit-quantity-input"
                />
              </div>
            </div>

            {editError && (
              <div className="mt-3 text-red-400 text-xs p-2 bg-red-900/20 rounded">
                {editError}
              </div>
            )}

            <div className="flex gap-2 mt-4">
              <button
                onClick={closeEdit}
                disabled={editLoading}
                className="flex-1 px-3 py-2 bg-gray-700 hover:bg-gray-600 text-white rounded text-sm">
                {t('ordersPanel.cancel')}
              </button>
              <button
                onClick={handleAmend}
                disabled={editLoading}
                className="flex-1 px-3 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded text-sm disabled:opacity-50"
                data-testid="edit-submit-button">
                {editLoading ? t('ordersPanel.amending') : t('ordersPanel.amend')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
