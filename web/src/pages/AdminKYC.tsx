import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { AdminLayout } from '../components/AdminLayout'
import { useToast } from '../components/Toast'

interface KycWithUser extends api.KycSubmission {
  email?: string
}

type FilterStatus = 'PENDING' | 'APPROVED' | 'REJECTED' | 'ALL'

export function AdminKYC() {
  const { t } = useTranslation()
  const toast = useToast()
  const [submissions, setSubmissions] = useState<KycWithUser[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<FilterStatus>('PENDING')
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<KycWithUser | null>(null)
  const [note, setNote] = useState('')
  const [reason, setReason] = useState('')
  const [actionLoading, setActionLoading] = useState(false)

  async function load() {
    setLoading(true)
    try {
      const data = await api.adminListKYC('')
      setSubmissions(data.map(s => ({ ...s })))
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to load', 'error')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [])

  const filtered = submissions
    .filter(s => filter === 'ALL' || s.Status === filter)
    .filter(s => {
      if (!search) return true
      const q = search.toLowerCase()
      return (
        s.FullName.toLowerCase().includes(q) ||
        s.IdNumber.toLowerCase().includes(q) ||
        s.Country.toLowerCase().includes(q) ||
        (s.email || '').toLowerCase().includes(q)
      )
    })

  const pendingCount = submissions.filter(s => s.Status === 'PENDING').length

  async function approve(id: string) {
    setActionLoading(true)
    try {
      await api.adminApproveKYC(id, note)
      toast.showToast(t('admin.kycApproved', 'KYC approved'), 'success')
      setNote('')
      setSelected(null)
      await load()
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to approve', 'error')
    } finally {
      setActionLoading(false)
    }
  }

  async function reject(id: string) {
    if (!reason.trim()) {
      toast.showToast(t('admin.rejectReasonRequired', 'Reason required'), 'warning')
      return
    }
    setActionLoading(true)
    try {
      await api.adminRejectKYC(id, reason)
      toast.showToast(t('admin.kycRejected', 'KYC rejected'), 'success')
      setReason('')
      setSelected(null)
      await load()
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to reject', 'error')
    } finally {
      setActionLoading(false)
    }
  }

  function statusBadge(status: string) {
    const colorMap: Record<string, string> = {
      PENDING: 'bg-yellow-900 text-yellow-200',
      APPROVED: 'bg-green-900 text-green-200',
      REJECTED: 'bg-red-900 text-red-200',
    }
    return `px-2 py-1 rounded text-xs ${colorMap[status] || 'bg-gray-700'}`
  }

  return (
    <AdminLayout title={t('admin.kycTitle', 'KYC Reviews')}>
      <div className="space-y-4">
        <div className="flex items-center justify-between flex-wrap gap-2">
          <div>
            <h2 className="text-2xl font-bold">{t('admin.kycTitle', 'KYC Reviews')}</h2>
            <p className="text-sm text-gray-400">
              {t('admin.pendingKycCount', '{{count}} pending submissions', { count: pendingCount })}
            </p>
          </div>
          <button onClick={load} className="btn-secondary text-sm">
            {t('common.refresh', 'Refresh')}
          </button>
        </div>

        <div className="card">
          <div className="flex flex-col md:flex-row gap-3">
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t('admin.kycSearchPlaceholder', 'Search by name, ID, country, email...')}
              className="flex-1 bg-panel border border-border rounded px-3 py-2 text-sm text-text"
            />
            <div className="flex gap-1">
              {(['PENDING', 'APPROVED', 'REJECTED', 'ALL'] as const).map(f => (
                <button
                  key={f}
                  onClick={() => setFilter(f)}
                  className={`px-3 py-1 text-sm rounded ${
                    filter === f
                      ? 'bg-brand text-bg font-semibold'
                      : 'bg-panel border border-border text-text'
                  }`}
                >
                  {f} ({submissions.filter(s => f === 'ALL' || s.Status === f).length})
                </button>
              ))}
            </div>
          </div>
        </div>

        {loading ? (
          <div className="card text-center py-8 text-gray-400">
            {t('common.loadingEllipsis', 'Loading...')}
          </div>
        ) : filtered.length === 0 ? (
          <div className="card text-center py-12">
            <div className="text-4xl mb-3">{t('common.empty')}</div>
            <p className="text-gray-400">{t('admin.noPendingKyc', 'No submissions found')}</p>
          </div>
        ) : (
          <div className="space-y-3">
            {filtered.map((s) => (
              <div key={s.ID} className="card">
                <div className="flex items-start justify-between mb-3 flex-wrap gap-2">
                  <div>
                    <div className="font-semibold text-lg">{s.FullName}</div>
                    <div className="text-xs text-gray-500 mt-1">
                      {t('admin.idLabel', 'ID')}: <span className="font-mono">{s.ID.slice(0, 8)}...</span>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className={statusBadge(s.Status)}>{s.Status}</span>
                    <span className="text-sm font-mono">L{s.TargetLevel}</span>
                  </div>
                </div>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-sm">
                  <div>
                    <span className="text-gray-500">{t('admin.country', 'Country')}: </span>
                    <span className="font-medium">{s.Country}</span>
                  </div>
                  <div>
                    <span className="text-gray-500">{t('admin.idNumber', 'ID Number')}: </span>
                    <span className="font-mono">{s.IdNumber}</span>
                  </div>
                  <div>
                    <span className="text-gray-500">{t('admin.submitted', 'Submitted')}: </span>
                    <span>{new Date(s.SubmittedAt).toLocaleString()}</span>
                  </div>
                  {s.ReviewedAt && (
                    <div>
                      <span className="text-gray-500">{t('admin.reviewed', 'Reviewed')}: </span>
                      <span>{new Date(s.ReviewedAt).toLocaleString()}</span>
                    </div>
                  )}
                </div>

                {s.ReviewerNote && (
                  <div className="mt-2 p-2 bg-bg rounded text-sm">
                    <span className="text-gray-500">{t('admin.reviewerNote', 'Note')}: </span>
                    {s.ReviewerNote}
                  </div>
                )}

                {s.Status === 'PENDING' && (
                  <div className="flex gap-2 mt-3 flex-wrap">
                    <a
                      href={`/api/v1/admin/kyc/${s.ID}/doc_front`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-xs px-2 py-1 bg-blue-900 text-blue-200 rounded hover:bg-blue-800"
                    >
                      {t('admin.viewDocFront', 'ID Front')}
                    </a>
                    <a
                      href={`/api/v1/admin/kyc/${s.ID}/doc_back`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-xs px-2 py-1 bg-blue-900 text-blue-200 rounded hover:bg-blue-800"
                    >
                      {t('admin.viewDocBack', 'ID Back')}
                    </a>
                    <a
                      href={`/api/v1/admin/kyc/${s.ID}/selfie`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-xs px-2 py-1 bg-blue-900 text-blue-200 rounded hover:bg-blue-800"
                    >
                      {t('admin.viewSelfie', 'Selfie')}
                    </a>
                    <div className="flex-1" />
                    <button
                      onClick={() => { setSelected(s); setNote(''); setReason(''); }}
                      className="text-xs px-3 py-1 bg-gray-700 text-white rounded hover:bg-gray-600"
                    >
                      {t('admin.review', 'Review')}
                    </button>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}

        {selected && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
            <div className="bg-panel border border-border rounded-lg shadow-xl max-w-lg w-full p-6">
              <h3 className="text-xl font-bold mb-4">
                {t('admin.reviewKyc', 'Review KYC')}
              </h3>
              <div className="space-y-2 mb-4 text-sm">
                <div><span className="text-gray-500">{t('admin.fullName')}:</span> {selected.FullName}</div>
                <div><span className="text-gray-500">{t('admin.country')}:</span> {selected.Country}</div>
                <div><span className="text-gray-500">{t('admin.idNumber')}:</span> {selected.IdNumber}</div>
              </div>

              <div className="mb-4">
                <label className="text-sm text-gray-500">{t('admin.approveNote', 'Approval note (optional)')}</label>
                <input
                  type="text"
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  placeholder={t('admin.notePlaceholder', 'Optional note...')}
                  className="w-full bg-bg border border-border rounded px-3 py-2 mt-1 text-text"
                />
              </div>

              <div className="mb-4">
                <label className="text-sm text-gray-500">{t('admin.rejectReason', 'Rejection reason (required if rejecting)')}</label>
                <input
                  type="text"
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  placeholder={t('admin.rejectReasonPlaceholder', 'e.g. ID image unclear')}
                  className="w-full bg-bg border border-border rounded px-3 py-2 mt-1 text-text"
                />
              </div>

              <div className="flex gap-2 justify-end">
                <button
                  onClick={() => setSelected(null)}
                  className="px-4 py-2 bg-gray-700 text-white rounded text-sm"
                >
                  {t('common.cancel', 'Cancel')}
                </button>
                <button
                  onClick={() => reject(selected.ID)}
                  disabled={actionLoading}
                  className="px-4 py-2 bg-red-600 text-white rounded text-sm disabled:opacity-50"
                >
                  {t('admin.reject', 'Reject')}
                </button>
                <button
                  onClick={() => approve(selected.ID)}
                  disabled={actionLoading}
                  className="px-4 py-2 bg-green-600 text-white rounded text-sm disabled:opacity-50"
                >
                  {t('admin.approve', 'Approve')}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </AdminLayout>
  )
}