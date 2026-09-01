-- 0032: add block_number to deposits so the scanner can compute
-- confirmations.
--
-- Migration 0029 created the deposits table but did not store
-- the chain block height. The scanner's "confirm/credit" pass
-- needs that to compute confirmations = head - deposit_block.
-- Block_hash is kept for forensic value but cannot be looked up
-- without a chain RPC roundtrip per row, which would multiply
-- our rate-limit pressure.

ALTER TABLE deposits ADD COLUMN IF NOT EXISTS block_number bigint;
CREATE INDEX IF NOT EXISTS idx_deposits_block_number ON deposits (chain, block_number);

-- Backfill any rows that already have block_hash so we can look
-- them up by number later. Existing rows that have neither are
-- left NULL; they will never be auto-confirmed by the new pass
-- and require manual reconciliation.