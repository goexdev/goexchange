import { useMemo } from 'react'

interface SparklineProps {
  data: number[]
  width?: number
  height?: number
  color?: string
  fill?: boolean
}

/**
 * Sparkline is a tiny SVG-based line chart for showing trends.
 * Pure SVG, no external charting library needed.
 *
 * Uses viewBox so it scales nicely in containers.
 */
export function Sparkline({
  data,
  width = 100,
  height = 30,
  color = '#3b82f6',  // blue-500
  fill = true,
}: SparklineProps) {
  const path = useMemo(() => {
    if (!data || data.length < 2) return ''

    const min = Math.min(...data)
    const max = Math.max(...data)
    const range = max - min || 1

    const points = data.map((v, i) => {
      const x = (i / (data.length - 1)) * 100
      const y = 100 - ((v - min) / range) * 100
      return `${x},${y}`
    })

    if (!fill) return `M ${points.join(' L ')}`

    const linePath = `M ${points.join(' L ')}`
    const fillPath = `${linePath} L 100,100 L 0,100 Z`
    return { line: linePath, fill: fillPath }
  }, [data, fill])

  if (!path) return null

  const trend = data && data.length >= 2
    ? (data[data.length - 1] >= data[0] ? 'up' : 'down')
    : 'neutral'

  const strokeColor = trend === 'down' ? '#ef4444' : trend === 'up' ? '#10b981' : color
  const fillColor = trend === 'down' ? '#7f1d1d' : trend === 'up' ? '#064e3b' : '#1e3a8a'

  return (
    <svg
      width={width}
      height={height}
      viewBox="0 0 100 100"
      preserveAspectRatio="none"
      className="overflow-visible"
    >
      {typeof path === 'string' ? (
        <path d={path} stroke={strokeColor} strokeWidth="2" fill="none" />
      ) : (
        <>
          <path d={path.fill} fill={fillColor} opacity="0.2" />
          <path d={path.line} stroke={strokeColor} strokeWidth="2" fill="none" />
        </>
      )}
    </svg>
  )
}
