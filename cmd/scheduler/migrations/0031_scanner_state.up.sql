-- 0031: Scanner cursor table.
--
-- Holds one row per chain so the tron-scanner (or any future chain
-- scanner) knows which block height it last delivered events for.
-- A restart of the daemon reads the cursor and resumes scanning
-- from cursor+1, avoiding the "scan from genesis on every
-- restart" failure mode.
--
-- Idempotent on re-apply because the table is genuinely new — the
-- IF NOT EXISTS guard makes the second application a no-op so we
-- can keep schema_migrations monotonic.

CREATE TABLE IF NOT EXISTS scanner_state (
    chain               VARCHAR(32) PRIMARY KEY,
    last_scanned_block  BIGINT NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);