-- Migration 0026 (down): drop nonce tracking

ALTER TABLE api_keys DROP COLUMN IF EXISTS last_nonce;
