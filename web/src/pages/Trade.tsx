import { FormEvent, useEffect, useState, useMemo, useRef } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { formatNumber, formatPrice } from '../lib/utils'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { PriceChart } from '../components/PriceChart'
import { Sparkline } from '../components/Sparkline'
import { OrdersPanel } from '../components/OrdersPanel'
import { DepthChart } from '../components/DepthChart'
import { useToast } from '../components/Toast'
import { ConfirmModal } from '../components/ConfirmModal'
import { getUserPrefs } from '../lib/userPrefs'
import { useAuth } from '../lib/auth'

type OrderBookTab = 'orderbook' | 'depth' | 'trades'
type Timeframe = '1d' | '1w' | '1m'
type Interval = '1m' | '5m' | '15m' | '1h' | '1d'
type OrderBookFilter = 'both' | 'bids' | 'asks'



export function Trade() {
  const { base, quote } = useParams<{ base: string; quote: string }>()
  const pair = `${base}_${quote}`
  const { t } = useTranslation()
  const { user } = useAuth()
  const toast = useToast()
  useDocumentTitle(`Trade ${base}/${quote} - goexchange`)
  const [orderbook, setOrderbook] = useState<api.OrderBook | null>(null)
  const [ticker, setTicker] = useState<api.Ticker | null>(null)
  const [stats, setStats] = useState<api.Market24hStats | null>(null)
  const [recentTrades, setRecentTrades] = useState<api.RecentTrade[]>([])
  const [wallets, setWallets] = useState<api.Balance[]>([])
  const [side, setSide] = useState<'BUY' | 'SELL'>('BUY')
  const [orderType, setOrderType] = useState<'LIMIT' | 'MARKET' | 'TRIGGER'>('LIMIT')
  const [TriggerType, setTriggerType] = useState<'STOP_LOSS' | 'TAKE_PROFIT'>('STOP_LOSS')
  const [triggerPrice, setTriggerPrice] = useState('')
  const [activeTriggers, setActiveTriggers] = useState<api.TriggerOrder[]>([])
  const [price, setPrice] = useState('')
  const [quantity, setQuantity] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [msg, setMsg] = useState('')

  // New state for cexius-style features
  const [obTab, setObTab] = useState<OrderBookTab>('orderbook')
  const [obFilter, setObFilter] = useState<OrderBookFilter>('both')
  const [pricePrecision] = useState(2)

  const [percent, setPercent] = useState<number | null>(null)

  // WebSocket state
  const [wsState, setWsState] = useState<'connecting' | 'open' | 'closed'>('connecting')
  const [wsLatencyMs, setWsLatencyMs] = useState<number>(0)
  const wsRef = useRef<WebSocket | null>(null)

  async function load() {
    if (!base || !quote) return
    try {
      const [ob, t, w, s, tr] = await Promise.all([
        api.getOrderBook(base, quote).catch(() => null),
        api.getTicker(base, quote).catch(() => null),
        api.getWallets().catch(() => []),
        api.get24hStats(base, quote).catch(() => null),
        api.getRecentTrades(base, quote, 50).catch(() => ({ trades: [], pair })),
      ])
      setOrderbook(ob)
      setTicker(t)
      setWallets(w || [])
      setStats(s)
      setRecentTrades(tr.trades || [])
    } catch (e) {
      console.error(e)
    }
  }

  useEffect(() => {
    if (!base || !quote) return
    load()  // initial load
    // Refresh data periodically (orderbook is real-time via WebSocket)
    // We also poll orderbook every 5s as a fallback in case WS events are dropped
    const t = setInterval(() => {
      // Refresh ticker, recent trades, wallets, AND orderbook (fallback)
      Promise.all([
        api.getTicker(base, quote).catch(() => null),
        api.getRecentTrades(base, quote, 50).catch(() => ({ trades: [], pair })),
        api.getWallets().catch(() => []),
        api.getOrderBook(base, quote).catch(() => null),
      ]).then(([tk, tr, w, ob]) => {
        if (tk) setTicker(tk)
        if (tr?.trades) setRecentTrades(tr.trades)
        if (w) setWallets(w)
        // Fallback: only update from REST if WS hasn't updated recently
        // This prevents stomping on live WS updates
        if (ob) {
          setOrderbook((prev) => {
            // Always accept the REST snapshot (it's authoritative)
            // The WS update arrives faster but REST catches any missed events
            return {
              pair: ob.pair || (prev?.pair ?? `${base}_${quote}`),
              bids: (ob.bids || []).map((b: any) => ({
                price: String(b.price),
                quantity: String(b.quantity),
              })),
              asks: (ob.asks || []).map((a: any) => ({
                price: String(a.price),
                quantity: String(a.quantity),
              })),
            }
          })
        }
      })
    }, 3000)  // Refresh every 3s
    return () => clearInterval(t)
  }, [base, quote])

  // WebSocket for real-time price
  useEffect(() => {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    // Connect to orderbook WS (proxied by nginx to matcher)
    const url = `${proto}//${window.location.host}/api/v1/ws/orderbook`
    const ws = new WebSocket(url)
    wsRef.current = ws
    const start = Date.now()
    ws.onopen = () => {
      setWsState('open')
      setWsLatencyMs(Date.now() - start)
      // Subscribe to our pair
      ws.send(JSON.stringify({ action: 'subscribe', pair }))
    }
    ws.onmessage = e => {
      try {
        const env = JSON.parse(e.data)
        // Real-time orderbook updates
        if ((env.type === 'snapshot' || env.type === 'update') && env.pair === pair) {
          const ob = env.data
          if (ob && (ob.bids || ob.asks)) {
            setOrderbook({
              pair: env.pair,
              bids: (ob.bids || []).map((b: any) => ({
                price: String(b.price || b[0]),
                quantity: String(b.quantity || b[1]),
              })),
              asks: (ob.asks || []).map((a: any) => ({
                price: String(a.price || a[0]),
                quantity: String(a.quantity || a[1]),
              })),
            })
          }
        }
        // Real-time ticker + candle updates on trades
        if (env.type === 'trade' && env.data?.pair === pair) {
          const last = parseFloat(env.data.price)
          if (last > 0) {
            setTicker(t => (t ? { ...t, last: String(last) } : t))
            // Dispatch trade event for PriceChart real-time updates
            window.dispatchEvent(new CustomEvent('ws-trade', {
              detail: {
                pair: env.data.pair,
                price: env.data.price,
                quantity: env.data.quantity,
                timestamp: env.data.executed_at || env.data.timestamp || new Date().toISOString(),
              }
            }))
          }
        }
      } catch {}
    }
    ws.onclose = () => setWsState('closed')
    ws.onerror = () => setWsState('closed')
    return () => ws.close()
  }, [pair])

  // Compute cumulative totals for order book
  const obWithTotals = useMemo(() => {
    if (!orderbook) return null
    let bidTotal = 0
    const bids = orderbook.bids.map(b => {
      const qty = parseFloat(b.quantity)
      bidTotal += qty
      return { ...b, total: bidTotal, totalUSD: bidTotal * parseFloat(b.price) }
    })
    let askTotal = 0
    const asks = orderbook.asks.map(a => {
      const qty = parseFloat(a.quantity)
      askTotal += qty
      return { ...a, total: askTotal, totalUSD: askTotal * parseFloat(a.price) }
    })
    return { bids, asks, maxTotal: Math.max(bidTotal, askTotal) }
  }, [orderbook])

  // Set price to best bid/ask when side changes
  useEffect(() => {
    if (ticker) {
      if (side === 'BUY' && ticker.bid) setPrice(ticker.bid)
      else if (side === 'SELL' && ticker.ask) setPrice(ticker.ask)
    }
  }, [side, ticker?.bid, ticker?.ask])

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setMsg('')

    // Check if user wants confirmation before placing order
    const prefs = getUserPrefs()
    if (prefs.confirmOrder && !confirmOpen) {
      setConfirmOpen(true)
      return
    }

    await placeOrderNow()
  }

  async function placeOrderNow() {
    setConfirmOpen(false)
    setSubmitting(true)
    try {
      if (orderType === 'TRIGGER') {
        const tp = triggerPrice || price
        await api.createTrigger(pair, side, TriggerType, tp, quantity)
        toast.showToast(
          t('trade.triggerCreated', { type: TriggerType === 'STOP_LOSS' ? t('trade.stopLoss') : t('trade.takeProfit'), price: tp }),
          'success'
        )
        setTriggerPrice('')
        return
      }
      const res = await api.placeOrder(pair, side, orderType === 'MARKET' ? '0' : price, quantity, orderType)
      const isFilled = res.status === 'FILLED'
      toast.showToast(
        t('trade.orderPlaced', { status: res.status, filled: res.filled }),
        isFilled ? 'success' : 'info'
      )
      setMsg('')
      setQuantity('')
      setPercent(null)
      load()
    } catch (e: any) {
      toast.showToast(e.message || 'Order failed', 'error')
      setMsg(e.message)
    } finally {
      setSubmitting(false)
    }
  }

  // Apply percentage of balance
  function applyPercent(p: number) {
    setPercent(p)
    const balance = wallets.find(w => w.asset === (side === 'BUY' ? quote : base))
    if (!balance || !price) return
    const bal = parseFloat(balance.available)
    if (side === 'BUY') {
      // Can buy quote/price = quantity of base
      const cost = (bal * p) / 100
      const qty = cost / parseFloat(price)
      setQuantity(qty.toFixed(6))
    } else {
      // Sell percentage of base balance
      const qty = (bal * p) / 100
      setQuantity(qty.toFixed(6))
    }
  }

  if (!base || !quote) return <div>{t('trade.invalidPair')}</div>

  const balance = wallets.find(w => w.asset === (side === 'BUY' ? quote : base))
  const available = balance ? parseFloat(balance.available) : 0
  const total = (parseFloat(price) || 0) * (parseFloat(quantity) || 0)
  const lastPrice = ticker && ticker.last ? parseFloat(ticker.last) : (stats?.last ? parseFloat(stats.last) : 0)
  const changePct = stats?.change_pct || 0
  const positive = changePct >= 0

  return (
    <div className="space-y-3">
      {/* Top: Pair header with 24h stats (cexius style) */}
      <div className="card flex flex-wrap items-center gap-6 py-3">
        <Link to="/markets" className="flex items-center gap-2 hover:opacity-80">
          <div className="w-8 h-8 rounded-full bg-gradient-to-br from-blue-500 to-purple-500 flex items-center justify-center text-white font-bold text-sm">
            {base[0]}
          </div>
          <div>
            <div className="font-bold text-lg">{base}/{quote}</div>
            <div className="text-xs text-gray-500">{t('trade.corePair')}</div>
          </div>
        </Link>

        <div>
          <div className="text-2xl font-bold">
            {lastPrice > 0 ? lastPrice.toFixed(quote === 'USDT' ? 2 : 8) : '—'}
          </div>
          {stats && (
            <div className={`text-sm flex items-center gap-1 ${positive ? 'text-green-400' : 'text-red-400'}`}>
              <span>{positive ? '▲' : '▼'}</span>
              <span>{positive ? '+' : ''}{changePct.toFixed(2)}%</span>
            </div>
          )}
        </div>

        <div className="hidden md:flex gap-6 text-sm">
          <div>
            <div className="text-gray-500 text-xs">{t('trade.24hHigh')}</div>
            <div className="font-medium">{stats ? parseFloat(stats.high).toFixed(2) : '—'}</div>
          </div>
          <div>
            <div className="text-gray-500 text-xs">{t('trade.24hLow')}</div>
            <div className="font-medium">{stats ? parseFloat(stats.low).toFixed(2) : '—'}</div>
          </div>
          <div>
            <div className="text-gray-500 text-xs">{t('trade.24hVolumeQuote', { quote })}</div>
            <div className="font-medium">{stats ? parseFloat(stats.volume_quote).toFixed(2) : '—'}</div>
          </div>
          <div>
            <div className="text-gray-500 text-xs">{t('trade.24hVolumeBase', { base })}</div>
            <div className="font-medium">{stats ? parseFloat(stats.volume_base).toFixed(4) : '—'}</div>
          </div>
        </div>

        <div className="ml-auto text-xs text-gray-400">
          {t('trade.wsLabel')} {wsState}{wsState === 'open' ? ` (${wsLatencyMs}ms)` : ''}
        </div>
      </div>

      {/* Main grid: Chart | Order Book + Trades | Buy/Sell form */}
      <div className="grid grid-cols-12 gap-3">
        {/* Chart - 6 cols */}
        <div className="col-span-12 lg:col-span-7 card">
          <PriceChart base={base} quote={quote} />
        </div>

        {/* Order Book + Recent Trades - 3 cols */}
        <div className="col-span-12 lg:col-span-3 card">
          <div className="flex gap-1 mb-2 border-b border-gray-700">
            <button
              onClick={() => setObTab('orderbook')}
              className={`px-2 py-1 text-xs ${
                obTab === 'orderbook'
                  ? 'border-b-2 border-blue-500 text-white'
                  : 'text-gray-400 hover:text-gray-200'
              }`}>
              {t('trade.orderBookTab')}
            </button>
            <button
              onClick={() => setObTab('depth')}
              className={`px-2 py-1 text-xs ${
                obTab === 'depth'
                  ? 'border-b-2 border-blue-500 text-white'
                  : 'text-gray-400 hover:text-gray-200'
              }`}>
              {t('depthChart.title')}
            </button>
            <button
              onClick={() => setObTab('trades')}
              className={`px-2 py-1 text-xs ${
                obTab === 'trades'
                  ? 'border-b-2 border-blue-500 text-white'
                  : 'text-gray-400 hover:text-gray-200'
              }`}>
              {t('trade.tradesTab')}
            </button>
          </div>

          {obTab === 'orderbook' ? (
            <>
              {/* Filter buttons */}
              <div className="flex gap-1 mb-2">
                {(['both', 'bids', 'asks'] as OrderBookFilter[]).map(f => (
                  <button key={f} onClick={() => setObFilter(f)}
                    className={`px-2 py-0.5 text-xs rounded ${
                      obFilter === f ? 'bg-blue-600 text-white' : 'bg-gray-700 text-gray-300'
                    }`}>
                    {f === 'both' ? t('common.all') : f === 'bids' ? t('trade.bids') : t('trade.asks')}
                  </button>
                ))}
              </div>

              {/* Headers */}
              <div className="grid grid-cols-3 text-xs text-gray-500 px-2 py-1">
                <div>{t('trade.priceLabel', { quote })}</div>
                <div className="text-right">{t('trade.qtyLabel', { base })}</div>
                <div className="text-right">{t('common.total')}</div>
              </div>

              {/* Asks (reversed, lowest at bottom) */}
              {(obFilter !== 'bids') && obWithTotals && (
                <div className="space-y-0.5 max-h-48 overflow-y-auto">
                  {obWithTotals.asks.slice().reverse().map((a, i) => (
                    <div key={i}
                      onClick={() => setPrice(a.price)}
                      className="grid grid-cols-3 text-xs px-2 py-1 cursor-pointer hover:bg-gray-800 relative">
                      <div className="absolute inset-y-0 right-0 bg-red-900/30"
                        style={{ width: `${(a.total / obWithTotals.maxTotal) * 100}%` }} />
                      <div className="relative text-red-400">{parseFloat(a.price).toFixed(2)}</div>
                      <div className="relative text-right">{parseFloat(a.quantity).toFixed(4)}</div>
                      <div className="relative text-right text-gray-400">{a.total.toFixed(4)}</div>
                    </div>
                  ))}
                </div>
              )}

              {/* Spread indicator */}
              {obWithTotals && obWithTotals.bids.length > 0 && obWithTotals.asks.length > 0 && (
                <div className="text-center text-xs text-gray-500 py-2 border-y border-gray-700 my-1">
                  {t('trade.bestAsk')} <span className="text-red-400">{parseFloat(obWithTotals.asks[0].price).toFixed(2)}</span> ↓
                  {t('trade.bestBid')} <span className="text-green-400">{parseFloat(obWithTotals.bids[0].price).toFixed(2)}</span>
                </div>
              )}

              {/* Bids */}
              {(obFilter !== 'asks') && obWithTotals && (
                <div className="space-y-0.5 max-h-48 overflow-y-auto">
                  {obWithTotals.bids.map((b, i) => (
                    <div key={i}
                      onClick={() => setPrice(b.price)}
                      className="grid grid-cols-3 text-xs px-2 py-1 cursor-pointer hover:bg-gray-800 relative">
                      <div className="absolute inset-y-0 right-0 bg-green-900/30"
                        style={{ width: `${(b.total / obWithTotals.maxTotal) * 100}%` }} />
                      <div className="relative text-green-400">{parseFloat(b.price).toFixed(2)}</div>
                      <div className="relative text-right">{parseFloat(b.quantity).toFixed(4)}</div>
                      <div className="relative text-right text-gray-400">{b.total.toFixed(4)}</div>
                    </div>
                  ))}
                </div>
              )}
            </>
          ) : obTab === 'depth' ? (
            <div className="p-2 bg-gray-800/50 rounded">
              {orderbook ? (
                <DepthChart orderbook={orderbook} precision={pricePrecision} />
              ) : (
                <div className="h-32 flex items-center justify-center text-gray-500 text-xs">
                  {t('depthChart.noData')}
                </div>
              )}
            </div>
          ) : (
            <div className="space-y-0.5 max-h-96 overflow-y-auto">
              <div className="grid grid-cols-3 text-xs text-gray-500 px-2 py-1">
                <div>{t('common.price')}</div>
                <div className="text-right">{t('common.quantity')}</div>
                <div className="text-right">{t('time.now')}</div>
              </div>
              {recentTrades.map(t => (
                <div key={t.id} className="grid grid-cols-3 text-xs px-2 py-1">
                  <div className={t.side === 'BUY' ? 'text-green-400' : 'text-red-400'}>
                    {parseFloat(t.price).toFixed(2)}
                  </div>
                  <div className="text-right">{parseFloat(t.quantity).toFixed(4)}</div>
                  <div className="text-right text-gray-500">
                    {new Date(t.executed_at).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })}
                  </div>
                </div>
              ))}
              {recentTrades.length === 0 && (
                <div className="text-center text-gray-500 text-sm py-8">{t('trade.noRecentTrades')}</div>
              )}
            </div>
          )}
        </div>

        {/* Buy/Sell form - 2 cols */}
        <div className="col-span-12 lg:col-span-2 card">
          {!user ? (
            <div className="text-center py-6">
              <div className="text-4xl mb-3">🔒</div>
              <p className="text-gray-300 mb-4 font-medium">{t('trade.loginRequired', 'Login required to trade')}</p>
              <Link to="/login" className="inline-block px-4 py-2 rounded bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold">
                {t('common.login', 'Login')}
              </Link>
            </div>
          ) : (
          <>
          {/* Buy/Sell toggle */}
          <div className="flex mb-3 rounded overflow-hidden border border-gray-700">
            <button
              onClick={() => setSide('BUY')}
              className={`flex-1 py-2 font-semibold ${
                side === 'BUY' ? 'bg-green-600 text-white' : 'bg-gray-800 text-gray-400'
              }`}>
              {t('trade.buy')}
            </button>
            <button
              onClick={() => setSide('SELL')}
              className={`flex-1 py-2 font-semibold ${
                side === 'SELL' ? 'bg-red-600 text-white' : 'bg-gray-800 text-gray-400'
              }`}>
              {t('trade.sell')}
            </button>
          </div>

          {/* Order Type: Limit / Market / Trigger */}
          <div className="flex mb-3 rounded overflow-hidden border border-gray-700">
            <button
              onClick={() => setOrderType('LIMIT')}
              className={`flex-1 py-1.5 text-sm font-semibold ${
                orderType === 'LIMIT' ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400'
              }`}>
              {t('trade.limit')}
            </button>
            <button
              onClick={() => setOrderType('MARKET')}
              className={`flex-1 py-1.5 text-sm font-semibold ${
                orderType === 'MARKET' ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400'
              }`}>
              {t('trade.market')}
            </button>
            <button
              onClick={() => setOrderType('TRIGGER')}
              className={`flex-1 py-1.5 text-sm font-semibold ${
                orderType === 'TRIGGER' ? 'bg-blue-600 text-white' : 'bg-gray-800 text-gray-400'
              }`}>
              {t('trade.trigger', 'Trigger')}
            </button>
          </div>

          {/* Available */}
          <div className="text-xs text-gray-500 mb-1">
            {t('trade.balance')}: {available.toFixed(4)} {side === 'BUY' ? quote : base}
          </div>

          <form onSubmit={onSubmit} className="space-y-2">
            {orderType === 'LIMIT' && (
              <div>
                <label className="text-xs text-gray-500">{t('trade.price')}</label>
                <input
                  type="text"
                  value={price}
                  onChange={e => setPrice(e.target.value)}
                  className="w-full bg-gray-700 px-2 py-1.5 rounded text-white text-sm"
                  placeholder="0.00"
                  required
                />
              </div>
            )}
            {orderType === 'MARKET' && (
              <div className="text-xs bg-blue-900/30 border border-blue-700 rounded p-2 text-blue-200">
                {t('trade.marketOrderInfo', { side: side === 'BUY' ? t('trade.buy') : t('trade.sell') })}
              </div>
            )}
            {orderType === 'TRIGGER' && (
              <>
                <div className="flex mb-2 rounded overflow-hidden border border-gray-700">
                  <button
                    type="button"
                    onClick={() => setTriggerType('STOP_LOSS')}
                    className={`flex-1 py-1 text-xs font-semibold ${
                      TriggerType === 'STOP_LOSS' ? 'bg-red-600 text-white' : 'bg-gray-800 text-gray-400'
                    }`}>
                    {t('trade.stopLoss', 'Stop Loss')}
                  </button>
                  <button
                    type="button"
                    onClick={() => setTriggerType('TAKE_PROFIT')}
                    className={`flex-1 py-1 text-xs font-semibold ${
                      TriggerType === 'TAKE_PROFIT' ? 'bg-green-600 text-white' : 'bg-gray-800 text-gray-400'
                    }`}>
                    {t('trade.takeProfit', 'Take Profit')}
                  </button>
                </div>
                <div>
                  <label className="text-xs text-gray-500">{t('trade.triggerPrice', 'Trigger Price')}</label>
                  <input
                    type="text"
                    value={triggerPrice}
                    onChange={e => setTriggerPrice(e.target.value)}
                    className="w-full bg-gray-700 px-2 py-1.5 rounded text-white text-sm"
                    placeholder="0.00"
                    required
                  />
                </div>
                <div className="text-xs bg-yellow-900/30 border border-yellow-700 rounded p-2 text-yellow-200">
                  {TriggerType === 'STOP_LOSS'
                    ? t('trade.stopLossInfo', 'Order will SELL when price falls to or below trigger price')
                    : t('trade.takeProfitInfo', 'Order will SELL when price rises to or above trigger price')}
                </div>
              </>
            )}

            <div>
              <label className="text-xs text-gray-500">{t('trade.quantity')}</label>
              <input
                type="text"
                value={quantity}
                onChange={e => setQuantity(e.target.value)}
                className="w-full bg-gray-700 px-2 py-1.5 rounded text-white text-sm"
                placeholder="0.00"
                required
              />
            </div>

            {/* Percentage buttons */}
            <div className="grid grid-cols-5 gap-1">
              {[0, 25, 50, 75, 100].map(p => (
                <button key={p} type="button" onClick={() => applyPercent(p)}
                  className={`text-xs py-1 rounded ${
                    percent === p ? 'bg-blue-600 text-white' : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
                  }`}>
                  {p}%
                </button>
              ))}
            </div>

            <div className="text-xs text-gray-400">
              {t('trade.total')}: <span className="text-white font-medium">{total.toFixed(2)}</span> {quote}
            </div>

            {msg && <div className="text-xs text-yellow-300 bg-gray-800 p-2 rounded">{msg}</div>}

            <button
              type="submit"
              disabled={submitting}
              className={`w-full py-2 rounded font-semibold text-white ${
                side === 'BUY' ? 'bg-green-600 hover:bg-green-700' : 'bg-red-600 hover:bg-red-700'
              } disabled:bg-gray-600`}>
              {submitting ? t('common.loading') : `${side === 'BUY' ? t('trade.buy') : t('trade.sell')} ${base}`}
            </button>
            <ConfirmModal
          open={confirmOpen}
          title={t('trade.confirmTitle', 'Confirm Order')}
          message={`${side} ${quantity} ${base}/${quote}
${orderType === 'MARKET' ? t('trade.atMarketPrice', 'At market price') : `${t('trade.atPrice', 'At')} ${price}`}

${t('trade.confirmMessage', 'Are you sure you want to place this order?')}`}
          confirmText={t('trade.placeOrder', 'Place Order')}
          cancelText={t('common.cancel', 'Cancel')}
          variant={orderType === 'MARKET' ? 'danger' : 'warning'}
          onConfirm={placeOrderNow}
          onCancel={() => setConfirmOpen(false)}
        />
      </form>
          </>
          )}
        </div>
      </div>

      {/* Bottom: Open Orders / Order History / Trade History tabs */}
      {user ? <OrdersPanel /> : null}
    </div>
  )
}
