import { ReactNode, useState, useRef, useEffect } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'

export type UserTab =
  | 'overview'
  | 'wallets'
  | 'orders'
  | 'withdraw'
  | 'deposit'
  | 'addresses'
  | 'pnl'
  | 'notifications'
  | 'kyc'
  | 'api-keys'
  | 'settings'

interface SidebarItem {
  tab: UserTab
  icon: string
  labelKey: string
}

const ITEMS: SidebarItem[] = [
  { tab: 'overview', icon: '\u25C9', labelKey: 'user.tab.overview' },
  { tab: 'wallets', icon: '\u229E', labelKey: 'user.tab.wallets' },
  { tab: 'orders', icon: '\u229F', labelKey: 'user.tab.orders' },
  { tab: 'withdraw', icon: '\u2191', labelKey: 'user.tab.withdraw' },
  { tab: 'deposit', icon: '\u2193', labelKey: 'user.tab.deposit' },
  { tab: 'addresses', icon: '\u2709', labelKey: 'user.tab.addresses' },
  { tab: 'pnl', icon: '\u20B1', labelKey: 'user.tab.pnl' },
  { tab: 'notifications', icon: '\u25D4', labelKey: 'user.tab.notifications' },
  { tab: 'kyc', icon: '\u2726', labelKey: 'user.tab.kyc' },
  { tab: 'api-keys', icon: '\u26B7', labelKey: 'user.tab.apiKeys' },
  { tab: 'settings', icon: '\u2699', labelKey: 'user.tab.settings' },
]

export interface UserLayoutProps {
  activeTab: UserTab
  children: ReactNode
}

export function UserLayout({ activeTab, children }: UserLayoutProps) {
  const { t } = useTranslation()
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const tabsRef = useRef<HTMLDivElement>(null)

  function handleSignOut() {
    logout()
    navigate('/login')
  }

  // Auto-scroll active tab into view on mobile
  useEffect(() => {
    const el = tabsRef.current?.querySelector('[data-active="true"]') as HTMLElement | null
    if (el && typeof el.scrollIntoView === 'function') {
      el.scrollIntoView({ behavior: 'smooth', inline: 'center', block: 'nearest' })
    }
  }, [activeTab])

  return (
    <div className="flex flex-col lg:flex-row gap-6">
      {/* Mobile: horizontal scrollable tabs */}
      <div className="lg:hidden -mx-4 px-4 sticky top-[57px] z-30 bg-bg/95 backdrop-blur">
        <div
          ref={tabsRef}
          className="flex overflow-x-auto gap-1 py-2 scrollbar-thin"
          style={{ scrollbarWidth: 'thin' }}
        >
          {ITEMS.map((item) => (
            <NavLink
              key={item.tab}
              to={`/user?tab=${item.tab}`}
              data-active={activeTab === item.tab}
              className={({ isActive }) =>
                `flex-shrink-0 flex items-center gap-1 px-3 py-2 rounded text-sm whitespace-nowrap transition-colors ${
                  isActive || activeTab === item.tab
                    ? 'bg-brand text-bg font-semibold'
                    : 'bg-panel text-text hover:bg-bg/70 border border-border'
                }`
              }
            >
              <span>{item.icon}</span>
              <span>{t(item.labelKey)}</span>
            </NavLink>
          ))}
        </div>
      </div>

      {/* Desktop: vertical sidebar */}
      <aside className="hidden lg:block lg:w-60 lg:flex-shrink-0">
        <nav className="bg-panel border border-border rounded-lg overflow-hidden">
          <ul>
            {ITEMS.map((item) => (
              <li key={item.tab}>
                <NavLink
                  to={`/user?tab=${item.tab}`}
                  className={({ isActive }) =>
                    `flex items-center gap-3 px-4 py-2.5 text-sm border-b border-border last:border-b-0 transition-colors ${
                      isActive || activeTab === item.tab
                        ? 'bg-brand/10 text-brand font-semibold border-l-2 border-l-brand'
                        : 'text-text hover:bg-bg/50 border-l-2 border-l-transparent'
                    }`
                  }
                >
                  <span className="text-lg w-5 text-center">{item.icon}</span>
                  <span>{t(item.labelKey)}</span>
                </NavLink>
              </li>
            ))}
            <li className="border-t border-border">
              <button
                onClick={handleSignOut}
                className="w-full flex items-center gap-3 px-4 py-2.5 text-sm text-muted hover:text-text hover:bg-bg/50 transition-colors text-left"
              >
                <span className="text-lg w-5 text-center">&#9203;</span>
                <span>{t('common.logout')}</span>
              </button>
            </li>
          </ul>
          {user && (
            <div className="px-4 py-2 text-xs text-muted border-t border-border bg-bg/30">
              {user.email}
            </div>
          )}
        </nav>
      </aside>

      {/* Main content */}
      <div className="flex-1 min-w-0">{children}</div>
    </div>
  )
}