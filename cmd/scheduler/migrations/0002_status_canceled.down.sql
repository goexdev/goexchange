-- 0002_status_canceled.down.sql
ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('OPEN', 'PARTIAL', 'FILLED', 'CANCELLED'));
