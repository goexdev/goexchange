// API client for goexchange backend.
// Talks to the server at :8099 (Go API).

const BASE_URL = '/api/v1'

// ---- Types ----

export interface User {
  id: string
  email: string
  kyc_level: number
  kyc_status?: string
  kyc_limit_usdt?: string
  role?: string
  created_at: string
}

export interface Balance {
  user_id: string
  asset: string
  available: string
  frozen: string
}

export interface Order {
  id: string
  user_id: string
  pair_id: number
  pair?: string
  base?: string
  quote?: string
  side: 'BUY' | 'SELL'
  type: string
  price: string
  quantity: string
  filled_quantity: string
  status: 'OPEN' | 'PARTIAL' | 'FILLED' | 'CANCELLED'
  created_at: string
  updated_at: string
}

export interface UserTrade {
  id: string
  pair: string
  base: string
  quote: string
  price: string
  quantity: string
  total: string
  side: 'BUY' | 'SELL'
  counter_side: string
  order_id: string
  executed_at: string
}

export interface Market {
  base: string
  quote: string
  pair: string
  enabled?: boolean  // only present in admin responses
}

export interface OrderBookLevel {
  price: string
  quantity: string
}

export interface OrderBook {
  pair: string
  bids: OrderBookLevel[]
  asks: OrderBookLevel[]
}

export interface Ticker {
  pair: string
  bid: string
  ask: string
  last: string
}

export interface Candle {
  time: number
  open: string
  high: string
  low: string
  close: string
  volume: string
}

export interface CandlesResponse {
  candles: Candle[]
  interval: number
  base: string
  quote: string
}

export interface Market24hStats {
  high: string
  low: string
  open: string
  last: string
  change_pct: number
  volume_base: string
  volume_quote: string
  trade_count: number
}

export interface RecentTrade {
  id: string
  price: string
  quantity: string
  side: 'BUY' | 'SELL'
  executed_at: string
}

export async function get24hStats(base: string, quote: string): Promise<Market24hStats> {
  return request<Market24hStats>(`/markets/${base}/${quote}/stats`)
}

export async function getRecentTrades(base: string, quote: string, limit: number = 50): Promise<{ trades: RecentTrade[]; pair: string }> {
  return request<{ trades: RecentTrade[]; pair: string }>(`/markets/${base}/${quote}/trades?limit=${limit}`)
}

export async function listOrders(): Promise<Order[]> {
  const res = await request<{ orders: Order[]; count: number }>('/orders')
  return res.orders || []
}

export async function getMyOrders(pair?: string, status?: string): Promise<{ orders: Order[]; count: number }> {
  const params = new URLSearchParams()
  if (pair) params.set('pair', pair)
  if (status) params.set('status', status)
  const q = params.toString()
  return request<{ orders: Order[]; count: number }>(`/orders${q ? '?' + q : ''}`)
}

export async function amendOrder(id: string, pair: string, price: string, quantity: string): Promise<{ order_id: string; status: string; filled: string; remaining: string }> {
  return request<{ order_id: string; status: string; filled: string; remaining: string }>(
    `/orders/${id}?pair=${encodeURIComponent(pair)}`,
    {
      method: 'PATCH',
      body: JSON.stringify({ price, quantity }),
    }
  )
}

export async function cancelOrder(id: string, pair?: string): Promise<{ status: string }> {
  const q = pair ? `?pair=${encodeURIComponent(pair)}` : ''
  return request<{ status: string }>(`/orders/${id}${q}`, { method: 'DELETE' })
}

export async function cancelAllOrders(pair?: string): Promise<{ status: string; cancelled: number }> {
  const q = pair ? `?pair=${pair}` : ''
  return request<{ status: string; cancelled: number }>(`/orders${q}`, { method: 'DELETE' })
}

// 2FA / TOTP
export interface TwoFAStatus {
  enabled: boolean
  backup_codes_remaining: number
}

export interface TOTPSetup {
  secret: string
  otpauth_url: string
}

export interface Enable2FAResponse {
  enabled: boolean
  backup_codes: string[]
  message: string
}

