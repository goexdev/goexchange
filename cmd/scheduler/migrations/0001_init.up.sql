-- 0001_init.up.sql
-- M0.1: Initial schema for goexchange
--
-- Includes: users, chains, currencies, trading_pairs, balances,
--           orders, trades, deposits, mock_state, sessions

-- ============================================================
-- USERS
-- ============================================================
CREATE TABLE users (
    id UUID PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,         -- bcrypt
    kyc_level INT NOT NULL DEFAULT 1,    -- 1=L1, 2=L2, 3=L3
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);

-- ============================================================
-- CHAINS
-- ============================================================
CREATE TABLE chains (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,           -- 'ethereum', 'bsc'
    chain_id INT NOT NULL,               -- actual chain id (1 for eth, 56 for bsc, etc.)
    native_currency TEXT NOT NULL,       -- 'ETH', 'BNB', 
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- CURRENCIES (assets)
-- ============================================================
CREATE TABLE currencies (
    symbol TEXT PRIMARY KEY,             -- 'BTC', 'ETH', 'USDT', 
    name TEXT NOT NULL,
    precision INT NOT NULL,              -- 8 for BTC, 6 for USDT
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    min_withdraw NUMERIC(38, 18) NOT NULL DEFAULT 0,
    max_withdraw NUMERIC(38, 18) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- TRADING PAIRS
-- ============================================================
CREATE TABLE trading_pairs (
    id SERIAL PRIMARY KEY,
    base TEXT NOT NULL,                  -- 'BTC'
    quote TEXT NOT NULL,                 -- 'USDT'
    min_qty NUMERIC(38, 18) NOT NULL,
    max_qty NUMERIC(38, 18) NOT NULL,
    price_precision INT NOT NULL,
    qty_precision INT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(base, quote)
);

-- ============================================================
-- BALANCES (single source of truth)
-- ============================================================
CREATE TABLE balances (
    user_id UUID NOT NULL,
    asset TEXT NOT NULL,
    available NUMERIC(38, 18) NOT NULL DEFAULT 0,
    frozen NUMERIC(38, 18) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, asset),
    CHECK (available >= 0),
    CHECK (frozen >= 0)
);

CREATE INDEX idx_balances_user ON balances(user_id);

-- ============================================================
-- ORDERS
-- ============================================================
CREATE TABLE orders (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    pair_id INT NOT NULL,
    side TEXT NOT NULL,                  -- 'BUY' or 'SELL'
    type TEXT NOT NULL,                  -- 'LIMIT' for M0
    price NUMERIC(38, 18) NOT NULL,
    quantity NUMERIC(38, 18) NOT NULL,
    filled_quantity NUMERIC(38, 18) NOT NULL DEFAULT 0,
    status TEXT NOT NULL,                -- 'OPEN', 'PARTIAL', 'FILLED', 'CANCELLED'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (filled_quantity >= 0),
    CHECK (filled_quantity <= quantity),
    CHECK (side IN ('BUY', 'SELL')),
    CHECK (type IN ('LIMIT', 'MARKET')),
    CHECK (status IN ('OPEN', 'PARTIAL', 'FILLED', 'CANCELLED'))
);

CREATE INDEX idx_orders_user ON orders(user_id);
CREATE INDEX idx_orders_pair_status ON orders(pair_id, status);
CREATE INDEX idx_orders_created ON orders(created_at DESC);

-- ============================================================
-- TRADES
-- ============================================================
CREATE TABLE trades (
    id UUID PRIMARY KEY,
    buy_order_id UUID NOT NULL,
    sell_order_id UUID NOT NULL,
    pair_id INT NOT NULL,
    price NUMERIC(38, 18) NOT NULL,
    quantity NUMERIC(38, 18) NOT NULL,
    taker_side TEXT NOT NULL,            -- 'BUY' or 'SELL'
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (taker_side IN ('BUY', 'SELL'))
);

CREATE INDEX idx_trades_pair_time ON trades(pair_id, executed_at DESC);
CREATE INDEX idx_trades_buy_order ON trades(buy_order_id);
CREATE INDEX idx_trades_sell_order ON trades(sell_order_id);

-- ============================================================
-- DEPOSITS
-- ============================================================
CREATE TABLE deposits (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    asset TEXT NOT NULL,
    amount NUMERIC(38, 18) NOT NULL,
    tx_hash TEXT NOT NULL,
    from_address TEXT,
    to_address TEXT,
    chain TEXT NOT NULL,
    confirmations INT NOT NULL DEFAULT 0,
    status TEXT NOT NULL,                -- 'PENDING', 'CONFIRMED', 'CREDITED', 'FAILED'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    confirmed_at TIMESTAMPTZ,
    credited_at TIMESTAMPTZ,
    UNIQUE(chain, tx_hash),
    CHECK (amount > 0),
    CHECK (status IN ('PENDING', 'CONFIRMED', 'CREDITED', 'FAILED'))
);

CREATE INDEX idx_deposits_user ON deposits(user_id, created_at DESC);
CREATE INDEX idx_deposits_status ON deposits(status);

-- ============================================================
-- MOCK STATE (chain-watcher driver state)
-- ============================================================
CREATE TABLE mock_state (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- SESSIONS (refresh tokens for JWT)
-- ============================================================
CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    refresh_token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_token ON sessions(refresh_token_hash);

-- ============================================================
-- SEED DATA
-- ============================================================

-- Chains
INSERT INTO chains (name, chain_id, native_currency) VALUES
    ('ethereum', 1, 'ETH'),
    ('bsc', 56, 'BNB');

-- Currencies
INSERT INTO currencies (symbol, name, precision, min_withdraw, max_withdraw) VALUES
    ('BTC', 'Bitcoin', 8, 0.0001, 100),
    ('ETH', 'Ethereum', 18, 0.001, 1000),
    ('BNB', 'Binance Coin', 18, 0.01, 10000),
    ('USDT', 'Tether', 6, 1, 1000000);

-- Trading pairs (base/quote)
INSERT INTO trading_pairs (base, quote, min_qty, max_qty, price_precision, qty_precision) VALUES
    ('BTC', 'USDT', 0.00001, 100, 2, 8),
    ('ETH', 'USDT', 0.0001, 1000, 2, 6),
    ('BNB', 'USDT', 0.001, 10000, 2, 6),

-- Mock state initial values
INSERT INTO mock_state (key, value) VALUES
    ('mock_deposits_total', '0'),
    ('last_mock_deposit_at', '');