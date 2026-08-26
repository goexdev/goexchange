import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { AdminLayout } from '../components/AdminLayout'
import { getMarketWS, type MarketWSEvent } from '../lib/marketWS'

export function AdminPairs() {
  const { t } = useTranslation()
  const [pairs, setPairs] = useState<api.Market[]>([])
  const [enabledCount, setEnabledCount] = useState(0)
  const [disabledCount, setDisabledCount] = useState(0)
  const [err, setErr] = useState('')
  const [success, setSuccess] = useState('')
  const [toggling, setToggling] = useState<string | null>(null)
  const [search, setSearch] = useState('')

  async function load() {
    setErr('')
    try {
      const all = await api.adminListPairs()
      setPairs(all)
      const enabled = all.filter((p) => p.enabled).length
      setEnabledCount(enabled)
      setDisabledCount(all.length - enabled)
    } catch (e: any) {
      setErr(e.message)
    }
  }

  useEffect(() => {
    load()

    // WebSocket: reload on any market change
    // (handles other admin tabs/devices changing pairs)
    const ws = getMarketWS()
    const handleEvent = (_e: MarketWSEvent) => {
      load()
    }
    ws.onEvent = handleEvent
    ws.connect()

    return () => {
      ws.onEvent = null
      ws.disconnect()
    }
  }, [])

  async function toggle(base: string, quote: string, enabled: boolean) {
    if (enabled === false) {
      if (!confirm(t('admin.pairs.disableConfirm', { pair: `${base}_${quote}` }))) {
        return
      }
    }
    setErr('')
    setSuccess('')
    setToggling(`${base}_${quote}`)
    try {
      await api.adminTogglePair(base, quote, enabled)
      setSuccess(
        t('admin.pairs.toggled', {
          pair: `${base}_${quote}`,
          state: enabled ? t('admin.pairs.enabled') : t('admin.pairs.disabled'),
        })
      )
      await load()
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setToggling(null)
    }
  }

  // Filter pairs by search
  const filtered = pairs.filter((p) => {
    if (!search) return true
    const q = search.toUpperCase()
    return p.pair.includes(q) || p.base.includes(q) || p.quote.includes(q)
  })

  return (
    <AdminLayout>
      <div className="space-y-4">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold text-text">{t('admin.pairs.title')}</h1>
            <p className="text-muted text-sm mt-1">{t('admin.pairs.subtitle')}</p>
          </div>
          <button
            onClick={load}
            className="btn-secondary text-sm"
            data-testid="refresh-pairs"
          >
            {t('common.refresh')}
          </button>
        </div>

        {/* Alerts */}
        {err && (
          <div className="bg-red-50 border border-red-200 text-red-800 px-4 py-3 rounded">
            <strong>{t('common.error')}:</strong> {err}
          </div>
        )}
        {success && (
          <div className="bg-green-50 border border-green-200 text-green-800 px-4 py-3 rounded">
            {success}
          </div>
        )}

        {/* Summary cards */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="bg-panel border border-border rounded-lg p-4">
            <div className="text-xs text-muted uppercase">{t('admin.pairs.total')}</div>
            <div className="text-2xl font-bold text-text mt-1">{pairs.length}</div>
            <div className="text-xs text-muted mt-1">{t('admin.pairs.configured')}</div>
          </div>
          <div className="bg-panel border border-border rounded-lg p-4">
            <div className="text-xs text-muted uppercase">{t('admin.pairs.enabled')}</div>
            <div className="text-2xl font-bold text-green-400 mt-1">{enabledCount}</div>
            <div className="text-xs text-muted mt-1">{t('admin.pairs.tradingActive')}</div>
          </div>
          <div className="bg-panel border border-border rounded-lg p-4">
            <div className="text-xs text-muted uppercase">{t('admin.pairs.disabled')}</div>
            <div className="text-2xl font-bold text-red-400 mt-1">{disabledCount}</div>
            <div className="text-xs text-muted mt-1">{t('admin.pairs.notTrading')}</div>
          </div>
        </div>

        {/* Search */}
        <div className="bg-panel border border-border rounded-lg p-3">
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('admin.pairs.searchPlaceholder')}
            className="w-full bg-bg border border-border rounded px-3 py-2 text-sm"
          />
        </div>

        {/* Pairs table */}
        <div className="bg-panel border border-border rounded-lg overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-border bg-bg/50">
                <th className="text-left py-3 px-4 text-sm font-semibold text-muted uppercase">
                  {t('admin.pairs.pair')}
                </th>
                <th className="text-left py-3 px-4 text-sm font-semibold text-muted uppercase">
                  {t('admin.pairs.base')}
                </th>
                <th className="text-left py-3 px-4 text-sm font-semibold text-muted uppercase">
                  {t('admin.pairs.quote')}
                </th>
                <th className="text-center py-3 px-4 text-sm font-semibold text-muted uppercase">
                  {t('admin.pairs.status')}
                </th>
                <th className="text-right py-3 px-4 text-sm font-semibold text-muted uppercase">
                  {t('admin.pairs.actions')}
                </th>
              </tr>
            </thead>
            <tbody>
              {filtered.length === 0 ? (
                <tr>
                  <td colSpan={5} className="text-center py-8 text-muted">
                    {t('admin.pairs.noMatch')}
                  </td>
                </tr>
              ) : (
                filtered.map((p) => (
                  <tr
                    key={p.pair}
                    className="border-b border-border last:border-b-0 hover:bg-bg/30 transition-colors"
                    data-testid={`pair-row-${p.pair}`}
                  >
                    <td className="py-3 px-4 font-mono font-semibold">{p.pair}</td>
                    <td className="py-3 px-4">{p.base}</td>
                    <td className="py-3 px-4">{p.quote}</td>
                    <td className="py-3 px-4 text-center">
                      {p.enabled ? (
                        <span className="inline-flex items-center px-2 py-1 rounded text-xs font-semibold bg-green-50 text-green-700 border border-green-200">
                          ● {t('admin.pairs.enabled')}
                        </span>
                      ) : (
                        <span className="inline-flex items-center px-2 py-1 rounded text-xs font-semibold bg-red-50 text-red-700 border border-red-200">
                          ○ {t('admin.pairs.disabled')}
                        </span>
                      )}
                    </td>
                    <td className="py-3 px-4 text-right">
                      {p.enabled ? (
                        <button
                          onClick={() => toggle(p.base, p.quote, false)}
                          disabled={toggling === p.pair}
                          className="btn-secondary text-sm text-red-400 hover:text-red-300 disabled:opacity-50"
                          data-testid={`disable-${p.pair}`}
                        >
                          {toggling === p.pair ? t('common.processing') : t('admin.pairs.disable')}
                        </button>
                      ) : (
                        <button
                          onClick={() => toggle(p.base, p.quote, true)}
                          disabled={toggling === p.pair}
                          className="btn-primary text-sm disabled:opacity-50"
                          data-testid={`enable-${p.pair}`}
                        >
                          {toggling === p.pair ? t('common.processing') : t('admin.pairs.enable')}
                        </button>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {/* Footer help */}
        <div className="bg-panel border border-border rounded-lg p-4 text-sm text-muted">
          <div className="font-semibold text-text mb-1">{t('common.info', 'ℹ️')} {t('admin.pairs.howToAdd')}</div>
          <p>{t('admin.pairs.howToAddDesc')}</p>
          {/* External docs link removed: docs/ is not shipped in the
              public repository. Operators should consult the private
              operator runbook for adding new trading pairs. */}
        </div>
      </div>
    </AdminLayout>
  )
}

export default AdminPairs