export async function get2FAStatus(): Promise<TwoFAStatus> {
  return request<TwoFAStatus>('/users/me/2fa/status')
}

export async function setup2FA(): Promise<TOTPSetup> {
  return request<TOTPSetup>('/users/me/2fa/setup', { method: 'POST' })
}

export async function enable2FA(code: string): Promise<Enable2FAResponse> {
  return request<Enable2FAResponse>('/users/me/2fa/enable', {
    method: 'POST',
    body: JSON.stringify({ code }),
  })
}

export async function disable2FA(code: string): Promise<{ enabled: boolean }> {
  return request<{ enabled: boolean }>('/users/me/2fa/disable', {
    method: 'POST',
    body: JSON.stringify({ code }),
  })
}

// Notification preferences
export interface NotifPrefs {
  user_id: string
  // In-app notification preferences
  notify_2fa_enabled: boolean
  notify_2fa_disabled: boolean
  notify_2fa_backup_used: boolean
  notify_2fa_failed: boolean
  notify_2fa_login_success: boolean
  notify_login: boolean
  notify_withdrawal: boolean
  notify_large_withdraw: boolean
  // Email notification preferences
  email_2fa_enabled: boolean
  email_2fa_disabled: boolean
  email_2fa_backup_used: boolean
  email_2fa_failed: boolean
  email_2fa_login_success: boolean
  email_login: boolean
  email_withdrawal: boolean
  email_large_withdraw: boolean
  updated_at: string
}

export async function getNotifPrefs(): Promise<NotifPrefs> {
  return request<NotifPrefs>('/users/me/notif-prefs')
}

export async function updateNotifPrefs(updates: Partial<NotifPrefs>): Promise<NotifPrefs> {
  // Strip user_id and updated_at - backend ignores these
  const cleanUpdates: Record<string, boolean> = {}
  for (const [key, value] of Object.entries(updates)) {
    if (key.startsWith('notify_') && typeof value === 'boolean') {
      cleanUpdates[key] = value
    }
  }
  return request<NotifPrefs>('/users/me/notif-prefs', {
    method: 'PATCH',
    body: JSON.stringify(cleanUpdates),
  })
}

export async function regenerateBackupCodes(code: string): Promise<Enable2FAResponse> {
  return request<Enable2FAResponse>('/users/me/2fa/backup-codes', {
    method: 'POST',
    body: JSON.stringify({ code }),
  })
}

export async function getMyTrades(pair?: string, limit: number = 50): Promise<{ trades: UserTrade[]; count: number }> {
  const params = new URLSearchParams()
  if (pair) params.set('pair', pair)
  params.set('limit', String(limit))
  return request<{ trades: UserTrade[]; count: number }>(`/users/me/trades?${params.toString()}`)
}

export async function getCandles(
  base: string,
  quote: string,
  interval: number,
  fromMs?: number,
  toMs?: number
): Promise<CandlesResponse> {
  const params = new URLSearchParams()
  params.set('interval', String(interval))
  if (fromMs) params.set('from', String(fromMs))
  if (toMs) params.set('to', String(toMs))
  return request<CandlesResponse>(
    `/markets/${base}/${quote}/candles?${params.toString()}`
  )
}

export interface Deposit {
  id: string
  user_id: string
  asset: string
  amount: string
  tx_hash: string
  chain: string
  status: string
  created_at: string
}

export interface PlaceOrderResult {
  order_id: string
  status: string
  trades: any[]
  filled: string
  remaining: string
}

// ---- Helper ----

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const token = localStorage.getItem('goexchange_token')
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(opts.headers as Record<string, string> || {}),
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const res = await fetch(`${BASE_URL}${path}`, { ...opts, headers })
  const text = await res.text()
  if (!res.ok) {
    throw new Error(text || res.statusText)
  }
  return text ? JSON.parse(text) : (null as any)
}

// ---- Auth ----

