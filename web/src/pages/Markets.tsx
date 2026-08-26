import { useEffect, useState, useRef, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { Link } from 'react-router-dom'
import * as api from '../lib/api'
import { formatPrice } from '../lib/utils'
import { Sparkline } from '../components/Sparkline'
import { getMarketWS, type MarketWSEvent } from '../lib/marketWS'
import { isFavorite, toggleFavorite, refreshFavorites, getLocalFavorites } from '../lib/favorites'
import { useToast } from '../components/Toast'

interface MarketRow {
  market: api.Market
  ticker?: api.Ticker
  history: number[]
}

const HISTORY_LENGTH = 30

export function Markets() {
  const { t } = useTranslation()
  const toast = useToast()
  useDocumentTitle('Markets - goexchange')
  const [rows, setRows] = useState<MarketRow[]>([])
  const [filter, setFilter] = useState('')
  const [sortBy, setSortBy] = useState<'pair' | 'price' | 'change' | 'spread'>('pair')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')
  const [favorites, setFavorites] = useState<string[]>(getLocalFavorites())

  useEffect(() => {
    refreshFavorites().then(setFavorites)
  }, [])
  const [showOnlyFavorites, setShowOnlyFavorites] = useState(false)
  const historyRef = useRef<Record<string, number[]>>({})

  useEffect(() => {
    async function load() {
      try {
        const markets = await api.listMarkets()
        const tickers = await Promise.all(
          markets.map((m) =>
            api.getTicker(m.base, m.quote).catch(() => undefined)
          )
        )
        // Update history cache
        markets.forEach((m, i) => {
          const t = tickers[i]
          if (t && parseFloat(t.last) > 0) {
            const p = m.pair
            if (!historyRef.current[p]) historyRef.current[p] = []
            historyRef.current[p].push(parseFloat(t.last))
            if (historyRef.current[p].length > HISTORY_LENGTH) {
              historyRef.current[p].shift()
            }
          }
        })
        setRows(
          markets.map((m, i) => ({
            market: m,
            ticker: tickers[i],
            history: historyRef.current[m.pair] || [],
          }))
        )
      } catch (e) {
        console.error('load markets failed', e)
      }
    }
    load()
    const t = setInterval(load, 5000)

    // WebSocket: real-time updates
    const ws = getMarketWS()
    const handleEvent = (e: MarketWSEvent) => {
      // Real-time ticker: update individual row without full reload
      if (e.type === 'ticker.update' && e.base && e.quote) {
        const pair = e.base + '_' + e.quote
        const last = parseFloat(e.last || '0')
        // Update history cache
        if (last > 0) {
          if (!historyRef.current[pair]) historyRef.current[pair] = []
          historyRef.current[pair].push(last)
          if (historyRef.current[pair].length > HISTORY_LENGTH) {
            historyRef.current[pair].shift()
          }
        }
        // Update row ticker in place
        setRows(prevRows => prevRows.map(row => {
          if (row.market.pair === pair) {
            return {
              ...row,
              ticker: {
                pair,
                bid: e.bid || '',
                ask: e.ask || '',
                last: e.last || '',
              },
              history: historyRef.current[pair] || [],
            }
          }
          return row
        }))
      } else if (e.type === 'pair.toggled' || e.type === 'pair.added' || e.type === 'pair.removed' || e.type === 'pairs.reloaded') {
        // Full reload on pair changes
        load()
      }
    }
    ws.onEvent = handleEvent
    ws.connect()

    return () => {
      clearInterval(t)
      ws.onEvent = null
      ws.disconnect()
    }
  }, [])

  function calcChange(history: number[]): number | null {
    if (history.length < 2) return null
    const first = history[0]
    const last = history[history.length - 1]
    if (first === 0) return null
    return ((last - first) / first) * 100
  }

  // Filter and sort
  const filteredRows = useMemo(() => {
    let r = rows
    if (showOnlyFavorites) {
      r = r.filter(row => favorites.includes(row.market.pair))
    }
    if (filter) {
      r = r.filter(row =>
        row.market.pair.toLowerCase().includes(filter.toLowerCase())
      )
    }
    r = [...r].sort((a, b) => {
      let cmp = 0
      switch (sortBy) {
        case 'pair':
          cmp = a.market.pair.localeCompare(b.market.pair)
          break
        case 'price': {
          const ap = parseFloat(a.ticker?.last || '0')
          const bp = parseFloat(b.ticker?.last || '0')
          cmp = ap - bp
          break
        }
        case 'change': {
          const ac = calcChange(a.history) ?? 0
          const bc = calcChange(b.history) ?? 0
          cmp = ac - bc
          break
        }
        case 'spread': {
          const aBid = parseFloat(a.ticker?.bid || '0')
          const aAsk = parseFloat(a.ticker?.ask || '0')
          const bBid = parseFloat(b.ticker?.bid || '0')
          const bAsk = parseFloat(b.ticker?.ask || '0')
          const aSpread = aAsk > 0 && aBid > 0 ? aAsk - aBid : Number.MAX_VALUE
          const bSpread = bAsk > 0 && bBid > 0 ? bAsk - bBid : Number.MAX_VALUE
          cmp = aSpread - bSpread
          break
        }
      }
      return sortDir === 'asc' ? cmp : -cmp
    })
    return r
  }, [rows, filter, sortBy, sortDir, showOnlyFavorites, favorites])

  function toggleSort(field: typeof sortBy) {
    if (sortBy === field) {
      setSortDir(sortDir === 'asc' ? 'desc' : 'asc')
    } else {
      setSortBy(field)
      setSortDir('asc')
    }
  }

  function SortHeader({ field, label }: { field: typeof sortBy; label: string }) {
    return (
      <th
        className="py-2 px-3 cursor-pointer hover:text-brand"
        onClick={() => toggleSort(field)}
      >
        <span className="inline-flex items-center gap-1">
          {label}
          {sortBy === field && (
            <span className="text-xs">{sortDir === 'asc' ? '↑' : '↓'}</span>
          )}
        </span>
      </th>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold">{t('markets.title')}</h1>
        <div className="text-sm text-gray-400">
          {t('markets.pairsCount', { count: rows.length })}
        </div>
      </div>

      {/* Search/Filter */}
      <div className="card">
        <div className="flex gap-2 items-center">
          <input
            type="text"
            value={filter}
            onChange={e => setFilter(e.target.value)}
            placeholder={t('markets.searchPlaceholder')}
            className="flex-1 bg-gray-700 px-3 py-2 rounded text-white"
          />
          <button
            onClick={() => setShowOnlyFavorites(v => !v)}
            className={`px-3 py-2 rounded text-sm whitespace-nowrap border transition-colors ${
              showOnlyFavorites
                ? 'bg-yellow-600 border-yellow-500 text-white'
                : 'bg-gray-700 border-gray-600 text-gray-300 hover:bg-gray-600'
            }`}
            aria-label={t('markets.favoritesOnly', 'Favorites only')}
            title={t('markets.favoritesOnly', 'Favorites only')}
          >
            ★ {favorites.length > 0 && <span className="ml-1">({favorites.length})</span>}
          </button>
        </div>
      </div>

      {/* Markets Table */}
      <div className="card overflow-x-auto">
        <table className="w-full text-left">
          <thead className="text-muted text-sm border-b border-gray-700">
            <tr>
              <SortHeader field="pair" label="Pair" />
              <SortHeader field="price" label="Last Price" />
              <th className="py-2 px-3 text-right">{t('markets.bestBid')}</th>
              <th className="py-2 px-3 text-right">{t('markets.bestAsk')}</th>
              <SortHeader field="spread" label="Spread" />
              <SortHeader field="change" label="24h Change" />
              <th className="py-2 px-3 text-center">{t('markets.trend')}</th>
              <th className="py-2 px-3 text-center">{t('markets.favCol', '')}</th>
            </tr>
          </thead>
          <tbody>
            {filteredRows.map(({ market, ticker, history }) => {
              const bid = parseFloat(ticker?.bid || '0')
              const ask = parseFloat(ticker?.ask || '0')
              const last = parseFloat(ticker?.last || '0')
              const spread = bid > 0 && ask > 0 ? ask - bid : 0
              const spreadPct = bid > 0 ? (spread / bid) * 100 : 0
              const change = calcChange(history)
              return (
                <tr key={market.pair} className="table-row hover:bg-border border-t border-gray-700">
                  <td className="py-3 px-3 font-semibold">
                    <div className="flex items-center gap-2">
                      <button
                        onClick={async () => {
                          const added = await toggleFavorite(market.pair)
                          setFavorites(getLocalFavorites())  // sync local cache read
                          toast.showToast(
                            added
                              ? t('markets.favAdded', `Added ${market.pair} to favorites`)
                              : t('markets.favRemoved', `Removed ${market.pair} from favorites`),
                            added ? 'success' : 'info'
                          )
                        }}
                        className="text-lg hover:scale-110 transition-transform"
                        aria-label={isFavorite(market.pair) ? t('markets.unfavorite', 'Unfavorite') : t('markets.favorite', 'Favorite')}
                        title={isFavorite(market.pair) ? t('markets.unfavorite', 'Unfavorite') : t('markets.favorite', 'Favorite')}
                      >
                        {isFavorite(market.pair) ? '★' : '☆'}
                      </button>
                      <span>{market.pair}</span>
                    </div>
                  </td>
                  <td className="py-3 px-3 font-mono">
                    {last > 0 ? formatPrice(ticker!.last) : '—'}
                  </td>
                  <td className="py-3 px-3 text-right text-success font-mono">
                    {bid > 0 ? formatPrice(ticker!.bid) : '—'}
                  </td>
                  <td className="py-3 px-3 text-right text-danger font-mono">
                    {ask > 0 ? formatPrice(ticker!.ask) : '—'}
                  </td>
                  <td className="py-3 px-3 text-right text-muted font-mono">
                    {spread > 0
                      ? `${formatPrice(spread)} (${spreadPct.toFixed(3)}%)`
                      : '—'}
                  </td>
                  <td className="py-3 px-3 text-right font-mono">
                    {change !== null ? (
                      <span
                        className={
                          change >= 0
                            ? 'text-green-400'
                            : 'text-red-400'
                        }
                      >
                        {change >= 0 ? '+' : ''}{change.toFixed(2)}%
                      </span>
                    ) : (
                      <span className="text-muted">—</span>
                    )}
                  </td>
                  <td className="py-3 px-3 text-center">
                    {history.length >= 2 ? (
                      <Sparkline data={history} width={80} height={30} />
                    ) : (
                      <span className="text-muted">—</span>
                    )}
                  </td>
                  <td className="py-3 px-3 text-right">
                    <Link
                      to={`/trade/${market.base}/${market.quote}`}
                      className="btn-primary text-sm"
                    >
                      Trade
                    </Link>
                  </td>
                </tr>
              )
            })}
            {filteredRows.length === 0 && (
              <tr>
                <td colSpan={8} className="p-8 text-center text-gray-500">
                  {filter ? `No pairs matching "${filter}"` : 'Loading markets...'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="text-xs text-gray-500 text-center">
        {t('markets.sortHint')}
      </div>
    </div>
  )
}
