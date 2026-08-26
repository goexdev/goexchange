import { FormEvent, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import * as api from '../lib/api'
import { useToast } from '../components/Toast'

interface AssetInfo {
  symbol: string
  name: string
  decimals: number
  chain?: string
  icon?: string
}

const SUPPORTED_ASSETS: AssetInfo[] = [
  { symbol: 'BTC', name: 'Bitcoin', decimals: 8, chain: 'btc', icon: '₿' },
  { symbol: 'ETH', name: 'Ethereum', decimals: 18, chain: 'eth', icon: 'Ξ' },
  { symbol: 'BNB', name: 'BNB', decimals: 18, chain: 'bsc', icon: 'B' },
  { symbol: 'USDT', name: 'Tether', decimals: 6, chain: 'eth', icon: '₮' },
  { symbol: 'USDC', name: 'USD Coin', decimals: 6, chain: 'eth', icon: '$' },
  { symbol: 'BUSD', name: 'Binance USD', decimals: 18, chain: 'bsc', icon: 'B' },
  { symbol: 'DAI', name: 'Dai', decimals: 18, chain: 'eth', icon: '◈' },
  { symbol: 'MATIC', name: 'Polygon', decimals: 18, chain: 'polygon', icon: 'M' },
]

function isValidAddress(asset: string, address: string): boolean {
  if (!address) return false
  if (asset === 'BTC' || asset === 'LTC') {
    return /^[a-zA-Z0-9]{25,62}$/.test(address)
  }
  return /^0x[a-fA-F0-9]{40}$/.test(address)
}

export function Withdraw() {
  const { t } = useTranslation()
  const toast = useToast()
  const [wallets, setWallets] = useState<api.Balance[]>([])
  const [selectedAsset, setSelectedAsset] = useState('USDT')
  const [destAddress, setDestAddress] = useState('')
  const [amount, setAmount] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [err, setErr] = useState('')
  const [success, setSuccess] = useState('')
  const [withdrawals, setWithdrawals] = useState<api.Withdrawal[]>([])
  const [confirmStep, setConfirmStep] = useState(false)
  const [savedAddresses, setSavedAddresses] = useState<api.AddressBookEntry[]>([])
  const [showSavePrompt, setShowSavePrompt] = useState(false)
  const [currencies, setCurrencies] = useState<api.Currency[]>([])

  useEffect(() => {
    load()
  }, [])

  async function load() {
    try {
      const [w, wd, addr, cur] = await Promise.all([
        api.getWallets(),
        api.listWithdrawals(),
        api.listAddresses().catch(() => ({ addresses: [] })),
        api.listCurrencies().catch(() => []),
      ])
      setWallets(w)
      setWithdrawals(wd)
      setSavedAddresses(addr.addresses)
      setCurrencies(cur)
    } catch (e: any) {
      setErr(e.message)
    }
  }

  async function saveCurrentAddress() {
    if (!destAddress || !isValidAddress(selectedAsset, destAddress)) return
    try {
      const label = prompt(t('withdraw.saveAddressPrompt'), '') || ''
      if (!label.trim()) return
      await api.addAddress(selectedAsset, destAddress, label, false)
      toast.showToast(t('withdraw.addressSaved'), 'success')
      await load()
      setShowSavePrompt(false)
    } catch (e: any) {
      toast.showToast(e.message || 'Failed to save address', 'error')
    }
  }

  const balance = wallets.find(w => w.asset === selectedAsset)
  const availableBalance = balance ? parseFloat(balance.available) : 0
  const assetInfo = SUPPORTED_ASSETS.find(a => a.symbol === selectedAsset)
  const validAddress = isValidAddress(selectedAsset, destAddress)
  const amountNum = parseFloat(amount) || 0
  const validAmount = amountNum > 0 && amountNum <= availableBalance

  // Calculate fee based on currency settings
  const currencyInfo = currencies.find(c => c.symbol === selectedAsset)
  const feeFlat = currencyInfo ? parseFloat(currencyInfo.withdraw_fee_flat) : 0
  const feePercent = currencyInfo ? parseFloat(currencyInfo.withdraw_fee_percent) : 0
  const feeMin = currencyInfo ? parseFloat(currencyInfo.withdraw_fee_min) : 0
  const feeComputed = amountNum * feePercent
  const feeRaw = Math.max(feeFlat, feeComputed)
  const withdrawFee = Math.max(feeRaw, feeMin)
  const receiveAmount = Math.max(0, amountNum - withdrawFee)
  const canSubmit = validAddress && validAmount && !submitting

  const addressesForAsset = savedAddresses.filter(a => a.asset === selectedAsset)

  function setMax() {
    if (availableBalance > 0) {
      setAmount(availableBalance.toFixed(assetInfo?.decimals || 8))
    }
  }

  function selectAddress(addr: api.AddressBookEntry) {
    setDestAddress(addr.address)
    setShowSavePrompt(false)
    toast.showToast(
      t('withdraw.addressSelected', { label: addr.label || addr.address.slice(0, 12) }),
      'info'
    )
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!canSubmit) return

    if (!confirmStep) {
      setConfirmStep(true)
      return
    }

    setSubmitting(true)
    setErr('')
    setSuccess('')
    try {
      const w = await api.createWithdrawal(selectedAsset, amount, destAddress)
      const successMsg = t('withdraw.createdSuccess', {
        amount: w.Amount,
        asset: selectedAsset,
        address: destAddress.slice(0, 8),
      })
      setSuccess(successMsg)
      toast.showToast(successMsg, 'success')
      setConfirmStep(false)
      setAmount('')
      setDestAddress('')
      const alreadySaved = savedAddresses.some(
        a => a.asset === selectedAsset && a.address === destAddress
      )
      if (!alreadySaved) {
        setShowSavePrompt(true)
      }
      await load()
    } catch (e: any) {
      setErr(e.message)
      toast.showToast(e.message || 'Withdrawal failed', 'error')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="max-w-3xl mx-auto space-y-6">
      <h1 className="text-2xl font-bold">{t('withdraw.title')}</h1>

      {err && <div className="bg-red-900 text-red-200 p-3 rounded">{err}</div>}
      {success && <div className="bg-green-900 text-green-200 p-3 rounded">{success}</div>}

      <div className="card bg-yellow-900 border-yellow-700">
        <div className="flex gap-2">
          <span className="text-xl">{t('withdraw.warningIcon')}</span>
          <div>
            <div className="font-semibold">{t('withdraw.verifyAddress')}</div>
            <div className="text-sm text-yellow-200">
              {t('withdraw.irreversibleWarning')}
            </div>
          </div>
        </div>
      </div>

      <form onSubmit={onSubmit} className="card space-y-4">
        <div>
          <label className="block text-sm font-medium mb-2">{t('withdraw.asset')}</label>
          <div className="grid grid-cols-3 md:grid-cols-5 gap-2">
            {SUPPORTED_ASSETS.map(a => (
              <button
                key={a.symbol}
                type="button"
                onClick={() => {
                  setSelectedAsset(a.symbol)
                  setDestAddress('')
                  setConfirmStep(false)
                  setShowSavePrompt(false)
                }}
                disabled={availableBalance === 0 && a.symbol !== selectedAsset}
                className={`p-3 rounded border-2 text-left ${
                  selectedAsset === a.symbol
                    ? 'border-blue-500 bg-blue-900 bg-opacity-30'
                    : 'border-gray-700 hover:border-gray-600'
                } ${availableBalance === 0 && a.symbol !== selectedAsset ? 'opacity-40' : ''}`}
              >
                <div className="text-2xl">{a.icon}</div>
                <div className="font-medium">{a.symbol}</div>
                <div className="text-xs text-gray-400">{a.name}</div>
              </button>
            ))}
          </div>
        </div>

        {balance && (
          <div className="flex justify-between items-center bg-gray-700 px-4 py-3 rounded">
            <span className="text-sm text-gray-400">{t('withdraw.available')}</span>
            <div className="flex items-center gap-2">
              <span className="font-mono">
                {availableBalance.toFixed(assetInfo?.decimals || 8)}
              </span>
              <span className="text-sm text-gray-400">{selectedAsset}</span>
              <button
                type="button"
                onClick={setMax}
                className="ml-2 text-xs bg-blue-900 hover:bg-blue-800 text-blue-200 px-2 py-0.5 rounded"
              >
                MAX
              </button>
            </div>
          </div>
        )}

        {addressesForAsset.length > 0 && (
          <div>
            <div className="flex justify-between items-center mb-2">
              <label className="block text-sm font-medium">
                {t('withdraw.savedAddresses')} ({addressesForAsset.length})
              </label>
              <Link
                to="/user?tab=addresses"
                className="text-xs text-blue-400 hover:text-blue-300"
              >
                {t('withdraw.manageAddresses')} →
              </Link>
            </div>
            <div className="space-y-1 max-h-40 overflow-y-auto">
              {addressesForAsset.map(addr => (
                <button
                  key={addr.id}
                  type="button"
                  onClick={() => selectAddress(addr)}
                  className={`w-full text-left p-2 rounded border ${
                    destAddress === addr.address
                      ? 'border-blue-500 bg-blue-900 bg-opacity-30'
                      : 'border-gray-700 hover:border-gray-600 bg-gray-800'
                  }`}
                >
                  <div className="flex justify-between items-center">
                    <div className="flex-1 min-w-0">
                      <div className="font-medium text-sm truncate">
                        {addr.label || t('withdraw.unlabeled')}
                        {addr.whitelisted && (
                          <span className="ml-2 text-xs text-green-400">★</span>
                        )}
                      </div>
                      <div className="font-mono text-xs text-gray-400 truncate">
                        {addr.address}
                      </div>
                    </div>
                    {addr.last_used_at && (
                      <div className="text-xs text-gray-500 ml-2 whitespace-nowrap">
                        {new Date(addr.last_used_at).toLocaleDateString()}
                      </div>
                    )}
                  </div>
                </button>
              ))}
            </div>
          </div>
        )}

        <div>
          <label className="block text-sm font-medium mb-2">
            {t('withdraw.destination')}
            {addressesForAsset.length === 0 && (
              <Link
                to="/user?tab=addresses"
                className="ml-2 text-xs text-blue-400 hover:text-blue-300"
              >
                ({t('withdraw.saveForLaterPrompt', 'Save for later')})
              </Link>
            )}
          </label>
          <input
            type="text"
            value={destAddress}
            onChange={e => {
              setDestAddress(e.target.value)
              setConfirmStep(false)
            }}
            placeholder={selectedAsset === 'BTC' ? 'bc1q... or 1A1zP1...' : '0x...'}
            required
            className={`w-full bg-gray-700 px-3 py-2 rounded text-white font-mono text-sm ${
              destAddress && !validAddress ? 'border border-red-500' : ''
            }`}
          />
          {destAddress && !validAddress && (
            <div className="text-xs text-red-400 mt-1">
              {t('withdraw.invalidAddressFor', { asset: selectedAsset })}
            </div>
          )}
          {showSavePrompt && destAddress && isValidAddress(selectedAsset, destAddress) && (
            <div className="mt-2 bg-blue-900 border border-blue-700 p-2 rounded flex justify-between items-center">
              <span className="text-xs text-blue-200">
                {t('withdraw.saveForFutureUse')}
              </span>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={saveCurrentAddress}
                  className="text-xs bg-blue-700 hover:bg-blue-600 text-white px-3 py-1 rounded"
                >
                  {t('common.save')}
                </button>
                <button
                  type="button"
                  onClick={() => setShowSavePrompt(false)}
                  className="text-xs text-blue-200 hover:text-white px-2 py-1"
                >
                  {t('common.cancel')}
                </button>
              </div>
            </div>
          )}
        </div>

        <div>
          <label className="block text-sm font-medium mb-2">{t('withdraw.amount')}</label>
          <div className="flex gap-2">
            <input
              type="number"
              value={amount}
              onChange={e => setAmount(e.target.value)}
              step="any"
              min="0"
              max={availableBalance}
              placeholder="0.00"
              required
              className="flex-1 bg-gray-700 px-3 py-2 rounded text-white"
            />
            <span className="flex items-center px-3 text-sm text-gray-400">
              {selectedAsset}
            </span>
          </div>
          {amount && amountNum > availableBalance && (
            <div className="text-xs text-red-400 mt-1">{t('withdraw.insufficient')}</div>
          )}
        </div>

        <div className="bg-gray-700 p-3 rounded text-sm space-y-1">
          <div className="flex justify-between">
            <span className="text-gray-400">{t('withdraw.networkFee')}</span>
            <span>
              {assetInfo?.chain === 'eth' || assetInfo?.chain === 'arbitrum' ||
               assetInfo?.chain === 'optimism' || assetInfo?.chain === 'base'
                ? t('withdraw.feeEstimateEth')
                : assetInfo?.chain === 'bsc'
                ? t('withdraw.feeEstimateBnb')
                : assetInfo?.chain === 'polygon'
                ? t('withdraw.feeEstimateMatic')
                : t('withdraw.feeEstimateAsset', { asset: selectedAsset })}
            </span>
          </div>
          <div className="flex justify-between">
            <span className="text-gray-400">{t('withdraw.estimatedTime')}</span>
            <span>{t("withdraw.approxTime", "~ 30s")}</span>
          </div>
          {amountNum > 0 && (
            <div className="bg-gray-800 border border-gray-700 rounded p-3 space-y-1 text-sm">
              <div className="flex justify-between">
                <span className="text-gray-400">{t("withdraw.amountLabel", "Amount")}</span>
                <span className="font-mono">{amountNum.toFixed(assetInfo?.decimals || 8)} {selectedAsset}</span>
              </div>
              {withdrawFee > 0 && (
                <div className="flex justify-between">
                  <span className="text-gray-400">{t("withdraw.feeLabel", "Network Fee")}</span>
                  <span className="font-mono text-yellow-300">−{withdrawFee.toFixed(assetInfo?.decimals || 8)} {selectedAsset}</span>
                </div>
              )}
              <div className="border-t border-gray-700 my-1"></div>
              <div className="flex justify-between font-semibold">
                <span>{t("withdraw.youReceive", "You will receive")}</span>
                <span className="font-mono text-green-400">{receiveAmount.toFixed(assetInfo?.decimals || 8)} {selectedAsset}</span>
              </div>
            </div>
          )}
          <div className="flex justify-between">
            <span className="text-gray-400">{t('withdraw.riskCheck')}</span>
            <span className="text-green-400">{t('withdraw.autoApproved')}</span>
          </div>
        </div>

        {!confirmStep ? (
          <button
            type="submit"
            disabled={!canSubmit}
            className="w-full bg-blue-600 hover:bg-blue-700 disabled:bg-gray-600 disabled:cursor-not-allowed text-white py-3 rounded font-medium"
          >
            {t('withdraw.review')} →
          </button>
        ) : (
          <div className="space-y-2">
            <div className="bg-yellow-900 border border-yellow-700 p-3 rounded text-sm">
              <div className="font-semibold text-yellow-200 mb-1">
                {t('withdraw.confirmTitle')}
              </div>
              <div className="text-yellow-100">
                {t('withdraw.confirmSend', {
                  amount: amount,
                  asset: selectedAsset,
                  address: destAddress,
                })}
              </div>
            </div>
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={submitting}
                className="flex-1 bg-red-600 hover:bg-red-700 disabled:bg-gray-600 text-white py-3 rounded font-medium"
              >
                {submitting ? t('withdraw.submitting') : t('withdraw.confirm')}
              </button>
              <button
                type="button"
                onClick={() => setConfirmStep(false)}
                className="px-6 bg-gray-700 hover:bg-gray-600 text-white py-3 rounded"
              >
                {t('common.cancel')}
              </button>
            </div>
          </div>
        )}
      </form>

      {withdrawals.length > 0 && (
        <div className="card">
          <h2 className="text-lg font-semibold mb-3">{t('withdraw.recent')}</h2>
          <div className="space-y-2">
            {withdrawals.slice(0, 5).map(w => (
              <div
 key={w.ID} className="flex justify-between items-center p-2 bg-gray-800 rounded">
                <div>
                  <div className="text-sm">
                    {w.Amount} {w.Asset}
                  </div>
                  <div className="font-mono text-xs text-gray-400">
                    {w.DestAddress.slice(0, 12)}...
                  </div>
                </div>
                <div className="text-xs">
                  <span
                    className={
                      w.Status === 'CONFIRMED' || w.Status === 'COMPLETED'
                        ? 'text-green-400'
                        : w.Status === 'PENDING'
                        ? 'text-yellow-400'
                        : 'text-gray-400'
                    }
                  >
                    {w.Status}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}