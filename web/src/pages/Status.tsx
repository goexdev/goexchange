import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'

interface ComponentStatus {
  name: string
  status: string
  latency_ms?: number
  message?: string
}

interface StatusInfo {
  status: string
  generated_at: string
  version: string
  uptime_seconds: number
  components: ComponentStatus[]
}

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return d + 'd ' + h + 'h ' + m + 'm'
  if (h > 0) return h + 'h ' + m + 'm'
  return m + 'm'
}

export function Status() {
  const { t } = useTranslation()
  const [data, setData] = useState<StatusInfo | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/v1/status')
      .then(r => r.json())
      .then(d => {
        setData(d)
        setLoading(false)
      })
      .catch(() => setLoading(false))
  }, [])

  if (loading) {
    return (
      <div className="max-w-3xl mx-auto py-12 text-center">
        <p className="text-muted">{t("status.loading", "Loading status...")}</p>
      </div>
    )
  }

  if (!data) {
    return (
      <div className="max-w-3xl mx-auto py-12 text-center">
        <p className="text-red-400">{t("status.unableToLoad", "Unable to load status")}</p>
      </div>
    )
  }

  const statusColor = {
    operational: 'bg-green-500',
    degraded: 'bg-yellow-500',
    down: 'bg-red-500',
  }[data.status] || 'bg-gray-500'

  return (
    <div className="max-w-3xl mx-auto space-y-6 py-8">
      <div>
        <h1 className="text-3xl font-bold">{t("status.title", "System Status")}</h1>
        <p className="text-sm text-muted mt-1">
          {t("status.lastUpdated", "Last updated:")}: {new Date(data.generated_at).toLocaleString()}
        </p>
      </div>

      <div className="card">
        <div className="flex items-center gap-3">
          <div className={`w-3 h-3 rounded-full ${statusColor}`} />
          <div>
            <div className="text-xl font-semibold capitalize">{data.status}</div>
            <div className="text-xs text-muted">{t("status.overallLabel", "Overall platform status")}</div>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className="card">
          <div className="text-xs text-muted">Version</div>
          <div className="text-lg font-semibold">{data.version}</div>
        </div>
        <div className="card">
          <div className="text-xs text-muted">Uptime</div>
          <div className="text-lg font-semibold">{formatUptime(data.uptime_seconds)}</div>
        </div>
      </div>

      <div>
        <h2 className="text-xl font-semibold mb-3">Components</h2>
        <div className="card divide-y divide-border">
          {data.components.map((c: ComponentStatus) => (
            <div key={c.name} className="flex items-center justify-between py-3">
              <div className="flex items-center gap-3">
                <div className={`w-2 h-2 rounded-full ${
                  (statusColor as unknown as Record<string, string>)[c.status] || 'bg-gray-500'
                }`} />
                <div>
                  <div className="font-medium capitalize">{c.name}</div>
                  {c.message && (
                    <div className="text-xs text-red-400">{c.message}</div>
                  )}
                </div>
              </div>
              <div className="text-right">
                <div className="text-sm capitalize">{c.status}</div>
                {c.latency_ms !== undefined && (
                  <div className="text-xs text-muted">{c.latency_ms}ms</div>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="text-center text-sm text-muted">
        <Link to="/" className="hover:text-brand">{t("status.backToHome", "Back to home")}</Link>
      </div>
    </div>
  )
}