-- 0007_risk_control.up.sql
-- M4: Risk control - login attempts + risk events

-- Login attempts (track every login)
CREATE TABLE login_attempts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL,
    user_id       UUID,
    ip            INET NOT NULL,
    user_agent    TEXT,
    success       BOOLEAN NOT NULL,
    failure_reason TEXT,
    timestamp     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_login_attempts_email ON login_attempts(email);
CREATE INDEX idx_login_attempts_user_id ON login_attempts(user_id);
CREATE INDEX idx_login_attempts_ip ON login_attempts(ip);
CREATE INDEX idx_login_attempts_timestamp ON login_attempts(timestamp DESC);

-- User known IPs (last successful login per IP)
CREATE TABLE user_known_ips (
    user_id     UUID NOT NULL REFERENCES users(id),
    ip          INET NOT NULL,
    first_seen  TIMESTAMPTZ DEFAULT NOW(),
    last_seen   TIMESTAMPTZ DEFAULT NOW(),
    login_count INT DEFAULT 1,
    PRIMARY KEY (user_id, ip)
);

CREATE INDEX idx_user_known_ips_user ON user_known_ips(user_id);

-- Risk events (audit trail)
CREATE TABLE risk_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    event_type  VARCHAR(50) NOT NULL,
    -- LOGIN, WITHDRAWAL, KYC, etc.
    risk_score  INT NOT NULL,
    factors     JSONB NOT NULL,
    -- e.g. {"failed_attempts": 6, "new_ip": true, "amount_ratio": 0.85}
    action      VARCHAR(20) NOT NULL,
    -- ALLOW, HOLD, BLOCK
    context     JSONB,
    -- e.g. {"ip": "1.2.3.4", "amount": "1500"}
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_risk_events_user ON risk_events(user_id);
CREATE INDEX idx_risk_events_score ON risk_events(risk_score DESC);
CREATE INDEX idx_risk_events_created ON risk_events(created_at DESC);

-- Add risk_score to withdrawals + status HOLD
ALTER TABLE withdrawals ADD COLUMN IF NOT EXISTS risk_score INT DEFAULT 0;
ALTER TABLE withdrawals ADD COLUMN IF NOT EXISTS risk_hold BOOLEAN DEFAULT FALSE;
ALTER TABLE withdrawals DROP CONSTRAINT IF EXISTS withdrawals_status_check;
ALTER TABLE withdrawals ADD CONSTRAINT withdrawals_status_check
  CHECK (status IN ('PENDING', 'HOLD', 'APPROVED', 'BROADCAST', 'DONE', 'FAILED', 'REJECTED'));

-- Add risk_score to users (last computed)
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_risk_score INT DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS risk_score_updated_at TIMESTAMPTZ;
