import { useEffect, useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'
import { AdminLayout } from '../components/AdminLayout'
import { useAuth } from '../lib/auth'

interface UserFilters {
  search: string
  role: string
  kycLevel: number  // 0 = all
  kycStatus: string  // '' = all
}

const PAGE_SIZE = 25

export function AdminUsers() {
  const { t } = useTranslation()
  const { user: currentUser } = useAuth()
  const [users, setUsers] = useState<api.PublicUser[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState<UserFilters>({
    search: '',
    role: '',
    kycLevel: 0,
    kycStatus: '',
  })
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [success, setSuccess] = useState('')
  const [actionUser, setActionUser] = useState<api.PublicUser | null>(null)

  // Debounce search input
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(filters.search), 300)
    return () => clearTimeout(t)
  }, [filters.search])

  // Load users when filters change
  useEffect(() => {
    load()
  }, [debouncedSearch, filters.role, filters.kycLevel, filters.kycStatus, page])

  async function load() {
    setLoading(true)
    setErr('')
    try {
      const res = await api.adminListUsers({
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
        search: debouncedSearch || undefined,
        role: filters.role || undefined,
        kyc_level: filters.kycLevel || undefined,
        kyc_status: filters.kycStatus || undefined,
      })
      setUsers(res.users)
      setTotal(res.total)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  const totalPages = Math.ceil(total / PAGE_SIZE)

  async function changeRole(u: api.PublicUser, newRole: string) {
    if (!confirm(t('admin.roleChangeConfirm', { email: u.email, role: newRole }))) return
    try {
      await api.adminSetUserRole(u.id, newRole)
      setSuccess(t('admin.roleUpdated', { email: u.email }))
      load()
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function resetPassword(u: api.PublicUser) {
    const newPwd = prompt(t('admin.newPasswordPrompt', { email: u.email }))
    if (!newPwd) return
    if (newPwd.length < 8) {
      setErr(t('admin.pwTooShort'))
      return
    }
    try {
      await api.adminSetUserPassword(u.id, newPwd)
      setSuccess(t('admin.pwResetFor', { email: u.email }))
    } catch (e: any) {
      setErr(e.message)
    }
  }

  function kycBadge(level: number) {
    const map: Record<number, { label: string; color: string }> = {
      0: { label: 'L0', color: 'bg-gray-700 text-gray-300' },
      1: { label: 'L1', color: 'bg-blue-700 text-blue-200' },
      2: { label: 'L2', color: 'bg-green-700 text-green-200' },
    }
    const b = map[level] || map[0]
    return <span className={`px-2 py-0.5 rounded text-xs font-medium ${b.color}`}>{b.label}</span>
  }

  function roleBadge(role: string) {
    const isAdmin = role === 'admin'
    return (
      <span className={`px-2 py-0.5 rounded text-xs ${
        isAdmin ? 'bg-purple-700 text-purple-200' : 'bg-gray-700 text-gray-300'
      }`}>
        {role}
      </span>
    )
  }

  function formatDate(iso: string) {
    return new Date(iso).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    })
  }

  return (
    <AdminLayout>
      <div className="space-y-4">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-2xl font-bold">{t('admin.userManagement')}</h1>
          <p className="text-sm text-gray-400">
            {t('admin.totalUsers', { count: total })} · {t('admin.pageOf', { page, total: totalPages || 1 })}
          </p>
        </div>
      </div>

      {err && <div className="bg-red-900 text-red-200 p-3 rounded text-sm">{err}</div>}
      {success && <div className="bg-green-900 text-green-200 p-3 rounded text-sm">{success}</div>}

      {/* Filters */}
      <div className="card space-y-3">
        <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
          {/* Search */}
          <div className="md:col-span-2">
            <label className="block text-xs text-gray-400 mb-1">{t('admin.search')}</label>
            <input
              type="text"
              value={filters.search}
              onChange={e => {
                setFilters(f => ({ ...f, search: e.target.value }))
                setPage(1)
              }}
              placeholder="user@example.com or UUID"
              className="w-full bg-gray-700 px-3 py-2 rounded text-white text-sm"
            />
          </div>

          {/* Role filter */}
          <div>
            <label className="block text-xs text-gray-400 mb-1">{t('admin.filterRole')}</label>
            <select
              value={filters.role}
              onChange={e => {
                setFilters(f => ({ ...f, role: e.target.value }))
                setPage(1)
              }}
              className="w-full bg-gray-700 px-3 py-2 rounded text-white text-sm"
            >
              <option value="">All</option>
              <option value="user">User</option>
              <option value="admin">Admin</option>
            </select>
          </div>

          {/* KYC filter */}
          <div>
            <label className="block text-xs text-gray-400 mb-1">{t('admin.filterKyc')}</label>
            <select
              value={filters.kycLevel}
              onChange={e => {
                setFilters(f => ({ ...f, kycLevel: Number(e.target.value) }))
                setPage(1)
              }}
              className="w-full bg-gray-700 px-3 py-2 rounded text-white text-sm"
            >
              <option value={0}>All</option>
              <option value={1}>{t('admin.l1Basic')}</option>
              <option value={2}>{t('admin.l2Full')}</option>
            </select>
          </div>
        </div>

        {/* Active filter badges */}
        {(filters.search || filters.role || filters.kycLevel || filters.kycStatus) && (
          <div className="flex flex-wrap gap-2 items-center text-xs">
            <span className="text-gray-400">{t('admin.activeFilters')}</span>
            {filters.search && (
              <span className="bg-blue-900 text-blue-200 px-2 py-1 rounded">
                {t('admin.searchLabel')}: {filters.search}
              </span>
            )}
            {filters.role && (
              <span className="bg-blue-900 text-blue-200 px-2 py-1 rounded">
                {t('admin.roleLabel')}: {filters.role}
              </span>
            )}
            {filters.kycLevel > 0 && (
              <span className="bg-blue-900 text-blue-200 px-2 py-1 rounded">
                <span>{t('admin.kycLabel')} L{filters.kycLevel}</span>
              </span>
            )}
            <button
              onClick={() => {
                setFilters({ search: '', role: '', kycLevel: 0, kycStatus: '' })
                setPage(1)
              }}
              className="text-blue-400 hover:underline"
            >
              {t('admin.clearAll')}
            </button>
          </div>
        )}
      </div>

      {/* Table */}
      <div className="card overflow-x-auto">
        <table className="w-full text-left">
          <thead className="text-muted text-sm border-b border-gray-700">
            <tr>
              <th className="py-2 px-3">{t('admin.email')}</th>
              <th className="py-2 px-3">{t('admin.role')}</th>
              <th className="py-2 px-3">{t('admin.kyc')}</th>
              <th className="py-2 px-3">{t('admin.limit')}</th>
              <th className="py-2 px-3">{t('admin.joined')}</th>
              <th className="py-2 px-3"></th>
            </tr>
          </thead>
          <tbody>
            {loading && users.length === 0 ? (
              <tr>
                <td colSpan={6} className="p-8 text-center text-gray-500">
                  {t('common.loadingEllipsis')}
                </td>
              </tr>
            ) : users.length === 0 ? (
              <tr>
                <td colSpan={6} className="p-8 text-center text-gray-500">
                  {t('admin.noUsers')}
                </td>
              </tr>
            ) : (
              users.map(u => {
                const isMe = currentUser && currentUser.id === u.id
                return (
                  <tr key={u.id} className="border-t border-gray-700 hover:bg-gray-800">
                    <td className="py-3 px-3">
                      <div className="font-medium">{u.email}</div>
                      <div className="text-xs text-gray-500 font-mono">{u.id.slice(0, 8)}...</div>
                    </td>
                    <td className="py-3 px-3">{roleBadge(u.role || 'user')}</td>
                    <td className="py-3 px-3">{kycBadge(u.kyc_level)}</td>
                    <td className="py-3 px-3 text-sm">
                      ${u.kyc_limit_usdt || '0'}/day
                    </td>
                    <td className="py-3 px-3 text-sm text-gray-400">
                      {formatDate(u.created_at)}
                    </td>
                    <td className="py-3 px-3 text-right">
                      {!isMe ? (
                        <div className="flex gap-1 justify-end">
                          <button
                            onClick={() => changeRole(u, u.role === 'admin' ? 'user' : 'admin')}
                            className="text-xs bg-gray-700 hover:bg-gray-600 text-gray-300 px-2 py-1 rounded"
                          >
                            {u.role === 'admin' ? t('admin.demote') : t('admin.makeAdmin')}
                          </button>
                          <button
                            onClick={() => resetPassword(u)}
                            className="text-xs bg-yellow-700 hover:bg-yellow-600 text-yellow-200 px-2 py-1 rounded"
                          >
                            {t('admin.resetPW')}
                          </button>
                        </div>
                      ) : (
                        <span className="text-xs text-gray-500">{t('admin.you')}</span>
                      )}
                    </td>
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex justify-between items-center">
          <div className="text-sm text-gray-400">
            {t('admin.showing', { count: users.length, total })}
          </div>
          <div className="flex gap-2">
            <button
              onClick={() => setPage(p => Math.max(1, p - 1))}
              disabled={page === 1}
              className="bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 disabled:text-gray-600 px-3 py-1 rounded"
            >
              {t('admin.prev')}
            </button>
            <span className="px-3 py-1 text-gray-400">
              {page} / {totalPages}
            </span>
            <button
              onClick={() => setPage(p => Math.min(totalPages, p + 1))}
              disabled={page >= totalPages}
              className="bg-gray-700 hover:bg-gray-600 disabled:bg-gray-800 disabled:text-gray-600 px-3 py-1 rounded"
            >
              {t('admin.next')}
            </button>
          </div>
        </div>
      )}

      {/* Bulk actions info */}
      <div className="card bg-gray-800 text-sm text-gray-400">
        <div className="font-semibold text-gray-300 mb-2">{t('admin.actions')}</div>
        <ul className="space-y-1 ml-4 list-disc">
          <li>{t('admin.actionsMakeDemote')}</li>
          <li>{t('admin.actionsResetPw')}</li>
          <li>{t('admin.actionsSearch')}</li>
        </ul>
      </div>
      </div>
    </AdminLayout>
  )
}
