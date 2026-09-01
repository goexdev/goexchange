-- 0019_withdrawal_fee.down.sql
ALTER TABLE withdrawals
  DROP COLUMN IF EXISTS fee,
  DROP COLUMN IF EXISTS receive_amount;