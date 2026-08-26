-- 0004_assigned_addresses.up.sql
-- M1: assigned_addresses table for chain watching

CREATE TABLE assigned_addresses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    address     VARCHAR(128) NOT NULL,
    chain       VARCHAR(32) NOT NULL,
    asset       VARCHAR(16),
    exp_time    TIMESTAMPTZ NOT NULL DEFAULT '2099-12-31 23:59:59+00',
    memo        VARCHAR(64) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(address, memo)
);

CREATE INDEX idx_assigned_addr_user ON assigned_addresses(user_id);
CREATE INDEX idx_assigned_addr_chain ON assigned_addresses(chain);
