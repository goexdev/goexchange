-- 0030 DOWN: re-enable BTC and ETH (revert small-exchange policy).
--
-- Idempotent: only flips rows that are currently FALSE back to TRUE.
-- Rows that an operator already re-enabled by hand are left alone.

UPDATE trading_pairs SET enabled = TRUE
WHERE base IN ('BTC', 'ETH') AND enabled = FALSE;