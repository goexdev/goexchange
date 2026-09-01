-- User notification preferences
-- Allows users to control which notifications they receive.
-- Defaults: critical notifications ON, informational ones OFF

CREATE TABLE IF NOT EXISTS user_notification_prefs (
    user_id                   UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

    -- 2FA notifications (critical, default ON)
    notify_2fa_enabled        BOOLEAN NOT NULL DEFAULT true,
    notify_2fa_disabled       BOOLEAN NOT NULL DEFAULT true,
    notify_2fa_backup_used    BOOLEAN NOT NULL DEFAULT true,
    notify_2fa_failed         BOOLEAN NOT NULL DEFAULT true,

    -- 2FA login notifications (informational, default OFF to avoid spam)
    notify_2fa_login_success  BOOLEAN NOT NULL DEFAULT false,

    -- Future: other notification prefs
    notify_login              BOOLEAN NOT NULL DEFAULT false,
    notify_withdrawal         BOOLEAN NOT NULL DEFAULT true,
    notify_large_withdraw     BOOLEAN NOT NULL DEFAULT true,

    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_notif_prefs_user ON user_notification_prefs(user_id);

-- Helper function: insert default preferences for new user
CREATE OR REPLACE FUNCTION create_default_notification_prefs(target_user_id UUID)
RETURNS VOID AS $$
BEGIN
    INSERT INTO user_notification_prefs (user_id)
    VALUES (target_user_id)
    ON CONFLICT (user_id) DO NOTHING;
END;
$$ LANGUAGE plpgsql;

-- Trigger: auto-create preferences when user is created
-- (Check if users table has trigger support; if not, app handles it)
