-- 0009_deposit_dedup.up.sql
-- M5.6: Prevent duplicate deposit records

-- Drop duplicates one more time (idempotent)
DELETE FROM deposits d
USING (
    SELECT MIN(id::text)::uuid as keep_id, user_id, to_address, amount
    FROM deposits
    WHERE tx_hash LIKE 'poll-%'
    GROUP BY user_id, to_address, amount
    HAVING COUNT(*) > 1
) g
WHERE d.user_id = g.user_id 
  AND d.to_address = g.to_address 
  AND d.amount = g.amount
  AND d.id::text != g.keep_id::text
  AND d.tx_hash LIKE 'poll-%';

-- Note: tx_hash is currently synthetic (poll-XXX-timestamp)
-- In production, this should be the real on-chain tx hash from listtransactions
-- A real UNIQUE(chain, tx_hash) constraint would prevent duplicates
-- For now, use (chain, to_address, amount) as composite dedup key
CREATE UNIQUE INDEX IF NOT EXISTS idx_deposits_dedup
    ON deposits (chain, to_address, amount)
    WHERE tx_hash LIKE 'poll-%';

-- Document the bug
COMMENT ON INDEX idx_deposits_dedup IS 'Prevents duplicate poll-based deposit records';
