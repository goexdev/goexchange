import { describe, it, expect } from 'vitest'

// Pure logic tests for Status page behavior
describe('Status Color Logic', () => {
  function getStatusColor(status: string): string {
    const colors: Record<string, string> = {
      operational: 'bg-green-500',
      degraded: 'bg-yellow-500',
      down: 'bg-red-500',
    }
    return colors[status] || 'bg-gray-500'
  }

  it('returns green for operational', () => {
    expect(getStatusColor('operational')).toBe('bg-green-500')
  })

  it('returns yellow for degraded', () => {
    expect(getStatusColor('degraded')).toBe('bg-yellow-500')
  })

  it('returns red for down', () => {
    expect(getStatusColor('down')).toBe('bg-red-500')
  })

  it('returns gray for unknown', () => {
    expect(getStatusColor('unknown')).toBe('bg-gray-500')
  })
})

describe('Uptime Formatting', () => {
  function formatUptime(seconds: number): string {
    const d = Math.floor(seconds / 86400)
    const h = Math.floor((seconds % 86400) / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (d > 0) return d + 'd ' + h + 'h ' + m + 'm'
    if (h > 0) return h + 'h ' + m + 'm'
    return m + 'm'
  }

  it('formats days', () => {
    expect(formatUptime(86400)).toBe('1d 0h 0m')
  })

  it('formats hours', () => {
    expect(formatUptime(3600)).toBe('1h 0m')
  })

  it('formats minutes', () => {
    expect(formatUptime(60)).toBe('1m')
  })

  it('handles 0', () => {
    expect(formatUptime(0)).toBe('0m')
  })
})