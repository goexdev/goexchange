-- 0018_withdraw_fees.up.sql
-- Adds per-currency withdrawal fee configuration
-- Admin can configure fees via /admin/currencies/{symbol} API

ALTER TABLE currencies
  ADD COLUMN IF NOT EXISTS withdraw_fee_flat NUMERIC(38, 18) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS withdraw_fee_percent NUMERIC(10, 6) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS withdraw_fee_min NUMERIC(38, 18) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Set sensible defaults per asset
UPDATE currencies SET withdraw_fee_flat = 0.0005, withdraw_fee_percent = 0.001, withdraw_fee_min = 0.0001 WHERE symbol = 'BTC';
UPDATE currencies SET withdraw_fee_flat = 0.005,  withdraw_fee_percent = 0.001, withdraw_fee_min = 0.001  WHERE symbol = 'ETH';
UPDATE currencies SET withdraw_fee_flat = 0.005,  withdraw_fee_percent = 0.001, withdraw_fee_min = 0.01  WHERE symbol = 'BNB';
UPDATE currencies SET withdraw_fee_flat = 1.0,    withdraw_fee_percent = 0.0,   withdraw_fee_min = 1.0    WHERE symbol = 'USDT';
UPDATE currencies SET withdraw_fee_flat = 1.0,    withdraw_fee_percent = 0.0,   withdraw_fee_min = 1.0    WHERE symbol = 'USDC';
UPDATE currencies SET withdraw_fee_flat = 0.01,   withdraw_fee_percent = 0.001, withdraw_fee_min = 0.01  WHERE symbol = 'SOL';
-- Other currencies keep defaults (0)