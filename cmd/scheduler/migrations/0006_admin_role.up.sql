-- 0006_admin_role.up.sql
-- M3.5: Admin role for admin dashboard

ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'user';
-- role: 'user' (default), 'admin'

-- Create admin user for BOSS (will be set via password reset flow)
-- Email: boss@goexchange.local (already exists from registration)
-- UPDATE users SET role = 'admin' WHERE email = 'boss@goexchange.local';
