-- 0022_trigger_orders.down.sql
-- Drop trigger_orders table and its indexes.
DROP INDEX IF EXISTS idx_trigger_orders_pending_pair;
DROP INDEX IF EXISTS idx_trigger_orders_user;
DROP TABLE IF EXISTS trigger_orders;
