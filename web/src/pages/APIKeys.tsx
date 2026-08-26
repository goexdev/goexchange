import { config } from '../lib/config';
import { FormEvent, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'

interface NewKeyResponse {
  key: api.APIKey
  secret: string
  warning: string
}

const SCOPES = [
  { value: 'read', labelKey: 'apiKeys.scopeRead', descKey: 'apiKeys.scopeReadDesc' },
  { value: 'trade', labelKey: 'apiKeys.scopeTrade', descKey: 'apiKeys.scopeTradeDesc' },
  { value: 'withdraw', labelKey: 'apiKeys.scopeWithdraw', descKey: 'apiKeys.scopeWithdrawDesc' },
] as const

export function APIKeys() {
  const { t } = useTranslation()
  const [keys, setKeys] = useState<api.APIKey[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [showCreate, setShowCreate] = useState(false)

  // Create form state
  const [name, setName] = useState('')
  const [selectedScopes, setSelectedScopes] = useState<Set<string>>(new Set(['read']))
  const [expiresInDays, setExpiresInDays] = useState(0)
  const [newKey, setNewKey] = useState<NewKeyResponse | null>(null)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    load()
  }, [])

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const res = await api.listAPIKeys()
      setKeys(res.keys || [])
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  async function handleCreate(e: FormEvent) {
    e.preventDefault()
    if (!name.trim()) {
      setErr(t('apiKeys.nameRequired'))
      return
    }
    if (selectedScopes.size === 0) {
      setErr(t('apiKeys.selectScope'))
      return
    }
    setSubmitting(true)
    setErr('')
    try {
      const res = await api.createAPIKey({
        name: name.trim(),
        scopes: Array.from(selectedScopes),
        expires_in_days: expiresInDays > 0 ? expiresInDays : undefined,
      })
      setNewKey(res)
      setName('')
      setSelectedScopes(new Set(['read']))
      setExpiresInDays(0)
      setShowCreate(false)
      load()
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setSubmitting(false)
    }
  }

  async function handleRevoke(id: string) {
    if (!confirm(t('apiKeys.revokeConfirm'))) return
    try {
      await api.revokeAPIKey(id)
      load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  function toggleScope(scope: string) {
    setSelectedScopes(prev => {
      const next = new Set(prev)
      if (next.has(scope)) next.delete(scope)
      else next.add(scope)
      return next
    })
  }

  function copyToClipboard(text: string) {
    navigator.clipboard.writeText(text).catch(() => {})
  }

  function scopeBadge(scope: string) {
    const colors: Record<string, string> = {
      read: 'bg-blue-700 text-blue-200',
      trade: 'bg-purple-700 text-purple-200',
      withdraw: 'bg-red-700 text-red-200',
    }
    return (
      <span className={`px-2 py-0.5 rounded text-xs font-mono ${colors[scope] || 'bg-gray-700'}`}>
        {scope}
      </span>
    )
  }

  return (
    <div className="max-w-4xl mx-auto space-y-4">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-bold">{t('apiKeys.title')}</h1>
          <p className="text-sm text-gray-400">
            {t('apiKeys.subtitle')}
          </p>
        </div>
        <button
          onClick={() => setShowCreate(s => !s)}
          className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded"
        >
          {showCreate ? t('common.cancel') : t('apiKeys.createNew')}
        </button>
      </div>

      {err && <div className="bg-red-900 text-red-200 p-3 rounded text-sm">{err}</div>}

      {/* New key reveal */}
      {newKey && (
        <div className="bg-green-900 border-2 border-green-500 p-4 rounded">
          <div className="font-bold mb-2 text-green-200">{t('apiKeys.keyCreated')}</div>
          <div className="text-yellow-300 mb-3 text-sm">
            {t('apiKeys.warnIcon')} {newKey.warning}
          </div>
          <div className="space-y-2">
            <div>
              <div className="text-xs text-gray-300 mb-1">Name</div>
              <div className="bg-gray-800 px-3 py-2 rounded font-mono text-sm">
                {newKey.key.name}
              </div>
            </div>
            <div>
              <div className="text-xs text-gray-300 mb-1">{t('apiKeys.secret')}</div>
              <div className="bg-gray-800 px-3 py-2 rounded font-mono text-sm flex justify-between items-center">
                <span className="text-yellow-300">{newKey.secret}</span>
                <button
                  onClick={() => copyToClipboard(newKey.secret)}
                  className="ml-2 text-xs bg-blue-600 hover:bg-blue-700 text-white px-2 py-1 rounded"
                >
                  Copy
                </button>
              </div>
            </div>
          </div>
          <button
            onClick={() => setNewKey(null)}
            className="mt-3 text-xs text-gray-400 hover:text-white"
          >
            {t('apiKeys.dismiss')}
          </button>
        </div>
      )}

      {/* Create form */}
      {showCreate && (
        <form onSubmit={handleCreate} className="card space-y-4">
          <h3 className="text-lg font-semibold">{t('apiKeys.createNewTitle')}</h3>

          <div>
            <label className="block text-sm text-gray-300 mb-1">Name</label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="e.g. Trading bot"
              className="w-full bg-gray-700 px-3 py-2 rounded text-white"
              required
            />
          </div>

          <div>
            <label className="block text-sm text-gray-300 mb-2">Scopes</label>
            <div className="space-y-2">
              {SCOPES.map(s => (
                <label key={s.value} className="flex items-start gap-3 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={selectedScopes.has(s.value)}
                    onChange={() => toggleScope(s.value)}
                    className="mt-1"
                  />
                  <div>
                    <div className="font-mono text-sm">{t(s.labelKey)}</div>
                    <div className="text-xs text-gray-400">{t(s.descKey)}</div>
                  </div>
                </label>
              ))}
            </div>
          </div>

          <div>
            <label className="block text-sm text-gray-300 mb-1">{t('apiKeys.expiresIn')}</label>
            <input
              type="number"
              value={expiresInDays}
              onChange={e => setExpiresInDays(Number(e.target.value))}
              min={0}
              max={365}
              className="w-full bg-gray-700 px-3 py-2 rounded text-white"
            />
          </div>

          <button
            type="submit"
            disabled={submitting}
            className="bg-blue-600 hover:bg-blue-700 disabled:bg-gray-600 text-white px-4 py-2 rounded"
          >
            {submitting ? 'Creating...' : 'Create key'}
          </button>
        </form>
      )}

      {/* Keys list */}
      <div className="card">
        {loading && keys.length === 0 ? (
          <div className="p-8 text-center text-gray-500">{t('common.loadingEllipsis')}</div>
        ) : keys.length === 0 ? (
          <div className="p-8 text-center text-gray-500">
            {t('apiKeys.noKeys')}
          </div>
        ) : (
          <table className="w-full text-left">
            <thead className="text-muted text-sm border-b border-gray-700">
              <tr>
                <th className="py-2 px-3">Name</th>
                <th className="py-2 px-3">{t('apiKeys.keyId')}</th>
                <th className="py-2 px-3">Scopes</th>
                <th className="py-2 px-3">{t('apiKeys.lastUsed')}</th>
                <th className="py-2 px-3">Expires</th>
                <th className="py-2 px-3">Status</th>
                <th className="py-2 px-3"></th>
              </tr>
            </thead>
            <tbody>
              {keys.map(k => (
                <tr key={k.id} className="border-t border-gray-700 hover:bg-gray-800">
                  <td className="py-3 px-3 font-medium">{k.name}</td>
                  <td className="py-3 px-3 font-mono text-xs text-gray-400">{k.key_id}</td>
                  <td className="py-3 px-3">
                    <div className="flex gap-1 flex-wrap">
                      {k.scopes.map(s => scopeBadge(s))}
                    </div>
                  </td>
                  <td className="py-3 px-3 text-sm text-gray-400">
                    {k.last_used_at ? new Date(k.last_used_at).toLocaleString() : 'Never'}
                  </td>
                  <td className="py-3 px-3 text-sm text-gray-400">
                    {k.expires_at ? new Date(k.expires_at).toLocaleDateString() : 'Never'}
                  </td>
                  <td className="py-3 px-3">
                    {k.revoked ? (
                      <span className="bg-red-900 text-red-200 px-2 py-0.5 rounded text-xs">Revoked</span>
                    ) : (
                      <span className="bg-green-900 text-green-200 px-2 py-0.5 rounded text-xs">Active</span>
                    )}
                  </td>
                  <td className="py-3 px-3 text-right">
                    {!k.revoked && (
                      <button
                        onClick={() => handleRevoke(k.id)}
                        className="text-xs bg-red-700 hover:bg-red-600 text-red-100 px-2 py-1 rounded"
                      >
                        Revoke
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="card bg-gray-800 text-sm text-gray-400">
        <h3 className="text-sm font-semibold text-gray-300 mb-2">{t('apiKeys.usingKeys')}</h3>
        <p className="mb-2">{t('apiKeys.usingKeysDesc')}</p>
        <pre className="bg-gray-900 p-2 rounded text-xs text-green-300 overflow-x-auto">
{`curl -H "Authorization: Bearer gk_live_xxxxxxxx_secretpart" \
  https://${config.domain}/api/v1/wallets`}
        </pre>
        <p className="mt-2 text-xs">
          <b className="text-red-400">{t('apiKeys.neverShare')}</b> {t('apiKeys.keepLikePassword')}
        </p>
      </div>
    </div>
  )
}
