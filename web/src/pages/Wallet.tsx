import { FormEvent, useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import * as api from '../lib/api'
import { formatNumber } from '../lib/utils'

type Toast = { id: number; type: 'success' | 'error' | 'info'; message: string }

function ToastContainer({ toasts, onDismiss }: { toasts: Toast[]; onDismiss: (id: number) => void }) {
  return (
    <div className="fixed top-4 right-4 z-50 space-y-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`px-4 py-3 rounded shadow-lg min-w-[280px] max-w-md flex items-start gap-2 animate-slide-in ${
            t.type === 'success'
              ? 'bg-green-900 border border-green-700 text-green-100'
              : t.type === 'error'
              ? 'bg-red-900 border border-red-700 text-red-100'
              : 'bg-blue-900 border border-blue-700 text-blue-100'
          }`}
        >
          <span className="text-lg">
            {t.type === 'success' ? '✓' : t.type === 'error' ? '✕' : 'ℹ'}
          </span>
          <span className="flex-1 text-sm">{t.message}</span>
          <button
            onClick={() => onDismiss(t.id)}
            className="text-muted hover:text-text text-sm"
          >
            ✕
          </button>
        </div>
      ))}
    </div>
  )
}

function CopyButton({ text, label = 'Copy' }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch (e) {
      // fallback
    }
  }
  return (
    <button
      onClick={handleCopy}
      className="text-xs text-brand hover:underline"
      title="Copy to clipboard"
    >
      {copied ? '✓ Copied' : label}
    </button>
  )
}

function StatusBadge({ status }: { status: string }) {
  const colors: Record<string, string> = {
    CREDITED: 'bg-green-100 text-green-800',
    DONE: 'bg-green-100 text-green-800',
    APPROVED: 'bg-green-100 text-green-800',
    PENDING: 'bg-yellow-100 text-yellow-800',
    BROADCAST: 'bg-blue-100 text-blue-800',
    HOLD: 'bg-yellow-100 text-yellow-800',
    FAILED: 'bg-red-100 text-red-800',
    REJECTED: 'bg-red-100 text-red-800',
    NONE: 'bg-gray-100 text-gray-600',
  }
  const cls = colors[status] || 'bg-gray-100 text-gray-600'
  return (
    <span className={`inline-block px-2 py-0.5 text-xs font-medium rounded ${cls}`}>
      {status}
    </span>
  )
}

function formatAge(date: string): string {
  const ms = Date.now() - new Date(date).getTime()
  const sec = Math.floor(ms / 1000)
  if (sec < 60) return `${sec}s ago`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr}h ago`
  const day = Math.floor(hr / 24)
  return `${day}d ago`
}

function formatFull(date: string): string {
  return new Date(date).toLocaleString()
}

export function Wallet() {
  const { t } = useTranslation()
  const [wallets, setWallets] = useState<api.Balance[]>([])
  const [deposits, setDeposits] = useState<api.Deposit[]>([])
  const [withdrawals, setWithdrawals] = useState<api.Withdrawal[]>([])
  const [pendingTxs, setPendingTxs] = useState<api.PendingTx[]>([])
  const [kycLimit, setKycLimit] = useState<api.KycLimit | null>(null)

  const [spawnAmount, setSpawnAmount] = useState('100')
  const [spawnAsset, setSpawnAsset] = useState('USDT')
  const [btcAddr, setBtcAddr] = useState<string | null>(null)
  const [loadingAddr, setLoadingAddr] = useState(false)

  const [wdAsset, setWdAsset] = useState('BTC')
  const [wdAmount, setWdAmount] = useState('')
  const [wdAddress, setWdAddress] = useState('')
  const [wdSubmitting, setWdSubmitting] = useState(false)

  const [loading, setLoading] = useState(true)
  const [toasts, setToasts] = useState<Toast[]>([])
  const toastIdRef = useState({ current: 0 })[0]

  const addToast = useCallback((type: Toast['type'], message: string) => {
    const id = Date.now() + Math.random()
    setToasts((prev) => [...prev, { id, type, message }])
    setTimeout(() => {
      setToasts((prev) => prev.filter((t) => t.id !== id))
    }, 4000)
  }, [])

  const dismissToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  async function submitWithdraw(e: FormEvent) {
    e.preventDefault()
    setWdSubmitting(true)
    try {
      await api.createWithdrawal(wdAsset, wdAmount, wdAddress)
      addToast('success', t('wallet.withdrawalRequested', { amount: wdAmount, asset: wdAsset }))
      setWdAmount('')
      setWdAddress('')
      load()
    } catch (e: any) {
      addToast('error', t('wallet.withdrawFailed', { error: e.message }))
    } finally {
      setWdSubmitting(false)
    }
  }

  async function showBtcAddress() {
    setLoadingAddr(true)
    try {
      const res = await api.getDepositAddress('BTC')
      setBtcAddr(res.address)
    } catch (e: any) {
      addToast('error', t('wallet.actionFailed', { error: e.message }))
    } finally {
      setLoadingAddr(false)
    }
  }

  async function importFromChain() {
    try {
      addToast('info', t('wallet.importingFromChain'))
      const res = await api.importDepositsFromChain()
      addToast('success', t('wallet.importedCount', { count: res.imported_count }))
      load()
    } catch (e: any) {
      addToast('error', t('wallet.importFailed', { error: e.message }))
    }
  }

  async function load() {
    try {
      const [w, d, ptx, wd, kl] = await Promise.all([
        api.getWallets(),
        api.getDeposits(),
        api.listPendingTxs().catch(() => []),
        api.listWithdrawals().catch(() => []),
        api.getKycLimit().catch(() => null),
      ])
      setWallets(w)
      setDeposits(d)
      setPendingTxs(ptx)
      setWithdrawals(wd)
      setKycLimit(kl)
    } catch (e: any) {
      addToast('error', t('wallet.loadFailed', { error: e.message }))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function onSpawn(e: React.FormEvent) {
    e.preventDefault()
    try {
      const d = await api.spawnDeposit(spawnAsset, spawnAmount)
      addToast('success', t('wallet.mockDepositCredited', { amount: d.amount, asset: d.asset }))
      load()
    } catch (e: any) {
      addToast('error', e.message)
    }
  }

  // Compute overview stats
  const nonZeroBalances = wallets.filter((w) => parseFloat(w.available) > 0 || parseFloat(w.frozen) > 0)
  const pendingTotal = pendingTxs.reduce((acc, p) => acc + parseFloat(p.amount), 0)
  const totalDeposited = deposits.reduce((acc, d) => acc + parseFloat(d.amount), 0)
  const totalWithdrawn = withdrawals
    .filter((w) => w.Status === 'DONE')
    .reduce((acc, w) => acc + parseFloat(w.Amount), 0)

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="text-center">
          <div className="inline-block w-8 h-8 border-4 border-brand border-t-transparent rounded-full animate-spin mb-3"></div>
          <div className="text-muted">{t('wallet.loadingWallet')}</div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6 max-w-6xl mx-auto">
      <ToastContainer toasts={toasts} onDismiss={dismissToast} />

      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">{t('wallet.title')}</h1>
        <div className="text-xs text-muted">
          {t('wallet.autoRefresh')} {t('wallet.lastUpdated')}: {new Date().toLocaleTimeString()}
        </div>
      </div>

      {/* Overview Stats */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <div className="card">
          <div className="text-xs text-muted uppercase tracking-wide">{t('wallet.nonZeroAssets')}</div>
          <div className="text-3xl font-bold mt-1">{nonZeroBalances.length}</div>
        </div>
        <div className="card">
          <div className="text-xs text-muted uppercase tracking-wide">{t('wallet.deposits')}</div>
          <div className="text-3xl font-bold mt-1">
            {deposits.length}
            {totalDeposited > 0 && (
              <span className="text-sm text-muted ml-2">
                ({formatNumber(totalDeposited, 0)})
              </span>
            )}
          </div>
        </div>
        <div className="card">
          <div className="text-xs text-muted uppercase tracking-wide">{t('wallet.pendingShort')}</div>
          <div className="text-3xl font-bold mt-1 text-yellow-600">
            {pendingTxs.length}
            {pendingTotal > 0 && (
              <span className="text-sm text-muted ml-2">
                ({formatNumber(pendingTotal, 0)})
              </span>
            )}
          </div>
        </div>
        <div className="card">
          <div className="text-xs text-muted uppercase tracking-wide">{t('wallet.withdrawals')}</div>
          <div className="text-3xl font-bold mt-1">
            {withdrawals.length}
            {totalWithdrawn > 0 && (
              <span className="text-sm text-muted ml-2">
                ({formatNumber(totalWithdrawn, 0)})
              </span>
            )}
          </div>
        </div>
      </div>

      {/* Balances */}
      <div>
        <h2 className="text-lg font-semibold mb-3">{t('wallet.balancesTitle')}</h2>
        <div className="card overflow-x-auto">
          {wallets.length === 0 ? (
            <div className="text-center py-8 text-muted">
              {t('wallet.empty')} {t('wallet.emptyHint')}
            </div>
          ) : (
            <table className="w-full text-left">
              <thead className="text-xs text-muted uppercase tracking-wide border-b">
                <tr>
                  <th className="py-2 px-3">{t('wallet.asset')}</th>
                  <th className="py-2 px-3 text-right">{t('wallet.available')}</th>
                  <th className="py-2 px-3 text-right">{t('wallet.inWithdrawal')}</th>
                  <th className="py-2 px-3 text-right">{t('wallet.total')}</th>
                  <th className="py-2 px-3 text-right">{t('common.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {wallets.map((w) => {
                  const avail = parseFloat(w.available)
                  const frozen = parseFloat(w.frozen)
                  const total = avail + frozen
                  const hasBalance = total > 0
                  return (
                    <tr
                      key={w.asset}
                      className={`table-row ${hasBalance ? '' : 'opacity-60'}`}
                    >
                      <td className="py-3 px-3">
                        <div className="font-semibold">{w.asset}</div>
                      </td>
                      <td className={`py-3 px-3 text-right font-mono ${avail > 0 ? 'text-green-600 font-semibold' : 'text-muted'}`}>
                        {formatNumber(w.available)}
                      </td>
                      <td className="py-3 px-3 text-right font-mono text-muted">
                        {frozen > 0 ? formatNumber(w.frozen) : '—'}
                      </td>
                      <td className="py-3 px-3 text-right font-mono font-semibold">
                        {formatNumber(total)}
                      </td>
                      <td className="py-3 px-3 text-right">
                        <Link
                          to={`/trade/${w.asset === 'USDT' ? 'BTC' : w.asset}/USDT`}
                          className={`text-sm ${hasBalance ? 'text-brand hover:underline' : 'text-muted cursor-not-allowed'}`}
                        >
                          {t('wallet.tradeArrow')}
                        </Link>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
          <div className="text-xs text-muted mt-3 px-3 pb-2">
            💡 <strong>{t('common.tip')}:</strong> {t('wallet.availableTip')}
          </div>
        </div>
      </div>

      {/* KYC Status - Clearly labeled as LIMIT, not balance */}
      {kycLimit && (
        <div className="card bg-gradient-to-r from-blue-50 to-purple-50">
          <div className="flex items-start justify-between">
            <div>
              <div className="text-xs text-muted uppercase tracking-wide">{t('wallet.kycVerification')}</div>
              <div className="mt-2 flex items-center gap-3">
                <div>
                  <div className="text-xs text-muted">{t('settings.kycLevel')}</div>
                  <div className="text-2xl font-bold text-blue-600">L{kycLimit.kyc_level}</div>
                </div>
                <div>
                  <div className="text-xs text-muted">{t('wallet.status')}</div>
                  <div className="mt-1">
                    <StatusBadge status={kycLimit.kyc_status} />
                  </div>
                </div>
                <div>
                  <div className="text-xs text-muted">{t('wallet.dailyWithdrawLimit')}</div>
                  <div className="text-2xl font-bold text-blue-600">
                    {formatNumber(kycLimit.limit_usdt, 0)} <span className="text-sm font-normal text-muted">{t('wallet.usdtPerDay')}</span>
                  </div>
                </div>
              </div>
              <div className="text-xs text-muted mt-3 px-3 pb-2">
            {t('wallet.infoIcon')} {t('wallet.limitCaption')}
          </div>
            </div>
          </div>
        </div>
      )}

      {/* Real BTC Deposit */}
      <div className="card">
        <h2 className="text-lg font-semibold mb-2">{t('wallet.depositBtc')}</h2>
        <p className="text-sm text-muted mb-3">
          {t('wallet.depositBtcDesc')}
        </p>
        {btcAddr ? (
          <div className="bg-bg p-3 rounded">
            <div className="text-xs text-muted mb-1">{t('wallet.btcAddress')}</div>
            <div className="flex items-center gap-2 font-mono text-sm break-all">
              <span className="flex-1">{btcAddr}</span>
              <CopyButton text={btcAddr} label="Copy" />
            </div>
          </div>
        ) : (
          <button
            onClick={showBtcAddress}
            disabled={loadingAddr}
            className="btn-primary"
          >
            {loadingAddr ? t('common.loadingEllipsis') : t('wallet.showBtcAddress')}
          </button>
        )}

        {/* Pending Transactions */}
        {pendingTxs.length > 0 && (
          <div className="mt-4">
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-semibold text-yellow-700">{t('wallet.pendingTransactions')}</h3>
              <span className="text-xs text-muted">{pendingTxs.length} {t('wallet.txWaiting')}</span>
            </div>
            <div className="space-y-2">
              {pendingTxs.map((p) => {
                const pct = Math.min(100, (p.confirmations / p.min_conf) * 100)
                const remaining = Math.max(0, p.min_conf - p.confirmations)
                const isMempool = p.confirmations === 0
                return (
                  <div
                    key={p.tx_hash}
                    className="bg-yellow-50 border border-yellow-200 rounded p-3"
                  >
                    <div className="flex items-center justify-between mb-1">
                      <div>
                        <span className="font-mono font-semibold">
                          {p.amount} {p.asset}
                        </span>
                        <span className="ml-2 text-xs text-muted">
                          {formatAge(new Date(p.time * 1000).toISOString())}
                        </span>
                      </div>
                      <span
                        className={`text-xs font-mono font-semibold ${
                          isMempool ? 'text-yellow-700' : 'text-blue-700'
                        }`}
                      >
                        {p.confirmations} / {p.min_conf} confs
                      </span>
                    </div>
                    <div className="w-full bg-gray-200 rounded-full h-2 overflow-hidden">
                      <div
                        className={`h-2 rounded-full transition-all duration-500 ${
                          isMempool ? 'bg-yellow-500' : 'bg-blue-500'
                        }`}
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                    <div className="flex items-center justify-between mt-2 text-xs">
                      <span className="text-muted">
                        {remaining > 0 ? t('wallet.moreNeeded', { n: remaining }) : t('wallet.readyToCredit')}
                      </span>
                      <CopyButton text={p.tx_hash} label={p.tx_hash.slice(0, 12) + '…'} />
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </div>

      {/* Withdraw */}
      <div className="card">
        <h2 className="text-lg font-semibold mb-2">{t('wallet.withdrawTitle')}</h2>
        <p className="text-sm text-muted mb-3">
          {t('wallet.withdrawDesc')}
        </p>
        <form onSubmit={submitWithdraw} className="space-y-3">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            <div>
              <label className="block text-xs text-muted mb-1 uppercase">{t('wallet.asset')}</label>
              <select
                className="input w-full"
                value={wdAsset}
                onChange={(e) => setWdAsset(e.target.value)}
              >
                
              </select>
            </div>
            <div>
              <label className="block text-xs text-muted mb-1 uppercase">{t('wallet.amount')}</label>
              <input
                type="number"
                step="any"
                placeholder="0.0"
                className="input w-full"
                value={wdAmount}
                onChange={(e) => setWdAmount(e.target.value)}
                required
              />
            </div>
            <div>
              <label className="block text-xs text-muted mb-1 uppercase">{t('wallet.destinationLabel')}</label>
              <input
                type="text"
                placeholder="FhZgciENDwJ5dhshNjFHYQebDasZSHeHS5"
                className="input w-full font-mono text-sm"
                value={wdAddress}
                onChange={(e) => setWdAddress(e.target.value)}
                required
              />
            </div>
          </div>
          <button
            type="submit"
            disabled={wdSubmitting}
            className="btn-primary w-full md:w-auto"
          >
            {wdSubmitting ? t('wallet.sending') : t('wallet.withdrawAction')}
          </button>
        </form>
      </div>

      {/* Withdrawal History */}
      <div>
        <h2 className="text-lg font-semibold mb-3">{t('wallet.withdrawalHistory')}</h2>
        <div className="card overflow-x-auto">
          {withdrawals.length === 0 ? (
            <div className="text-center py-8 text-muted">{t('wallet.noWithdrawals')}</div>
          ) : (
            <table className="w-full text-left">
              <thead className="text-xs text-muted uppercase tracking-wide border-b">
                <tr>
                  <th className="py-2 px-3">{t('orders.date')}</th>
                  <th className="py-2 px-3">{t('wallet.asset')}</th>
                  <th className="py-2 px-3 text-right">{t('wallet.amount')}</th>
                  <th className="py-2 px-3">{t('wallet.destinationLabel')}</th>
                  <th className="py-2 px-3">{t('wallet.txHash')}</th>
                  <th className="py-2 px-3">{t('wallet.status')}</th>
                </tr>
              </thead>
              <tbody>
                {withdrawals.map((w) => (
                  <tr key={w.ID} className="table-row">
                    <td className="py-3 px-3 text-xs">
                      <div>{formatAge(w.CreatedAt)}</div>
                      <div className="text-muted">{formatFull(w.CreatedAt)}</div>
                    </td>
                    <td className="py-3 px-3 font-semibold">{w.Asset}</td>
                    <td className="py-3 px-3 text-right font-mono font-semibold text-red-600">
                      -{formatNumber(w.Amount)}
                    </td>
                    <td className="py-3 px-3 text-xs font-mono text-muted">
                      <CopyButton text={w.DestAddress} label={w.DestAddress.slice(0, 10) + '…'} />
                    </td>
                    <td className="py-3 px-3 text-xs font-mono text-muted">
                      {w.TxHash ? (
                        <CopyButton text={w.TxHash} label={w.TxHash.slice(0, 10) + '…'} />
                      ) : (
                        <span className="text-muted">—</span>
                      )}
                    </td>
                    <td className="py-3 px-3">
                      <StatusBadge status={w.Status} />
                      {w.ErrorMsg && (
                        <div className="text-xs text-red-600 mt-1" title={w.ErrorMsg}>
                          ⚠ {w.ErrorMsg.slice(0, 40)}{w.ErrorMsg.length > 40 ? '…' : ''}
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {/* Deposit History */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-lg font-semibold">{t('wallet.depositHistory')}</h2>
          <button
            onClick={importFromChain}
            className="text-xs px-3 py-1 bg-brand text-black rounded hover:opacity-80"
            title="Scan chain for unrecorded confirmed deposits"
          >
            {t('wallet.syncFromChain')}
          </button>
        </div>
        <div className="card overflow-x-auto">
          {deposits.length === 0 ? (
            <div className="text-center py-8 text-muted">
              {t('wallet.noDeposits')}
            </div>
          ) : (
            <table className="w-full text-left">
              <thead className="text-xs text-muted uppercase tracking-wide border-b">
                <tr>
                  <th className="py-2 px-3">{t('orders.date')}</th>
                  <th className="py-2 px-3">{t('wallet.asset')}</th>
                  <th className="py-2 px-3 text-right">{t('wallet.amount')}</th>
                  <th className="py-2 px-3">{t('wallet.txHash')}</th>
                  <th className="py-2 px-3">{t('wallet.status')}</th>
                </tr>
              </thead>
              <tbody>
                {deposits.map((d) => (
                  <tr key={d.id} className="table-row">
                    <td className="py-3 px-3 text-xs">
                      <div>{formatAge(d.created_at)}</div>
                      <div className="text-muted">{formatFull(d.created_at)}</div>
                    </td>
                    <td className="py-3 px-3 font-semibold">{d.asset}</td>
                    <td className="py-3 px-3 text-right font-mono font-semibold text-green-600">
                      +{formatNumber(d.amount)}
                    </td>
                    <td className="py-3 px-3 text-xs font-mono text-muted">
                      <CopyButton text={d.tx_hash} label={d.tx_hash.slice(0, 12) + '…'} />
                    </td>
                    <td className="py-3 px-3">
                      <StatusBadge status={d.status} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>

      {/* Mock deposit (dev only) */}
      <details className="card text-sm">
        <summary className="cursor-pointer text-muted hover:text-text font-medium">
          {t('wallet.devTools')}
        </summary>
        <form onSubmit={onSpawn} className="mt-3 flex gap-3 items-end">
          <div>
            <label className="block text-xs text-muted mb-1">{t('wallet.asset')}</label>
            <select
              className="input"
              value={spawnAsset}
              onChange={(e) => setSpawnAsset(e.target.value)}
            >
              <option>USDT</option>
              <option>BTC</option>
              <option>ETH</option>
              <option>BNB</option>
              
            </select>
          </div>
          <div>
            <label className="block text-xs text-muted mb-1">{t('wallet.amount')}</label>
            <input
              type="number"
              step="any"
              className="input"
              value={spawnAmount}
              onChange={(e) => setSpawnAmount(e.target.value)}
            />
          </div>
          <button type="submit" className="btn-secondary">
            {t('wallet.spawnMock')}
          </button>
        </form>
      </details>

      {/* KYC upgrade link - redirects to dedicated KYC page with document upload */}
      {kycLimit && kycLimit.kyc_level < 2 && kycLimit.kyc_status !== 'PENDING' && (
        <div className="card">
          <h2 className="text-lg font-semibold mb-2">{t('wallet.submitKycL2')}</h2>
          <p className="text-sm text-gray-400 mb-3">
            {t('wallet.kycFormMoved', 'Document upload moved to dedicated page')}
          </p>
          <Link to="/user?tab=kyc" className="btn-primary inline-block">
            {t('wallet.goToKycPage', 'Submit KYC Documents')}
          </Link>
        </div>
      )}

      {kycLimit && kycLimit.kyc_status === 'PENDING' && (
        <div className="card border-yellow-300 bg-yellow-50">
          <div className="text-sm">
            <div className="font-semibold text-yellow-800">{t('wallet.kycUnderReview')}</div>
            <div className="text-yellow-700 mt-1">
              {t('wallet.kycReviewMsg', { limit: kycLimit.limit_usdt })}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
