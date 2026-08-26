import { Link, useNavigate } from 'react-router-dom'
import { FormEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import * as api from '../lib/api'
import { useToast } from '../components/Toast'

export function Login() {
  useDocumentTitle('Sign In - goexchange')
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { login } = useAuth()
  const toast = useToast()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [twoFactorCode, setTwoFactorCode] = useState('')
  const [requires2FA, setRequires2FA] = useState(false)
  const [err, setErr] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setErr('')
    setSubmitting(true)
    try {
      const result = await login(email, password, requires2FA ? twoFactorCode : undefined)
      if (result.requires_2fa) {
        // Show 2FA input
        setRequires2FA(true)
        toast.showToast(t('auth.login.twoFactorPrompt', 'Enter your 2FA code'), 'info')
      } else {
        // Login complete
        toast.showToast(t('auth.login.welcomeBack', 'Welcome back!'), 'success')
        navigate('/')
      }
    } catch (e: any) {
      setErr(e.message)
      toast.showToast(e.message || 'Login failed', 'error')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="max-w-md mx-auto mt-12">
      <div className="card">
        <h1 className="text-2xl font-bold mb-2">{t('auth.login.title')}</h1>
        <p className="text-sm text-gray-400 mb-6">{t('auth.login.subtitle')}</p>
        {err && <div className="bg-red-900 text-red-200 p-3 rounded mb-4 text-sm">{err}</div>}
        <form onSubmit={onSubmit} className="space-y-4">
          <div>
            <label className="block text-sm text-gray-300 mb-1">{t('auth.login.email')}</label>
            <input
              type="email"
              value={email}
              onChange={e => setEmail(e.target.value)}
              autoComplete="username"
              required
              className="w-full bg-gray-700 px-3 py-2 rounded text-white"
            />
          </div>
          {requires2FA && (
            <div>
              <label className="block text-sm text-gray-300 mb-1">
                {t('auth.login.twoFactorCode') || 'Two-Factor Code'}
              </label>
              <input
                type="text"
                value={twoFactorCode}
                onChange={e => setTwoFactorCode(e.target.value)}
                maxLength={10}
                placeholder={t("auth.login.twoFactorPlaceholder", "123456 (or backup code)")}
                autoFocus
                className="w-full bg-gray-700 px-3 py-2 rounded text-white font-mono"
              />
              <p className="text-xs text-gray-500 mt-1">
                {t('auth.login.twoFactorHint', 'Enter the 6-digit code from your authenticator app, or one of your backup codes.')}
              </p>
            </div>
          )}
          <div>
            <label className="block text-sm text-gray-300 mb-1">{t('auth.login.password')}</label>
            <input
              type="password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              autoComplete="current-password"
              required
              className="w-full bg-gray-700 px-3 py-2 rounded text-white"
            />
          </div>
          <button
            type="submit"
            disabled={submitting}
            className="w-full bg-blue-600 hover:bg-blue-700 disabled:bg-gray-600 text-white py-2 rounded"
          >
            {submitting ? t('common.loading') : t('auth.login.signIn')}
          </button>
        </form>
        <p className="text-sm text-gray-400 mt-4">
          {t('auth.login.noAccount')}{' '}
          <Link to="/register" className="text-blue-400 hover:underline">{t('auth.login.signUp')}</Link>
        </p>
      </div>
    </div>
  )
}
