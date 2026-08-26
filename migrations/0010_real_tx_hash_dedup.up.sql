-- 0010_real_tx_hash_dedup.up.sql
-- M5.7: Use real on-chain tx_hash for deposit dedup
-- Drop the partial index on synthetic poll-XXX hashes
-- Add full UNIQUE constraint on (chain, tx_hash)

DROP INDEX IF EXISTS idx_deposits_dedup;

-- Add real UNIQUE constraint
ALTER TABLE deposits ADD CONSTRAINT deposits_chain_txhash_unique UNIQUE (chain, tx_hash);

-- Update recordDeposit to use ON CONFLICT (chain, tx_hash)
COMMENT ON CONSTRAINT deposits_chain_txhash_unique ON deposits IS
  'Prevents duplicate deposits using real on-chain tx_hash (replaces poll-XXX synthetic hash)';
