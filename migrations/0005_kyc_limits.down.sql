-- 0005_kyc_limits.down.sql
DROP TABLE IF EXISTS kyc_submissions;
ALTER TABLE users DROP COLUMN IF EXISTS kyc_rejected_reason;
ALTER TABLE users DROP COLUMN IF EXISTS kyc_approved_at;
ALTER TABLE users DROP COLUMN IF EXISTS kyc_submitted_at;
ALTER TABLE users DROP COLUMN IF EXISTS kyc_status;
ALTER TABLE users ALTER COLUMN kyc_level SET DEFAULT 1;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_kyc_level_check;
ALTER TABLE users ADD CONSTRAINT users_kyc_level_check 
  CHECK (kyc_level >= 1 AND kyc_level <= 3);
