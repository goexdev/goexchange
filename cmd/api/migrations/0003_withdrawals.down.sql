-- 0003_withdrawals.down.sql
DROP TABLE IF EXISTS withdrawals;
ALTER TABLE chains DROP COLUMN IF EXISTS driver;
