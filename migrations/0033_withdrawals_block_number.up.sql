-- 0033 — add block_number to withdrawals so the confirmation
-- watcher can compute "block depth" against the current
-- solidified head.
--
-- block_number is set by the confirmation watcher when
-- gettransactioninfobyid first returns a receipt. NULL means
-- the tx is still in mempool or has not been picked up by any
-- node yet.
ALTER TABLE withdrawals
  ADD COLUMN block_number BIGINT;

-- Index so the watcher's "BROADCASTED + IN_BLOCK rows by created_at"
-- scan can use a partial index instead of a full scan.
CREATE INDEX idx_withdrawals_pending_confirm
  ON withdrawals (created_at)
  WHERE status IN ('BROADCASTED', 'IN_BLOCK');