// 404 Not Found page - shown when user navigates to a non-existent route
import { Link, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

export function NotFound() {
  const { t } = useTranslation()
  const location = useLocation()

  return (
    <div className="min-h-[60vh] flex items-center justify-center p-6">
      <div className="max-w-lg w-full text-center">
        <div className="text-8xl font-bold text-brand mb-2">404</div>
        <h1 className="text-2xl font-semibold text-text mb-2">
          {t('notFound.title', 'Page Not Found')}
        </h1>
        <p className="text-muted mb-2">
          {t(
            'notFound.description',
            "The page you're looking for doesn't exist or has been moved."
          )}
        </p>
        <p className="text-xs text-muted/70 font-mono mb-6">
          {location.pathname}
        </p>
        <div className="flex flex-col sm:flex-row gap-3 justify-center">
          <Link
            to="/"
            className="px-4 py-2 bg-brand text-bg font-semibold rounded hover:opacity-90 transition"
          >
            {t('notFound.goHome', 'Go Home')}
          </Link>
          <Link
            to="/markets"
            className="px-4 py-2 bg-panel border border-border text-text rounded hover:bg-bg transition"
          >
            {t('notFound.viewMarkets', 'View Markets')}
          </Link>
        </div>

        <div className="mt-8 text-xs text-muted/60">
          <p>{t('notFound.helpText', 'Need help? Try these popular links:')}</p>
          <div className="flex gap-2 justify-center mt-2 flex-wrap">
            <Link to="/trade/BTC/USDT" className="text-brand hover:underline">{t('pairs.btcUsdt', 'BTC/USDT')}</Link>
            <span>•</span>
            <Link to="/trade/ETH/USDT" className="text-brand hover:underline">{t('pairs.ethUsdt', 'ETH/USDT')}</Link>
            <span>•</span>
            <Link to="/user" className="text-brand hover:underline">
              {t('notFound.myAccount', 'My Account')}
            </Link>
            <span>•</span>
            <Link to="/admin" className="text-brand hover:underline">
              {t('notFound.admin', 'Admin')}
            </Link>
          </div>
        </div>
      </div>
    </div>
  )
}