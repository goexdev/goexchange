-- Reverts the bot-user marker. Existing bot user rows become
-- regular users again; the matching engine will reject their
-- uuid.Nil ids on next order, so callers must retire the bot
-- account before running this migration.

DROP INDEX IF EXISTS idx_users_one_bot_account;

ALTER TABLE users DROP COLUMN IF EXISTS is_bot_user;