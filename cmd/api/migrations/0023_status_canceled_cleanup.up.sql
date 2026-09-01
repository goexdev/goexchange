-- 0023_status_canceled_cleanup.up.sql
-- Bug #21 fix: orders.status check accepts both 'CANCELED' (1 L, used by
-- the matching engine via matching.StatusCanceled) and 'CANCELLED' (2 L,
-- used by the API / migrations). Two spellings have caused subtle bugs:
-- a row inserted with 'CANCELED' from the matching engine cache-reload
-- path can match either spelling and confuse downstream queries.
--
-- Strategy:
--   1. Backfill: rewrite any existing 'CANCELED' rows to 'CANCELLED'.
--   2. Tighten the check to accept only 'CANCELLED'.
--   3. Add a BEFORE INSERT / UPDATE trigger that normalizes 'CANCELED' to
--      'CANCELLED' so any legacy code path inserting the 1-L spelling
--      silently writes the canonical 2-L value.
--
-- The matching engine (proprietary repo) uses matching.StatusCanceled =
-- "CANCELED" in its in-memory state. Any new code path that translates
-- the engine status to DB must normalize to 'CANCELLED' before INSERT.
-- The trigger below is a safety net for any code that forgets.

-- 1. Backfill first so the new (tighter) check constraint does not reject
-- existing rows during the migration itself.
UPDATE orders SET status = 'CANCELLED' WHERE status = 'CANCELED';

-- 2. Tighten the check constraint.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('OPEN', 'PARTIAL', 'FILLED', 'CANCELLED'));

-- 3. Normalization trigger: rewrite CANCELED -> CANCELLED on write.
CREATE OR REPLACE FUNCTION normalize_orders_status()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'CANCELED' THEN
        NEW.status := 'CANCELLED';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_normalize_orders_status ON orders;
CREATE TRIGGER trg_normalize_orders_status
    BEFORE INSERT OR UPDATE OF status ON orders
    FOR EACH ROW
    EXECUTE FUNCTION normalize_orders_status();
