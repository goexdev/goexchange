-- 0003_withdrawals.up.sql
-- M1: Withdrawals table for chain sendtoaddress

CREATE TABLE withdrawals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id),
    asset        VARCHAR(16) NOT NULL,
    amount       NUMERIC(38,18) NOT NULL,
    dest_address VARCHAR(128) NOT NULL,
    tx_hash      VARCHAR(128),
    chain        VARCHAR(32) NOT NULL DEFAULT 'fortuneblock',
    status       VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    -- PENDING: debit done, send pending
    -- BROADCAST: sendtoaddress returned tx_hash, waiting for confirmations
    -- DONE: 6+ confirmations
    -- FAILED: send failed, balance refunded
    confirmations INT NOT NULL DEFAULT 0,
    error_msg    TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at      TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ
);

CREATE INDEX idx_withdrawals_user_id ON withdrawals(user_id);
CREATE INDEX idx_withdrawals_status ON withdrawals(status);
CREATE INDEX idx_withdrawals_tx_hash ON withdrawals(tx_hash);

-- Add driver column to chains
ALTER TABLE chains ADD COLUMN driver VARCHAR(32) NOT NULL DEFAULT 'mock';
