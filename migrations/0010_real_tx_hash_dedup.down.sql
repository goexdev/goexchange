-- 0010_real_tx_hash_dedup.down.sql
ALTER TABLE deposits DROP CONSTRAINT IF EXISTS deposits_chain_txhash_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_deposits_dedup
    ON deposits (chain, to_address, amount)
    WHERE tx_hash LIKE 'poll-%';
