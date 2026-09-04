-- 0036 -- mark bot user accounts so business logic can
-- bypass risk scoring, withdrawal 2FA, and trade-history reporting
-- for them.
--
-- A "bot user" is an account owned by the market-making bot
-- (cmd/mmbot). The bot places orders as this user, but is not a
-- human trader, so it should not:
--
--   * trip risk-based withdrawal blocks
--   * require 2FA for withdrawals
--   * show up in a normal user's "trade history" reports
--   * be rate-limited under the user-facing order-place limiter
--
-- Adding the column is forward-only; the default FALSE preserves
-- behaviour for the ~100k existing rows.
--
-- A partial unique index enforces the operational rule that
-- exactly one bot user account exists at any time. If a future
-- migration splits market-making across multiple accounts (one
-- per region, per asset class, etc.), drop this index.

ALTER TABLE users ADD COLUMN is_bot_user BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN users.is_bot_user IS
 'TRUE for accounts owned by the bot engine (cmd/mmbot). '
  'Such accounts bypass risk scoring, withdrawal 2FA, and '
  'audit logging. The partial unique index '
  'idx_users_one_bot_account ensures at most one such row exists.';

CREATE UNIQUE INDEX idx_users_one_bot_account
  ON users (is_bot_user)
  WHERE is_bot_user = TRUE;