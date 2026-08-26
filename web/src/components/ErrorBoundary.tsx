// Global Error Boundary - catches React component errors
import { Component, type ErrorInfo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

interface Props {
  children: ReactNode
}

interface State {
  hasError: boolean
  error: Error | null
  errorInfo: ErrorInfo | null
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null, errorInfo: null }
  }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    // Log to console (could send to monitoring service in production)
    console.error('ErrorBoundary caught error:', error, errorInfo)
    this.setState({ errorInfo })
  }

  handleReload = () => {
    window.location.reload()
  }

  handleGoHome = () => {
    window.location.href = '/'
  }

  render() {
    if (this.state.hasError) {
      return <ErrorFallback error={this.state.error} onReload={this.handleReload} onHome={this.handleGoHome} />
    }
    return this.props.children
  }
}

function ErrorFallback({ error, onReload, onHome }: { error: Error | null; onReload: () => void; onHome: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="min-h-screen flex items-center justify-center bg-bg p-6">
      <div className="max-w-lg w-full bg-panel border border-red-700 rounded-lg p-8 text-center">
        <div className="text-6xl mb-4">{t('common.warning', '⚠️')}</div>
        <h1 className="text-2xl font-bold text-text mb-2">{t('error.title')}</h1>
        <p className="text-muted mb-4">{t('error.subtitle')}</p>
        {error && (
          <details className="text-left bg-bg border border-border rounded p-3 mb-4">
            <summary className="text-sm text-muted cursor-pointer">{t('error.details')}</summary>
            <pre className="text-xs text-red-300 mt-2 overflow-auto max-h-32">
              {error.message}
              {'\n'}
              {error.stack?.split('\n').slice(0, 5).join('\n')}
            </pre>
          </details>
        )}
        <div className="flex gap-2 justify-center">
          <button onClick={onReload} className="btn-primary text-sm">
            {t('error.reload')}
          </button>
          <button onClick={onHome} className="btn-secondary text-sm">
            {t('error.goHome')}
          </button>
        </div>
      </div>
    </div>
  )
}
