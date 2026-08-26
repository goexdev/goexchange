import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'

interface AdminStats {
  total_users: number
  admin_users: number
  l0_users: number
  l1_users: number
  l2_users: number
  pending_kyc: number
  total_orders: number
  open_orders: number
  filled_orders: number
  canceled_orders: number
  total_deposits: number
  pending_deposits: number
  done_deposits: number
  total_deposit_amount: string
  total_withdrawals: number
  pending_withdrawals: number
  broadcast_withdrawals: number
  done_withdrawals: number
  failed_withdrawals: number
  total_withdrawal_amount: string
}

export function AdminDashboard() {
  const { t } = useTranslation()
  const [stats, setStats] = useState<AdminStats | null>(null)
  const [users, setUsers] = useState<api.PublicUser[]>([])
  const [kycPending, setKycPending] = useState<api.KycSubmission[]>([])
  const [withdrawals, setWithdrawals] = useState<any[]>([])
  const [err, setErr] = useState('')
  const [success, setSuccess] = useState('')
  const [tab, setTab] = useState<'overview' | 'users' | 'kyc' | 'withdrawals' | 'audit'>('overview')
  const [resetPwUser, setResetPwUser] = useState<{ id: string; email: string } | null>(null)
  const [newPassword, setNewPassword] = useState('')

  async function load() {
    setErr('')
    try {
      const [s, u, kp, w] = await Promise.all([
        api.adminStats(),
        api.adminListUsers(50),
        api.adminListPendingKYC(),
        api.adminListWithdrawals(50),
      ])
      setStats(s)
      setUsers(u.users || [])  // unwrap UsersResponse
      setKycPending(kp)
      setWithdrawals(w)
    } catch (e: any) {
      setErr(e.message)
    }
  }

  useEffect(() => {
    load()
    const intervalId = setInterval(load, 5000)
    return () => clearInterval(intervalId)
  }, [])

  async function approveKYC(id: string) {
    try {
      await api.adminApproveKYC(id, 'approved via dashboard')
      load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function rejectKYC(id: string) {
    try {
      await api.adminRejectKYC(id, 'rejected via dashboard')
      load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function setRole(id: string, role: string) {
    try {
      await api.adminSetUserRole(id, role)
      setErr('')
      setSuccess(t('admin.roleUpdatedTo', { role }))
      setTimeout(() => setSuccess(''), 5000)
      load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function resetPassword() {
    if (!resetPwUser || !newPassword) return
    if (newPassword.length < 8) {
      setErr(t('admin.pwTooShort'))
      return
    }
    try {
      await api.adminSetUserPassword(resetPwUser.id, newPassword)
      setErr('')
      setSuccess(t('admin.pwResetFor', { email: resetPwUser.email }))
      setResetPwUser(null)
      setNewPassword('')
      setTimeout(() => setSuccess(''), 5000)
      load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  if (err) {
    return (
      <div className="p-6">
        <div className="card">
          <h2 className="text-xl font-bold text-red-600">{t('admin.accessError')}</h2>
          <p className="text-sm text-gray-600 mt-2">{err}</p>
          <p className="text-sm text-gray-600 mt-2">
            {t('admin.loginAsAdmin')}
          </p>
        </div>
      </div>
    )
  }

  if (!stats) {
    return <div className="p-6">{t('admin.loadingDashboard')}</div>
  }

  return (
    <div className="p-6 max-w-7xl mx-auto">
      <h1 className="text-2xl font-bold mb-4">{t('admin.title')}</h1>

      {success && (
        <div className="bg-green-100 border border-green-400 text-green-700 px-4 py-2 rounded mb-4">
          {success}
        </div>
      )}

      {/* Tabs */}
      <div className="border-b mb-4 flex gap-4">
        {(['overview', 'users', 'kyc', 'withdrawals', 'audit'] as const).map((tabKey) => (
          <button
            key={tabKey}
            onClick={() => setTab(tabKey)}
            className={`px-4 py-2 ${tab === tabKey ? 'border-b-2 border-blue-600 font-semibold' : 'text-gray-600'}`}
          >
            {tabKey === 'overview' ? t('admin.overview') : tabKey === 'kyc' ? `${t('admin.kycTab')} (${stats.pending_kyc})` : tabKey === 'audit' ? t('admin.auditLogTab') : tabKey.charAt(0).toUpperCase() + tabKey.slice(1)}
          </button>
        ))}
      </div>

      {/* Overview */}
      {tab === 'overview' && (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <StatCard label="Total Users" value={stats.total_users} color="blue" />
          <StatCard label="Admins" value={stats.admin_users} color="purple" />
          <StatCard label="L0 Users" value={stats.l0_users} color="yellow" />
          <StatCard label="L1 Users" value={stats.l1_users} color="green" />
          <StatCard label="L2 Users" value={stats.l2_users} color="indigo" />
          <StatCard label="Pending KYC" value={stats.pending_kyc} color="red" />
          <StatCard label="Total Orders" value={stats.total_orders} />
          <StatCard label="Open Orders" value={stats.open_orders} />
          <StatCard label="Filled Orders" value={stats.filled_orders} />
          <StatCard label="Total Deposits" value={stats.total_deposits} />
          <StatCard label="Pending Deposits" value={stats.pending_deposits} />
          <StatCard label="Total Deposit Amount" value={stats.total_deposit_amount} />
          <StatCard label="Total Withdrawals" value={stats.total_withdrawals} />
          <StatCard label="Broadcast Withdrawals" value={stats.broadcast_withdrawals} />
          <StatCard label="Done Withdrawals" value={stats.done_withdrawals} />
          <StatCard label="Failed Withdrawals" value={stats.failed_withdrawals} />
          <StatCard label="Total Withdrawal Amount" value={stats.total_withdrawal_amount} />
        </div>
      )}

      {/* Users */}
      {tab === 'users' && (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b">
                <th className="text-left p-2">Email</th>
                <th className="text-left p-2">KYC</th>
                <th className="text-left p-2">Status</th>
                <th className="text-left p-2">Limit</th>
                <th className="text-left p-2">Role</th>
                <th className="text-left p-2">Actions</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id} className="border-b hover:bg-gray-50">
                  <td className="p-2">{u.email}</td>
                  <td className="p-2">L{u.kyc_level}</td>
                  <td className="p-2">{u.kyc_status}</td>
                  <td className="p-2">{u.kyc_limit_usdt}</td>
                  <td className="p-2">
                    <span className={`px-2 py-1 rounded text-xs ${u.role === 'admin' ? 'bg-purple-100 text-purple-800' : 'bg-gray-100'}`}>
                      {u.role}
                    </span>
                  </td>
                  <td className="p-2">
                    {u.role === 'admin' ? (
                      <button
                        onClick={() => setRole(u.id, 'user')}
                        className="text-xs px-2 py-1 bg-gray-600 text-white rounded"
                      >
                        Demote
                      </button>
                    ) : (
                      <button
                        onClick={() => setRole(u.id, 'admin')}
                        className="text-xs px-2 py-1 bg-purple-600 text-white rounded"
                      >
                        Promote
                      </button>
                    )}
                    <button
                      onClick={() => setResetPwUser({ id: u.id, email: u.email })}
                      className="text-xs px-2 py-1 bg-yellow-600 text-white rounded"
                    >
                      {t('admin.resetPW')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Password Reset Modal */}
      {resetPwUser && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white p-6 rounded-lg shadow-xl max-w-md w-full">
            <h3 className="text-lg font-bold mb-2 text-gray-800">{t('admin.resetPassword')}</h3>
            <p className="text-sm text-gray-600 mb-4">
              {t('admin.resetPasswordFor')} <span className="font-medium">{resetPwUser.email}</span>
            </p>
            <input
              type="password"
              placeholder="New password (min 8 chars)"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              className="w-full px-3 py-2 border rounded text-gray-800 mb-4"
              autoFocus
            />
            <div className="flex gap-2 justify-end">
              <button
                onClick={() => { setResetPwUser(null); setNewPassword('') }}
                className="px-4 py-2 bg-gray-300 text-gray-800 rounded"
              >
                Cancel
              </button>
              <button
                onClick={resetPassword}
                disabled={newPassword.length < 8}
                className="px-4 py-2 bg-blue-600 text-white rounded disabled:opacity-50"
              >
                {t('admin.resetPassword')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* KYC */}
      {tab === 'kyc' && (
        <div className="space-y-4">
          {kycPending.length === 0 && <p className="text-gray-500">{t('admin.noPendingKyc')}</p>}
          {kycPending.map((k) => (
            <div key={k.ID} className="card">
              <div className="grid grid-cols-2 gap-2 text-sm">
                <div><span className="font-medium">{t('admin.idLabel')}</span> {k.ID}</div>
                <div><span className="font-medium">{t('admin.targetLevel')}</span> L{k.TargetLevel}</div>
                <div><span className="font-medium">{t('admin.fullName')}</span> {k.FullName}</div>
                <div><span className="font-medium">{t('admin.country')}</span> {k.Country}</div>
                <div><span className="font-medium">{t('admin.idNumber')}</span> {k.IdNumber}</div>
                <div><span className="font-medium">{t('admin.submitted')}</span> {k.SubmittedAt}</div>
              </div>
              <div className="flex gap-2 mt-3">
                <button
                  onClick={() => approveKYC(k.ID)}
                  className="px-3 py-1 bg-green-600 text-white rounded text-sm"
                >
                  Approve
                </button>
                <button
                  onClick={() => rejectKYC(k.ID)}
                  className="px-3 py-1 bg-red-600 text-white rounded text-sm"
                >
                  Reject
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Withdrawals */}
      {tab === 'withdrawals' && (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b">
                <th className="text-left p-2">Created</th>
                <th className="text-left p-2">Asset</th>
                <th className="text-left p-2">Amount</th>
                <th className="text-left p-2">To</th>
                <th className="text-left p-2">Status</th>
                <th className="text-left p-2">Tx</th>
              </tr>
            </thead>
            <tbody>
              {withdrawals.map((w) => (
                <tr key={w.ID} className="border-b hover:bg-gray-50">
                  <td className="p-2">{w.created_at || w.CreatedAt}</td>
                  <td className="p-2">{w.Asset}</td>
                  <td className="p-2">{w.Amount}</td>
                  <td className="p-2 font-mono text-xs">{w.DestAddress}</td>
                  <td className="p-2">
                    <span className={`px-2 py-1 rounded text-xs ${w.Status === 'DONE' ? 'bg-green-100 text-green-800' : w.Status === 'BROADCAST' ? 'bg-blue-100 text-blue-800' : w.Status === 'FAILED' ? 'bg-red-100 text-red-800' : 'bg-yellow-100'}`}>
                      {w.Status}
                    </span>
                  </td>
                  <td className="p-2 font-mono text-xs">{w.TxHash || w.tx_hash || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

{/* Audit Log */}
      {tab === 'audit' && (
        <div className="card">
          <div className="text-center py-12">
            <div className="text-4xl mb-4">📋</div>
            <h3 className="text-xl font-semibold mb-2">{t('admin.adminAuditLog')}</h3>
            <p className="text-muted mb-6 max-w-md mx-auto">
              {t('admin.adminAuditLogDesc')}
            </p>
            <a
              href="/admin/audit"
              className="btn-primary inline-block"
            >
              {t('admin.openAuditLogViewer')}
            </a>
          </div>
        </div>
      )}    </div>
  )
}

function StatCard({ label, value, color }: { label: string; value: any; color?: string }) {
  const colorClass = color === 'red' ? 'border-red-500' : color === 'green' ? 'border-green-500' : color === 'blue' ? 'border-blue-500' : color === 'yellow' ? 'border-yellow-500' : color === 'purple' ? 'border-purple-500' : color === 'indigo' ? 'border-indigo-500' : 'border-gray-200'
  return (
    <div className={`card border-l-4 ${colorClass}`}>
      <div className="text-sm text-gray-600">{label}</div>
      <div className="text-xl font-bold mt-1">{value}</div>
    </div>
  )
}
