-- 0028: Email verification + password reset tokens.
--
-- Adds:
--   * users.email_verified: explicit verification flag, separate from
--     "user has logged in before". The check is enforced by the
--     loginHandler: a user whose flag is false gets
--     {requires_email_verification: true} back instead of a token.
--   * email_verify_tokens: short-lived (24h) tokens emailed after
--     registration and again on each "resend verification" request.
--     Single-use; consumed by the verify-email handler.
--   * password_reset_tokens: short-lived (1h) tokens emailed when
--     a user requests a reset. Single-use; consumed by the
--     reset-password handler. Tokens are stored as bcrypt hashes
--     so a DB leak does not let an attacker reset accounts.
--   * email_outbox.html_body: nullable HTML body for transactional
--     emails (verify, reset, deposit). When set, the outbox
--     worker prefers it over the plain body. Existing rows stay
--     valid (NULL = text-only, legacy).

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_verified BOOLEAN NOT NULL DEFAULT FALSE;

-- Backfill: existing users (alice/bob/admin) are treated as already
-- verified. They were created by the seed script for testing; they
-- never had a verification token, and refusing to let them log in
-- after this migration would break the test environment.
UPDATE users SET email_verified = TRUE WHERE created_at < NOW();

CREATE TABLE IF NOT EXISTS email_verify_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,                       -- bcrypt of opaque token
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,                                -- NULL until consumed
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email_verify_tokens_user
    ON email_verify_tokens(user_id) WHERE used_at IS NULL;

CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user
    ON password_reset_tokens(user_id) WHERE used_at IS NULL;

ALTER TABLE email_outbox
    ADD COLUMN IF NOT EXISTS html_body TEXT;
