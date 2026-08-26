-- 0002_status_canceled.up.sql
-- M0.5: Allow 'CANCELED' status (Go convention, 1 L) in addition to 'CANCELLED' (2 L)
-- We use 'CANCELED' throughout the code (matching.StatusCanceled)

ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('OPEN', 'PARTIAL', 'FILLED', 'CANCELED', 'CANCELLED'));
