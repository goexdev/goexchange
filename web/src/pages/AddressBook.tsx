import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { useToast } from '../components/Toast'

export function AddressBook() {
  const { t } = useTranslation()
  const toast = useToast()
  const [addresses, setAddresses] = useState<api.AddressBookEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<string>('all')
  const [showAdd, setShowAdd] = useState(false)
  const [newAsset, setNewAsset] = useState('BTC')
  const [newAddress, setNewAddress] = useState('')
  const [newLabel, setNewLabel] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    load()
  }, [])

  async function load() {
    setLoading(true)
    try {
      const result = await api.listAddresses()
      setAddresses(result.addresses)
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to load addresses', 'error')
    } finally {
      setLoading(false)
    }
  }

  async function handleAdd() {
    if (!newAddress) {
      toast.showToast(t('addressBook.addressRequired', 'Address is required'), 'warning')
      return
    }
    setSubmitting(true)
    try {
      await api.addAddress(newAsset, newAddress, newLabel, false)
      toast.showToast(t('addressBook.addressAdded', 'Address added'), 'success')
      setNewAddress('')
      setNewLabel('')
      setShowAdd(false)
      await load()
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to add address', 'error')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleDelete(id: string, label: string) {
    if (!confirm(t('addressBook.deleteConfirm', { label }))) return
    try {
      await api.deleteAddress(id)
      toast.showToast(t('addressBook.addressDeleted', 'Address deleted'), 'success')
      await load()
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to delete', 'error')
    }
  }

  async function handleToggleWhitelist(addr: api.AddressBookEntry) {
    try {
      await api.updateAddress(addr.id, { whitelisted: !addr.whitelisted })
      toast.showToast(
        addr.whitelisted ? 'Removed from whitelist' : 'Added to whitelist',
        'success'
      )
      await load()
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to update', 'error')
    }
  }

  const filtered = filter === 'all' ? addresses : addresses.filter((a) => a.asset === filter)
  const assets = Array.from(new Set(addresses.map((a) => a.asset)))

  return (
    <div className="space-y-4">
      <div className="flex justify-between items-center">
        <h2 className="text-xl font-bold">{t('addressBook.title')}</h2>
        <button
          onClick={() => setShowAdd(!showAdd)}
          className="btn-primary text-sm"
        >
          {showAdd ? t('common.cancel') : t('addressBook.addNew')}
        </button>
      </div>

      {showAdd && (
        <div className="card bg-bg/50">
          <h3 className="text-lg font-semibold mb-3">{t('addressBook.addNew')}</h3>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <div>
              <label className="text-xs text-muted block mb-1">{t('addressBook.asset')}</label>
              <select
                value={newAsset}
                onChange={(e) => setNewAsset(e.target.value)}
                className="input w-full"
              >
                <option value="BTC">BTC</option>
                <option value="ETH">ETH</option>
                <option value="USDT">USDT</option>
                <option value="USDC">USDC</option>
                <option value="SOL">SOL</option>
                <option value="BNB">BNB</option>
              </select>
            </div>
            <div>
              <label className="text-xs text-muted block mb-1">{t('addressBook.label')}</label>
              <input
                type="text"
                value={newLabel}
                onChange={(e) => setNewLabel(e.target.value)}
                className="input w-full"
                placeholder={t('addressBook.labelPlaceholder')}
              />
            </div>
            <div className="md:col-span-2">
              <label className="text-xs text-muted block mb-1">{t('addressBook.address')}</label>
              <input
                type="text"
                value={newAddress}
                onChange={(e) => setNewAddress(e.target.value)}
                className="input w-full font-mono text-sm"
                placeholder={t('addressBook.addressPlaceholder')}
              />
            </div>
            <div className="md:col-span-2 flex gap-2">
              <button
                onClick={handleAdd}
                disabled={submitting}
                className="btn-primary text-sm"
              >
                {submitting ? t('common.loading') : t('common.save')}
              </button>
              <button
                onClick={() => setShowAdd(false)}
                className="btn-secondary text-sm"
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        </div>
      )}

      {assets.length > 0 && (
        <div className="flex gap-2 text-sm flex-wrap">
          <button
            onClick={() => setFilter('all')}
            className={filter === 'all' ? 'px-3 py-1 rounded bg-brand text-bg' : 'px-3 py-1 rounded bg-panel border border-border text-text'}
          >
            {t('common.all')} ({addresses.length})
          </button>
          {assets.map((a) => (
            <button
              key={a}
              onClick={() => setFilter(a)}
              className={filter === a ? 'px-3 py-1 rounded bg-brand text-bg' : 'px-3 py-1 rounded bg-panel border border-border text-text'}
            >
              {a} ({addresses.filter((x) => x.asset === a).length})
            </button>
          ))}
        </div>
      )}

      {loading ? (
        <div className="text-center text-muted py-8">{t('common.loading')}</div>
      ) : filtered.length === 0 ? (
        <div className="card text-center text-muted py-8">
          {t('addressBook.empty')}
        </div>
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border">
                <th className="text-left p-2">{t('addressBook.asset')}</th>
                <th className="text-left p-2">{t('addressBook.label')}</th>
                <th className="text-left p-2">{t('addressBook.address')}</th>
                <th className="text-left p-2">{t('addressBook.whitelist')}</th>
                <th className="text-left p-2">{t('addressBook.lastUsed')}</th>
                <th className="text-right p-2">{t('common.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((addr) => (
                <tr key={addr.id} className="border-b border-border/50">
                  <td className="p-2 font-semibold">{addr.asset}</td>
                  <td className="p-2">{addr.label || '—'}</td>
                  <td className="p-2 font-mono text-xs">
                    {addr.address.slice(0, 12)}...{addr.address.slice(-8)}
                  </td>
                  <td className="p-2">
                    <button
                      onClick={() => handleToggleWhitelist(addr)}
                      className={
                        addr.whitelisted
                          ? 'px-2 py-1 text-xs rounded bg-green-900 text-green-200 border border-green-700'
                          : 'px-2 py-1 text-xs rounded bg-bg/50 text-muted border border-border'
                      }
                    >
                      {addr.whitelisted ? t('addressBook.whitelisted') : t('addressBook.notWhitelisted')}
                    </button>
                  </td>
                  <td className="p-2 text-xs text-muted">
                    {addr.last_used_at ? new Date(addr.last_used_at).toLocaleDateString() : t('addressBook.never')}
                  </td>
                  <td className="p-2 text-right">
                    <button
                      onClick={() => handleDelete(addr.id, addr.label || addr.address)}
                      className="text-xs text-red-400 hover:text-red-300"
                    >
                      {t('common.delete')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}