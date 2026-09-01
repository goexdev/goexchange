-- 0029: Wallet Service V1 schema (USDT-TRC20 + multi-chain abstraction).
--
-- Renames the existing assigned_addresses table to wallet_addresses and
-- adds the columns needed by the wallet service abstraction:
--
--   * wallet_type: discriminates between user-facing DEPOSIT addresses,
--     the company HOT wallet (sweep destination / withdrawal source),
--     the COLD wallet (offline, periodic refills), and OPERATIONAL
--     addresses used by infra (e.g. funding for TRX).
--   * address_hex: hex form for hashing / indexing. TRON hex has a
--     leading "41" before the EVM-style 20-byte address; EVM hex is
--     "0x..."; BTC uses bech32 / P2PKH encoding and we keep base58
--     only there for now (NULL acceptable).
--   * chain / asset now explicit columns (NOT NULL by default) so the
--     adapter layer can index by (chain, asset) without inspecting
--     caller-supplied strings.
--
-- The deposits table gains event_index and the withdrawals table gains
-- the full status machine (RISK_CHECK, QUEUED, SIGNING, ...). Both
-- tables also drop their old single-txhash uniqueness constraint and
-- replace it with the (chain, tx_hash, event_index) variant so a single
-- smart contract transaction carrying multiple Transfer events (TRC20,
-- ERC20, etc.) does not collide.
--
-- New tables:
--   * blockchain_transactions: per-chain tx tracking used by the
--     reconciler and the BROADCAST_UNKNOWN recovery flow.
--   * withdrawal_idempotency: (user_id, idempotency_key) primary key
--     so duplicate POST /wallet/withdrawals calls return the existing
--     withdrawal instead of creating a new one.
--   * sweep_tasks: tracks deposit -> hot wallet sweeps, including
--     separate status for the TRX funding sub-tx.
--   * reconciliation_runs: per-day report emitted by the reconciler.

-- ---------------------------------------------------------------------------
-- 1. wallet_addresses (rename + extend assigned_addresses)
-- ---------------------------------------------------------------------------

-- Idempotent rename: assigned_addresses -> wallet_addresses. Skipped
-- if wallet_addresses already exists (which happens on re-run after
-- a partial failure). The IF EXISTS in / DROP/ADD below assume this
-- is the case.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_class WHERE relname = 'assigned_addresses'
    ) AND NOT EXISTS (
        SELECT 1 FROM pg_class WHERE relname = 'wallet_addresses'
    ) THEN
        ALTER TABLE assigned_addresses RENAME TO wallet_addresses;
    END IF;
END $$;

-- The old assigned_addresses.user_id FK was on a different table layout
-- than what the wallet service needs. Re-state the constraint explicitly
-- so the schema is self-documenting.
ALTER TABLE wallet_addresses
    ADD COLUMN IF NOT EXISTS wallet_type  VARCHAR(32) NOT NULL DEFAULT 'DEPOSIT',
    ADD COLUMN IF NOT EXISTS address_hex  VARCHAR(128),
    ADD COLUMN IF NOT EXISTS asset        VARCHAR(32)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS memo         TEXT,
    ADD COLUMN IF NOT EXISTS exp_time     TIMESTAMPTZ;

-- The old unique (user_id, chain, asset) is no longer the right
-- invariant: a single user may have a DEPOSIT and a separate HOT
-- address on the same chain (different wallet_type), and two users
-- must never share an address. Switch to UNIQUE(chain, address).
-- Note the constraint name was set by migration 0027 as
-- "assigned_addresses_user_chain_asset_unique" (no "id_" infix),
-- not the "_user_id_chain_asset_key" form used elsewhere; both
-- candidate names are dropped defensively to make this migration
-- robust against re-orderings.
ALTER TABLE wallet_addresses
    DROP CONSTRAINT IF EXISTS assigned_addresses_user_id_chain_asset_key;
ALTER TABLE wallet_addresses
    DROP CONSTRAINT IF EXISTS assigned_addresses_user_chain_asset_unique;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'wallet_addresses_chain_address_key'
           AND conrelid = 'wallet_addresses'::regclass
    ) THEN
        ALTER TABLE wallet_addresses
            ADD CONSTRAINT wallet_addresses_chain_address_key
            UNIQUE (chain, address);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_wallet_addresses_user
    ON wallet_addresses(user_id, chain);

CREATE INDEX IF NOT EXISTS idx_wallet_addresses_wallet_type
    ON wallet_addresses(chain, wallet_type);

-- ---------------------------------------------------------------------------
-- 2. deposits (event_index + new unique + extended statuses)
-- ---------------------------------------------------------------------------

ALTER TABLE deposits
    ADD COLUMN IF NOT EXISTS event_index  INT          NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS block_hash   VARCHAR(128);

