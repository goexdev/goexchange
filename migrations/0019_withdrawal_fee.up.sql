-- 0019_withdrawal_fee.up.sql
-- Tracks fees deducted from withdrawals

ALTER TABLE withdrawals
  ADD COLUMN IF NOT EXISTS fee NUMERIC(38, 18) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS receive_amount NUMERIC(38, 18) NOT NULL DEFAULT 0;

-- Initial: existing withdrawals have fee=0, receive=amount
UPDATE withdrawals SET receive_amount = amount WHERE receive_amount = 0;