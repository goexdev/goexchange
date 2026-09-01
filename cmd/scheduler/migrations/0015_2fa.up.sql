-- 2FA (TOTP) tables
--
-- user_totp: stores the TOTP secret for each user (encrypted)
-- user_backup_codes: stores hashed backup codes for account recovery

CREATE TABLE IF NOT EXISTS user_totp (
    user_id       UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret_encrypted  BYTEA NOT NULL,  -- encrypted with app secret
    enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_at  TIMESTAMPTZ,
    last_used_counter BIGINT,           -- prevent replay attacks
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_totp_enabled ON user_totp(enabled) WHERE enabled = true;

-- Backup codes: 8 single-use codes per user
-- Stored as bcrypt hashes
CREATE TABLE IF NOT EXISTS user_backup_codes (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash    TEXT NOT NULL,         -- bcrypt hash
    used         BOOLEAN NOT NULL DEFAULT FALSE,
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_codes_user ON user_backup_codes(user_id);
CREATE INDEX IF NOT EXISTS idx_backup_codes_user_unused ON user_backup_codes(user_id) WHERE used = false;

-- Add 2fa fields to audit log via JSONB details (no schema change needed)
