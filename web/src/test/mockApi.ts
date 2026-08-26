// Mock api module for tests
import { vi } from 'vitest'

export const mockCandles = [
  { time: 1786800000, open: '50000', high: '50100', low: '49900', close: '50050', volume: '0.1' },
  { time: 1786800300, open: '50050', high: '50200', low: '50000', close: '50100', volume: '0.15' },
  { time: 1786800600, open: '50100', high: '50150', low: '50050', close: '50150', volume: '0.2' },
]

export const mockCandlesResponse = {
  candles: mockCandles,
  interval: 300,
  base: 'BTC',
  quote: 'USDT',
}

export const mockOpenOrders = [
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

export const mockWallets = [
  { user_id: 'user-1', asset: 'BTC', available: '2.0', frozen: '0' },
  { user_id: 'user-1', asset: 'USDT', available: '100000', frozen: '0' },
]

export const mockApi = {
  getCandles: vi.fn().mockResolvedValue(mockCandlesResponse),
  listOrders: vi.fn().mockResolvedValue(mockOpenOrders),
  getWallets: vi.fn().mockResolvedValue(mockWallets),
  getOrderBook: vi.fn().mockResolvedValue({
    pair: 'BTC_USDT',
    bids: [{ price: '50000', quantity: '0.01' }],
    asks: [],
  }),
  cancelOrder: vi.fn().mockResolvedValue({ order_id: 'order-1', status: 'canceled' }),
  getTicker: vi.fn().mockResolvedValue({
    last: '50000',
    bid: '49900',
    ask: '50100',
    volume_24h: '100',
    change_24h: '1.5',
  }),
  get24hStats: vi.fn().mockResolvedValue({
    high: '51000',
    low: '49000',
    volume: '100',
    change: '2.5',
  }),
  getRecentTrades: vi.fn().mockResolvedValue({ trades: [], pair: 'BTC_USDT' }),
  amendOrder: vi.fn().mockResolvedValue({ order_id: 'order-1', status: 'OPEN' }),
}
