-- 0021_sol_trading_pair.up.sql
-- Add SOL_USDT trading pair (was missing from initial migration)
INSERT INTO trading_pairs (base, quote, min_qty, max_qty, price_precision, qty_precision, enabled)
VALUES ('SOL', 'USDT', 0.01, 10000, 2, 4, true)
ON CONFLICT (base, quote) DO NOTHING;