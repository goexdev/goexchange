-- 0021_sol_trading_pair.down.sql
-- Remove SOL_USDT trading pair
DELETE FROM trading_pairs WHERE base = 'SOL' AND quote = 'USDT';