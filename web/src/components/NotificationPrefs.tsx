import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'

// Notification preferences component
// Each notification type has 2 independent toggles: In-app + Email

type NotifKey =
  | 'notify_2fa_enabled' | 'email_2fa_enabled'
  | 'notify_2fa_disabled' | 'email_2fa_disabled'
  | 'notify_2fa_backup_used' | 'email_2fa_backup_used'
  | 'notify_2fa_failed' | 'email_2fa_failed'
  | 'notify_2fa_login_success' | 'email_2fa_login_success'
  | 'notify_login' | 'email_login'
  | 'notify_withdrawal' | 'email_withdrawal'
  | 'notify_large_withdraw' | 'email_large_withdraw'

interface PrefConfig {
  notifKey: NotifKey
  emailKey: NotifKey
  labelKey: string
  descKey: string
  recommended?: boolean
  category: 'critical' | 'informational' | 'advanced'
}

export function NotificationPrefs() {
  const { t } = useTranslation()
  const [prefs, setPrefs] = useState<api.NotifPrefs | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [success, setSuccess] = useState('')

  useEffect(() => {
    load()
  }, [])

  async function load() {
    try {
      const data = await api.getNotifPrefs()
      setPrefs(data)
    } catch (e: any) {
      setErr(e.message)
    } finally {
      setLoading(false)
    }
  }

  async function toggle(key: NotifKey, value: boolean) {
    if (!prefs) return
    setSaving(true)
    setErr('')
    setSuccess('')
    const updated = { ...prefs, [key]: value }
    setPrefs(updated)

    try {
      const result = await api.updateNotifPrefs({ [key]: value } as any)
      setPrefs(result)
      setSuccess(t('settings.notifications.saved'))
      setTimeout(() => setSuccess(''), 2000)
    } catch (e: any) {
      setPrefs(prefs)
      setErr(e.message)
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="card">
        <h3 className="text-lg font-bold mb-3">{t('settings.notifications.title')}</h3>
        <div className="text-gray-400">{t('common.loadingEllipsis')}</div>
      </div>
    )
  }

  if (!prefs) return null

  // Notification configurations
  const configs: PrefConfig[] = [
    {
      notifKey: 'notify_2fa_disabled', emailKey: 'email_2fa_disabled',
      labelKey: 'settings.notifications.disabled',
      descKey: 'settings.notifications.disabledDesc',
      recommended: true, category: 'critical'
    },
    {
      notifKey: 'notify_2fa_backup_used', emailKey: 'email_2fa_backup_used',
      labelKey: 'settings.notifications.backup',
      descKey: 'settings.notifications.backupDesc',
      recommended: true, category: 'critical'
    },
    {
      notifKey: 'notify_2fa_failed', emailKey: 'email_2fa_failed',
      labelKey: 'settings.notifications.failed',
      descKey: 'settings.notifications.failedDesc',
      recommended: true, category: 'critical'
    },
    {
      notifKey: 'notify_2fa_enabled', emailKey: 'email_2fa_enabled',
      labelKey: 'settings.notifications.enabled',
      descKey: 'settings.notifications.enabledDesc', category: 'informational'
    },
    {
      notifKey: 'notify_2fa_login_success', emailKey: 'email_2fa_login_success',
      labelKey: 'settings.notifications.login',
      descKey: 'settings.notifications.loginDesc', category: 'informational'
    },
    {
      notifKey: 'notify_withdrawal', emailKey: 'email_withdrawal',
      labelKey: 'settings.notifications.withdrawal',
      descKey: 'settings.notifications.withdrawalDesc', category: 'advanced'
    },
    {
      notifKey: 'notify_large_withdraw', emailKey: 'email_large_withdraw',
      labelKey: 'settings.notifications.largeWithdraw',
      descKey: 'settings.notifications.largeWithdrawDesc', category: 'advanced'
    },
    {
      notifKey: 'notify_login', emailKey: 'email_login',
      labelKey: 'settings.notifications.notifyLogin',
      descKey: 'settings.notifications.notifyLoginDesc', category: 'advanced'
    },
  ]

  // Toggle component (extracted for clarity)
  function Toggle({ value, onChange, disabled, size = 'normal' }: { value: boolean; onChange: (v: boolean) => void; disabled?: boolean; size?: 'normal' | 'small' }) {
    const dim = size === 'small' ? 'w-10 h-5' : 'w-12 h-6'
    const dot = size === 'small' ? 'w-4 h-4' : 'w-5 h-5'
    const translateX = size === 'small' ? 'translate-x-5' : 'translate-x-6'
    return (
      <button
        onClick={() => onChange(!value)}
        disabled={disabled}
        className={`relative ${dim} rounded-full transition-colors flex-shrink-0 ${
          value ? 'bg-green-600' : 'bg-gray-600'
        } ${disabled ? 'opacity-50' : ''}`}
      >
        <span
          className={`absolute top-0.5 left-0.5 ${dot} rounded-full bg-white transition-transform ${
            value ? translateX : 'translate-x-0'
          }`}
        />
      </button>
    )
  }

  // Row component for a single notification type
  function NotifRow({ config }: { config: PrefConfig }) {
    const notifValue = prefs![config.notifKey] as boolean
    const emailValue = prefs![config.emailKey] as boolean
    return (
      <div className="py-3 border-b border-gray-700 last:border-b-0">
        <div className="flex items-start justify-between gap-4">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="font-medium text-sm">{t(config.labelKey)}</span>
              {config.recommended && (
                <span className="text-xs px-2 py-0.5 rounded bg-blue-900/40 text-blue-300 flex-shrink-0">
                  {t('settings.notifications.recommended')}
                </span>
              )}
            </div>
            <p className="text-xs text-gray-500 mt-0.5">{t(config.descKey)}</p>
          </div>
        </div>
        <div className="flex items-center gap-3 mt-2 ml-0">
          {/* In-app toggle */}
          <div className="flex items-center gap-1.5 flex-1 min-w-0">
            <span className="text-xs text-gray-400 flex-shrink-0 w-12">
              {t('settings.notifications.inApp')}
            </span>
            <Toggle
              value={notifValue}
              onChange={(v) => toggle(config.notifKey, v)}
              disabled={saving}
              size="small"
            />
            <span className={`text-xs ${notifValue ? 'text-green-400' : 'text-gray-500'}`}>
              {notifValue ? '✓' : '✗'}
            </span>
          </div>
          {/* Email toggle */}
          <div className="flex items-center gap-1.5 flex-1 min-w-0">
            <span className="text-xs text-gray-400 flex-shrink-0 w-12">
              {t('settings.notifications.email')}
            </span>
            <Toggle
              value={emailValue}
              onChange={(v) => toggle(config.emailKey, v)}
              disabled={saving}
              size="small"
            />
            <span className={`text-xs ${emailValue ? 'text-green-400' : 'text-gray-500'}`}>
              {emailValue ? '✓' : '✗'}
            </span>
          </div>
        </div>
      </div>
    )
  }

  const critical = configs.filter(c => c.category === 'critical')
  const informational = configs.filter(c => c.category === 'informational')
  const advanced = configs.filter(c => c.category === 'advanced')

  return (
    <div className="card">
      <h3 className="text-lg font-bold">{t('settings.notifications.title')}</h3>
      <p className="text-sm text-gray-400 mt-1 mb-3">
        {t('settings.notifications.subtitle')}
      </p>

      {err && <div className="text-red-400 text-sm mb-2">{err}</div>}
      {success && <div className="text-green-400 text-sm mb-2">{success}</div>}

      <div className="bg-blue-900/20 border border-blue-500/30 p-3 rounded mb-4 text-xs text-blue-300">
        {t('settings.notifications.recommendationNotice')}
      </div>

      {/* Channel legend */}
      <div className="flex gap-4 mb-3 text-xs text-gray-400 px-2">
        <span>{t('settings.notifications.inApp')}</span>
        <span>·</span>
        <span>📧 {t('settings.notifications.email')}</span>
      </div>

      {/* Critical notifications */}
      <div className="mb-4">
        <div className="text-xs uppercase tracking-wider text-gray-500 mb-2 font-semibold">
          {t('settings.notifications.critical')}
        </div>
        {critical.map(c => <NotifRow key={c.notifKey} config={c} />)}
      </div>

      {/* Informational */}
      <div className="mb-4">
        <div className="text-xs uppercase tracking-wider text-gray-500 mb-2 font-semibold">
          {t('settings.notifications.informational')}
        </div>
        {informational.map(c => <NotifRow key={c.notifKey} config={c} />)}
      </div>

      {/* Advanced */}
      <details className="mt-4">
        <summary className="text-sm text-gray-400 cursor-pointer hover:text-gray-200">
          {t('settings.notifications.advancedSettings')}
        </summary>
        <div className="mt-3 pl-4 border-l-2 border-gray-700">
          {advanced.map(c => <NotifRow key={c.notifKey} config={c} />)}
        </div>
      </details>
    </div>
  )
}
