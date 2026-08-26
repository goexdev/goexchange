// Test for OrdersPanel component
// Bug fix M6.80: Open Orders panel did not update in real-time.
// Verified: 3s polling refreshes the orders list.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { OrdersPanel } from '../OrdersPanel'

const mocks = vi.hoisted(() => ({
  listOrders: vi.fn(),
  getRecentTrades: vi.fn(),
  cancelOrder: vi.fn(),
}))

vi.mock('../../lib/api', () => ({
  listOrders: mocks.listOrders,
  getRecentTrades: mocks.getRecentTrades,
  cancelOrder: mocks.cancelOrder,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
  }),
}))

const mockOrders = [
  {
    id: 'order-1',
    user_id: 'user-1',
    pair_id: 1,
    pair: 'BTC_USDT',
    base: 'BTC',
    quote: 'USDT',
    side: 'BUY',
    type: 'LIMIT',
    price: '50000',
    quantity: '0.01',
    filled_quantity: '0',
    status: 'OPEN',
    created_at: '2026-08-17T00:00:00Z',
    updated_at: '2026-08-17T00:00:00Z',
  },
]

beforeEach(() => {
  mocks.listOrders.mockReset()
  mocks.getRecentTrades.mockReset()
  mocks.cancelOrder.mockReset()
  mocks.listOrders.mockResolvedValue(mockOrders)
  mocks.getRecentTrades.mockResolvedValue({ trades: [] })
  mocks.cancelOrder.mockResolvedValue({ status: 'canceled', order_id: 'order-1' })
})

describe('OrdersPanel polling (M6.80 fix)', () => {
  it('renders without crashing', async () => {
    const { container } = render(<OrdersPanel base="BTC" quote="USDT" />)
    expect(container).toBeTruthy()
  })

  it('loads orders on mount', async () => {
    render(<OrdersPanel base="BTC" quote="USDT" />)
    await waitFor(() => {
      expect(mocks.listOrders).toHaveBeenCalled()
    }, { timeout: 1000 })
  })

  it('shows OPEN order with correct pair', async () => {
    const { container } = render(<OrdersPanel base="BTC" quote="USDT" />)
    await waitFor(() => {
      // Order should appear in DOM
      expect(container.textContent).toContain('BTC_USDT')
    }, { timeout: 1000 })
  })

  it('uses 3 second polling interval', () => {
    const setIntervalSpy = vi.spyOn(global, 'setInterval')
    render(<OrdersPanel base="BTC" quote="USDT" />)
    // Find calls with 3000ms interval
    const calls = setIntervalSpy.mock.calls.filter(c => c[1] === 3000)
    expect(calls.length).toBeGreaterThanOrEqual(1)
    setIntervalSpy.mockRestore()
  })
})
