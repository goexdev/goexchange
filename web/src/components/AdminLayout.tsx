import { Link, useLocation } from 'react-router-dom'

const TABS = [
  { path: '/admin', label: 'Dashboard', exact: true },
  { path: '/admin/users', label: 'Users' },
  { path: '/admin/kyc', label: 'KYC Reviews' },
  { path: '/admin/currencies', label: 'Currencies' },
  { path: '/admin/fee-stats', label: 'Fee Stats' },
  { path: '/admin/chains', label: 'Chains' },
  { path: '/admin/audit', label: 'Audit Log' },
  { path: '/admin/pairs', label: 'Markets' },
]

export function AdminLayout({ children, title }: { children: React.ReactNode; title?: string }) {
  const location = useLocation()

  function isActive(tabPath: string, exact = false) {
    if (exact) return location.pathname === tabPath
    return location.pathname === tabPath || location.pathname.startsWith(tabPath + '/')
  }

  return (
    <div className="space-y-4">
      {/* Sub-nav tabs */}
      <div className="border-b border-gray-700">
        <nav className="flex gap-1 -mb-px overflow-x-auto">
          {TABS.map(tab => {
            const active = isActive(tab.path, tab.exact)
            return (
              <Link
                key={tab.path}
                to={tab.path}
                className={`px-4 py-2 text-sm whitespace-nowrap border-b-2 transition-colors ${
                  active
                    ? 'border-purple-500 text-purple-300 font-semibold'
                    : 'border-transparent text-gray-400 hover:text-white hover:border-gray-600'
                }`}
              >
                {tab.label}
              </Link>
            )
          })}
        </nav>
      </div>

      {/* Page content */}
      <div>{children}</div>
    </div>
  )
}
