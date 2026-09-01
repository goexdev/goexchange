-- 0023_status_canceled_cleanup.down.sql
-- Reverse the normalization trigger and the tightened check constraint.
-- We re-add the original two-spelling check for rollback safety.

DROP TRIGGER IF EXISTS trg_normalize_orders_status ON orders;
DROP FUNCTION IF EXISTS normalize_orders_status();

ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('OPEN', 'PARTIAL', 'FILLED', 'CANCELED', 'CANCELLED'));
