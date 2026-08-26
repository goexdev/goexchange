-- 0007_risk_control.down.sql
ALTER TABLE users DROP COLUMN IF EXISTS risk_score_updated_at;
ALTER TABLE users DROP COLUMN IF EXISTS last_risk_score;
ALTER TABLE withdrawals DROP CONSTRAINT IF EXISTS withdrawals_status_check;
ALTER TABLE withdrawals ADD CONSTRAINT withdrawals_status_check
  CHECK (status IN ('PENDING', 'APPROVED', 'BROADCAST', 'DONE', 'FAILED'));
ALTER TABLE withdrawals DROP COLUMN IF EXISTS risk_hold;
ALTER TABLE withdrawals DROP COLUMN IF EXISTS risk_score;
DROP TABLE IF EXISTS risk_events;
DROP TABLE IF EXISTS user_known_ips;
DROP TABLE IF EXISTS login_attempts;
