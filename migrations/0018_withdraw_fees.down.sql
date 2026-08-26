-- 0018_withdraw_fees.down.sql
-- Remove withdraw fee columns

ALTER TABLE currencies
  DROP COLUMN IF EXISTS withdraw_fee_flat,
  DROP COLUMN IF EXISTS withdraw_fee_percent,
  DROP COLUMN IF EXISTS withdraw_fee_min,
  DROP COLUMN IF EXISTS updated_at;