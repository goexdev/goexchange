import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import * as api from '../lib/api'

interface DepthChartProps {
  orderbook: api.OrderBook
  precision?: number  // price precision
}

interface DepthPoint {
  price: number
  cumulative: number
}

/**
 * Depth chart - shows cumulative bid/ask volume at each price level.
 * Bids (green) on left, asks (red) on right, mid price in center.
 * Updates in real-time as orderbook WS data changes.
 */
export function DepthChart({ orderbook, precision = 2 }: DepthChartProps) {
  const { t } = useTranslation()

  const data = useMemo(() => {
    // Parse bids (descending) and asks (ascending)
    const bids: DepthPoint[] = []
    const asks: DepthPoint[] = []

    let bidCum = 0
    for (const b of orderbook.bids) {
      const qty = parseFloat(b.quantity)
      if (qty > 0) {
        bidCum += qty
        bids.push({ price: parseFloat(b.price), cumulative: bidCum })
      }
    }

    let askCum = 0
    for (const a of orderbook.asks) {
      const qty = parseFloat(a.quantity)
      if (qty > 0) {
        askCum += qty
        asks.push({ price: parseFloat(a.price), cumulative: askCum })
      }
    }

    return { bids, asks }
  }, [orderbook])

  // Show partial chart if at least one side has data
  const hasBids = data.bids.length > 0
  const hasAsks = data.asks.length > 0
  if (!hasBids && !hasAsks) {
    return (
      <div className="h-32 flex items-center justify-center text-gray-500 text-xs">
        {t('depthChart.noData')}
      </div>
    )
  }
  // For single-sided books, use the available side for mid price calculation
  const bestBid = hasBids ? data.bids[0].price : data.asks[0].price
  const bestAsk = hasAsks ? data.asks[0].price : data.bids[0].price
  const midPrice = (bestBid + bestAsk) / 2
  const spread = bestAsk - bestBid
  const spreadPct = (spread / midPrice) * 100

  // Find price range to display
  // Use actual data range with padding
  const minBid = hasBids ? data.bids[data.bids.length - 1].price : bestBid
  const maxAsk = hasAsks ? data.asks[data.asks.length - 1].price : bestAsk
  const priceRange = maxAsk - minBid
  const rangePadding = Math.max(priceRange * 0.1, midPrice * 0.05)
  const minPrice = Math.max(0, minBid - rangePadding)
  const maxPrice = maxAsk + rangePadding

  // Filter bids/asks in range (single-sided book handled gracefully)
  const bidsInRange = hasBids ? data.bids.filter(b => b.price >= minPrice) : []
  const asksInRange = hasAsks ? data.asks.filter(a => a.price <= maxPrice) : []

  // Max cumulative for scaling
  const maxCum = Math.max(
    bidsInRange[bidsInRange.length - 1]?.cumulative || 0,
    asksInRange[asksInRange.length - 1]?.cumulative || 0
  )

  if (maxCum === 0) {
    return (
      <div className="h-32 flex items-center justify-center text-gray-500 text-xs">
        {t('depthChart.noData')}
      </div>
    )
  }

  // SVG dimensions
  const width = 600
  const height = 100
  const padding = 4
  const chartWidth = width - padding * 2
  const chartHeight = height - padding * 2

  // Build step path for bids (descending price)
  // Convert each point: x = price position, y = cumulative position (inverted)
  const buildBidPath = (points: DepthPoint[]): string => {
    if (points.length === 0) return ''
    const path: string[] = []
    // Start at right edge (best bid)
    const first = points[0]
    const x0 = ((first.price - minPrice) / (maxPrice - minPrice)) * chartWidth + padding
    const y0 = padding + chartHeight
    path.push(`M ${x0} ${y0}`)
    // For each point, draw step to current y
    for (const p of points) {
      const x = ((p.price - minPrice) / (maxPrice - minPrice)) * chartWidth + padding
      const y = padding + chartHeight - (p.cumulative / maxCum) * chartHeight
      path.push(`L ${x} ${y}`)
      // Step: move x to next point at same y
      const nextIdx = points.indexOf(p) + 1
      if (nextIdx < points.length) {
        const nextX = ((points[nextIdx].price - minPrice) / (maxPrice - minPrice)) * chartWidth + padding
        path.push(`L ${nextX} ${y}`)
      } else {
        // Close to left edge
        path.push(`L ${padding} ${y}`)
        path.push(`L ${padding} ${y0}`)
      }
    }
    return path.join(' ')
  }

  const buildAskPath = (points: DepthPoint[]): string => {
    if (points.length === 0) return ''
    const path: string[] = []
    // Start at left edge (best ask)
    const first = points[0]
    const x0 = ((first.price - minPrice) / (maxPrice - minPrice)) * chartWidth + padding
    const y0 = padding + chartHeight
    path.push(`M ${x0} ${y0}`)
    for (const p of points) {
      const x = ((p.price - minPrice) / (maxPrice - minPrice)) * chartWidth + padding
      const y = padding + chartHeight - (p.cumulative / maxCum) * chartHeight
      path.push(`L ${x} ${y}`)
      const nextIdx = points.indexOf(p) + 1
      if (nextIdx < points.length) {
        const nextX = ((points[nextIdx].price - minPrice) / (maxPrice - minPrice)) * chartWidth + padding
        path.push(`L ${nextX} ${y}`)
      } else {
        // Close to right edge
        path.push(`L ${chartWidth + padding} ${y}`)
        path.push(`L ${chartWidth + padding} ${y0}`)
      }
    }
    return path.join(' ')
  }

  const bidPath = buildBidPath(bidsInRange)
  const askPath = buildAskPath(asksInRange)
  const bidArea = bidPath + ` L ${padding} ${padding + chartHeight} Z`
  const askArea = askPath + ` L ${chartWidth + padding} ${padding + chartHeight} Z`

  // Mid price line position
  const midX = ((midPrice - minPrice) / (maxPrice - minPrice)) * chartWidth + padding

  // Price tick marks
  const ticks = 4
  const tickPositions = Array.from({ length: ticks + 1 }, (_, i) => minPrice + (maxPrice - minPrice) * (i / ticks))

  return (
    <div className="w-full">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        className="w-full"
        style={{ height: '100px' }}
      >
        {/* Gradient definitions */}
        <defs>
          <linearGradient id="bidGradient" x1="0%" y1="0%" x2="0%" y2="100%">
            <stop offset="0%" stopColor="rgba(16, 185, 129, 0.6)" />
            <stop offset="100%" stopColor="rgba(16, 185, 129, 0.05)" />
          </linearGradient>
          <linearGradient id="askGradient" x1="0%" y1="0%" x2="0%" y2="100%">
            <stop offset="0%" stopColor="rgba(239, 68, 68, 0.6)" />
            <stop offset="100%" stopColor="rgba(239, 68, 68, 0.05)" />
          </linearGradient>
        </defs>

        {/* Bid area (left, green) */}
        <path d={bidArea} fill="url(#bidGradient)" />
        {/* Bid line */}
        <path d={bidPath} fill="none" stroke="#10B981" strokeWidth="1.5" />

        {/* Ask area (right, red) */}
        <path d={askArea} fill="url(#askGradient)" />
        {/* Ask line */}
        <path d={askPath} fill="none" stroke="#EF4444" strokeWidth="1.5" />

        {/* Mid price line */}
        <line
          x1={midX} y1={padding}
          x2={midX} y2={padding + chartHeight}
          stroke="#6B7280"
          strokeWidth="1"
          strokeDasharray="3,2"
        />

        {/* Mid price label */}
        <text
          x={midX}
          y={padding + 8}
          fill="#9CA3AF"
          fontSize="9"
          textAnchor="middle"
          className="font-mono"
        >
          {midPrice.toFixed(precision)}
        </text>
      </svg>

      {/* Stats below */}
      <div className="grid grid-cols-3 text-xs mt-1 px-1">
        <div>
          <div className="text-green-400 font-mono">{bestBid.toFixed(precision)}</div>
          <div className="text-gray-500 text-[10px]">{t('depthChart.bid')}</div>
        </div>
        <div className="text-center">
          <div className="text-yellow-400 font-mono">{spread.toFixed(precision)}</div>
          <div className="text-gray-500 text-[10px]">{t('depthChart.spread')} ({spreadPct.toFixed(3)}{t('depthChart.percent')})</div>
        </div>
        <div className="text-right">
          <div className="text-red-400 font-mono">{bestAsk.toFixed(precision)}</div>
          <div className="text-gray-500 text-[10px]">{t('depthChart.ask')}</div>
        </div>
      </div>

      {/* Volume info */}
      <div className="grid grid-cols-2 text-xs mt-1 px-1 text-gray-500">
        <div>
          <span className="text-green-400">▮</span> {t('depthChart.bidVol')}: <span className="text-gray-300 font-mono">{(bidsInRange[bidsInRange.length - 1]?.cumulative || 0).toFixed(2)}</span>
        </div>
        <div className="text-right">
          <span className="text-red-400">▮</span> {t('depthChart.askVol')}: <span className="text-gray-300 font-mono">{(asksInRange[asksInRange.length - 1]?.cumulative || 0).toFixed(2)}</span>
        </div>
      </div>
    </div>
  )
}
