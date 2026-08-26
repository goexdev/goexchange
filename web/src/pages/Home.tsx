import { Link } from 'react-router-dom'
import { useEffect, useState, useRef } from 'react'
import * as api from '../lib/api'
import { formatPrice } from '../lib/utils'
import { Sparkline } from '../components/Sparkline'
import { useAuth } from '../lib/auth'
import { useTranslation } from 'react-i18next'
import { useDocumentTitle } from '../lib/useDocumentTitle'

interface MarketRow {
  market: api.Market
  ticker?: api.Ticker
  history: number[]
}

// Historical price points (client-side cache)
const HISTORY_LENGTH = 24

function useMarketHistory(markets: api.Market[]) {
  const historyRef = useRef<Record<string, number[]>>({})

  function update(p: string, price: number) {
    if (!historyRef.current[p]) historyRef.current[p] = []
    const h = historyRef.current[p]
    h.push(price)
    if (h.length > HISTORY_LENGTH) h.shift()
  }

  function getHistory(p: string): number[] {
    return historyRef.current[p] || []
  }

  return { update, getHistory }
}

export function Home() {
  useDocumentTitle('goexchange - Trade Crypto Across 7 Chains')
  const { t } = useTranslation()
  const { user } = useAuth()
  const [rows, setRows] = useState<MarketRow[]>([])
  const { update: updateHistory, getHistory } = useMarketHistory([])

  useEffect(() => {
    async function load() {
      try {
        const markets = await api.listMarkets()
        const tickers = await Promise.all(
          markets.map((m) =>
            api.getTicker(m.base, m.quote).catch(() => undefined)
          )
        )
        // Update history
        markets.forEach((m, i) => {
          const t = tickers[i]
          if (t && parseFloat(t.last) > 0) {
            updateHistory(m.pair, parseFloat(t.last))
          }
        })
        setRows(
          markets.map((m, i) => ({
            market: m,
            ticker: tickers[i],
            history: getHistory(m.pair),
          }))
        )
      } catch (e) {
        console.error('load markets failed', e)
      }
    }
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [])

  // Calculate 24h change% from history
  function calcChange(history: number[]): number | null {
    if (history.length < 2) return null
    const first = history[0]
    const last = history[history.length - 1]
    if (first === 0) return null
    return ((last - first) / first) * 100
  }

  // Get top 3 markets by volume/activity for hero section
  const featured = rows.slice(0, 3)
  const totalMarkets = rows.length
  const activePairs = rows.filter(r => r.ticker && parseFloat(r.ticker.last) > 0).length

  return (
    <div className="space-y-8">
      {/* Hero */}
      <div className="relative overflow-hidden rounded-lg bg-gradient-to-br from-blue-900 via-purple-900 to-gray-900 p-8 md:p-12">
        <div className="absolute inset-0 bg-grid-pattern opacity-10" />
        <div className="relative">
          <h1 className="text-3xl md:text-5xl font-bold mb-3 bg-clip-text text-transparent bg-gradient-to-r from-white to-blue-200">
            {t('home.title')}
          </h1>
          <p className="text-lg text-blue-100 mb-6 max-w-2xl">
            {t('home.subtitle')}
          </p>
          <div className="flex flex-wrap gap-3">
            {!user ? (
              <>
                <Link to="/register" className="bg-white text-blue-900 hover:bg-blue-50 px-6 py-3 rounded font-medium">
                  {t('home.createAccount')}
                </Link>
                <Link to="/markets" className="border border-white text-white hover:bg-white hover:text-blue-900 px-6 py-3 rounded font-medium">
                  {t('home.viewMarkets')}
                </Link>
              </>
            ) : (
              <>
                <Link to="/wallet" className="bg-white text-blue-900 hover:bg-blue-50 px-6 py-3 rounded font-medium">
                  {t('home.myWallet')}
                </Link>
                <Link to="/markets" className="border border-white text-white hover:bg-white hover:text-blue-900 px-6 py-3 rounded font-medium">
                  {t('home.viewMarkets')}
                </Link>
              </>
            )}
          </div>
        </div>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <div className="bg-gray-800 p-4 rounded">
          <div className="text-sm text-gray-400">{t('home.stats.activePairs')}</div>
          <div className="text-2xl font-bold">{activePairs}/{totalMarkets}</div>
        </div>
        <div className="bg-gray-800 p-4 rounded">
          <div className="text-sm text-gray-400">{t('home.stats.chains')}</div>
          <div className="text-2xl font-bold">7</div>
          <div className="text-xs text-gray-500 mt-1">{t('home.stats.l1L2')}</div>
        </div>
        <div className="bg-gray-800 p-4 rounded">
          <div className="text-sm text-gray-400">{t('home.stats.tokens')}</div>
          <div className="text-2xl font-bold">{t('home.stats.tokensCount')}</div>
          <div className="text-xs text-gray-500 mt-1">{t('home.stats.supported')}</div>
        </div>
        <div className="bg-gray-800 p-4 rounded">
          <div className="text-sm text-gray-400">{t('home.stats.status')}</div>
          <div className="text-2xl font-bold text-green-400">{t('home.stats.online')}</div>
          <div className="text-xs text-gray-500 mt-1">{t('home.stats.normal')}</div>
        </div>
      </div>

      {/* {t('home.featured')} (top 3 with sparkline) */}
      {featured.length > 0 && (
        <div>
          <div className="flex justify-between items-center mb-3">
            <h2 className="text-xl font-bold">{t('home.featured')}</h2>
            <Link to="/markets" className="text-sm text-blue-400 hover:underline">
              {t('home.viewAll')}
            </Link>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {featured.map(({ market, ticker, history }) => {
              const change = calcChange(history)
              return (
                <Link
                  key={market.pair}
                  to={`/trade/${market.base}/${market.quote}`}
                  className="bg-gray-800 hover:bg-gray-750 p-5 rounded transition-colors"
                >
                  <div className="flex justify-between items-start mb-3">
                    <div>
                      <div className="font-bold text-lg">{market.pair}</div>
                      <div className="text-xs text-gray-400 mt-1">
                        {market.base} / {market.quote}
                      </div>
                    </div>
                    {change !== null && (
                      <div
                        className={`text-sm px-2 py-1 rounded ${
                          change >= 0
                            ? 'bg-green-900 text-green-300'
                            : 'bg-red-900 text-red-300'
                        }`}
                      >
                        {change >= 0 ? '+' : ''}{change.toFixed(2)}%
                      </div>
                    )}
                  </div>
                  <div className="flex justify-between items-end">
                    <div>
                      <div className="text-xs text-gray-400">{t('home.lastPrice')}</div>
                      <div className="text-xl font-mono font-bold">
                        {ticker && parseFloat(ticker.last) > 0
                          ? formatPrice(ticker.last)
                          : '—'}
                      </div>
                    </div>
                    {history.length >= 2 && (
                      <Sparkline
                        data={history}
                        width={80}
                        height={40}
                      />
                    )}
                  </div>
                </Link>
              )
            })}
          </div>
        </div>
      )}

      {/* {t('home.why')} */}
      <div>
        <h2 className="text-xl font-bold mb-4">{t('home.why')}</h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="bg-gray-800 p-5 rounded">
            <div className="text-2xl mb-2">🔐</div>
            <div className="font-semibold mb-2">{t('home.features.selfCustodial.title')}</div>
            <div className="text-sm text-gray-400">
              {t('home.features.selfCustodial.desc')}
            </div>
          </div>
          <div className="bg-gray-800 p-5 rounded">
            <div className="text-2xl mb-2">⚡</div>
            <div className="font-semibold mb-2">{t('home.features.realOnchain.title')}</div>
            <div className="text-sm text-gray-400">
              {t('home.features.realOnchain.desc')}
            </div>
          </div>
          <div className="bg-gray-800 p-5 rounded">
            <div className="text-2xl mb-2">🌐</div>
            <div className="font-semibold mb-2">{t('home.features.multiChain.title')}</div>
            <div className="text-sm text-gray-400">
              {t('home.features.multiChain.desc')}
            </div>
          </div>
        </div>
      </div>

      {/* CTA */}
      {!user && (
        <div className="bg-gradient-to-r from-green-900 to-blue-900 rounded-lg p-8 text-center">
          <h2 className="text-2xl font-bold mb-2">{t('home.ready')}</h2>
          <p className="text-blue-100 mb-4">
            {t('home.ctaDesc')}
          </p>
          <Link to="/register" className="inline-block bg-white text-blue-900 hover:bg-blue-50 px-8 py-3 rounded font-medium">
            {t('home.freeAccount')}
          </Link>
        </div>
      )}

      <p className="text-muted text-xs text-center">
        {t('home.testnet')}
      </p>
    </div>
  )
}
