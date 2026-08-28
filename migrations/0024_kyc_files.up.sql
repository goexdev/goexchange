CREATE TABLE IF NOT EXISTS kyc_files (
  user_id UUID NOT NULL,
  file_path TEXT NOT NULL,
  uploaded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (file_path)
);
CREATE INDEX IF NOT EXISTS idx_kyc_files_user ON kyc_files(user_id, uploaded_at DESC);