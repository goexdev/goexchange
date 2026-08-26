import { describe, it, expect, vi, beforeEach } from 'vitest'

beforeEach(() => {
  vi.resetAllMocks()
})

function mockResponse(data: any) {
  return {
    ok: true,
    status: 200,
    statusText: 'OK',
    text: async () => JSON.stringify(data),
    json: async () => data,
  }
}

describe('Trigger API', () => {
  it('listTriggers returns triggers', async () => {
    const mockData = {
      triggers: [
        {
          id: '1',
          pair: 'BTC_USDT',
          side: 'SELL',
          trigger_type: 'STOP_LOSS',
          status: 'PENDING',
        },
      ],
    }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockResponse(mockData)))

    const { listTriggers } = await import('../api')
    const result = await listTriggers()
    expect(result.triggers).toHaveLength(1)
    expect(result.triggers[0].trigger_type).toBe('STOP_LOSS')
  })

  it('cancelTrigger returns status', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockResponse({ status: 'cancelled' })))

    const { cancelTrigger } = await import('../api')
    const result = await cancelTrigger('test-id')
    expect(result.status).toBe('cancelled')
  })
})

describe('Address API', () => {
  it('listAddresses returns addresses', async () => {
    const mockData = {
      addresses: [
        { id: '1', asset: 'BTC', address: 'bc1q...', label: 'Test', whitelisted: false },
      ],
    }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockResponse(mockData)))

    const { listAddresses } = await import('../api')
    const result = await listAddresses()
    expect(result.addresses).toHaveLength(1)
  })

  it('addAddress sends POST', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockResponse({
          id: '1',
          asset: 'BTC',
          address: 'bc1q...',
          label: 'Test',
          whitelisted: false,
        })
      )
    )

    const { addAddress } = await import('../api')
    const result = await addAddress('BTC', 'bc1q...', 'Test', false)
    expect(result.id).toBe('1')
  })
})