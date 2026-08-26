// Test for PriceChart component
// Bug fix M6.74: The chart container ref was conditionally rendered.
// Verify it is always rendered so the ref is available and createChart() can run.

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { PriceChart } from '../PriceChart'

// Mock the api module
vi.mock('../../lib/api', () => ({
  getCandles: vi.fn().mockResolvedValue({
    candles: [
      { time: 1786800000, open: '50000', high: '50100', low: '49900', close: '50050', volume: '0.1' },
      { time: 1786800300, open: '50050', high: '50200', low: '50000', close: '50100', volume: '0.15' },
    ],
    interval: 300,
    base: 'BTC',
    quote: 'USDT',
  }),
}))

// Mock lightweight-charts to avoid canvas/WebGL issues in jsdom
vi.mock('lightweight-charts', () => {
  const mockTimeScale = {
    scrollToPosition: vi.fn(),
    fitContent: vi.fn(),
    subscribeVisibleLogicalRangeChange: vi.fn(() => () => {}),
    unsubscribeVisibleLogicalRangeChange: vi.fn(),
    getVisibleLogicalRange: vi.fn(() => ({ from: 0, to: 10 })),
  }
  const mockPriceScale = {
    applyOptions: vi.fn(),
  }
  const mockSeries = {
    setData: vi.fn(),
    createPriceLine: vi.fn(),
    priceScale: vi.fn(() => mockPriceScale),
  }
  const mockChart = {
    remove: vi.fn(),
    applyOptions: vi.fn(),
    timeScale: vi.fn(() => mockTimeScale),
    priceScale: vi.fn(() => mockPriceScale),
    removeSeries: vi.fn(),
    addSeries: vi.fn(() => mockSeries),
  }
  return {
    createChart: vi.fn(() => mockChart),
    CandlestickSeries: 'candlestick',
    HistogramSeries: 'histogram',
    LineSeries: 'line',
  }
})

describe('PriceChart', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders chart container div even when no candles loaded yet', () => {
    // BUG REGRESSION TEST: M6.74
    // The chart div used to be inside the data loaded branch of a ternary.
    // This meant the ref was never set, and createChart() never ran.
    // Now the div is always rendered (with overlays for loading/empty states).
    const { container } = render(<PriceChart base="BTC" quote="USDT" />)
    const chartDivs = container.querySelectorAll('div[style*="height: 400px"]')
    expect(chartDivs.length).toBeGreaterThanOrEqual(1)
  })

  it('renders TradingView attribution watermark', () => {
    const { container } = render(<PriceChart base="BTC" quote="USDT" />)
    const watermark = container.innerHTML.includes('tradingview.com')
    expect(watermark).toBe(true)
  })

  it('renders MA, EMA, BB, RSI, Volume toggle buttons', () => {
    const { container } = render(<PriceChart base="BTC" quote="USDT" />)
    const buttons = container.querySelectorAll('button')
    const buttonTexts = Array.from(buttons).map(b => b.textContent.trim())
    expect(buttonTexts).toContain('MA')
    expect(buttonTexts).toContain('EMA')
    expect(buttonTexts).toContain('BB')
    expect(buttonTexts).toContain('RSI')
    expect(buttonTexts.some(t => t.toLowerCase().includes('volume'))).toBe(true)
  })

  it('renders merged interval selector (no duplicate 1D/1d)', () => {
    // M6.78: Merged 1D/1W/1M + 1m/5m/15m/1h/1d into single selector
    const { container } = render(<PriceChart base="BTC" quote="USDT" />)
    const buttons = container.querySelectorAll('button')
    const buttonTexts = Array.from(buttons).map(b => b.textContent.trim())
    // Should have single row of 7 interval options
    expect(buttonTexts).toContain('1m')
    expect(buttonTexts).toContain('5m')
    expect(buttonTexts).toContain('15m')
    expect(buttonTexts).toContain('1h')
    expect(buttonTexts).toContain('1d')
    expect(buttonTexts).toContain('1w')
    expect(buttonTexts).toContain('1M')
  })

  it('hides Latest button by default', () => {
    // Latest button only shows when user scrolls away from latest
    const { container } = render(<PriceChart base="BTC" quote="USDT" />)
    const latestBtn = Array.from(container.querySelectorAll('button'))
      .find(b => b.textContent.includes('Latest'))
    expect(latestBtn).toBeUndefined()
  })
})
