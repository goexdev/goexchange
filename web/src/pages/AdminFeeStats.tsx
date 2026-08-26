import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { AdminLayout } from '../components/AdminLayout'

export function AdminFeeStats() {
  const { t } = useTranslation()
  const [stats, setStats] = useState<api.FeeStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  async function load() {
    try {
      const data = await api.adminGetFeeStats()
      setStats(data)
    } catch (e: any) {
      setErr(e.message || 'Failed to load')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  if (loading && !stats) {
    return (
      <AdminLayout>
        <div className="p-8 text-center text-gray-400">
          {t('common.loadingEllipsis', 'Loading...')}
        </div>
      </AdminLayout>
    )
  }

  if (!stats) {
    return (
      <AdminLayout>
        <div className="bg-red-900 text-red-200 p-4 rounded">{err}</div>
      </AdminLayout>
    )
  }

  const grandTotalFee = stats.grand_total_fee
  const grandTotalVolume = stats.grand_total_volume
  const totalWithdrawals = stats.by_asset.reduce((sum, a) => sum + a.withdrawal_count, 0)

  return (
    <AdminLayout>
      <div>
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-2xl font-bold">
              {t('admin.feeStatsTitle', 'Fee Revenue Statistics')}
            </h1>
            <p className="text-sm text-gray-400 mt-1">
              {t('admin.feeStatsDesc', 'Withdraw fees collected by the platform')}
            </p>
          </div>
          <button
            onClick={load}
            className="px-3 py-1 rounded text-sm bg-panel text-muted border border-border hover:text-white"
          >
            {t('admin.refresh', 'Refresh')}
          </button>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
          <div className="card">
            <div className="text-sm text-gray-400 mb-1">
              {t('admin.totalFees', 'Total Fees Collected')}
            </div>
            <div className="text-3xl font-bold text-green-400 font-mono">
              {grandTotalFee}
            </div>
            <div className="text-xs text-gray-500 mt-1">
              {t('admin.acrossAllAssets', 'across all assets')}
            </div>
          </div>

          <div className="card">
            <div className="text-sm text-gray-400 mb-1">
              {t('admin.totalVolume', 'Total Withdrawal Volume')}
            </div>
            <div className="text-3xl font-bold font-mono">
              {grandTotalVolume}
            </div>
            <div className="text-xs text-gray-500 mt-1">
              {t('admin.successfulWithdrawals', 'from successful withdrawals')}
            </div>
          </div>

          <div className="card">
            <div className="text-sm text-gray-400 mb-1">
              {t('admin.feeCount', 'Fees Collected On')}
            </div>
            <div className="text-3xl font-bold text-blue-400">
              {totalWithdrawals}
            </div>
            <div className="text-xs text-gray-500 mt-1">
              {t('admin.withdrawals', 'withdrawals')}
            </div>
          </div>
        </div>

        <div className="card mb-6">
          <h2 className="text-lg font-semibold mb-3">
            {t('admin.byAsset', 'Fees by Asset')}
          </h2>
          {stats.by_asset.length === 0 ? (
            <p className="text-gray-400 text-center py-4">
              {t('admin.noFeeData', 'No completed withdrawals yet')}
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left">
                  <th className="p-2">{t('admin.asset', 'Asset')}</th>
                  <th className="p-2 text-right">{t('admin.feeCount', 'Withdrawals')}</th>
                  <th className="p-2 text-right">{t('admin.feeAmount', 'Total Fee')}</th>
                  <th className="p-2 text-right">{t('admin.volume', 'Total Volume')}</th>
                  <th className="p-2 text-right">{t('admin.received', 'Total Received')}</th>
                  <th className="p-2 text-right">{t('admin.feePct', 'Fee %')}</th>
                </tr>
              </thead>
              <tbody>
                {stats.by_asset.map((a) => {
                  const feePct = parseFloat(a.total_volume) > 0
                    ? (parseFloat(a.total_fee) / parseFloat(a.total_volume) * 100).toFixed(3)
                    : '0.000'
                  return (
                    <tr key={a.asset} className="border-b border-border/50 hover:bg-bg/30">
                      <td className="p-2 font-semibold">{a.asset}</td>
                      <td className="p-2 text-right font-mono">{a.withdrawal_count}</td>
                      <td className="p-2 text-right font-mono text-green-400">
                        {a.total_fee}
                      </td>
                      <td className="p-2 text-right font-mono">{a.total_volume}</td>
                      <td className="p-2 text-right font-mono">{a.total_received}</td>
                      <td className="p-2 text-right font-mono text-yellow-400">
                        {feePct}%
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>

        {stats.daily.length > 0 && (
          <div className="card">
            <h2 className="text-lg font-semibold mb-3">
              {t('admin.dailyBreakdown', 'Last 30 Days')}
            </h2>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left">
                  <th className="p-2">{t('admin.date', 'Date')}</th>
                  <th className="p-2 text-right">{t('admin.count', 'Count')}</th>
                  <th className="p-2 text-right">{t('admin.dailyFee', 'Daily Fee')}</th>
                  <th className="p-2 text-right">{t('admin.dailyVolume', 'Daily Volume')}</th>
                </tr>
              </thead>
              <tbody>
                {stats.daily.map((d) => (
                  <tr key={d.day} className="border-b border-border/50 hover:bg-bg/30">
                    <td className="p-2 font-mono">{d.day}</td>
                    <td className="p-2 text-right font-mono">{d.count}</td>
                    <td className="p-2 text-right font-mono text-green-400">
                      {d.daily_fee}
                    </td>
                    <td className="p-2 text-right font-mono">{d.daily_volume}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </AdminLayout>
  )
}