import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

// Mock the Toast hook
vi.mock('../Toast', () => ({
  useToast: () => ({
    showToast: vi.fn(),
  }),
}))

beforeEach(() => {
  vi.resetAllMocks()
})

function mockResponse(data: any) {
  return {
    ok: true,
    status: 200,
    text: async () => JSON.stringify(data),
    json: async () => data,
  }
}

describe('ActiveTriggers', () => {
  it('renders empty state when no triggers', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockResponse({ triggers: [] }))
    )

    const { ActiveTriggers } = await import('../ActiveTriggers')
    render(<ActiveTriggers />)

    await waitFor(() => {
      expect(screen.getByText(/No pending triggers/)).toBeTruthy()
    })
  })

  it('renders trigger list', async () => {
    const mockData = {
      triggers: [
        {
          id: '1',
          pair: 'BTC_USDT',
          side: 'SELL',
          trigger_type: 'STOP_LOSS',
          trigger_price: '50000',
          quantity: '0.1',
          status: 'PENDING',
          created_at: new Date().toISOString(),
        },
      ],
    }
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockResponse(mockData))
    )

    const { ActiveTriggers } = await import('../ActiveTriggers')
    render(<ActiveTriggers />)

    await waitFor(() => {
      expect(screen.getByText('BTC_USDT')).toBeTruthy()
      expect(screen.getByText(/STOP/)).toBeTruthy()
    })
  })
})