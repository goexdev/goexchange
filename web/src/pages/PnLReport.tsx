import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

interface PairPnL {
  pair: string
  realized_pnl: string
  unrealized_pnl: string
  total_pnl: string
  total_bought: string
  total_sold: string
  current_holdings: string
  avg_buy_price: string
  avg_sell_price: string
  total_volume: string
  total_trades: number
}

interface UserPnL {
  user_id: string
  generated_at: string
  pairs: PairPnL[]
  total_pnl: string
  total_realized: string
  total_unrealized: string
  total_trades: number
  total_volume: string
}

function fmt(n: string, decimals: number = 4): string {
  const num = parseFloat(n)
  if (isNaN(num)) return n
  return num.toFixed(decimals)
}

function fmtPnLColor(n: string): string {
  const num = parseFloat(n)
  if (num > 0) return 'text-green-400'
  if (num < 0) return 'text-red-400'
  return 'text-muted'
}

export function PnLReport() {
  const { t } = useTranslation()
  const [data, setData] = useState<UserPnL | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    const token = localStorage.getItem('goexchange_token')
    if (!token) {
      setError('Not authenticated')
      setLoading(false)
      return
    }
    fetch('/api/v1/users/me/pnl', {
      headers: { Authorization: 'Bearer ' + token },
    })
      .then(r => r.json())
      .then(d => {
        if (d.error) {
          setError(d.error)
        } else {
          setData(d)
        }
        setLoading(false)
      })
      .catch(e => {
        setError(e.message)
        setLoading(false)
      })
  }, [])

  if (loading) {
    return (
      <div className="card text-center text-muted py-8">{t("pnl.loading", "Loading P&L...")}</div>
    )
  }

  if (error) {
    return (
      <div className="card text-center text-red-400 py-8">{error}</div>
    )
  }

  if (!data || data.pairs.length === 0) {
    return (
      <div className="card text-center text-muted py-8">
        {t('pnl.noTrades', 'No trades yet. P&L will appear after your first trade.')}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex justify-between items-center">
        <h2 className="text-xl font-bold">{t('pnl.title', 'Profit & Loss')}</h2>
        <Link
          to="/user"
          className="text-sm text-blue-400 hover:text-blue-300"
        >
          {t('common.back', 'Back')}
        </Link>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
        <div className="card">
          <div className="text-xs text-muted">{t("pnl.totalTitle", "Total P&L")}</div>
          <div className={`text-2xl font-bold ${fmtPnLColor(data.total_pnl)}`}>
            ${fmt(data.total_pnl, 2)}
          </div>
        </div>
        <div className="card">
          <div className="text-xs text-muted">Realized</div>
          <div className={`text-xl font-semibold ${fmtPnLColor(data.total_realized)}`}>
            ${fmt(data.total_realized, 2)}
          </div>
        </div>
        <div className="card">
          <div className="text-xs text-muted">Trades</div>
          <div className="text-xl font-semibold">{data.total_trades}</div>
        </div>
        <div className="card">
          <div className="text-xs text-muted">Volume</div>
          <div className="text-xl font-semibold">${fmt(data.total_volume, 2)}</div>
        </div>
      </div>

      <div className="card overflow-x-auto">
        <h3 className="text-lg font-semibold mb-3">{t("pnl.perPair", "Per Pair")}</h3>
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left">
              <th className="p-2">Pair</th>
              <th className="p-2 text-right">{t("pnl.shortLabel", "P&L")}</th>
              <th className="p-2 text-right">Realized</th>
              <th className="p-2 text-right">{t("pnl.avgBuy", "Avg Buy")}</th>
              <th className="p-2 text-right">{t("pnl.avgSell", "Avg Sell")}</th>
              <th className="p-2 text-right">Bought</th>
              <th className="p-2 text-right">Sold</th>
              <th className="p-2 text-right">Holdings</th>
              <th className="p-2 text-right">Trades</th>
            </tr>
          </thead>
          <tbody>
            {data.pairs.map(p => (
              <tr key={p.pair} className="border-b border-border/50">
                <td className="p-2 font-semibold">{p.pair}</td>
                <td className={`p-2 text-right font-semibold ${fmtPnLColor(p.total_pnl)}`}>
                  ${fmt(p.total_pnl, 2)}
                </td>
                <td className={`p-2 text-right ${fmtPnLColor(p.realized_pnl)}`}>
                  ${fmt(p.realized_pnl, 2)}
                </td>
                <td className="p-2 text-right">${fmt(p.avg_buy_price, 2)}</td>
                <td className="p-2 text-right">${fmt(p.avg_sell_price, 2)}</td>
                <td className="p-2 text-right">{fmt(p.total_bought, 4)}</td>
                <td className="p-2 text-right">{fmt(p.total_sold, 4)}</td>
                <td className="p-2 text-right">{fmt(p.current_holdings, 4)}</td>
                <td className="p-2 text-right">{p.total_trades}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="text-xs text-muted text-center">
        {t("pnl.generatedAt", "Generated at")} {new Date(data.generated_at).toLocaleString()} · {t("pnl.fifoNote", "FIFO accounting")}
      </div>
    </div>
  )
}