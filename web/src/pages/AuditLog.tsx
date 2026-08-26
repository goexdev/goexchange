import { useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import * as api from '../lib/api'
import { AdminLayout } from '../components/AdminLayout'

interface AuditEntry extends api.AuditEntry {}

const ACTION_OPTIONS = [
  { value: '', label: 'All actions' },
  // User
  { value: 'user.set_role', label: 'Set user role' },
  { value: 'user.reset_password', label: 'Reset user password' },
  { value: 'user.register', label: 'User register' },
  { value: 'user.login', label: 'User login' },
  { value: 'user.login_failed', label: 'User login failed' },
  // KYC
  { value: 'kyc.approve', label: 'Approve KYC' },
  { value: 'kyc.reject', label: 'Reject KYC' },
  // Withdrawal
  { value: 'withdrawal.approve_hold', label: 'Approve held withdrawal' },
  { value: 'withdrawal.reject_hold', label: 'Reject held withdrawal' },
  // Market (M6.111)
  { value: 'market.pair_enable', label: 'Enable market pair' },
  { value: 'market.pair_disable', label: 'Disable market pair' },
  // Chain (M6.6)
  { value: 'chain.enable', label: 'Enable chain' },
  { value: 'chain.disable', label: 'Disable chain' },
  { value: 'chain.add', label: 'Add chain' },
  { value: 'chain.remove', label: 'Remove chain' },
  { value: 'chain.reload', label: 'Reload chains' },
  { value: 'chain.test', label: 'Test chain' },
  // Trading
  { value: 'trading.cancel_order', label: 'Cancel order' },
  { value: 'trading.cancel_all', label: 'Cancel all orders' },
  // 2FA
  { value: '2fa.enable', label: 'Enable 2FA' },
  { value: '2fa.disable', label: 'Disable 2FA' },
]

const TARGET_OPTIONS = [
  { value: '', label: 'All targets' },
  { value: 'user', label: 'User' },
  { value: 'kyc', label: 'KYC' },
  { value: 'withdrawal', label: 'Withdrawal' },
  { value: 'deposit', label: 'Deposit' },
  { value: 'order', label: 'Order' },
  { value: 'chain', label: 'Chain' },
  { value: 'market_pair', label: 'Market Pair' },
]

function formatTime(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleString()
}

function formatAge(iso: string): string {
  const ms = Date.now() - d().getTime()
  const sec = Math.floor(ms / 1000)
  if (sec < 60) return `${sec}s ago`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}h ago`
  const day = Math.floor(hr / 24)
  return `${day}d ago`
}

function d(): Date { return new Date() }

function getActionColor(action: string): string {
  if (action.startsWith('kyc.approve')) return 'bg-green-100 text-green-800'
  if (action.startsWith('kyc.reject')) return 'bg-red-100 text-red-800'
  if (action.startsWith('user.set_role')) return 'bg-blue-100 text-blue-800'
  if (action.startsWith('user.login_failed')) return 'bg-red-100 text-red-800'
  if (action.startsWith('user.login')) return 'bg-indigo-100 text-indigo-800'
  if (action.startsWith('user.register')) return 'bg-purple-100 text-purple-800'
  if (action.startsWith('user.reset_password')) return 'bg-yellow-100 text-yellow-800'
  if (action.startsWith('withdrawal.approve')) return 'bg-green-100 text-green-800'
  if (action.startsWith('withdrawal.reject')) return 'bg-red-100 text-red-800'
  if (action.startsWith('market.pair_enable')) return 'bg-green-100 text-green-800'
  if (action.startsWith('market.pair_disable')) return 'bg-orange-100 text-orange-800'
  if (action.startsWith('chain.enable')) return 'bg-green-100 text-green-800'
  if (action.startsWith('chain.disable')) return 'bg-red-100 text-red-800'
  if (action.startsWith('chain.add')) return 'bg-blue-100 text-blue-800'
  if (action.startsWith('chain.remove')) return 'bg-red-100 text-red-800'
  if (action.startsWith('chain.reload')) return 'bg-indigo-100 text-indigo-800'
  if (action.startsWith('chain.test')) return 'bg-cyan-100 text-cyan-800'
  if (action.startsWith('trading.cancel')) return 'bg-orange-100 text-orange-800'
  if (action.startsWith('2fa.enable')) return 'bg-emerald-100 text-emerald-800'
  if (action.startsWith('2fa.disable')) return 'bg-red-100 text-red-800'
  return 'bg-gray-100 text-gray-700'
}

export function AuditLog() {
  const { t } = useTranslation()
  const [entries, setEntries] = useState<AuditEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)

  // Filters
  const [actionFilter, setActionFilter] = useState('')
  const [targetFilter, setTargetFilter] = useState('')
  const [adminFilter, setAdminFilter] = useState('')
  const [sinceFilter, setSinceFilter] = useState('')
  const [limit, setLimit] = useState(50)

  const load = useCallback(async () => {
    setLoading(true)
    setErr('')
    try {
      const params: Record<string, string> = { limit: String(limit) }
      if (actionFilter) params.action = actionFilter
      if (targetFilter) params.target_type = targetFilter
      if (adminFilter) params.admin_id = adminFilter
      if (sinceFilter) {
        const since = new Date(sinceFilter)
        params.since = since.toISOString()
      }
      const data = await api.listAuditLogs(params)
      setEntries(data || [])
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }, [actionFilter, targetFilter, adminFilter, sinceFilter, limit])

  useEffect(() => {
    load()
  }, [load])

  function clearFilters() {
    setActionFilter('')
    setTargetFilter('')
    setAdminFilter('')
    setSinceFilter('')
    setLimit(50)
  }

  return (
    <AdminLayout>
      <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">{t('admin.auditLog')}</h1>
          <p className="text-muted text-sm mt-1">
            {t('admin.auditLogDesc')}
          </p>
        </div>
        <div className="flex gap-2">
          <button onClick={load} className="btn-secondary" disabled={loading}>
            {loading ? t('common.loadingEllipsis') : t('common.refresh')}
          </button>
          <Link to="/admin" className="btn-secondary">{t('admin.backToAdmin')}</Link>
        </div>
      </div>

      {/* Filters */}
      <div className="card">
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-lg font-semibold">{t('admin.auditFilters')}</h2>
          <button
            onClick={clearFilters}
            className="text-xs text-brand hover:underline"
          >
            {t('admin.clearAll')}
          </button>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
          <div>
            <label className="block text-xs text-muted mb-1 uppercase">{t('admin.action')}</label>
            <select
              className="input w-full"
              value={actionFilter}
              onChange={(e) => setActionFilter(e.target.value)}
            >
              {ACTION_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-xs text-muted mb-1 uppercase">{t('admin.targetType')}</label>
            <select
              className="input w-full"
              value={targetFilter}
              onChange={(e) => setTargetFilter(e.target.value)}
            >
              {TARGET_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="block text-xs text-muted mb-1 uppercase">{t('admin.since')}</label>
            <input
              type="datetime-local"
              className="input w-full"
              value={sinceFilter}
              onChange={(e) => setSinceFilter(e.target.value)}
            />
          </div>
          <div>
            <label className="block text-xs text-muted mb-1 uppercase">{t('admin.limit')}</label>
            <select
              className="input w-full"
              value={limit}
              onChange={(e) => setLimit(Number(e.target.value))}
            >
              <option value={20}>20</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
              <option value={200}>200</option>
            </select>
          </div>
        </div>
        <div className="mt-3 text-sm text-muted">
          {t('admin.showingEntries', { count: entries.length })}
          {adminFilter && <span> · {t('admin.filteredByAdmin')}</span>}
          {sinceFilter && <span> · {t('admin.sinceTime', { time: new Date(sinceFilter).toLocaleString() })}</span>}
        </div>
      </div>

      {err && (
        <div className="card border-red-300 bg-red-50 text-red-800">
          ⚠ {err}
        </div>
      )}

      {/* Results */}
      <div className="card overflow-x-auto">
        <table className="w-full text-left">
          <thead className="text-xs text-muted uppercase tracking-wide border-b">
            <tr>
              <th className="py-2 px-3">{t('admin.when')}</th>
              <th className="py-2 px-3">{t('admin.adminUser')}</th>
              <th className="py-2 px-3">{t('admin.action')}</th>
              <th className="py-2 px-3">{t('admin.target')}</th>
              <th className="py-2 px-3">{t('admin.result')}</th>
              <th className="py-2 px-3">{t('admin.ip')}</th>
              <th className="py-2 px-3"></th>
            </tr>
          </thead>
          <tbody>
            {loading && entries.length === 0 ? (
              <tr>
                <td colSpan={7} className="text-center py-8 text-muted">
                  <div className="inline-block w-6 h-6 border-4 border-brand border-t-transparent rounded-full animate-spin mb-2"></div>
                  <div>{t('admin.loadingAuditLog')}</div>
                </td>
              </tr>
            ) : entries.length === 0 ? (
              <tr>
                <td colSpan={7} className="text-center py-8 text-muted">
                  {t('admin.noEntries')}
                </td>
              </tr>
            ) : (
              entries.map((e) => (
                <AuditEntryRow
                  key={e.id}
                  entry={e}
                  expanded={expanded === e.id}
                  onToggle={() => setExpanded(expanded === e.id ? null : e.id)}
                />
              ))
            )}
          </tbody>
        </table>
      </div>

      {/* Summary stats */}
      {!loading && entries.length > 0 && (
        <div className="card bg-blue-50 border-blue-200">
          <div className="text-sm">
            <div className="font-semibold mb-2">{t('admin.summary')}</div>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-xs">
              <div>
                <div className="text-muted">{t('admin.totalShown')}</div>
                <div className="text-lg font-bold">{entries.length}</div>
              </div>
              <div>
                <div className="text-muted">{t('admin.success_short')}</div>
                <div className="text-lg font-bold text-green-700">
                  {entries.filter((e) => e.status === 'success').length}
                </div>
              </div>
              <div>
                <div className="text-muted">{t('admin.failures')}</div>
                <div className="text-lg font-bold text-red-700">
                  {entries.filter((e) => e.status === 'failure').length}
                </div>
              </div>
              <div>
                <div className="text-muted">{t('admin.uniqueAdmins')}</div>
                <div className="text-lg font-bold">
                  {new Set(entries.map((e) => e.admin_user_id || "anonymous")).size}
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
      </div>
    </AdminLayout>
  )
}

function AuditEntryRow({
  entry,
  expanded,
  onToggle,
}: {
  entry: AuditEntry
  expanded: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const isFail = entry.status === 'failure'
  return (
    <>
      <tr
        className={`table-row cursor-pointer hover:bg-bg ${isFail ? 'bg-red-50' : ''}`}
        onClick={onToggle}
      >
        <td className="py-3 px-3 text-xs">
          <div>{formatAge(entry.created_at)}</div>
          <div className="text-muted text-[10px]">{formatTime(entry.created_at)}</div>
        </td>
        <td className="py-3 px-3">
          <div className="font-semibold text-sm">{entry.admin_email}</div>
          <div className="text-xs text-muted font-mono">{(entry.admin_user_id || "").slice(0, 8)}…</div>
        </td>
        <td className="py-3 px-3">
          <span className={`inline-block px-2 py-0.5 text-xs font-medium rounded ${getActionColor(entry.action)}`}>
            {entry.action}
          </span>
        </td>
        <td className="py-3 px-3">
          <div className="text-xs">
            <span className="px-1 bg-bg rounded">{entry.target_type}</span>
          </div>
          <div className="text-xs text-muted mt-0.5">
            {entry.target_label && entry.target_label !== entry.target_id ? (
              <span className="font-mono">{entry.target_label}</span>
            ) : (
              <span className="font-mono">{entry.target_id?.slice(0, 12)}…</span>
            )}
          </div>
        </td>
        <td className="py-3 px-3">
          {isFail ? (
            <span className="text-xs font-semibold text-red-700">{t('admin.failShort')}</span>
          ) : (
            <span className="text-xs font-semibold text-green-700">{t('admin.okShort')}</span>
          )}
        </td>
        <td className="py-3 px-3 text-xs font-mono text-muted">
          {entry.ip || '—'}
        </td>
        <td className="py-3 px-3 text-xs text-muted">
          {expanded ? '▼' : '▶'}
        </td>
      </tr>
      {expanded && (
        <tr className="bg-bg">
          <td colSpan={7} className="px-3 py-3">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
              <div>
                <div className="text-muted uppercase tracking-wide mb-1">{t('admin.auditId')}</div>
                <div className="font-mono">{entry.id}</div>
              </div>
              <div>
                <div className="text-muted uppercase tracking-wide mb-1">Admin</div>
                <div className="font-mono">{entry.admin_user_id || "(anonymous)"}</div>
                <div className="text-text">{entry.admin_email}</div>
              </div>
              <div>
                <div className="text-muted uppercase tracking-wide mb-1">{t('admin.target')}</div>
                <div>{t('admin.targetType')}: {entry.target_type}</div>
                <div>{t('admin.idLabel')}: <span className="font-mono">{entry.target_id}</span></div>
                {entry.target_label && (
                  <div>{t('admin.label')}: <span className="font-mono">{entry.target_label}</span></div>
                )}
              </div>
              <div>
                <div className="text-muted uppercase tracking-wide mb-1">{t('admin.request')}</div>
                <div>{t('admin.ip')}: <span className="font-mono">{entry.ip || '—'}</span></div>
                <div className="truncate" title={entry.user_agent}>
                  {t('admin.ua')}: {entry.user_agent ? entry.user_agent.slice(0, 60) + (entry.user_agent.length > 60 ? '…' : '') : '—'}
                </div>
              </div>
              {entry.details && Object.keys(entry.details).length > 0 && (
                <div className="md:col-span-2">
                  <div className="text-muted uppercase tracking-wide mb-1">Details</div>
                  <pre className="bg-bg p-2 rounded text-[10px] overflow-x-auto">
                    {JSON.stringify(entry.details, null, 2)}
                  </pre>
                </div>
              )}
              {entry.error_msg && (
                <div className="md:col-span-2">
                  <div className="text-muted uppercase tracking-wide mb-1">Error</div>
                  <div className="text-red-700 bg-red-50 p-2 rounded">{entry.error_msg}</div>
                </div>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  )
}
