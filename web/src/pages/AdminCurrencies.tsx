import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { AdminLayout } from '../components/AdminLayout'
import { useToast } from '../components/Toast'

export function AdminCurrencies() {
  const { t } = useTranslation()
  const toast = useToast()
  const [currencies, setCurrencies] = useState<api.Currency[]>([])
  const [loading, setLoading] = useState(true)
  const [editing, setEditing] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<Partial<api.Currency>>({})
  const [saving, setSaving] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const data = await api.adminListCurrencies()
      setCurrencies(data)
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to load', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  function startEdit(c: api.Currency) {
    setEditing(c.symbol)
    setEditForm({
      min_withdraw: c.min_withdraw,
      max_withdraw: c.max_withdraw,
      withdraw_fee_flat: c.withdraw_fee_flat,
      withdraw_fee_percent: c.withdraw_fee_percent,
      withdraw_fee_min: c.withdraw_fee_min,
      is_active: c.is_active,
    })
  }

  function cancelEdit() {
    setEditing(null)
    setEditForm({})
  }

  async function save(symbol: string) {
    setSaving(true)
    try {
      // Convert percentage from display (e.g. 0.001) - keep as-is (decimal)
      await api.adminUpdateCurrency(symbol, editForm)
      toast.showToast(
        t('admin.currencySaved', `Saved ${symbol}`),
        'success'
      )
      setEditing(null)
      setEditForm({})
      await load()
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to save', 'error')
    } finally {
      setSaving(false)
    }
  }

  if (loading && currencies.length === 0) {
    return (
      <AdminLayout>
        <div className="p-8 text-center">{t('common.loading')}</div>
      </AdminLayout>
    )
  }

  return (
    <AdminLayout>
      <div>
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-2xl font-bold">
              {t('admin.currencyManagement', 'Currency Management')}
            </h1>
            <p className="text-sm text-gray-400 mt-1">
              {t('admin.currencyManagementDesc', 'Configure withdraw fees and limits per asset')}
            </p>
          </div>
          <button
            onClick={load}
            className="px-3 py-1 rounded text-sm bg-panel text-muted border border-border hover:text-white"
          >
            {t('common.refresh', 'Refresh')}
          </button>
        </div>

        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border text-left">
                <th className="p-2">{t('admin.currency', 'Currency')}</th>
                <th className="p-2">{t('admin.active', 'Active')}</th>
                <th className="p-2 text-right">{t('admin.minWithdraw', 'Min Withdraw')}</th>
                <th className="p-2 text-right">{t('admin.maxWithdraw', 'Max Withdraw')}</th>
                <th className="p-2 text-right">{t('admin.feeFlat', 'Fee (flat)')}</th>
                <th className="p-2 text-right">{t('admin.feePercent', 'Fee (%)')}</th>
                <th className="p-2 text-right">{t('admin.feeMin', 'Fee Min')}</th>
                <th className="p-2 text-right">{t('admin.actions', 'Actions')}</th>
              </tr>
            </thead>
            <tbody>
              {currencies.map((c) => {
                const isEditing = editing === c.symbol
                return (
                  <tr key={c.symbol} className="border-b border-border/50 hover:bg-bg/30">
                    <td className="p-2">
                      <div className="font-semibold">{c.symbol}</div>
                      <div className="text-xs text-gray-400">{c.name}</div>
                    </td>
                    <td className="p-2">
                      {isEditing ? (
                        <input
                          type="checkbox"
                          checked={editForm.is_active || false}
                          onChange={(e) => setEditForm({ ...editForm, is_active: e.target.checked })}
                          className="w-4 h-4"
                        />
                      ) : (
                        <span className={c.is_active ? 'text-green-400' : 'text-red-400'}>
                          {c.is_active ? '✓' : '✗'}
                        </span>
                      )}
                    </td>
                    {isEditing ? (
                      <>
                        <td className="p-1">
                          <input
                            type="text"
                            value={editForm.min_withdraw || ''}
                            onChange={(e) => setEditForm({ ...editForm, min_withdraw: e.target.value })}
                            className="w-full px-2 py-1 bg-gray-700 rounded text-xs font-mono"
                          />
                        </td>
                        <td className="p-1">
                          <input
                            type="text"
                            value={editForm.max_withdraw || ''}
                            onChange={(e) => setEditForm({ ...editForm, max_withdraw: e.target.value })}
                            className="w-full px-2 py-1 bg-gray-700 rounded text-xs font-mono"
                          />
                        </td>
                        <td className="p-1">
                          <input
                            type="text"
                            value={editForm.withdraw_fee_flat || ''}
                            onChange={(e) => setEditForm({ ...editForm, withdraw_fee_flat: e.target.value })}
                            className="w-full px-2 py-1 bg-gray-700 rounded text-xs font-mono"
                          />
                        </td>
                        <td className="p-1">
                          <input
                            type="text"
                            value={editForm.withdraw_fee_percent || ''}
                            onChange={(e) => setEditForm({ ...editForm, withdraw_fee_percent: e.target.value })}
                            className="w-full px-2 py-1 bg-gray-700 rounded text-xs font-mono"
                            placeholder="0.001 = 0.1%"
                          />
                        </td>
                        <td className="p-1">
                          <input
                            type="text"
                            value={editForm.withdraw_fee_min || ''}
                            onChange={(e) => setEditForm({ ...editForm, withdraw_fee_min: e.target.value })}
                            className="w-full px-2 py-1 bg-gray-700 rounded text-xs font-mono"
                          />
                        </td>
                        <td className="p-2 text-right whitespace-nowrap">
                          <button
                            onClick={() => save(c.symbol)}
                            disabled={saving}
                            className="text-xs px-2 py-1 bg-green-700 text-white rounded hover:bg-green-600 disabled:opacity-50 mr-1"
                          >
                            {saving ? t('common.loading') : t('admin.save', 'Save')}
                          </button>
                          <button
                            onClick={cancelEdit}
                            disabled={saving}
                            className="text-xs px-2 py-1 bg-gray-700 text-white rounded hover:bg-gray-600 disabled:opacity-50"
                          >
                            {t('admin.cancel', 'Cancel')}
                          </button>
                        </td>
                      </>
                    ) : (
                      <>
                        <td className="p-2 text-right font-mono text-xs">{c.min_withdraw}</td>
                        <td className="p-2 text-right font-mono text-xs">{c.max_withdraw}</td>
                        <td className="p-2 text-right font-mono text-xs">{c.withdraw_fee_flat}</td>
                        <td className="p-2 text-right font-mono text-xs">{(parseFloat(c.withdraw_fee_percent) * 100).toFixed(3)}%</td>
                        <td className="p-2 text-right font-mono text-xs">{c.withdraw_fee_min}</td>
                        <td className="p-2 text-right whitespace-nowrap">
                          <button
                            onClick={() => startEdit(c)}
                            className="text-xs px-2 py-1 bg-blue-700 text-white rounded hover:bg-blue-600"
                          >
                            {t('admin.edit', 'Edit')}
                          </button>
                        </td>
                      </>
                    )}
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>

        <div className="mt-4 text-xs text-gray-500">
          {t('admin.feeFormula', 'Fee formula: max(flat, amount × percent), then max(fee, min)')}
        </div>
      </div>
    </AdminLayout>
  )
}