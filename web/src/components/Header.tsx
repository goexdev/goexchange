import { Link, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useState, useRef, useEffect } from 'react'
import { NotificationBell } from './NotificationBell'
import { LanguageSwitcher } from './LanguageSwitcher'
import { useAuth } from '../lib/auth'

export function Header() {
  const { t } = useTranslation()
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const [userMenuOpen, setUserMenuOpen] = useState(false)
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const userMenuRef = useRef<HTMLDivElement>(null)

  function handleLogout() {
    logout()
    navigate('/login')
    setMobileMenuOpen(false)
  }

  // Close user dropdown on outside click
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (userMenuRef.current && !userMenuRef.current.contains(e.target as Node)) {
        setUserMenuOpen(false)
      }
    }
    if (userMenuOpen) {
      document.addEventListener('mousedown', handleClick)
      return () => document.removeEventListener('mousedown', handleClick)
    }
  }, [userMenuOpen])

  function closeMobileMenu() {
    setMobileMenuOpen(false)
  }

  return (
    <header className="border-b border-border bg-panel sticky top-0 z-40">
      <div className="container mx-auto px-4 py-3 flex items-center justify-between gap-4">
        {/* Brand */}
        <Link to="/" className="text-brand font-bold text-xl flex-shrink-0">
          {t('header.brand')}
        </Link>

        {/* Desktop nav - hidden on mobile */}
        <nav className="hidden md:flex items-center gap-4 flex-1">
          <Link to="/" className="hover:text-brand">{t('header.home')}</Link>
          <Link to="/markets" className="hover:text-brand">{t('header.markets')}</Link>
          {user && user.role === 'admin' && (
            <Link to="/admin" className="hover:text-brand font-semibold text-purple-400">
              {t('header.admin')}
            </Link>
          )}
        </nav>

        {/* Right side */}
        <div className="flex gap-3 items-center">
          <LanguageSwitcher />

          {user ? (
            <>
              <NotificationBell />

              {/* User dropdown - hidden on mobile */}
              <div className="relative hidden md:block" ref={userMenuRef}>
                <button
                  onClick={() => setUserMenuOpen(!userMenuOpen)}
                  className="flex items-center gap-1 text-sm text-text hover:text-brand"
                >
                  <span className="hidden lg:inline">{user.email}</span>
                  <span className="lg:hidden">{user.email.split('@')[0]}</span>
                  <span className="text-xs">&#9660;</span>
                </button>
                {userMenuOpen && (
                  <div className="absolute right-0 mt-2 w-48 bg-panel border border-border rounded shadow-lg z-50">
                    <Link
                      to="/user"
                      onClick={() => setUserMenuOpen(false)}
                      className="block px-4 py-2 text-sm hover:bg-bg/50 border-b border-border"
                    >
                      {t('header.myAccount')}
                    </Link>
                    <button
                      onClick={handleLogout}
                      className="block w-full text-left px-4 py-2 text-sm hover:bg-bg/50"
                    >
                      {t('common.logout')}
                    </button>
                  </div>
                )}
              </div>

              {/* Hamburger button - mobile only */}
              <button
                onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
                className="md:hidden p-2 hover:bg-bg/50 rounded"
                aria-label="Menu"
              >
                {mobileMenuOpen ? (
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <line x1="18" y1="6" x2="6" y2="18" />
                    <line x1="6" y1="6" x2="18" y2="18" />
                  </svg>
                ) : (
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <line x1="3" y1="6" x2="21" y2="6" />
                    <line x1="3" y1="12" x2="21" y2="12" />
                    <line x1="3" y1="18" x2="21" y2="18" />
                  </svg>
                )}
              </button>
            </>
          ) : (
            <>
              <Link to="/login" className="btn-secondary text-sm hidden sm:inline-block">
                {t('common.login')}
              </Link>
              <Link to="/register" className="btn-primary text-sm hidden sm:inline-block">
                {t('common.register')}
              </Link>
              <button
                onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
                className="sm:hidden p-2 hover:bg-bg/50 rounded"
                aria-label="Menu"
              >
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <line x1="3" y1="6" x2="21" y2="6" />
                  <line x1="3" y1="12" x2="21" y2="12" />
                  <line x1="3" y1="18" x2="21" y2="18" />
                </svg>
              </button>
            </>
          )}
        </div>
      </div>

      {/* Mobile menu drawer */}
      {mobileMenuOpen && (
        <div className="md:hidden border-t border-border bg-panel">
          <nav className="container mx-auto px-4 py-4 space-y-2">
            <Link to="/" onClick={closeMobileMenu} className="block py-2 hover:text-brand">
              {t('header.home')}
            </Link>
            <Link to="/markets" onClick={closeMobileMenu} className="block py-2 hover:text-brand">
              {t('header.markets')}
            </Link>
            {user && user.role === 'admin' && (
              <Link to="/admin" onClick={closeMobileMenu} className="block py-2 hover:text-brand text-purple-400 font-semibold">
                {t('header.admin')}
              </Link>
            )}
            {user && (
              <Link to="/user" onClick={closeMobileMenu} className="block py-2 hover:text-brand">
                {t('header.myAccount')}
              </Link>
            )}
            {!user && (
              <>
                <Link to="/login" onClick={closeMobileMenu} className="block py-2 hover:text-brand">
                  {t('common.login')}
                </Link>
                <Link to="/register" onClick={closeMobileMenu} className="block py-2 hover:text-brand">
                  {t('common.register')}
                </Link>
              </>
            )}
            {user && (
              <button
                onClick={handleLogout}
                className="block w-full text-left py-2 text-red-400 hover:text-red-300"
              >
                {t('common.logout')}
              </button>
            )}
            {user && (
              <div className="pt-2 mt-2 border-t border-border text-xs text-muted">
                {user.email}
              </div>
            )}
          </nav>
        </div>
      )}
    </header>
  )
}