-- 0012_audit_nullable_admin.down.sql
-- Note: will fail if there are any rows with NULL admin_user_id
UPDATE audit_log SET admin_user_id = '00000000-0000-0000-0000-000000000000' WHERE admin_user_id IS NULL;
ALTER TABLE audit_log ALTER COLUMN admin_user_id SET NOT NULL;
