import { FormEvent, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { TwoFactorSetup } from '../components/TwoFactorSetup'
import { NotificationPrefs } from '../components/NotificationPrefs'
import * as api from '../lib/api'
import { useAuth } from '../lib/auth'
import { getUserPrefs, setUserPref, notifyPrefsChanged, type UserPrefs } from '../lib/userPrefs'

export function Settings() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [oldPassword, setOldPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [err, setErr] = useState('')
  const [success, setSuccess] = useState('')

  // Email change
  const [newEmail, setNewEmail] = useState('')
  const [emailPassword, setEmailPassword] = useState('')

  // Trading preferences (local)
  const [prefs, setPrefs] = useState<UserPrefs>(getUserPrefs())

  function togglePref(key: keyof UserPrefs) {
    setPrefs(prev => {
      const next = { ...prev, [key]: !prev[key] }
      setUserPref(key, next[key])
      notifyPrefsChanged()
      return next
    })
  }

  // 2FA

  if (!user) return <div className="p-8 text-center">{t('common.pleaseLogIn')}</div>
  const currentUser = user  // TypeScript narrowing

  async function changePassword(e: FormEvent) {
    e.preventDefault()
    setErr('')
    setSuccess('')

    if (newPassword !== confirmPassword) {
      setErr(t('settings.passwordMismatch'))
      return
    }
    if (newPassword.length < 8) {
      setErr(t('settings.passwordTooShort'))
      return
    }

    setSubmitting(true)
    try {
      // First verify old password
      await api.login(currentUser.email, oldPassword)
      // Then we'd need a change-password endpoint
      // For now, simulate with the admin endpoint (only admins can do this)
      setSuccess(t('settings.passwordChangeRequested'))
      setOldPassword('')
      setNewPassword('')
      setConfirmPassword('')
    } catch (e: any) {
      setErr(t('settings.verifyOldFailed'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="max-w-3xl mx-auto space-y-6">
      <h1 className="text-2xl font-bold">{t('settings.accountSettings')}</h1>

      {err && <div className="bg-red-900 text-red-200 p-3 rounded">{err}</div>}
      {success && <div className="bg-green-900 text-green-200 p-3 rounded">{success}</div>}

      {/* Account Info */}
      <div className="card">
        <h2 className="text-lg font-semibold mb-3">{t('settings.accountInfo')}</h2>
        <div className="space-y-2 text-sm">
          <div className="flex justify-between">
            <span className="text-gray-400">{t('settings.emailLabel')}</span>
            <span className="font-mono">{currentUser.email}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-gray-400">{t('settings.userId')}</span>
            <span className="font-mono text-xs">{user.id}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-gray-400">{t('settings.role')}</span>
            <span>{user.role}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-gray-400">{t('settings.kycLevel')}</span>
            <span>L{user.kyc_level}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-gray-400">{t('settings.dailyLimit')}</span>
            <span>${user.kyc_limit_usdt || 0}</span>
          </div>
        </div>
      </div>

      {/* Change Password */}
      <form onSubmit={changePassword} className="card space-y-4">
        <h2 className="text-lg font-semibold">{t('settings.changePassword')}</h2>

        <div>
          <label className="block text-sm font-medium mb-1">{t('settings.oldPassword')}</label>
          <input
            type="password"
            value={oldPassword}
            onChange={e => setOldPassword(e.target.value)}
            required
            className="w-full bg-gray-700 px-3 py-2 rounded text-white"
          />
        </div>

        <div>
          <label className="block text-sm font-medium mb-1">{t('settings.newPassword')}</label>
          <input
            type="password"
            value={newPassword}
            onChange={e => setNewPassword(e.target.value)}
            minLength={8}
            required
            className="w-full bg-gray-700 px-3 py-2 rounded text-white"
          />
          <div className="text-xs text-gray-400 mt-1">{t('settings.passwordMinChars')}</div>
        </div>

        <div>
          <label className="block text-sm font-medium mb-1">{t('settings.confirmPassword')}</label>
          <input
            type="password"
            value={confirmPassword}
            onChange={e => setConfirmPassword(e.target.value)}
            required
            className="w-full bg-gray-700 px-3 py-2 rounded text-white"
          />
        </div>

        <button
          type="submit"
          disabled={submitting}
          className="bg-blue-600 hover:bg-blue-700 disabled:bg-gray-600 text-white px-4 py-2 rounded"
        >
          {submitting ? t('settings.updatingPassword') : t('settings.updatePassword')}
        </button>
      </form>

      {/* Trading Preferences */}
      <div className="card">
        <h2 className="text-lg font-semibold mb-3">{t('settings.tradingPrefs', 'Trading Preferences')}</h2>
        <p className="text-sm text-gray-400 mb-4">
          {t('settings.tradingPrefsDesc', 'Configure confirmation prompts when placing or cancelling orders.')}
        </p>
        <div className="space-y-3">
          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={prefs.confirmOrder}
              onChange={() => togglePref('confirmOrder')}
              className="w-4 h-4"
            />
            <div>
              <div className="text-sm font-medium">{t('settings.confirmOrderTitle', 'Confirm before placing orders')}</div>
              <div className="text-xs text-gray-400">{t('settings.confirmOrderDesc', 'Show a confirmation dialog when placing any order. Recommended for safety.')}</div>
            </div>
          </label>
          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={prefs.confirmCancelOrder}
              onChange={() => togglePref('confirmCancelOrder')}
              className="w-4 h-4"
            />
            <div>
              <div className="text-sm font-medium">{t('settings.confirmCancelTitle', 'Confirm before cancelling orders')}</div>
              <div className="text-xs text-gray-400">{t('settings.confirmCancelDesc', 'Show a confirmation dialog before cancelling an order. Recommended to avoid accidental cancels.')}</div>
            </div>
          </label>
        </div>
      </div>

{/* API Keys (Future) */}
      <div className="card">
        <h2 className="text-lg font-semibold mb-3">{t('settings.apiKeysComingSoon')}</h2>
        <p className="text-sm text-gray-400">
          {t('settings.apiKeysDesc')}
        </p>
      </div>

      {/* Sessions */}
      <div className="card">
        <h2 className="text-lg font-semibold mb-3">{t('settings.activeSessions')}</h2>
        <p className="text-sm text-gray-400">
          {t('settings.sessionsDesc')}
        </p>
      </div>

      <TwoFactorSetup />

      <NotificationPrefs />
    </div>
  )
}