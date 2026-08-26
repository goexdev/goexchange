import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { UserLayout, type UserTab } from '../components/UserLayout'
import { Wallet as WalletPage } from './Wallet'
import { Orders as OrdersPage } from './Orders'
import { Withdraw as WithdrawPage } from './Withdraw'
import { Notifications as NotificationsPage } from './Notifications'
import { KYC as KYCPage } from './KYC'
import { APIKeys as APIKeysPage } from './APIKeys'
import { Settings as SettingsPage } from './Settings'
import * as api from '../lib/api'
import { useAuth } from '../lib/auth'
import { Link } from 'react-router-dom'
import { AddressBook } from './AddressBook'
import { PnLReport } from './PnLReport'

const VALID_TABS: UserTab[] = [
  'overview',
  'wallets',
  'orders',
  'withdraw',
  'deposit',
  'addresses',
  'pnl',
  'notifications',
  'kyc',
  'api-keys',
  'settings',
]

function parseTab(s: string | null): UserTab {
  if (s && (VALID_TABS as string[]).includes(s)) return s as UserTab
  return 'overview'
}

function UserOverview() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [wallets, setWallets] = useState<api.Balance[]>([])
  const [deposits, setDeposits] = useState<api.Deposit[]>([])
  const [withdrawals, setWithdrawals] = useState<api.Withdrawal[]>([])
  const [openOrders, setOpenOrders] = useState<api.Order[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!user) return
    let cancelled = false
    async function load() {
      try {
        const [w, d, wd, o] = await Promise.all([
          api.getWallets().catch(() => []),
          api.getDeposits().catch(() => []),
          api.listWithdrawals().catch(() => []),
          api.listOrders().catch(() => []),
        ])
        if (cancelled) return
        setWallets(w)
        setDeposits(d)
        setWithdrawals(wd)
        setOpenOrders(o)
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => {
      cancelled = true
    }
  }, [user])

  if (loading) {
    return <div className="text-muted text-sm">{t('common.loadingEllipsis')}</div>
  }

  // Compute totals
  const totalBalance = wallets.reduce((sum, w) => {
    const bal = parseFloat(w.available || '0') + parseFloat(w.frozen || '0')
    return sum + bal
  }, 0)
  const totalDeposited = deposits.reduce(
    (s, d) => s + parseFloat(d.amount || '0'),
    0
  )
  const totalWithdrawn = withdrawals
    .filter((w) => w.Status === 'BROADCAST' || w.Status === 'CONFIRMED')
    .reduce((s, w) => s + parseFloat(w.Amount || '0'), 0)

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-xl font-bold text-text">{t('user.overview.title')}</h2>
        <p className="text-muted text-sm">{t('user.overview.subtitle')}</p>
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="bg-panel border border-border rounded-lg p-4">
          <div className="text-xs text-muted uppercase">{t('user.overview.portfolio')}</div>
          <div className="text-2xl font-bold text-text mt-1">
            {totalBalance.toFixed(4)}
          </div>
          <div className="text-xs text-muted mt-1">
            {wallets.length} {wallets.length === 1 ? 'asset' : 'assets'}
          </div>
        </div>
        <div className="bg-panel border border-border rounded-lg p-4">
          <div className="text-xs text-muted uppercase">{t('user.overview.deposit')}</div>
          <div className="text-2xl font-bold text-green-400 mt-1">
            +{totalDeposited.toFixed(4)}
          </div>
          <div className="text-xs text-muted mt-1">{deposits.length} txs</div>
        </div>
        <div className="bg-panel border border-border rounded-lg p-4">
          <div className="text-xs text-muted uppercase">{t('user.overview.withdraw')}</div>
          <div className="text-2xl font-bold text-red-400 mt-1">
            -{totalWithdrawn.toFixed(4)}
          </div>
          <div className="text-xs text-muted mt-1">{withdrawals.length} txs</div>
        </div>
        <div className="bg-panel border border-border rounded-lg p-4">
          <div className="text-xs text-muted uppercase">{t('user.overview.openOrders')}</div>
          <div className="text-2xl font-bold text-text mt-1">{openOrders.length}</div>
          <Link to="/user?tab=orders" className="text-xs text-brand mt-1 inline-block">
            {t('user.overview.trades')}
          </Link>
        </div>
      </div>

      {/* Quick links */}
      <div className="bg-panel border border-border rounded-lg p-4">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-2 text-sm">
          <Link to="/user?tab=wallets" className="btn-secondary text-center">
            {t('user.tab.wallets')}
          </Link>
          <Link to="/user?tab=deposit" className="btn-secondary text-center">
            {t('user.tab.deposit')}
          </Link>
          <Link to="/user?tab=withdraw" className="btn-secondary text-center">
            {t('user.tab.withdraw')}
          </Link>
          <Link to="/user?tab=orders" className="btn-secondary text-center">
            {t('user.tab.orders')}
          </Link>
        </div>
      </div>
    </div>
  )
}

function UserDeposit() {
  // Reuse the deposit section from Wallet.tsx
  // For now, delegate to WalletPage which has deposit functionality
  // (the page internally handles state)
  return <WalletPage />
}

export function User() {
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = parseTab(searchParams.get('tab'))

  function setTab(newTab: UserTab) {
    setSearchParams({ tab: newTab }, { replace: true })
  }

  // Make the tab available globally so the existing pages can navigate
  useEffect(() => {
    (window as any).__userTab = tab;
    (window as any).__setUserTab = setTab;
    return () => {
      delete (window as any).__userTab;
      delete (window as any).__setUserTab;
    }
  }, [tab])

  function renderTab() {
    switch (tab) {
      case 'overview':
        return <UserOverview />
      case 'wallets':
        return <WalletPage />
      case 'orders':
        return <OrdersPage />
      case 'withdraw':
        return <WithdrawPage />
      case 'deposit':
        return <UserDeposit />
      case 'addresses':
        return <AddressBook />
      case 'pnl':
        return <PnLReport />
      case 'notifications':
        return <NotificationsPage />
      case 'kyc':
        return <KYCPage />
      case 'api-keys':
        return <APIKeysPage />
      case 'settings':
        return <SettingsPage />
      default:
        return <UserOverview />
    }
  }

  return <UserLayout activeTab={tab}>{renderTab()}</UserLayout>
}

export default User