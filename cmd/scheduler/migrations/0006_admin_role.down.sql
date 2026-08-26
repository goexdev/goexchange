-- 0006_admin_role.down.sql
ALTER TABLE users DROP COLUMN IF EXISTS role;
