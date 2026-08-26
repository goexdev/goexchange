import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { useToast } from './Toast'

interface ActiveTriggersProps {
  refreshKey?: number
}

export function ActiveTriggers({ refreshKey }: ActiveTriggersProps) {
  const { t } = useTranslation()
  const toast = useToast()
  const [triggers, setTriggers] = useState<api.TriggerOrder[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<'all' | 'PENDING' | 'TRIGGERED'>('PENDING')

  useEffect(() => {
    load()
  }, [refreshKey])

  async function load() {
    setLoading(true)
    try {
      const result = await api.listTriggers()
      setTriggers(result.triggers)
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to load triggers', 'error')
    } finally {
      setLoading(false)
    }
  }

  async function handleCancel(id: string) {
    if (!confirm(t('triggers.cancelConfirm'))) return
    try {
      await api.cancelTrigger(id)
      toast.showToast(t('triggers.cancelled'), 'success')
      await load()
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to cancel', 'error')
    }
  }

  const filtered = filter === 'all' ? triggers : triggers.filter((t) => t.status === filter)

  if (loading) {
    return <div className="text-muted text-sm">{t("common.loading", "Loading triggers...")}</div>
  }

  if (filtered.length === 0) {
    return (
      <div className="text-muted text-sm py-4">
        {filter === 'PENDING'
          ? t('triggers.noPending', 'No pending triggers. Create one from the Trade page.')
          : t('triggers.none', 'No triggers found.')}
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <div className="flex gap-2 text-xs">
        {(['PENDING', 'TRIGGERED', 'all'] as const).map((f) => (
          <button
            key={f}
            onClick={() => setFilter(f)}
            className={
              filter === f
                ? 'px-3 py-1 rounded bg-brand text-bg font-semibold'
                : 'px-3 py-1 rounded bg-panel border border-border text-text'
            }
          >
            {f === 'all' ? t('common.all', 'All') : f} (
            {triggers.filter((x) => f === 'all' || x.status === f).length})
          </button>
        ))}
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left">
              <th className="p-2">Pair</th>
              <th className="p-2">Type</th>
              <th className="p-2">Side</th>
              <th className="p-2 text-right">Trigger</th>
              <th className="p-2 text-right">Quantity</th>
              <th className="p-2">Status</th>
              <th className="p-2">Created</th>
              <th className="p-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((tr) => (
              <tr key={tr.id} className="border-b border-border/50">
                <td className="p-2 font-semibold">{tr.pair}</td>
                <td className="p-2">
                  <span
                    className={
                      tr.trigger_type === 'STOP_LOSS'
                        ? 'text-red-400'
                        : 'text-green-400'
                    }
                  >
                    {tr.trigger_type === 'STOP_LOSS' ? 'v' : '^'} {tr.trigger_type.replace('_', ' ')}
                  </span>
                </td>
                <td className="p-2">{tr.side}</td>
                <td className="p-2 text-right font-mono">${tr.trigger_price}</td>
                <td className="p-2 text-right">{tr.quantity}</td>
                <td className="p-2">
                  <span
                    className={
                      tr.status === 'PENDING'
                        ? 'px-2 py-0.5 text-xs rounded bg-yellow-900 text-yellow-200'
                        : tr.status === 'TRIGGERED'
                        ? 'px-2 py-0.5 text-xs rounded bg-green-900 text-green-200'
                        : 'px-2 py-0.5 text-xs rounded bg-gray-700 text-gray-300'
                    }
                  >
                    {tr.status}
                  </span>
                </td>
                <td className="p-2 text-xs text-muted">
                  {new Date(tr.created_at).toLocaleString()}
                </td>
                <td className="p-2 text-right">
                  {tr.status === 'PENDING' && (
                    <button
                      onClick={() => handleCancel(tr.id)}
                      className="text-xs text-red-400 hover:text-red-300"
                    >
                      {t('common.cancel', 'Cancel')}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}