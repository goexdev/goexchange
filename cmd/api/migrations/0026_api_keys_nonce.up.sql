-- Migration 0026: nonce tracking on api_keys
--
-- HMAC-signed user-api requests carry an X-Api-Nonce header that
-- must be strictly greater than the last accepted nonce for the
-- key, within a ±5 minute clock-skew window. Without this, an
-- attacker who captures a valid signed request could replay it
-- up to 5 minutes after the original (the timestamp check
-- already blocks anything older than that).
--
-- The previous migration (0014) created api_keys without nonce
-- state, so we add it now. last_nonce starts at 0 — strictly
-- greater comparisons work as long as the first request uses a
-- nonce > 0 (which it always will be, since nonces are unix ms
-- timestamps like 1735689600123).

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS last_nonce BIGINT NOT NULL DEFAULT 0;

-- No unique constraint: last_nonce is per-key, not global, and
-- the WHERE key_id=$1 predicate in service.go prevents two
-- concurrent requests from interleaving.
