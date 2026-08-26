// Domain and deployment configuration
// Override at build time with VITE_DOMAIN env var, or in .env file

export const config = {
  // Public domain for the deployed site
  domain: import.meta.env.VITE_DOMAIN || 'goexchange.top',

  // API endpoints - relative to current host (works with nginx proxy)
  apiBase: '/api/v1',
  wsBase: '/api/v1/ws',

  // External integrations
  tradingViewSymbolPrefix: 'BINANCE',
} as const;

// Helper: build full URL for current pair
export function pairToTradingViewSymbol(pair: string): string {
  // BTC_USDT -> BINANCE:BTCUSDT
  return `${config.tradingViewSymbolPrefix}:${pair.replace('_', '')}`;
}

// Helper: get current domain (browser-side, fallback to config)
export function getCurrentDomain(): string {
  if (typeof window !== 'undefined') {
    return window.location.hostname;
  }
  return config.domain;
}