// Utility helpers used across the app.

export function formatNumber(value: string | number, decimals = 4): string {
  const n = typeof value === 'string' ? parseFloat(value) : value
  if (isNaN(n)) return value.toString()
  if (n === 0) return '0'
  if (Math.abs(n) < 0.0001) return '< 0.0001'
  return n.toFixed(decimals).replace(/\.?0+$/, '')
}

export function formatPrice(value: string | number): string {
  const n = typeof value === 'string' ? parseFloat(value) : value
  if (isNaN(n) || n === 0) return '—'
  if (n >= 1000) return n.toFixed(2)
  if (n >= 1) return n.toFixed(4)
  return n.toFixed(8)
}

export function formatPercent(value: string | number): string {
  const n = typeof value === 'string' ? parseFloat(value) : value
  if (isNaN(n)) return '—'
  const sign = n >= 0 ? '+' : ''
  return `${sign}${n.toFixed(2)}%`
}

export function formatTime(iso: string): string {
  try {
    const d = new Date(iso)
    return d.toLocaleString()
  } catch {
    return iso
  }
}