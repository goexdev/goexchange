import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { AdminLayout } from '../components/AdminLayout'

export function AdminChains() {
  const { t } = useTranslation()
  const [chains, setChains] = useState<api.ChainInfo[]>([])
  const [activeCount, setActiveCount] = useState(0)
  const [totalCount, setTotalCount] = useState(0)
  const [err, setErr] = useState('')
  const [success, setSuccess] = useState('')
  const [testing, setTesting] = useState<string | null>(null)
  const [filterFamily, setFilterFamily] = useState<string>('all')
  const [filterStatus, setFilterStatus] = useState<string>('all')

  async function load() {
    setErr('')
    try {
      const r = await api.adminListChains()
      setChains(r.chains)
      setActiveCount(r.active_count)
      setTotalCount(r.total_count)
    } catch (e: any) {
      setErr(e.message)
    }
  }

  useEffect(() => {
    load()
  }, [])

  async function enable(chainId: string) {
    setErr('')
    setSuccess('')
    try {
      await api.adminEnableChain(chainId)
      setSuccess(t('admin.chainEnabled', { id: chainId }))
      await load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function disable(chainId: string) {
    if (!confirm(t('admin.chainDisableConfirm', { id: chainId }))) return
    setErr('')
    setSuccess('')
    try {
      await api.adminDisableChain(chainId)
      setSuccess(t('admin.chainDisabled', { id: chainId }))
      await load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function test(chainId: string) {
    setErr('')
    setSuccess('')
    setTesting(chainId)
    try {
      const r = await api.adminTestChain(chainId)
      setSuccess(t('admin.chainHealthy', { id: chainId, block: r.block_count }))
    } catch (e: any) {
      setErr(t('admin.chainTestFailed', { id: chainId, error: e.message }))
    } finally {
      setTesting(null)
    }
  }

  async function reload() {
    setErr('')
    setSuccess('')
    try {
      const r = await api.adminReloadChains()
      setSuccess(t('admin.reloaded', { changes: r.changes.join(', ') || t('admin.noChanges') }))
      await load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  const filteredChains = chains.filter(c => {
    if (filterFamily !== 'all' && c.family !== filterFamily) return false
    if (filterStatus === 'active' && !c.active) return false
    if (filterStatus === 'inactive' && c.active) return false
    if (filterStatus === 'enabled' && !c.enabled) return false
    if (filterStatus === 'disabled' && c.enabled) return false
    return true
  })

  const families = Array.from(new Set(chains.map(c => c.family).filter(Boolean)))

  const familyEmoji = (family?: string) => {
    switch (family) {
      case 'bitcoin': return '🟠'
      case 'evm': return '🔵'
      default: return '⚪'
    }
  }

  return (
    <AdminLayout>
      <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold">{t('admin.chainsManagement')}</h2>
        <div className="flex gap-2">
          <button onClick={reload} className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded">
            {t('admin.reloadConfig')}
          </button>
        </div>
      </div>

      <div className="grid grid-cols-4 gap-4">
        <div className="bg-gray-800 p-4 rounded">
          <div className="text-gray-400 text-sm">{t('admin.totalChains')}</div>
          <div className="text-3xl font-bold">{totalCount}</div>
        </div>
        <div className="bg-green-900 p-4 rounded">
          <div className="text-green-300 text-sm">{t('admin.activeChains')}</div>
          <div className="text-3xl font-bold">{activeCount}</div>
        </div>
        <div className="bg-gray-700 p-4 rounded">
          <div className="text-gray-400 text-sm">{t('admin.bitcoinFamily')}</div>
          <div className="text-3xl font-bold">
            {chains.filter(c => c.family === 'bitcoin').length}
          </div>
        </div>
        <div className="bg-blue-900 p-4 rounded">
          <div className="text-blue-300 text-sm">{t('admin.evmFamily')}</div>
          <div className="text-3xl font-bold">
            {chains.filter(c => c.family === 'evm').length}
          </div>
        </div>
      </div>

      {err && <div className="bg-red-900 text-red-200 p-3 rounded">{err}</div>}
      {success && <div className="bg-green-900 text-green-200 p-3 rounded">{success}</div>}

      <div className="flex gap-4 items-center bg-gray-800 p-3 rounded">
        <div>
          <label className="text-sm text-gray-400 mr-2">{t('admin.family')}:</label>
          <select value={filterFamily} onChange={e => setFilterFamily(e.target.value)}
            className="bg-gray-700 text-white px-2 py-1 rounded">
            <option value="all">All</option>
            {families.map(f => <option key={f} value={f}>{f}</option>)}
          </select>
        </div>
        <div>
          <label className="text-sm text-gray-400 mr-2">{t('admin.statusLabel')}:</label>
          <select value={filterStatus} onChange={e => setFilterStatus(e.target.value)}
            className="bg-gray-700 text-white px-2 py-1 rounded">
            <option value="all">All</option>
            <option value="active">{t('admin.statusActive')}</option>
            <option value="inactive">{t('admin.statusInactive')}</option>
            <option value="enabled">{t('admin.statusEnabled')}</option>
            <option value="disabled">{t('admin.statusDisabled')}</option>
          </select>
        </div>
      </div>

      <div className="bg-gray-800 rounded overflow-hidden">
        <table className="w-full">
          <thead className="bg-gray-700">
            <tr className="text-left text-gray-300 text-sm">
              <th className="p-3">{t('admin.chain')}</th>
              <th className="p-3">{t('admin.family')}</th>
              <th className="p-3">{t('admin.driver')}</th>
              <th className="p-3">{t('wallet.asset')}</th>
              <th className="p-3">{t('admin.chainIdCoinType')}</th>
              <th className="p-3">{t('admin.hotWallet')}</th>
              <th className="p-3">{t('admin.tokens')}</th>
              <th className="p-3">{t('admin.statusLabel')}</th>
              <th className="p-3">{t('common.actions')}</th>
            </tr>
          </thead>
          <tbody>
            {filteredChains.map(c => (
              <tr key={c.id} className="border-t border-gray-700 hover:bg-gray-750">
                <td className="p-3 font-mono">{c.id}</td>
                <td className="p-3">
                  <span className="text-sm">
                    {familyEmoji(c.family)} {c.family || '—'}
                  </span>
                </td>
                <td className="p-3 font-mono text-sm">{c.driver}</td>
                <td className="p-3 font-mono text-sm">{c.asset}</td>
                <td className="p-3 font-mono text-sm">
                  {c.family === 'bitcoin' && c.coin_type !== undefined && (
                    <span>{t('admin.coin')}{c.coin_type} {t('admin.prefix')}{(c.p2pkh_prefix || 0).toString(16).padStart(2, '0')}</span>
                  )}
                  {c.family === 'evm' && c.chain_id !== undefined && c.chain_id !== 0 && (
                    <span>{t('admin.chainId')}= {c.chain_id}</span>
                  )}
                  {!c.family && '—'}
                </td>
                <td className="p-3 font-mono text-xs">
                  {c.hot_wallet ?
                    <span className={c.hot_wallet.length > 30 ? '' : 'text-blue-400'}>
                      {c.hot_wallet.length > 30
                        ? `${c.hot_wallet.slice(0, 6)}...${c.hot_wallet.slice(-4)}`
                        : c.hot_wallet}
                    </span> :
                    <span className="text-gray-500">{t('admin.none')}</span>}
                </td>
                <td className="p-3 text-sm">
                  {c.tokens && c.tokens.length > 0 ? (
                    <div className="flex flex-wrap gap-1">
                      {c.tokens.map(t => (
                        <span key={t.symbol} className="bg-gray-700 px-2 py-0.5 rounded text-xs">
                          {t.symbol}({t.decimals})
                        </span>
                      ))}
                    </div>
                  ) : (
                    <span className="text-gray-500">—</span>
                  )}
                </td>
                <td className="p-3">
                  {c.active ? (
                    <span className="bg-green-700 text-green-100 px-2 py-1 rounded text-xs">
                      {t('admin.statusActive')}
                    </span>
                  ) : c.enabled ? (
                    <span className="bg-yellow-700 text-yellow-100 px-2 py-1 rounded text-xs">
                      {t('admin.statusEnabled')}
                    </span>
                  ) : (
                    <span className="bg-gray-700 text-gray-300 px-2 py-1 rounded text-xs">
                      {t('admin.statusDisabled')}
                    </span>
                  )}
                </td>
                <td className="p-3">
                  <div className="flex gap-1">
                    <button
                      onClick={() => test(c.id)}
                      disabled={testing === c.id || !c.active}
                      className="text-xs bg-blue-700 hover:bg-blue-800 disabled:bg-gray-600 text-white px-2 py-1 rounded"
                    >
                      {testing === c.id ? t('common.loading') : t('common.test')}
                    </button>
                    {c.enabled ? (
                      <button
                        onClick={() => disable(c.id)}
                        className="text-xs bg-red-700 hover:bg-red-800 text-white px-2 py-1 rounded"
                      >
                        {t('admin.disable')}
                      </button>
                    ) : (
                      <button
                        onClick={() => enable(c.id)}
                        className="text-xs bg-green-700 hover:bg-green-800 text-white px-2 py-1 rounded"
                      >
                        {t('admin.enable')}
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
            {filteredChains.length === 0 && (
              <tr>
                <td colSpan={9} className="p-6 text-center text-gray-500">
                  {t('admin.noMatch')}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="bg-blue-900 border-l-4 border-blue-500 p-4 rounded text-sm">
        <div className="font-bold mb-2">{t('admin.howToAdd')}</div>
        <ul className="space-y-1 ml-4 list-disc">
          <li>{t('admin.btcForkHint')}</li>
          <li>{t('admin.evmHint')}</li>
          <li>{t('admin.newFamilyHint')}</li>
        </ul>
      </div>
      </div>
    </AdminLayout>
  )
}