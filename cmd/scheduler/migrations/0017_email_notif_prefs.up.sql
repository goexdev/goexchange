-- Email notification preferences
-- Allows users to opt-in to email delivery per notification type
-- (separate from in-app notification preferences)

ALTER TABLE user_notification_prefs
    ADD COLUMN IF NOT EXISTS email_2fa_enabled        BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS email_2fa_disabled       BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS email_2fa_backup_used    BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS email_2fa_failed         BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS email_2fa_login_success  BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS email_login              BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS email_withdrawal         BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS email_large_withdraw     BOOLEAN NOT NULL DEFAULT true;

-- Add comments
COMMENT ON COLUMN user_notification_prefs.email_2fa_enabled
    IS 'Send email when 2FA is enabled';
COMMENT ON COLUMN user_notification_prefs.email_2fa_disabled
    IS 'Send email when 2FA is disabled (critical)';
