-- Migration 0014: API Keys for programmatic access
CREATE TABLE api_keys (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    name TEXT NOT NULL,
    key_id TEXT NOT NULL UNIQUE,           -- public identifier (first 12 chars)
    key_hash TEXT NOT NULL,                -- bcrypt hash of secret
    scopes TEXT[] NOT NULL DEFAULT ARRAY['read'],
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    revoked BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_api_keys_user ON api_keys(user_id) WHERE NOT revoked;
CREATE INDEX idx_api_keys_key_id ON api_keys(key_id) WHERE NOT revoked;
