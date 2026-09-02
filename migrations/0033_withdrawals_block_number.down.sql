-- 0033 — rollback block_number tracking
DROP INDEX IF EXISTS idx_withdrawals_pending_confirm;
ALTER TABLE withdrawals DROP COLUMN IF EXISTS block_number;