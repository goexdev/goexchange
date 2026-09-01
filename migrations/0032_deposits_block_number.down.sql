-- 0032 DOWN: drop the index and column added in 0032.
--
-- The down migration drops the block_number column. Any existing
-- deposits with block_number filled will lose that information
-- after the down migration; in practice down migrations are only
-- run when rolling back a freshly-applied migration, so this is
-- safe.

DROP INDEX IF EXISTS idx_deposits_block_number;
ALTER TABLE deposits DROP COLUMN IF EXISTS block_number;