import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import {
  createChart,
  AreaSeries,
  HistogramSeries,
  type IChartApi,
  type AreaData,
  type HistogramData,
  type Time,
} from 'lightweight-charts'
import * as api from '../lib/api'

interface DashboardChartsProps {
  data: api.DashboardCharts
}

function MiniChart({ values, dates, color, suffix }: {
  values: number[]
  dates: (string | number)[]
  color: string
  suffix: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)

  useEffect(() => {
    if (!ref.current || values.length === 0) return

    const chart = createChart(ref.current, {
      width: ref.current.clientWidth,
      height: 120,
      layout: {
        background: { color: 'transparent' },
        textColor: '#9CA3AF',
        fontSize: 10,
      },
      grid: {
        vertLines: { visible: false },
        horzLines: { color: 'rgba(75, 85, 99, 0.1)' },
      },
      timeScale: { visible: false },
      rightPriceScale: { borderVisible: false },
      handleScroll: false,
      handleScale: false,
      crosshair: {
        vertLine: { visible: false },
        horzLine: { visible: false },
      },
    })
    chartRef.current = chart

    const series = chart.addSeries(AreaSeries, {
      lineColor: color,
      topColor: color + '40',
      bottomColor: color + '00',
      lineWidth: 2,
    })

    const seriesData: AreaData[] = values.map((v, i) => ({
      time: dates[i] as Time,
      value: v,
    }))
    series.setData(seriesData)
    chart.timeScale().fitContent()

    const observer = new ResizeObserver(() => {
      if (chartRef.current && ref.current) {
        chartRef.current.applyOptions({ width: ref.current.clientWidth })
      }
    })
    observer.observe(ref.current)

    return () => {
      observer.disconnect()
      chart.remove()
      chartRef.current = null
    }
  }, [values, dates, color, suffix])

  return <div ref={ref} className="w-full" style={{ height: '120px' }} />
}

function BarChartMini({ labels, values, color }: {
  labels: string[]
  values: number[]
  color: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)

  useEffect(() => {
    if (!ref.current || values.length === 0) return

    const chart = createChart(ref.current, {
      width: ref.current.clientWidth,
      height: 120,
      layout: {
        background: { color: 'transparent' },
        textColor: '#9CA3AF',
        fontSize: 10,
      },
      grid: {
        vertLines: { visible: false },
        horzLines: { color: 'rgba(75, 85, 99, 0.1)' },
      },
      timeScale: { visible: false },
      rightPriceScale: { borderVisible: false },
      handleScroll: false,
      handleScale: false,
      crosshair: {
        vertLine: { visible: false },
        horzLine: { visible: false },
      },
    })
    chartRef.current = chart

    const series = chart.addSeries(HistogramSeries, {
      color,
    })

    const seriesData: HistogramData[] = values.map((v, i) => ({
      time: (Math.floor(Date.now() / 1000) - (values.length - i) * 86400) as Time,
      value: v,
      color,
    }))
    series.setData(seriesData)
    chart.timeScale().fitContent()

    const observer = new ResizeObserver(() => {
      if (chartRef.current && ref.current) {
        chartRef.current.applyOptions({ width: ref.current.clientWidth })
      }
    })
    observer.observe(ref.current)

    return () => {
      observer.disconnect()
      chart.remove()
      chartRef.current = null
    }
  }, [labels, values, color])

  return <div ref={ref} className="w-full" style={{ height: '120px' }} />
}

export function DashboardCharts({ data }: DashboardChartsProps) {
  const { t } = useTranslation()

  // Prepare data - extract from API response
  const signups = data.user_signups || []
  const volumes = data.volume_hourly || []
  const tokens = data.token_distribution || []
  const withdrawals = data.withdrawal_statuses || []

  const signupData = {
    labels: signups.map((s) => s.day),
    values: signups.map((s) => s.count),
  }
  const volumeData = {
    labels: volumes.map((v) => v.hour),
    values: volumes.map((v) => v.volume),
  }
  const tokensData = {
    labels: tokens.map((t) => t.asset),
    values: tokens.map((t) => t.holders),
  }
  const withdrawalsData = {
    labels: withdrawals.map((w) => w.status),
    values: withdrawals.map((w) => w.count),
  }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
      {/* Signups */}
      <div className="bg-gray-800 p-4 rounded">
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-semibold">{t('dashboardCharts.signups')}</h3>
          <span className="text-xs text-gray-400">
            {signupData.values.reduce((a: number, b: number) => a + b, 0)} {t('dashboardCharts.total')}
          </span>
        </div>
        {signupData.labels.length > 0 ? (
          <MiniChart values={signupData.values} dates={signupData.labels} color="#3B82F6" suffix="" />
        ) : (
          <div className="h-[120px] flex items-center justify-center text-gray-500">
            {t('dashboardCharts.noSignupData')}
          </div>
        )}
      </div>

      {/* Volume */}
      <div className="bg-gray-800 p-4 rounded">
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-semibold">{t('dashboardCharts.volume')}</h3>
          <span className="text-xs text-gray-400">
            {volumeData.values.reduce((a: number, b: number) => a + b, 0).toFixed(0)} USDT
          </span>
        </div>
        {volumeData.labels.length > 0 ? (
          <MiniChart values={volumeData.values} dates={volumeData.labels} color="#10B981" suffix="" />
        ) : (
          <div className="h-[120px] flex items-center justify-center text-gray-500">
            {t('dashboardCharts.noVolumeData')}
          </div>
        )}
      </div>

      {/* Tokens */}
      <div className="bg-gray-800 p-4 rounded">
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-semibold">{t('dashboardCharts.tokens')}</h3>
          <span className="text-xs text-gray-400">
            {tokensData.values.reduce((a: number, b: number) => a + b, 0)} {t('dashboardCharts.total')}
          </span>
        </div>
        {tokensData.labels.length > 0 ? (
          <BarChartMini labels={tokensData.labels} values={tokensData.values} color="#F59E0B" />
        ) : (
          <div className="h-[120px] flex items-center justify-center text-gray-500">
            {t('dashboardCharts.noTokenData')}
          </div>
        )}
      </div>

      {/* Withdrawals */}
      <div className="bg-gray-800 p-4 rounded">
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-semibold">{t('dashboardCharts.withdrawals')}</h3>
          <span className="text-xs text-gray-400">
            {withdrawalsData.values.reduce((a: number, b: number) => a + b, 0).toFixed(0)} USDT
          </span>
        </div>
        {withdrawalsData.labels.length > 0 ? (
          <MiniChart values={withdrawalsData.values} dates={Array.from({length: withdrawalsData.values.length}, (_, i) => Math.floor(Date.now() / 1000) - (withdrawalsData.values.length - i) * 86400)} color="#EF4444" suffix="" />
        ) : (
          <div className="h-[120px] flex items-center justify-center text-gray-500">
            {t('dashboardCharts.noWithdrawalData')}
          </div>
        )}
      </div>
    </div>
  )
}
