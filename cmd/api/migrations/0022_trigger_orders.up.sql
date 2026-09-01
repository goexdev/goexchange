-- 0022_trigger_orders.up.sql
-- Create trigger_orders table for stop loss / take profit conditional orders.
-- The trigger monitor (5s loop) scans pending rows and places a market order
-- when price reaches trigger_price.
--
-- Columns mirror the Go struct in internal/trigger/service.go:
-- TriggerOrder (id, user_id, pair, side, trigger_type, trigger_price,
-- quantity, status, triggered_at, triggered_order_id, cancelled_at,
-- created_at, updated_at).
CREATE TABLE trigger_orders (
    id                  UUID PRIMARY KEY,
    user_id             UUID NOT NULL REFERENCES users(id),
    pair                TEXT NOT NULL,
    side                TEXT NOT NULL,
    trigger_type        TEXT NOT NULL,
    trigger_price       NUMERIC(38,18) NOT NULL,
    quantity            NUMERIC(38,18) NOT NULL,
    status              TEXT NOT NULL DEFAULT 'PENDING',
    triggered_at        TIMESTAMPTZ,
    triggered_order_id  UUID REFERENCES orders(id),
    cancelled_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT trigger_orders_type_check
        CHECK (trigger_type IN ('STOP_LOSS', 'TAKE_PROFIT')),
    CONSTRAINT trigger_orders_status_check
        CHECK (status IN ('PENDING', 'TRIGGERED', 'CANCELLED', 'EXPIRED')),
    CONSTRAINT trigger_orders_side_check
        CHECK (side IN ('BUY', 'SELL')),
    CONSTRAINT trigger_orders_qty_positive
        CHECK (quantity > 0),
    CONSTRAINT trigger_orders_price_positive
        CHECK (trigger_price > 0)
);

-- Monitor scans pending triggers per pair. The (status, pair) prefix lets
-- the monitor skip rows it has already evaluated without a heap scan.
CREATE INDEX idx_trigger_orders_pending_pair
    ON trigger_orders (pair, status)
    WHERE status = 'PENDING';

-- Per-user listing ("my triggers").
CREATE INDEX idx_trigger_orders_user
    ON trigger_orders (user_id, created_at DESC);
