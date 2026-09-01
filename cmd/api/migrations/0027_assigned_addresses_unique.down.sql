-- Migration 0027 (down): revert unique constraint

ALTER TABLE assigned_addresses
    DROP CONSTRAINT IF EXISTS assigned_addresses_user_chain_asset_unique;

ALTER TABLE assigned_addresses
    ADD CONSTRAINT assigned_addresses_address_memo_key
    UNIQUE (address, memo);