-- Old single-tx uniqueness conflicts with multi-event TRC20/ERC20
-- transactions. Drop and replace. Note the original constraint was
-- added by migration 0010 as "deposits_chain_txhash_unique" (no
-- underscore between "tx" and "hash"); the early naming convention
-- used a different separator than migration 0027, so we drop both
-- candidate names defensively.
ALTER TABLE deposits DROP CONSTRAINT IF EXISTS deposits_chain_tx_hash_key;
ALTER TABLE deposits DROP CONSTRAINT IF EXISTS deposits_chain_txhash_unique;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'deposits_chain_tx_event_key'
           AND conrelid = 'deposits'::regclass
    ) THEN
        ALTER TABLE deposits
            ADD CONSTRAINT deposits_chain_tx_event_key
            UNIQUE (chain, tx_hash, event_index);
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- 3. withdrawals (full status machine)
-- ---------------------------------------------------------------------------

-- The original schema had a CHECK with 6 statuses. Replace it with the
-- 15-status machine specified in the design doc (5.1).
ALTER TABLE withdrawals DROP CONSTRAINT IF EXISTS withdrawals_status_check;
ALTER TABLE withdrawals ADD CONSTRAINT withdrawals_status_check CHECK (
    status IN (
        'PENDING',           -- awaiting risk check
        'RISK_CHECK',
        'APPROVED',
        'QUEUED',            -- picked up by withdrawal worker
        'SIGNING',
        'SIGNED',
        'BROADCASTED',
        'BROADCAST_UNKNOWN', -- RPC did not respond; reconciler recovers
        'IN_BLOCK',
        'SOLIDIFIED',
        'COMPLETED',
        'REJECTED',
        'FAILED',
        'CANCELLED',
        'MANUAL_REVIEW'
    )
);

-- ---------------------------------------------------------------------------
-- 4. blockchain_transactions
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS blockchain_transactions (
    id              BIGSERIAL PRIMARY KEY,
    chain           VARCHAR(32)  NOT NULL,
    tx_hash         VARCHAR(128) NOT NULL,
    block_number    BIGINT,
    block_hash      VARCHAR(128),
    status          VARCHAR(32),                       -- SUCCESS / FAILED / PENDING
    raw_response    JSONB,                             -- raw RPC reply, debug only
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    included_at     TIMESTAMPTZ,
    solidified_at   TIMESTAMPTZ,
    UNIQUE(chain, tx_hash)
);

CREATE INDEX IF NOT EXISTS idx_bctx_status
    ON blockchain_transactions(chain, status);

-- ---------------------------------------------------------------------------
-- 5. withdrawal_idempotency
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS withdrawal_idempotency (
    user_id          UUID        NOT NULL,
    idempotency_key  VARCHAR(128) NOT NULL,
    withdrawal_id    UUID        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_withdrawal_idempotency_withdrawal
    ON withdrawal_idempotency(withdrawal_id);

-- ---------------------------------------------------------------------------
-- 6. sweep_tasks
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS sweep_tasks (
    id                BIGSERIAL PRIMARY KEY,
    chain             VARCHAR(32)  NOT NULL,
    asset             VARCHAR(32)  NOT NULL,
    from_address_id   UUID         NOT NULL REFERENCES wallet_addresses(id),
    to_address_id     UUID         NOT NULL REFERENCES wallet_addresses(id),
    amount            NUMERIC(38,0) NOT NULL,
    status            VARCHAR(32)  NOT NULL,
    tx_hash           VARCHAR(128),
    funding_tx_hash   VARCHAR(128),
    resource_check_at TIMESTAMPTZ,
    built_at          TIMESTAMPTZ,
    signed_at         TIMESTAMPTZ,
    broadcast_at      TIMESTAMPTZ,
    included_at       TIMESTAMPTZ,
    solidified_at     TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    last_error        TEXT,
    retry_count       INT          NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sweep_tasks_status
    ON sweep_tasks(chain, status, created_at);

-- ---------------------------------------------------------------------------
-- 7. reconciliation_runs
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS reconciliation_runs (
    id              BIGSERIAL PRIMARY KEY,
    chain           VARCHAR(32)  NOT NULL,
    asset           VARCHAR(32)  NOT NULL,
    run_type        VARCHAR(32)  NOT NULL DEFAULT 'DAILY',
    start_at        TIMESTAMPTZ  NOT NULL,
    end_at          TIMESTAMPTZ  NOT NULL,
    onchain_amount  NUMERIC(38,0),
    ledger_amount   NUMERIC(38,0),
    difference      NUMERIC(38,0),
    status          VARCHAR(32)  NOT NULL,
    error           TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reconciliation_runs_chain
    ON reconciliation_runs(chain, created_at DESC);