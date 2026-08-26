import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { AdminLayout } from '../components/AdminLayout'
import { DashboardCharts } from '../components/DashboardCharts'

export function AdminDashboardPage() {
  const { t } = useTranslation()
  const [data, setData] = useState<api.DashboardData | null>(null)
  const [chartsData, setChartsData] = useState<api.DashboardCharts | null>(null)
  const [err, setErr] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null)

  async function load() {
    setErr('')
    try {
      const [d, c] = await Promise.all([
        api.adminDashboard(),
        api.adminDashboardCharts(),
      ])
      setData(d)
      setChartsData(c)
      setLastRefresh(new Date())
    } catch (e: any) {
      setErr(e.message)
    }
  }

  useEffect(() => {
    load()
    if (autoRefresh) {
      const interval = setInterval(load, 30000)
      return () => clearInterval(interval)
    }
  }, [autoRefresh])

  if (err && !data) {
    return <div className="bg-red-900 text-red-200 p-4 rounded">{t('admin.errorLabel')}: {err}</div>
  }

  if (!data) {
    return <div className="p-8 text-center text-gray-400">{t('common.loadingEllipsis')}</div>
  }

  const familyEmoji = (family?: string) => {
    switch (family) {
      case 'bitcoin': return 'B'
      case 'evm': return 'E'
      default: return '-'
    }
  }

  const chainStatusColor = (status: string) => {
    switch (status) {
      case 'healthy': return 'text-green-400'
      case 'error': return 'text-red-400'
      default: return 'text-gray-400'
    }
  }

  const alertColor = (level: string) => {
    switch (level) {
      case 'error': return 'bg-red-900 border-red-500 text-red-200'
      case 'warning': return 'bg-yellow-900 border-yellow-500 text-yellow-200'
      case 'info': return 'bg-blue-900 border-blue-500 text-blue-200'
      default: return 'bg-gray-700 border-gray-500'
    }
  }

  return (
    <AdminLayout>
      <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold">{t('admin.systemHealth')}</h2>
          <p className="text-sm text-gray-400">
            {t('admin.lastRefresh')}: {lastRefresh ? lastRefresh.toLocaleTimeString() : t('common.never')}
          </p>
        </div>
        <div className="flex gap-2 items-center">
          <label className="text-sm text-gray-400">
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={e => setAutoRefresh(e.target.checked)}
              className="mr-2"
            />
            {t('admin.autoRefresh')} {t('admin.interval30s')}
          </label>
          <button onClick={load} className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded">
            {t('common.refresh')}
          </button>
        </div>
      </div>

      {err && <div className="bg-red-900 text-red-200 p-3 rounded">{err}</div>}

      {/* Charts Section */}
      {chartsData && <DashboardCharts data={chartsData} />}

      {data.alerts.items.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-lg font-semibold text-gray-300">{t('admin.alertsTitle', { count: data.alerts.count })}</h3>
          {data.alerts.items.map((a, i) => (
            <div key={i} className={`border-l-4 p-3 rounded ${alertColor(a.level)}`}>
              <div className="flex justify-between items-start">
                <div>
                  <div className="font-bold">{a.title}</div>
                  <div className="text-sm">{a.action}</div>
                </div>
                <span className="bg-black bg-opacity-30 px-3 py-1 rounded text-xl font-bold">
                  {a.count}
                </span>
              </div>
            </div>
          ))}
        </div>
      )}

      <div>
        <h3 className="text-lg font-semibold text-gray-300 mb-2">{t('admin.systemHealthTitle')}</h3>
        <div className="grid grid-cols-4 gap-4">
          <div className={`p-4 rounded ${data.system.db && data.system.db.connected ? 'bg-green-900' : 'bg-red-900'}`}>
            <div className="text-sm opacity-80">{t('admin.database')}</div>
            <div className="text-2xl font-bold mb-1">
              {data.system.db && data.system.db.connected ? 'Connected' : 'Disconnected'}
            </div>
            {data.system.db && data.system.db.connected && (
              <div className="text-xs opacity-70">
                <div>{data.system.db.idle_conns}/{data.system.db.max_conns} idle</div>
                <div>{data.system.db.total_conns} total</div>
                {data.system.db.version && (
                  <div className="truncate">{data.system.db.version.split(' ').slice(0, 2).join(' ')}</div>
                )}
              </div>
            )}
          </div>

          <div className={`p-4 rounded ${data.system.redis && data.system.redis.connected ? 'bg-green-900' : 'bg-gray-700'}`}>
            <div className="text-sm opacity-80">{t('admin.redis')}</div>
            <div className="text-2xl font-bold mb-1">
              {data.system.redis && data.system.redis.connected ? 'Connected' : 'N/A'}
            </div>
            {data.system.redis && data.system.redis.connected && (
              <div className="text-xs opacity-70">
                <div>{data.system.redis.key_count ? data.system.redis.key_count : 0} keys</div>
                <div>{data.system.redis.ping}</div>
              </div>
            )}
          </div>

          <div className={`p-4 rounded ${data.system.vault && data.system.vault.connected ? 'bg-green-900' : 'bg-red-900'}`}>
            <div className="text-sm opacity-80">{t('admin.vault')}</div>
            <div className="text-2xl font-bold mb-1">
              {data.system.vault && data.system.vault.connected ? 'Connected' : 'Disconnected'}
            </div>
            {data.system.vault && data.system.vault.connected && (
              <div className="text-xs opacity-70">
                <div>{t('admin.sealed')}: {data.system.vault.sealed ? t('common.yes') : t('common.no')}</div>
              </div>
            )}
          </div>

          <div className="p-4 rounded bg-blue-900">
            <div className="text-sm opacity-80">{t('admin.goRuntime')}</div>
            <div className="text-2xl font-bold mb-1">{data.system.goroutines}</div>
            <div className="text-xs opacity-70">
              <div>{t('admin.goroutines')}</div>
              <div>{data.system.go_version}</div>
            </div>
          </div>
        </div>
      </div>

      <div>
        <h3 className="text-lg font-semibold text-gray-300 mb-2">{t('admin.volume24h')}</h3>
        <div className="grid grid-cols-2 gap-4">
          <div className="bg-gray-800 p-4 rounded">
            <div className="text-sm text-gray-400">{t('admin.deposits24h')}</div>
            <div className="text-3xl font-bold">{data.volume.deposits_24h.count}</div>
            <div className="text-sm text-gray-500">{t('admin.txCount')}</div>
          </div>
          <div className="bg-gray-800 p-4 rounded">
            <div className="text-sm text-gray-400">{t('admin.withdrawals24h')}</div>
            <div className="text-3xl font-bold">{data.volume.withdrawals_24h.count}</div>
            <div className="text-sm text-gray-500">
              {t('admin.byStatus')}: {data.volume.withdrawals_by_status_24h ?
                Object.entries(data.volume.withdrawals_by_status_24h).map(([k, v]) => `${k}:${v}`).join(', ')
                : 'N/A'}
            </div>
          </div>
        </div>
      </div>

      <div>
        <h3 className="text-lg font-semibold text-gray-300 mb-2">
          {t('admin.chainsStatus', { active: data.chains.active, total: data.chains.total })}
        </h3>
        <div className="grid grid-cols-2 md:grid-cols-3 gap-3">
          {data.chains.chains.map(c => (
            <div key={c.id}
              className={`p-3 rounded border-2 ${
                c.status === 'healthy' ? 'border-green-500 bg-green-900 bg-opacity-30' :
                c.status === 'error' ? 'border-red-500 bg-red-900 bg-opacity-30' :
                'border-gray-600 bg-gray-800'
              }`}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-mono opacity-60">{familyEmoji(c.family)}</span>
                  <span className="font-bold">{c.id}</span>
                </div>
                <span className={`text-sm ${chainStatusColor(c.status)}`}>
                  {c.status === 'healthy' ? 'OK' : c.status === 'error' ? 'ERR' : 'OFF'}
                </span>
              </div>
              <div className="text-xs opacity-70 mt-1">
                {c.active && c.block_count && (
                  <div>{t('admin.block')}: {c.block_count.toLocaleString()}</div>
                )}
                <div>{t('admin.minConf')}: {c.min_conf}</div>
                {c.has_signer === false && c.active && (
                  <div className="text-yellow-400">{t('admin.noSigner')}</div>
                )}
                {c.error && <div className="text-red-400 truncate">{c.error}</div>}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div>
        <h3 className="text-lg font-semibold text-gray-300 mb-2">
          {t('admin.tokenDistribution', { count: data.tokens.count })}
        </h3>
        <div className="bg-gray-800 rounded overflow-hidden">
          <table className="w-full">
            <thead className="bg-gray-700">
              <tr className="text-left text-gray-300 text-sm">
                <th className="p-2">Asset</th>
                <th className="p-2">Holders</th>
                <th className="p-2">Available</th>
                <th className="p-2">Frozen</th>
                <th className="p-2">Total</th>
              </tr>
            </thead>
            <tbody>
              {data.tokens.tokens.map(t => (
                <tr key={t.asset} className="border-t border-gray-700">
                  <td className="p-2 font-mono">{t.asset}</td>
                  <td className="p-2">{t.holders}</td>
                  <td className="p-2 font-mono">{t.available.toFixed(2)}</td>
                  <td className="p-2 font-mono text-yellow-400">{t.frozen.toFixed(2)}</td>
                  <td className="p-2 font-mono font-bold">{t.total.toFixed(2)}</td>
                </tr>
              ))}
              {data.tokens.tokens.length === 0 && (
                <tr><td colSpan={5} className="p-4 text-center text-gray-500">{t('admin.noBalances')}</td></tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {data.volume.top_withdrawn_tokens_24h && data.volume.top_withdrawn_tokens_24h.length > 0 && (
        <div>
          <h3 className="text-lg font-semibold text-gray-300 mb-2">{t('admin.topWithdrawnTokens')}</h3>
          <div className="bg-gray-800 rounded p-4">
            <div className="grid grid-cols-2 md:grid-cols-5 gap-2">
              {data.volume.top_withdrawn_tokens_24h.map(t => (
                <div key={t.asset} className="text-center">
                  <div className="text-sm text-gray-400">{t.asset}</div>
                  <div className="text-xl font-bold">{t.count}</div>
                  <div className="text-xs text-gray-500">{t.volume.toFixed(2)}</div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
      </div>
    </AdminLayout>
  )
}