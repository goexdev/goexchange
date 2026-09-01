-- 0028 rollback. Drop in reverse order.

ALTER TABLE email_outbox DROP COLUMN IF EXISTS html_body;

DROP TABLE IF EXISTS password_reset_tokens;
DROP TABLE IF EXISTS email_verify_tokens;

ALTER TABLE users DROP COLUMN IF EXISTS email_verified;
