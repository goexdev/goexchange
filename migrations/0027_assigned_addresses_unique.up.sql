-- Migration 0027: fix assigned_addresses unique constraint
--
-- The original 0004 migration declared UNIQUE(address, memo) but
-- address+memo collisions across users are actually expected:
-- the demo EVM driver returns the placeholder 0x0000...0000 for
-- every user, so the moment two users request a deposit address
-- for the same asset, the second INSERT fails with
--   23505 duplicate key value violates unique constraint
--   "assigned_addresses_address_memo_key"
--
-- The correct invariant is "one row per (user, chain, asset)"
-- — each user gets at most one deposit address per chain/asset
-- combination, and rows from different users never collide.
--
-- UAPI-6 audit fix: also distinct from the previous attempt to
-- add a per-(address, memo) row, which was a workaround for the
-- demo driver, not the underlying constraint.
ALTER TABLE assigned_addresses
    DROP CONSTRAINT IF EXISTS assigned_addresses_address_memo_key;

ALTER TABLE assigned_addresses
    ADD CONSTRAINT assigned_addresses_user_chain_asset_unique
    UNIQUE (user_id, chain, asset);
