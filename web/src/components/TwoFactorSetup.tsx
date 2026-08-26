import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { QRCodeSVG } from 'qrcode.react'
import * as api from '../lib/api'

export function TwoFactorSetup() {
  const { t } = useTranslation()
  const [enabled, setEnabled] = useState(false)
  const [remainingCodes, setRemainingCodes] = useState(0)
  const [setupData, setSetupData] = useState<{ secret: string; otpauth_url: string } | null>(null)
  const [code, setCode] = useState('')
  const [backupCodes, setBackupCodes] = useState<string[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    refresh()
  }, [])

  async function refresh() {
    try {
      const status = await api.get2FAStatus()
      setEnabled(status.enabled)
      setRemainingCodes(status.backup_codes_remaining)
    } catch (e) {
      console.error(e)
    }
  }

  async function startSetup() {
    setError('')
    setLoading(true)
    try {
      const data = await api.setup2FA()
      setSetupData(data)
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  async function enable() {
    setError('')
    setLoading(true)
    try {
      const res = await api.enable2FA(code)
      setBackupCodes(res.backup_codes)
      setSetupData(null)
      setCode('')
      await refresh()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  async function disable() {
    if (!confirm(t('twoFactor.disableConfirm'))) return
    setError('')
    const disableCode = prompt('Enter your current 6-digit 2FA code:')
    if (!disableCode) return

    setLoading(true)
    try {
      await api.disable2FA(disableCode)
      await refresh()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  async function regenerateBackupCodes() {
    const regenCode = prompt('Enter your current 6-digit 2FA code to regenerate backup codes:')
    if (!regenCode) return

    setLoading(true)
    try {
      const res = await api.regenerateBackupCodes(regenCode)
      setBackupCodes(res.backup_codes)
      await refresh()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  if (enabled && backupCodes.length === 0) {
    // Just show enabled state
    return (
      <div className="card">
        <h3 className="text-lg font-bold mb-3">{t('settings.twoFA')}</h3>
        <div className="bg-green-900/20 border border-green-500/30 p-4 rounded">
          <div className="text-green-400 font-semibold mb-2">{t('settings.notifications.enabled')}</div>
          <p className="text-sm text-gray-400 mb-3">
            {t('twoFactor.backupRemaining', { n: remainingCodes })}
          </p>
          <div className="flex gap-2">
            <button onClick={regenerateBackupCodes} disabled={loading}
              className="px-3 py-1.5 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded disabled:opacity-50">
              {t('twoFactor.regenerateBackup')}
            </button>
            <button onClick={disable} disabled={loading}
              className="px-3 py-1.5 text-sm bg-red-600 hover:bg-red-700 text-white rounded disabled:opacity-50">
              {t('settings.disable')}
            </button>
          </div>
        </div>
        {error && <div className="text-red-400 text-sm mt-2">{error}</div>}
      </div>
    )
  }

  if (backupCodes.length > 0) {
    // Show backup codes (one time only)
    return (
      <div className="card">
        <h3 className="text-lg font-bold mb-3">{t('twoFactor.backupCodesTitle')}</h3>
        <div className="bg-yellow-900/20 border border-yellow-500/30 p-4 rounded">
          <div className="text-yellow-400 font-semibold mb-2">{t('twoFactor.saveCodesWarning')}</div>
          <p className="text-sm text-gray-400 mb-3">
            {t('twoFactor.saveCodesDesc')}
            {t('twoFactor.eachCodeOnce')}
          </p>
          <div className="grid grid-cols-2 gap-2 font-mono text-sm mb-3">
            {backupCodes.map((c, i) => (
              <div key={i} className="bg-gray-800 px-3 py-2 rounded">{c}</div>
            ))}
          </div>
          <button onClick={() => setBackupCodes([])}
            className="px-3 py-1.5 text-sm bg-gray-600 hover:bg-gray-700 text-white rounded">
            {t('twoFactor.iHaveSaved')}
          </button>
        </div>
      </div>
    )
  }

  if (setupData) {
    // Show setup wizard
    return (
      <div className="card">
        <h3 className="text-lg font-bold mb-3">{t('twoFactor.setupTitle')}</h3>
        <div className="space-y-3">
          <div className="bg-blue-900/20 border border-blue-500/30 p-4 rounded">
            <p className="text-sm text-gray-300 mb-3">
              {t('twoFactor.step1Install')}
            </p>
            <p className="text-sm text-gray-300 mb-3">
              {t('twoFactor.step2Scan')}
            </p>
            {/* QR Code - SVG so it scales perfectly on any device */}
            <div className="flex justify-center my-4 p-4 bg-white rounded">
              <QRCodeSVG
                value={setupData.otpauth_url}
                size={200}
                level="M"
                includeMargin={true}
              />
            </div>
            <details className="mt-3">
              <summary className="text-sm text-gray-400 cursor-pointer hover:text-gray-200">
                {t('twoFactor.cantScanHint')}
              </summary>
              <div className="mt-2 font-mono text-sm bg-gray-800 px-3 py-2 rounded break-all select-all">
                {setupData.secret}
              </div>
              <p className="text-xs text-gray-500 mt-2 break-all">
                {t('twoFactor.otpauthUrl')}: {setupData.otpauth_url}
              </p>
            </details>
          </div>

          <div className="bg-blue-900/20 border border-blue-500/30 p-4 rounded">
            <p className="text-sm text-gray-300 mb-2">
              {t('twoFactor.step3Enter')}
            </p>
            <input type="text" value={code} onChange={e => setCode(e.target.value)}
              maxLength={6} placeholder="123456"
              className="w-full bg-gray-800 px-3 py-2 rounded text-white font-mono" />
          </div>

          <div className="flex gap-2">
            <button onClick={enable} disabled={code.length !== 6 || loading}
              className="px-4 py-2 bg-green-600 hover:bg-green-700 text-white rounded disabled:opacity-50">
              {t('twoFactor.enable')}
            </button>
            <button onClick={() => { setSetupData(null); setCode('') }}
              className="px-4 py-2 bg-gray-600 hover:bg-gray-700 text-white rounded">
              Cancel
            </button>
          </div>

          {error && <div className="text-red-400 text-sm">{error}</div>}
        </div>
      </div>
    )
  }

  // Default: not enabled, show "Enable" button
  return (
    <div className="card">
      <h3 className="text-lg font-bold mb-3">{t('settings.twoFA')}</h3>
      <div className="bg-gray-800 p-4 rounded mb-3">
        <p className="text-sm text-gray-400 mb-2">
          {t('twoFactor.description')}
          {t('twoFactor.youllNeed')}
        </p>
        <p className="text-xs text-gray-500">
          {t('twoFactor.isWhat')} <strong>optional</strong>{t('twoFactor.isWhat2')}
          {t('twoFactor.accountWorks')}
        </p>
      </div>
      <button onClick={startSetup} disabled={loading}
        className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded disabled:opacity-50">
        {t('twoFactor.enable')}
      </button>
      {error && <div className="text-red-400 text-sm mt-2">{error}</div>}
    </div>
  )
}
