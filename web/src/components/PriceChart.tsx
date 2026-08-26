import { useEffect, useState, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import {
  createChart,
  CandlestickSeries,
  HistogramSeries,
  LineSeries,
  type IChartApi,
  type ISeriesApi,
  type CandlestickData,
  type HistogramData,
  type LineData,
  type Time,
} from 'lightweight-charts'
import * as api from '../lib/api'

// ============================================================
// TradingView Lightweight Charts (Apache 2.0)
// Free, open-source financial charting library from TradingView
// Used by Binance, Kraken, OKX, Bybit, etc.
// ============================================================

interface PriceChartProps {
  base: string
  quote: string
}

// Single timeframe + interval selector. Each option combines:
// - Candle size (interval seconds)
// - Default duration (how much history to load)
// Renamed: 1D -> 1d, 1W -> 1w, 1M -> 1M for consistency
type Interval = '1m' | '5m' | '15m' | '1h' | '1d' | '1w' | '1M'

interface IntervalConfig {
  label: string         // button text
  seconds: number       // candle size in seconds
  durationMs: number    // how much history to load
}

const INTERVALS: Record<Interval, IntervalConfig> = {
  '1m':  { label: '1m',  seconds: 60,            durationMs: 24 * 3600 * 1000 },
  '5m':  { label: '5m',  seconds: 300,           durationMs: 24 * 3600 * 1000 },
  '15m': { label: '15m', seconds: 900,           durationMs: 24 * 3600 * 1000 },
  '1h':  { label: '1h',  seconds: 3600,          durationMs: 7 * 24 * 3600 * 1000 },
  '1d':  { label: '1d',  seconds: 86400,         durationMs: 30 * 24 * 3600 * 1000 },
  '1w':  { label: '1w',  seconds: 7 * 86400,     durationMs: 180 * 24 * 3600 * 1000 },
  '1M':  { label: '1M',  seconds: 30 * 86400,    durationMs: 1825 * 24 * 3600 * 1000 },  // 5 years
}

export function PriceChart({ base, quote }: PriceChartProps) {
  const { t } = useTranslation()
  const [candles, setCandles] = useState<api.Candle[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')

  const [interval, setInterval] = useState<Interval>('5m')
  const [showMA, setShowMA] = useState(true)
  const [showEMA, setShowEMA] = useState(true)
  const [showVolume, setShowVolume] = useState(true)
  const [showRSI, setShowRSI] = useState(false)
  const [showBB, setShowBB] = useState(false)

  const chartContainerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const candleSeriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const volumeSeriesRef = useRef<ISeriesApi<'Histogram'> | null>(null)
  const maSeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const emaSeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const rsiSeriesRef = useRef<ISeriesApi<'Line'> | null>(null)
  const bbUpperRef = useRef<ISeriesApi<'Line'> | null>(null)
  const bbMiddleRef = useRef<ISeriesApi<'Line'> | null>(null)
  const bbLowerRef = useRef<ISeriesApi<'Line'> | null>(null)
  // Track if user has scrolled the chart away from the latest position.
  // When false (default), new candles auto-scroll to keep latest visible.
  // When true, user has panned/zoomed and we respect their view.
  const userScrolledRef = useRef(false)
  const [showLatestButton, setShowLatestButton] = useState(false)
  // Ref to track latest candles count for the visible-range subscription
  const candlesRef = useRef<api.Candle[]>([])
  // Track earliest candle time to know where to fetch from
  const earliestCandleRef = useRef<number | null>(null)
  // Flag to prevent duplicate historical fetches
  const fetchingHistoricalRef = useRef(false)
  const [historicalLoading, setHistoricalLoading] = useState(false)

  // Keep candlesRef in sync with state for the visible-range subscription
  useEffect(() => {
    candlesRef.current = candles
  }, [candles])

  // Fetch initial candles (most recent based on timeframe)
  async function loadCandles() {
    if (!base || !quote) return
    setLoading(true)
    setErr('')
    try {
      const cfg = INTERVALS[interval]
      const toMs = Date.now()
      const fromMs = toMs - cfg.durationMs
      const data = await api.getCandles(base, quote, cfg.seconds, fromMs, toMs)
      const newCandles = data.candles || []
      setCandles(newCandles)
      if (newCandles.length > 0) {
        earliestCandleRef.current = newCandles[0].time
      }
    } catch (e: any) {
      setErr(e.message)
      setCandles([])
    } finally {
      setLoading(false)
    }
  }

  // Fetch older historical candles (called when user scrolls to the left edge)
  async function fetchHistoricalCandles() {
    if (fetchingHistoricalRef.current) return
    if (!earliestCandleRef.current) return
    const cfg = INTERVALS[interval]
    fetchingHistoricalRef.current = true
    setHistoricalLoading(true)
    try {
      // Fetch the same duration BEFORE the current earliest candle
      const toMs = earliestCandleRef.current * 1000 - 1
      const fromMs = toMs - cfg.durationMs
      const data = await api.getCandles(base, quote, cfg.seconds, fromMs, toMs)
      const olderCandles = data.candles || []
      if (olderCandles.length > 0) {
        setCandles(prev => {
          // Avoid duplicates by time
          const existingTimes = new Set(prev.map(c => c.time))
          const unique = olderCandles.filter(c => !existingTimes.has(c.time))
          const combined = [...unique, ...prev]
          if (combined.length > 0) {
            earliestCandleRef.current = combined[0].time
          }
          return combined
        })
      }
    } catch (e: any) {
      // Silently fail for historical loads
      console.warn('historical fetch failed:', e.message)
    } finally {
      fetchingHistoricalRef.current = false
      setHistoricalLoading(false)
    }
  }

  // Recompute indicators from candles
  // Includes: MA 7, EMA 50, RSI 14, Bollinger Bands (20, 2)
  const indicators = useMemo(() => {
    if (candles.length === 0) return { ma: [], ema: [], rsi: [], bbUpper: [], bbMiddle: [], bbLower: [] }
    const closes = candles.map(c => parseFloat(c.close))

    // MA 7 (Simple Moving Average)
    const ma7: number[] = []
    const ma7Period = 7
    for (let i = 0; i < closes.length; i++) {
      if (i < ma7Period - 1) {
        ma7.push(NaN)
      } else {
        let sum = 0
        for (let j = i - ma7Period + 1; j <= i; j++) sum += closes[j]
        ma7.push(sum / ma7Period)
      }
    }

    // EMA 50 (Exponential Moving Average)
    const emaPeriod = 50
    const k = 2 / (emaPeriod + 1)
    const ema50: number[] = []
    let prev = closes[0]
    ema50.push(prev)
    for (let i = 1; i < closes.length; i++) {
      const val = closes[i] * k + prev * (1 - k)
      ema50.push(val)
      prev = val
    }

    // RSI 14 (Relative Strength Index)
    // RSI = 100 - (100 / (1 + RS))
    // RS = average gain / average loss over period
    const rsiPeriod = 14
    const rsi: number[] = []
    // First RSI value needs at least rsiPeriod+1 closes for change calculation
    if (closes.length < rsiPeriod + 1) {
      return { ma: [ma7], ema: [ema50], rsi: [], bbUpper: [], bbMiddle: [], bbLower: [] }
    }
    // Calculate price changes
    const changes: number[] = []
    for (let i = 1; i < closes.length; i++) {
      changes.push(closes[i] - closes[i - 1])
    }
    // Initial average gain/loss
    let avgGain = 0
    let avgLoss = 0
    for (let i = 0; i < rsiPeriod; i++) {
      if (changes[i] > 0) avgGain += changes[i]
      else avgLoss += Math.abs(changes[i])
    }
    avgGain /= rsiPeriod
    avgLoss /= rsiPeriod
    // First RSI
    const firstRs = avgLoss === 0 ? 100 : avgGain / avgLoss
    rsi.push(NaN) // index 0 has no RSI (no previous close)
    // Push NaN for indices 1 to rsiPeriod-1
    for (let i = 1; i < rsiPeriod; i++) rsi.push(NaN)
    rsi.push(avgLoss === 0 ? 100 : 100 - 100 / (1 + firstRs))
    // Subsequent RSI values use smoothed averages
    for (let i = rsiPeriod + 1; i < closes.length; i++) {
      const change = changes[i - 1]
      const gain = change > 0 ? change : 0
      const loss = change < 0 ? Math.abs(change) : 0
      avgGain = (avgGain * (rsiPeriod - 1) + gain) / rsiPeriod
      avgLoss = (avgLoss * (rsiPeriod - 1) + loss) / rsiPeriod
      const rs = avgLoss === 0 ? 100 : avgGain / avgLoss
      rsi.push(100 - 100 / (1 + rs))
    }

    // Bollinger Bands (20-period, 2 standard deviations)
    const bbPeriod = 20
    const bbStdDev = 2
    const bbMiddle: number[] = []  // SMA
    const bbUpper: number[] = []   // SMA + 2*stddev
    const bbLower: number[] = []   // SMA - 2*stddev
    for (let i = 0; i < closes.length; i++) {
      if (i < bbPeriod - 1) {
        bbMiddle.push(NaN)
        bbUpper.push(NaN)
        bbLower.push(NaN)
      } else {
        // Calculate SMA
        let sum = 0
        for (let j = i - bbPeriod + 1; j <= i; j++) sum += closes[j]
        const sma = sum / bbPeriod
        // Calculate standard deviation
        let variance = 0
        for (let j = i - bbPeriod + 1; j <= i; j++) {
          variance += Math.pow(closes[j] - sma, 2)
        }
        const stddev = Math.sqrt(variance / bbPeriod)
        bbMiddle.push(sma)
        bbUpper.push(sma + bbStdDev * stddev)
        bbLower.push(sma - bbStdDev * stddev)
      }
    }

    return { ma: [ma7], ema: [ema50], rsi, bbUpper, bbMiddle, bbLower }
  }, [candles])

  // Initialize chart once
  useEffect(() => {
    if (!chartContainerRef.current) return

    const chart = createChart(chartContainerRef.current, {
      width: chartContainerRef.current.clientWidth,
      height: 400,
      layout: {
        background: { color: 'transparent' },
        textColor: '#9CA3AF',
        fontSize: 12,
        fontFamily: 'inherit',
      },
      grid: {
        vertLines: { color: 'rgba(75, 85, 99, 0.2)' },
        horzLines: { color: 'rgba(75, 85, 99, 0.2)' },
      },
      timeScale: {
        timeVisible: true,
        secondsVisible: interval === '1m',
        borderColor: '#374151',
      },
      rightPriceScale: {
        borderColor: '#374151',
        scaleMargins: {
          top: 0.1,
          bottom: showVolume ? 0.25 : 0.1,
        },
      },
      crosshair: {
        mode: 1,
        vertLine: {
          width: 1,
          color: '#6B7280',
          style: 3,
        },
        horzLine: {
          width: 1,
          color: '#6B7280',
          style: 3,
        },
      },
      handleScroll: {
        mouseWheel: true,
        pressedMouseMove: true,
        horzTouchDrag: true,
        vertTouchDrag: true,
      },
      handleScale: {
        axisPressedMouseMove: true,
        mouseWheel: true,
        pinch: true,
      },
    })

    chartRef.current = chart

    // Subscribe to visible logical range changes to detect user scrolling away
    const handleVisibleTimeRangeChange = () => {
      if (!chartRef.current) return
      const ts = chartRef.current.timeScale()
      const visible = ts.getVisibleLogicalRange()
      if (!visible) return
      const logicalLen = candlesRef.current.length
      if (logicalLen === 0) return
      // distance from latest candle
      // If visible.to < logicalLen - 1, user has scrolled away from latest
      const distFromLatest = logicalLen - visible.to
      if (distFromLatest >= 0.5) {
        if (!userScrolledRef.current) {
          userScrolledRef.current = true
          setShowLatestButton(true)
        }
      } else {
        if (userScrolledRef.current) {
          userScrolledRef.current = false
          setShowLatestButton(false)
        }
      }
      // If user is at the LEFT edge (near historical data), fetch more
      // visible.from <= 5 means within 5 bars of leftmost candle
      if (visible.from <= 5 && !fetchingHistoricalRef.current && earliestCandleRef.current) {
        fetchHistoricalCandles()
      }
    }
    chart.timeScale().subscribeVisibleLogicalRangeChange(handleVisibleTimeRangeChange)

    // Resize observer
    const resizeObserver = new ResizeObserver(entries => {
      if (entries.length === 0 || !chartRef.current) return
      const { width } = entries[0].contentRect
      chartRef.current.applyOptions({ width })
    })
    resizeObserver.observe(chartContainerRef.current)

    return () => {
      chart.timeScale().unsubscribeVisibleLogicalRangeChange(handleVisibleTimeRangeChange)
      resizeObserver.disconnect()
      chart.remove()
      chartRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Bucket time helper - groups trades into candle intervals
  function getBucketTime(ts: number): number {
    const intervalSec = INTERVALS[interval].seconds
    return Math.floor(ts / intervalSec) * intervalSec
  }

  // Real-time updates via WS trade events
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail
      if (!detail || !detail.pair) return
      const expectedPair = `${base}_${quote}`
      if (detail.pair !== expectedPair) return
      const trade = {
        price: parseFloat(detail.price),
        quantity: parseFloat(detail.quantity || detail.qty || 0),
        timestamp: new Date(detail.timestamp || Date.now()).getTime() / 1000,
      }
      if (isNaN(trade.price) || trade.price <= 0) return
      // Use setCandles functional form to get latest state
      setCandles(prev => {
        if (prev.length === 0) return prev
        const intervalSec = INTERVALS[interval].seconds
        const tradeBucket = Math.floor(trade.timestamp / intervalSec) * intervalSec
        const latest = prev[prev.length - 1]
        const latestBucket = latest.time
        if (tradeBucket < latestBucket) return prev

        const close = trade.price
        const high = Math.max(parseFloat(latest.high), close)
        const low = Math.min(parseFloat(latest.low), close)
        const volume = (parseFloat(latest.volume) + trade.quantity).toString()

        if (tradeBucket > latestBucket) {
          // New candle in a new bucket
          const newCandle: api.Candle = {
            time: tradeBucket,
            open: close.toString(),
            high: close.toString(),
            low: close.toString(),
            close: close.toString(),
            volume: trade.quantity.toString(),
          }
          return [...prev, newCandle]
        }

        // Update existing latest candle in place
        const updated: api.Candle = {
          ...latest,
          high: high.toString(),
          low: low.toString(),
          close: close.toString(),
          volume,
        }
        return [...prev.slice(0, -1), updated]
      })
    }
    window.addEventListener('ws-trade', handler)
    return () => window.removeEventListener('ws-trade', handler)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [base, quote, interval])

  // Update series when candles or settings change
  useEffect(() => {
    const chart = chartRef.current
    if (!chart || candles.length === 0) return

    // Convert candles to lightweight-charts format
    const candleData: CandlestickData[] = candles.map(c => ({
      time: c.time as Time,
      open: parseFloat(c.open),
      high: parseFloat(c.high),
      low: parseFloat(c.low),
      close: parseFloat(c.close),
    }))

    // Remove old series if they exist
    if (candleSeriesRef.current) {
      chart.removeSeries(candleSeriesRef.current)
      candleSeriesRef.current = null
    }
    if (volumeSeriesRef.current) {
      chart.removeSeries(volumeSeriesRef.current)
      volumeSeriesRef.current = null
    }
    if (maSeriesRef.current) {
      chart.removeSeries(maSeriesRef.current)
      maSeriesRef.current = null
    }
    if (emaSeriesRef.current) {
      chart.removeSeries(emaSeriesRef.current)
      emaSeriesRef.current = null
    }
    if (rsiSeriesRef.current) {
      chart.removeSeries(rsiSeriesRef.current)
      rsiSeriesRef.current = null
    }
    if (bbUpperRef.current) {
      chart.removeSeries(bbUpperRef.current)
      bbUpperRef.current = null
    }
    if (bbMiddleRef.current) {
      chart.removeSeries(bbMiddleRef.current)
      bbMiddleRef.current = null
    }
    if (bbLowerRef.current) {
      chart.removeSeries(bbLowerRef.current)
      bbLowerRef.current = null
    }

    // Create candlestick series
    const candleSeries = chart.addSeries(CandlestickSeries, {
      upColor: '#10B981',
      downColor: '#EF4444',
      borderUpColor: '#10B981',
      borderDownColor: '#EF4444',
      wickUpColor: '#10B981',
      wickDownColor: '#EF4444',
    })
    candleSeries.setData(candleData)
    candleSeriesRef.current = candleSeries

    // Create volume histogram
    if (showVolume) {
      const volumeSeries = chart.addSeries(HistogramSeries, {
        color: '#26a69a',
        priceFormat: { type: 'volume' },
        priceScaleId: '',
      })
      volumeSeries.priceScale().applyOptions({
        scaleMargins: {
          top: 0.8,
          bottom: 0,
        },
      })
      const volumeData: HistogramData[] = candles.map(c => ({
        time: c.time as Time,
        value: parseFloat(c.volume),
        color: parseFloat(c.close) >= parseFloat(c.open)
          ? 'rgba(16, 185, 129, 0.4)'
          : 'rgba(239, 68, 68, 0.4)',
      }))
      volumeSeries.setData(volumeData)
      volumeSeriesRef.current = volumeSeries
    }

    // Create MA lines
    if (showMA) {
      const colors = ['#3B82F6', '#F59E0B', '#EC4899'] // blue, amber, pink
      const maColors = [
        { line: colors[0], title: t('priceChart.ma7') },
        { line: colors[1], title: t('priceChart.ma25') },
        { line: colors[2], title: t('priceChart.ma99') },
      ]
      const periods = [7, 25, 99]
      // Show first MA only to avoid clutter
      const maData: LineData[] = indicators.ma[0]
        .map((v, i) => ({ time: candles[i].time as Time, value: v }))
        .filter(d => !isNaN(d.value))
      const maSeries = chart.addSeries(LineSeries, {
        color: maColors[0].line,
        lineWidth: 1,
        title: maColors[0].title,
      })
      maSeries.setData(maData)
      maSeriesRef.current = maSeries
    }

    // Create EMA line
    if (showEMA) {
      const emaData: LineData[] = indicators.ema[0]
        .map((v, i) => ({ time: candles[i].time as Time, value: v }))
        .filter(d => !isNaN(d.value))
      const emaSeries = chart.addSeries(LineSeries, {
        color: '#A78BFA', // violet
        lineWidth: 1,
        lineStyle: 2, // dashed
        title: t('priceChart.ema50'),
      })
      emaSeries.setData(emaData)
      emaSeriesRef.current = emaSeries
    }

    // Create RSI line (separate pane with 30/70 reference lines)
    if (showRSI && indicators.rsi.length > 0) {
      const rsiData: LineData[] = indicators.rsi
        .map((v, i) => ({ time: candles[i].time as Time, value: v }))
        .filter(d => !isNaN(d.value))
      const rsiSeries = chart.addSeries(LineSeries, {
        color: '#F59E0B',
        lineWidth: 1,
        title: t('priceChart.rsi14'),
        priceScaleId: 'rsi',
      })
      rsiSeries.priceScale().applyOptions({
        scaleMargins: { top: 0.8, bottom: 0 },
      })
      rsiSeries.createPriceLine({
        price: 70,
        color: 'rgba(239, 68, 68, 0.5)',
        lineWidth: 1,
        lineStyle: 2,
        axisLabelVisible: true,
        title: '70',
      })
      rsiSeries.createPriceLine({
        price: 30,
        color: 'rgba(16, 185, 129, 0.5)',
        lineWidth: 1,
        lineStyle: 2,
        axisLabelVisible: true,
        title: '30',
      })
      rsiSeries.setData(rsiData)
      rsiSeriesRef.current = rsiSeries
    }

    // Create Bollinger Bands (3 lines: upper, middle SMA20, lower)
    if (showBB && indicators.bbUpper.length > 0) {
      const bbUpperData: LineData[] = indicators.bbUpper
        .map((v, i) => ({ time: candles[i].time as Time, value: v }))
        .filter(d => !isNaN(d.value))
      const bbMiddleData: LineData[] = indicators.bbMiddle
        .map((v, i) => ({ time: candles[i].time as Time, value: v }))
        .filter(d => !isNaN(d.value))
      const bbLowerData: LineData[] = indicators.bbLower
        .map((v, i) => ({ time: candles[i].time as Time, value: v }))
        .filter(d => !isNaN(d.value))
      const bbUpSeries = chart.addSeries(LineSeries, {
        color: '#6366F1',
        lineWidth: 1,
        title: t('priceChart.bbUpper'),
      })
      bbUpSeries.setData(bbUpperData)
      bbUpperRef.current = bbUpSeries
      const bbMidSeries = chart.addSeries(LineSeries, {
        color: '#8B5CF6',
        lineWidth: 1,
        lineStyle: 2,
        title: t('priceChart.bbMiddle'),
      })
      bbMidSeries.setData(bbMiddleData)
      bbMiddleRef.current = bbMidSeries
      const bbLoSeries = chart.addSeries(LineSeries, {
        color: '#6366F1',
        lineWidth: 1,
        title: t('priceChart.bbLower'),
      })
      bbLoSeries.setData(bbLowerData)
      bbLowerRef.current = bbLoSeries
    }

    // Fit content
    chart.timeScale().fitContent()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [candles, showMA, showEMA, showVolume])

  // Reload on interval change
  useEffect(() => {
    loadCandles()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [base, quote, interval])

  const lastCandle = candles[candles.length - 1]
  const lastPrice = lastCandle ? parseFloat(lastCandle.close) : null

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-3 flex-wrap gap-2">
        <div className="flex items-center gap-2">
          <span className="text-sm text-gray-400">{t('priceChart.intervalLabel')}:</span>
          {(Object.keys(INTERVALS) as Interval[]).map(iv => (
            <button
              key={iv}
              onClick={() => setInterval(iv)}
              className={`px-2 py-1 text-xs rounded transition-colors ${
                interval === iv
                  ? 'bg-blue-500 text-white'
                  : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
              }`}
            >
              {INTERVALS[iv].label}
            </button>
          ))}
        </div>

        {/* Indicator toggles */}
        <div className="flex items-center gap-2 text-xs">
          <span className="text-gray-500">{t('priceChart.indicators')}:</span>
          <button
            onClick={() => setShowMA(!showMA)}
            className={`px-2 py-1 rounded ${showMA ? 'bg-blue-900 text-blue-300' : 'bg-gray-800 text-gray-500'}`}
          >
            MA
          </button>
          <button
            onClick={() => setShowEMA(!showEMA)}
            className={`px-2 py-1 rounded ${showEMA ? 'bg-purple-900 text-purple-300' : 'bg-gray-800 text-gray-500'}`}
          >
            EMA
          </button>
          <button
            onClick={() => setShowBB(!showBB)}
            className={`px-2 py-1 rounded ${showBB ? 'bg-indigo-900 text-indigo-300' : 'bg-gray-800 text-gray-500'}`}
          >
            BB
          </button>
          <button
            onClick={() => setShowRSI(!showRSI)}
            className={`px-2 py-1 rounded ${showRSI ? 'bg-amber-900 text-amber-300' : 'bg-gray-800 text-gray-500'}`}
          >
            RSI
          </button>
          <button
            onClick={() => setShowVolume(!showVolume)}
            className={`px-2 py-1 rounded ${showVolume ? 'bg-green-900 text-green-300' : 'bg-gray-800 text-gray-500'}`}
          >
            {t('priceChart.volume')}
          </button>
        </div>
      </div>

      {/* Chart container */}
      {err && (
        <div className="bg-red-900/30 border border-red-500/30 p-3 rounded mb-3 text-sm text-red-300">
          {err}
        </div>
      )}

      <div className="relative">
        {/* Chart container - always rendered so the ref is set and createChart can run */}
        <div
          ref={chartContainerRef}
          className="w-full"
          style={{ height: '400px' }}
        />

        {/* Loading / no-data overlay */}
        {loading && candles.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center text-gray-400 bg-gray-900/50 pointer-events-none">
            {t('priceChart.loadingData')}
          </div>
        )}
        {!loading && candles.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center text-gray-400 bg-gray-900/50 pointer-events-none">
            {t('priceChart.noData')}
          </div>
        )}

        {/* Last price badge */}
        {lastPrice !== null && (
          <div className="absolute top-2 left-2 z-10 bg-gray-800/80 px-3 py-1 rounded text-sm">
            <span className="text-gray-400">{base}/{quote} </span>
            <span className="font-mono font-semibold text-white">
              {lastPrice.toFixed(2)}
            </span>
          </div>
        )}

        {/* Historical loading indicator */}
        {historicalLoading && (
          <div className="absolute top-2 left-1/2 -translate-x-1/2 z-20 px-3 py-1 bg-gray-800/80 text-gray-300 text-xs rounded shadow-lg flex items-center gap-2">
            <span className="inline-block animate-spin h-3 w-3 border-2 border-blue-500 border-t-transparent rounded-full" />
            {t('priceChart.loadingHistory')}
          </div>
        )}

        {/* Latest button - shown when user has scrolled away from latest */}
        {showLatestButton && (
          <button
            onClick={() => {
              if (chartRef.current) {
                chartRef.current.timeScale().scrollToPosition(0, true)
                userScrolledRef.current = false
                setShowLatestButton(false)
              }
            }}
            className="absolute top-2 right-2 z-20 px-3 py-1 bg-blue-600 hover:bg-blue-500 text-white text-xs rounded shadow-lg flex items-center gap-1"
          >
            ↓ {t('priceChart.latest')}
          </button>
        )}

        {/* Watermark - required by Apache 2.0 */}
        <div className="absolute bottom-1 right-2 text-[10px] text-gray-600 pointer-events-none">
          {t('priceChart.poweredBy')} <a href="https://tradingview.com/" target="_blank" rel="noopener noreferrer" className="hover:text-gray-400">TradingView</a>
        </div>
      </div>
    </div>
  )
}