export async function register(email: string, password: string): Promise<{ user: User; token: string }> {
  const data = await request<any>('/users/register', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
  return { user: data.user, token: data.token }
}

export interface Login2FAResponse {
  requires_2fa?: boolean
  temp_token?: string
  user?: User
  token?: string
  used_backup_code?: boolean
}

export async function login(email: string, password: string): Promise<Login2FAResponse> {
  const res = await fetch('/api/v1/users/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  })
  if (!res.ok) {
    throw new Error((await res.json()).error || 'login failed')
  }
  return res.json()
}

export async function complete2FALogin(tempToken: string, code: string): Promise<{ token: string; user: User; used_backup_code: boolean }> {
  const res = await fetch('/api/v1/auth/2fa/complete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ temp_token: tempToken, code }),
  })
  if (!res.ok) {
    throw new Error((await res.json()).error || '2FA verification failed')
  }
  return res.json()
}

export async function loginLegacy(email: string, password: string): Promise<{ user: User; token: string }> {
  const data = await request<any>('/users/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
  return { user: data.user, token: data.token }
}

export async function me(): Promise<User> {
  return request<User>('/users/me')
}

// ---- Wallet ----

export async function getWallets(): Promise<Balance[]> {
  return request<Balance[]>('/wallets')
}

export async function getDeposits(): Promise<Deposit[]> {
  return request<Deposit[]>('/deposits')
}

export async function importDepositsFromChain(): Promise<{ imported_count: number; imported_ids: string[] }> {
  return request<{ imported_count: number; imported_ids: string[] }>('/deposits/import', { method: 'POST' })
}

export interface PendingTx {
  tx_hash: string
  address: string
  asset: string
  amount: string
  confirmations: number
  min_conf: number
  block_height: number
  time: number
  status: 'mempool' | 'confirming'
}

export async function listPendingTxs(): Promise<PendingTx[]> {
  return request<PendingTx[]>('/pending-txs')
}

export async function spawnDeposit(asset: string, amount: string): Promise<Deposit> {
  return request<Deposit>('/admin/spawn-deposit', {
    method: 'POST',
    body: JSON.stringify({ asset, amount }),
  })
}

// ---- Orders ----

export async function placeOrder(
  pair: string,
  side: string,
  price: string,
  quantity: string,
  type: 'LIMIT' | 'MARKET' = 'LIMIT'
): Promise<PlaceOrderResult> {
  return request<PlaceOrderResult>('/orders', {
    method: 'POST',
    body: JSON.stringify({ pair, side, price, quantity, type }),
  })
}

// ---- Markets ----

export async function listMarkets(): Promise<Market[]> {
  return request<Market[]>('/markets?enabled_only=true')
}

// Admin: toggle pair enabled state
export async function adminTogglePair(base: string, quote: string, enabled: boolean): Promise<Market> {
  return request<Market>('/admin/pairs/toggle', {
    method: 'POST',
    body: JSON.stringify({ base, quote, enabled }),
  })
}

// Admin: list all pairs (enabled + disabled)
export async function adminListPairs(): Promise<Market[]> {
  return request<Market[]>('/admin/pairs')
}

export async function getOrderBook(base: string, quote: string): Promise<OrderBook> {
  return request<OrderBook>(`/markets/${base}/${quote}/orderbook`)
}

export async function getTicker(base: string, quote: string): Promise<Ticker> {
  return request<Ticker>(`/markets/${base}/${quote}/ticker`)
}
export interface DepositAddress {
  asset: string
  address: string
}

export async function getDepositAddress(asset: string): Promise<DepositAddress> {
  return request<DepositAddress>(`/deposit-address/${asset}`)
}

export interface Withdrawal {
  ID: string
  UserID: string
  Asset: string
  Amount: string
  DestAddress: string
  TxHash: string
  Chain: string
  Status: string
  Confirmations: number
  ErrorMsg: string
  CreatedAt: string
  SentAt: string | null
  ConfirmedAt: string | null
}

export async function createWithdrawal(asset: string, amount: string, destAddress: string): Promise<Withdrawal> {
  return request<Withdrawal>('/withdrawals', {
    method: 'POST',
    body: JSON.stringify({ asset, amount, dest_address: destAddress }),
  })
}

export async function listWithdrawals(): Promise<Withdrawal[]> {
  return request<Withdrawal[]>('/withdrawals')
}


export interface PendingDeposit {
  address: string
  asset: string
  confirmed: string
  pending: string
  total: string
  min_conf: number
  block_height: number
}

export async function listPendingDeposits(): Promise<PendingDeposit[]> {
  return request<PendingDeposit[]>('/pending-deposits')
}


export interface PublicUser {
  id: string
  email: string
  kyc_level: number
  kyc_status: string
  kyc_limit_usdt: string
  role: string
  created_at: string
}

export interface KycLimit {
  kyc_level: number
  kyc_status: string
  limit_usdt: string
}

export interface KycSubmission {
  ID: string
  UserID: string
  TargetLevel: number
  FullName: string
  IdNumber: string
  Country: string
  Status: string
  SubmittedAt: string
  ReviewedAt?: string
  ReviewerNote?: string
}

export async function getKycLimit(): Promise<KycLimit> {
  return request<KycLimit>('/users/me/limit')
}

export async function submitKYC(input: {
  target_level: number
  full_name: string
  id_number: string
  country: string
  doc_front: string
  doc_back: string
  selfie: string
}): Promise<KycSubmission> {
  return request<KycSubmission>('/users/me/kyc', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

// KYC document upload - returns the file path to use in submitKYC
export async function uploadKYCDoc(
  type: 'front' | 'back' | 'selfie',
  file: File
): Promise<{ path: string; content_type: string; doc_type: string }> {
  const form = new FormData()
  form.append('file', file)
  form.append('type', type)
  const token = localStorage.getItem('auth_token')
  const res = await fetch('/api/v1/users/me/kyc/upload', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: form,
  })
  if (!res.ok) {
    const err = await res.text()
    throw new Error(err || `Upload failed: ${res.status}`)
  }
  return res.json()
}


// Admin Dashboard Types
export interface AdminStats {
  total_users: number
  admin_users: number
  l0_users: number
  l1_users: number
  l2_users: number
  pending_kyc: number
  total_orders: number
  open_orders: number
  filled_orders: number
  canceled_orders: number
  total_deposits: number
  pending_deposits: number
  done_deposits: number
  total_deposit_amount: string
  total_withdrawals: number
  pending_withdrawals: number
  broadcast_withdrawals: number
  done_withdrawals: number
  failed_withdrawals: number
  total_withdrawal_amount: string
}

export interface AdminWithdrawal {
  ID: string
  Asset: string
  Amount: string
  DestAddress: string
  Status: string
  TxHash: string
  created_at: string
}

export async function adminStats(): Promise<AdminStats> {
  return request<AdminStats>('/admin/stats')
}

export interface UsersResponse {
  users: PublicUser[]
  total: number
  limit: number
  offset: number
}

export interface UsersQuery {
  limit?: number
  offset?: number
  search?: string
  role?: string
  kyc_level?: number
  kyc_status?: string
}

export async function adminListUsers(
  query: UsersQuery | number = 50
): Promise<UsersResponse> {
  let q = ''
  if (typeof query === 'number') {
    q = `?limit=${query}`
  } else {
    const params = new URLSearchParams()
    if (query.limit) params.set('limit', String(query.limit))
    if (query.offset) params.set('offset', String(query.offset))
    if (query.search) params.set('search', query.search)
    if (query.role) params.set('role', query.role)
    if (query.kyc_level) params.set('kyc_level', String(query.kyc_level))
    if (query.kyc_status) params.set('kyc_status', query.kyc_status)
    const s = params.toString()
    q = s ? `?${s}` : ''
  }
  return request<UsersResponse>(`/admin/users${q}`)
}

export async function adminListPendingKYC(): Promise<KycSubmission[]> {
  return request<KycSubmission[]>('/admin/kyc/pending')
}

export async function adminListKYC(status: string): Promise<KycSubmission[]> {
  const q = status ? `?status=${status}` : ''
  return request<KycSubmission[]>(`/admin/kyc${q}`)
}

export async function adminApproveKYC(id: string, note: string): Promise<void> {
  await request(`/admin/kyc/${id}/approve`, {
    method: 'POST',
    body: JSON.stringify({ note }),
  })
}

export async function adminRejectKYC(id: string, reason: string): Promise<void> {
  await request(`/admin/kyc/${id}/reject`, {
    method: 'POST',
    body: JSON.stringify({ reason }),
  })
}

export async function adminSetUserPassword(id: string, password: string): Promise<void> {
  await request(`/admin/users/${id}/password`, {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

export interface Notification {
  id: string
  type: string
  title: string
  body: string
  created_at: string
  read_at?: string
}

export async function listNotifications(): Promise<Notification[]> {
  return request(`/users/me/notifications`)
}

export async function markNotificationRead(id: string): Promise<void> {
  await request(`/users/me/notifications/${id}/read`, { method: 'POST' })
}

export async function markAllNotificationsRead(): Promise<{ status: string; marked: number }> {
  return request(`/users/me/notifications/read-all`, { method: 'POST' })
}

export async function adminSetUserRole(id: string, role: string): Promise<void> {
  await request(`/admin/users/${id}/role`, {
    method: 'POST',
    body: JSON.stringify({ role }),
  })
}

export async function adminListWithdrawals(limit: number = 50): Promise<AdminWithdrawal[]> {
  return request<AdminWithdrawal[]>(`/admin/withdrawals?limit=${limit}`)
}

export async function adminListDeposits(limit: number = 50): Promise<any[]> {
  return request<any[]>(`/admin/deposits?limit=${limit}`)
}

export async function adminListOrders(limit: number = 50): Promise<any[]> {
  return request<any[]>(`/admin/orders?limit=${limit}`)
}


// Admin Audit Log
export interface AuditEntry {
  id: string
  admin_user_id: string
  admin_email: string
  action: string
  target_type: string
  target_id?: string
  target_label: string
  details: Record<string, any>
  ip: string
  user_agent: string
  status: string
  error_msg: string
  created_at: string
}

export async function listAuditLogs(params: {
  action?: string
  target_type?: string
  admin_id?: string
  since?: string
  limit?: number
} = {}): Promise<AuditEntry[]> {
  const q = new URLSearchParams()
  if (params.action) q.set('action', params.action)
  if (params.target_type) q.set('target_type', params.target_type)
  if (params.admin_id) q.set('admin_id', params.admin_id)
  if (params.since) q.set('since', params.since)
  q.set('limit', String(params.limit || 50))
  return request<AuditEntry[]>(`/admin/audit-logs?${q.toString()}`)
}


// ==================== Chain Admin ====================

export interface ChainToken {
  symbol: string
  contract: string
  decimals: number
  min_conf?: number
}

export interface ChainInfo {
  id: string
  enabled: boolean
  active: boolean
  family?: string
  driver: string
  asset: string
  hot_wallet: string
  coin_type?: number
  p2pkh_prefix?: number
  chain_id?: number
  min_conf: number
  display_name?: string
  rpc_url?: string
  explorer_url?: string
  tokens?: ChainToken[]
}

export interface ChainsResponse {
  chains: ChainInfo[]
  active_count: number
  total_count: number
}

export async function adminListChains(): Promise<ChainsResponse> {
  return request<ChainsResponse>('/admin/chains')
}

export async function adminEnableChain(chainId: string): Promise<{ok: boolean}> {
  return request(`/admin/chains/${chainId}/enable`, {method: 'POST'})
}

export async function adminDisableChain(chainId: string): Promise<{ok: boolean}> {
  return request(`/admin/chains/${chainId}/disable`, {method: 'POST'})
}

export async function adminTestChain(chainId: string): Promise<{ok: boolean, block_count: number}> {
  return request(`/admin/chains/${chainId}/test`, {method: 'POST'})
}

export async function adminReloadChains(): Promise<{ok: boolean, changes: string[]}> {
  return request('/admin/chains/reload', {method: 'POST'})
}

export async function adminGetHotWallet(chainId: string): Promise<{has_signer: boolean, hot_address: string}> {
  return request(`/admin/hot-wallet?chain=${chainId}`)
}


// ==================== Admin Dashboard ====================

export interface SystemHealth {
  goroutines: number
  go_version: string
  db?: { connected: boolean; total_conns: number; idle_conns: number; max_conns: number; version?: string }
  redis?: { connected: boolean; ping?: string; key_count?: number; error?: string }
  vault?: { connected: boolean; sealed?: boolean; error?: string }
}

export interface ChainHealth {
  id: string
  family?: string
  driver: string
  asset: string
  enabled: boolean
  active: boolean
  status: string
  block_count?: number
  error?: string
  hot_wallet?: string
  has_signer?: boolean
  min_conf: number
}

export interface TokenStats {
  asset: string
  holders: number
  available: number
  frozen: number
  total: number
}

export interface Alert {
  level: 'info' | 'warning' | 'error'
  title: string
  count: number
  action: string
}

export interface DashboardCharts {
  user_signups?: UserSignup[]
  volume_hourly?: VolumeHour[]
  token_distribution?: TokenDistribution[]
  withdrawal_statuses?: WithdrawalStatus[]
}

export interface UserSignup {
  day: string
  count: number
}

export interface VolumeHour {
  hour: string
  type: 'deposit' | 'withdrawal'
  count: number
  volume: number
}

export interface TokenDistribution {
  asset: string
  holders: number
  total: number
}

export interface WithdrawalStatus {
  status: string
  count: number
}

export async function adminDashboardCharts(): Promise<DashboardCharts> {
  return request<DashboardCharts>('/admin/dashboard/charts')
}

// ==================== API Keys ====================

export interface APIKey {
  id: string
  user_id: string
  name: string
  key_id: string
  scopes: string[]
  last_used_at?: string
  expires_at?: string
  revoked: boolean
  created_at: string
}

export interface APIKeysResponse {
  keys: APIKey[]
  count: number
}

export interface APIKeyCreateRequest {
  name: string
  scopes: string[]
  expires_in_days?: number
}

export interface APIKeyCreateResponse {
  key: APIKey
  secret: string
  warning: string
}

export async function listAPIKeys(): Promise<APIKeysResponse> {
  return request<APIKeysResponse>('/users/me/api-keys')
}

export async function createAPIKey(req: APIKeyCreateRequest): Promise<APIKeyCreateResponse> {
  return request<APIKeyCreateResponse>('/users/me/api-keys', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function revokeAPIKey(id: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/users/me/api-keys/${id}`, { method: 'DELETE' })
}

export interface VolumeStats {
  deposits_24h: { count: number; volume: number }
  withdrawals_24h: { count: number; volume: number }
  withdrawals_by_status_24h?: Record<string, number>
  top_withdrawn_tokens_24h?: { asset: string; volume: number; count: number }[]
}

export interface DashboardData {
  timestamp: string
  system: SystemHealth
  chains: { chains: ChainHealth[]; total: number; active: number }
  tokens: { tokens: TokenStats[]; count: number }
  volume: VolumeStats
  alerts: { items: Alert[]; count: number; has_warnings: boolean }
}

export async function adminDashboard(): Promise<DashboardData> {
  return request<DashboardData>('/admin/dashboard')
}

// ---- Address Book ----

export interface AddressBookEntry {
  id: string
  user_id: string
  asset: string
  address: string
  label: string
  whitelisted: boolean
  last_used_at: string | null
  created_at: string
  updated_at: string
}

export async function listAddresses(asset?: string): Promise<{ addresses: AddressBookEntry[] }> {
  const q = asset ? `?asset=${encodeURIComponent(asset)}` : ''
  return request<{ addresses: AddressBookEntry[] }>(`/users/me/addresses${q}`)
}

export async function addAddress(
  asset: string,
  address: string,
  label: string = '',
  whitelisted: boolean = false
): Promise<AddressBookEntry> {
  return request<AddressBookEntry>('/users/me/addresses', {
    method: 'POST',
    body: JSON.stringify({ asset, address, label, whitelisted }),
  })
}

export async function deleteAddress(id: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/users/me/addresses/${id}`, { method: 'DELETE' })
}

export async function updateAddress(
  id: string,
  updates: { label?: string; whitelisted?: boolean }
): Promise<AddressBookEntry> {
  return request<AddressBookEntry>(`/users/me/addresses/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(updates),
  })
}
// ---- Trigger Orders (Stop Loss / Take Profit) ----

export interface TriggerOrder {
  id: string
  user_id: string
  pair: string
  side: 'BUY' | 'SELL'
  trigger_type: 'STOP_LOSS' | 'TAKE_PROFIT'
  trigger_price: string
  quantity: string
  status: 'PENDING' | 'TRIGGERED' | 'CANCELLED' | 'EXPIRED'
  triggered_at: string | null
  triggered_order_id: string | null
  cancelled_at: string | null
  created_at: string
  updated_at: string
}

export async function listTriggers(): Promise<{ triggers: TriggerOrder[] }> {
  return request<{ triggers: TriggerOrder[] }>('/users/me/triggers')
}

export async function createTrigger(
  pair: string,
  side: 'BUY' | 'SELL',
  triggerType: 'STOP_LOSS' | 'TAKE_PROFIT',
  triggerPrice: string,
  quantity: string
): Promise<TriggerOrder> {
  return request<TriggerOrder>('/users/me/triggers', {
    method: 'POST',
    body: JSON.stringify({
      pair,
      side,
      trigger_type: triggerType,
      trigger_price: triggerPrice,
      quantity,
    }),
  })
}

export async function cancelTrigger(id: string): Promise<{ status: string }> {
  return request<{ status: string }>(`/users/me/triggers/${id}`, { method: 'DELETE' })
}// Currency / withdraw fee APIs

export interface Currency {
  symbol: string
  name: string
  precision: number
  is_active?: boolean
  min_withdraw: string
  max_withdraw: string
  withdraw_fee_flat: string
  withdraw_fee_percent: string
  withdraw_fee_min: string
  updated_at?: string
}

export async function listCurrencies(): Promise<Currency[]> {
  return request<Currency[]>('/currencies')
}

export async function adminListCurrencies(): Promise<Currency[]> {
  return request<Currency[]>('/admin/currencies')
}

export async function adminUpdateCurrency(
  symbol: string,
  updates: Partial<{
    min_withdraw: string
    max_withdraw: string
    withdraw_fee_flat: string
    withdraw_fee_percent: string
    withdraw_fee_min: string
    is_active: boolean
  }>
): Promise<{ status: string; symbol: string }> {
  return request<{ status: string; symbol: string }>(`/admin/currencies/${symbol}`, {
    method: 'PUT',
    body: JSON.stringify(updates),
  })
}export interface FeeStats {
  by_asset: Array<{
    asset: string
    withdrawal_count: number
    total_fee: string
    total_volume: string
    total_received: string
  }>
  daily: Array<{
    day: string
    count: number
    daily_fee: string
    daily_volume: string
  }>
  grand_total_fee: string
  grand_total_volume: string
}

export async function adminGetFeeStats(): Promise<FeeStats> {
  return request<FeeStats>('/admin/fee-stats')
}// Favorites (watchlist) APIs
export interface FavoritesResponse {
  favorites: string[]
}

export async function listFavorites(): Promise<FavoritesResponse> {
  return request<FavoritesResponse>('/users/me/favorites')
}

export async function addFavorite(pair: string): Promise<{ status: string; pair: string }> {
  return request<{ status: string; pair: string }>('/users/me/favorites', {
    method: 'POST',
    body: JSON.stringify({ pair }),
  })
}

export async function removeFavorite(pair: string): Promise<{ status: string; pair: string }> {
  return request<{ status: string; pair: string }>(`/users/me/favorites/${encodeURIComponent(pair)}`, {
    method: 'DELETE',
  })
}