-- 0005_kyc_limits.up.sql
-- M3: KYC levels + withdraw limits

-- 1. Change kyc_level default to 0 (L0 = registered, no KYC)
ALTER TABLE users ALTER COLUMN kyc_level SET DEFAULT 0;

-- 2. Add kyc_status and kyc_submitted_at columns
ALTER TABLE users ADD COLUMN IF NOT EXISTS kyc_status VARCHAR(20) NOT NULL DEFAULT 'NONE';
-- kyc_status: NONE, PENDING, APPROVED, REJECTED

ALTER TABLE users ADD COLUMN IF NOT EXISTS kyc_submitted_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS kyc_approved_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS kyc_rejected_reason TEXT;

-- 3. Allow existing data to be valid (CHECK constraint update)
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_kyc_level_check;
ALTER TABLE users ADD CONSTRAINT users_kyc_level_check 
  CHECK (kyc_level >= 0 AND kyc_level <= 2);

-- 4. KYC submissions table (audit trail)
CREATE TABLE IF NOT EXISTS kyc_submissions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id),
    target_level INT NOT NULL CHECK (target_level BETWEEN 1 AND 2),
    full_name    TEXT NOT NULL,
    id_number    TEXT NOT NULL,           -- ID number (encrypted in production)
    country      TEXT NOT NULL,
    doc_front    TEXT,                    -- URL to ID front image
    doc_back     TEXT,                    -- URL to ID back image
    selfie       TEXT,                    -- URL to selfie with ID
    status       VARCHAR(20) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    submitted_at TIMESTAMPTZ DEFAULT NOW(),
    reviewed_at  TIMESTAMPTZ,
    reviewer_note TEXT
);

CREATE INDEX idx_kyc_submissions_user ON kyc_submissions(user_id);
CREATE INDEX idx_kyc_submissions_status ON kyc_submissions(status);

-- 5. Withdraw limits are purely computed from kyc_level, no need for separate table
-- (keeps state simple, can be overridden by admin in future)
