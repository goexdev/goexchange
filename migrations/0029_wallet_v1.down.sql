-- 0029 DOWN: reverse the wallet V1 schema additions.
--
-- Order matters: drop dependent objects first, then rename, then
-- restore the original uniqueness on deposits / assigned_addresses.

-- 7. reconciliation_runs
DROP TABLE IF EXISTS reconciliation_runs;

-- 6. sweep_tasks
DROP TABLE IF EXISTS sweep_tasks;

-- 5. withdrawal_idempotency
DROP TABLE IF EXISTS withdrawal_idempotency;

-- 4. blockchain_transactions
DROP TABLE IF EXISTS blockchain_transactions;

-- 3. withdrawals (restore original status CHECK)
ALTER TABLE withdrawals DROP CONSTRAINT IF EXISTS withdrawals_status_check;
ALTER TABLE withdrawals ADD CONSTRAINT withdrawals_status_check CHECK (
    status IN ('PENDING', 'APPROVED', 'REJECTED', 'BROADCASTED', 'CONFIRMED', 'FAILED', 'CANCELLED')
);

-- 2. deposits (revert uniqueness + drop added columns)
ALTER TABLE deposits DROP CONSTRAINT IF EXISTS deposits_chain_tx_event_key;
ALTER TABLE deposits
    DROP COLUMN IF EXISTS block_hash,
    DROP COLUMN IF EXISTS event_index;

-- Re-create the original single-txhash constraint under both candidate
-- names so the migration is symmetric (regardless of which name was
-- in place before 0029).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'deposits_chain_txhash_unique'
           AND conrelid = 'deposits'::regclass
    ) THEN
        ALTER TABLE deposits
            ADD CONSTRAINT deposits_chain_txhash_unique
            UNIQUE (chain, tx_hash);
    END IF;
END $$;

-- 1. wallet_addresses (drop extension columns + uniqueness, rename back)
ALTER TABLE wallet_addresses DROP CONSTRAINT IF EXISTS wallet_addresses_chain_address_key;

-- Idempotent rename back to assigned_addresses first: the rest of the
-- section (drop columns / drop / re-add constraints) must run on the
-- table under its original name so that the column-level IF EXISTS guards
-- line up with the original schema. The rename is guarded against
-- re-running with a DO block.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_class WHERE relname = 'wallet_addresses'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_class WHERE relname = 'assigned_addresses'
    ) THEN
        ALTER TABLE wallet_addresses RENAME TO assigned_addresses;
    END IF;
END $$;

ALTER TABLE assigned_addresses
    DROP COLUMN IF EXISTS exp_time,
    DROP COLUMN IF EXISTS memo,
    DROP COLUMN IF EXISTS address_hex,
    DROP COLUMN IF EXISTS wallet_type;

-- Restore the original nullable asset column. Migration 0029 added
-- asset as VARCHAR(32) NOT NULL DEFAULT ''; the original column
-- (from 0004) was VARCHAR(16) NULL with no default. We can't shrink
-- the column width without rewriting the table, so we just relax the
-- NOT NULL and drop the DEFAULT to match the original schema.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
         WHERE table_name = 'assigned_addresses'
           AND column_name  = 'asset'
           AND is_nullable  = 'NO'
    ) THEN
        ALTER TABLE assigned_addresses ALTER COLUMN asset DROP NOT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
         WHERE table_name = 'assigned_addresses'
           AND column_name  = 'asset'
           AND column_default IS NOT NULL
    ) THEN
        ALTER TABLE assigned_addresses ALTER COLUMN asset DROP DEFAULT;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'assigned_addresses_user_chain_asset_unique'
           AND conrelid = 'assigned_addresses'::regclass
    ) THEN
        ALTER TABLE assigned_addresses
            ADD CONSTRAINT assigned_addresses_user_chain_asset_unique
            UNIQUE (user_id, chain, asset);
    END IF;
END $$;