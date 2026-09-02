-- 0034 — sweep planner dedup. One PENDING sweep per
-- (from_address_id, asset). Partial unique index lets the
-- planner ON CONFLICT DO NOTHING without churning
-- COMPLETED/FAILED rows.
CREATE UNIQUE INDEX idx_sweep_pending_unique
  ON sweep_tasks (from_address_id, asset)
  WHERE status = 'PENDING';