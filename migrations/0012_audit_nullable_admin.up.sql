-- 0012_audit_nullable_admin.up.sql
-- Allow audit log entries without an authenticated admin (login/register attempts)
ALTER TABLE audit_log ALTER COLUMN admin_user_id DROP NOT NULL;
