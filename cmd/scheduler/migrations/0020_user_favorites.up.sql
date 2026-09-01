-- 0020_user_favorites.up.sql
-- Per-user market favorites (watchlist) - cloud synced

CREATE TABLE IF NOT EXISTS user_favorites (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pair VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, pair)
);

CREATE INDEX IF NOT EXISTS idx_user_favorites_user ON user_favorites(user_id, created_at DESC);

-- Down
-- DROP TABLE IF EXISTS user_favorites;