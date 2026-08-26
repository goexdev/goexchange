-- 0011_admin_audit_log.up.sql
-- M5.13: Admin operation audit log
-- Records all sensitive admin actions (KYC approval, role change, withdrawal approval, etc)
-- for compliance, security, and debugging.

CREATE TABLE audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Who did the action
    admin_user_id UUID NOT NULL,
    admin_email VARCHAR(255) NOT NULL,
    
    -- What action
    action VARCHAR(64) NOT NULL,
    
    -- What was affected
    target_type VARCHAR(32) NOT NULL,  -- 'user', 'kyc', 'withdrawal', 'deposit'
    target_id UUID,
    target_label VARCHAR(255),         -- human-readable: email, tx_hash, etc
    
    -- Action-specific data (old value, new value, reason, etc)
    details JSONB DEFAULT '{}'::jsonb,
    
    -- Request metadata
    ip INET,
    user_agent TEXT,
    
    -- Result
    status VARCHAR(16) NOT NULL DEFAULT 'success',  -- 'success', 'failure'
    error_msg TEXT,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for common queries
CREATE INDEX idx_audit_admin ON audit_log (admin_user_id, created_at DESC);
CREATE INDEX idx_audit_action ON audit_log (action, created_at DESC);
CREATE INDEX idx_audit_target ON audit_log (target_type, target_id, created_at DESC);
CREATE INDEX idx_audit_created ON audit_log (created_at DESC);

-- Prevent modification (append-only)
-- Note: We use a trigger to enforce immutability
CREATE OR REPLACE FUNCTION audit_log_immutable()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only, cannot %', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_log_no_update
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW
    EXECUTE FUNCTION audit_log_immutable();

COMMENT ON TABLE audit_log IS 'Append-only audit trail of all admin actions';
COMMENT ON COLUMN audit_log.action IS 'Dot-separated: e.g. kyc.approve, user.set_role, withdrawal.approve_hold';
COMMENT ON COLUMN audit_log.target_type IS 'Entity type affected: user, kyc, withdrawal, deposit, system';
COMMENT ON COLUMN audit_log.target_label IS 'Human-readable identifier of target (email, tx_hash, etc) for quick reference';
COMMENT ON COLUMN audit_log.details IS 'Action-specific JSON data: old_value, new_value, reason, etc';
