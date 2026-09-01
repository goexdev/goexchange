-- 0025: user address book table
-- The address book handler in /api/v1/users/me/addresses was
-- implemented against a `withdrawal_addresses` table that this
-- migration creates. Before this migration the POST handler errored
-- with `relation "withdrawal_addresses" does not exist` (which the
-- H2 audit of 2026-08-28 v0.2 flagged as a SQL-leak: we now return
-- "internal error" instead, and the underlying table actually
-- exists so legitimate address-book operations succeed).

CREATE TABLE IF NOT EXISTS withdrawal_addresses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    asset       VARCHAR(16) NOT NULL,
    address     VARCHAR(128) NOT NULL,
    label       VARCHAR(64) NOT NULL DEFAULT '',
    whitelisted BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, asset, address)
);
CREATE INDEX IF NOT EXISTS idx_withdrawal_addresses_user
    ON withdrawal_addresses(user_id, asset);
