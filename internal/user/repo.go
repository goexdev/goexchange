package user

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo provides DB access for users.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo creates a new Repo.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create inserts a new user. Returns ErrEmailTaken if email exists.
// Uses RETURNING to populate CreatedAt/UpdatedAt.
func (r *Repo) Create(ctx context.Context, u *User) error {
	// Force L0 (default, no KYC) for new users
	u.KycLevel = 0
	u.KycStatus = "NONE"
	// NEW-M3: persist Role so the public user payload always has a
	// non-empty value; default to "user" if the caller didn't set one.
	if u.Role == "" {
		u.Role = "user"
	}
	const q = `
		INSERT INTO users (id, email, password_hash, kyc_level, kyc_status, role)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`
	err := r.pool.QueryRow(ctx, q, u.ID, u.Email, u.PasswordHash, u.KycLevel, u.KycStatus, u.Role).Scan(
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrEmailTaken
		}
		return err
	}
	return nil
}

// FindByEmail retrieves a user by email.
func (r *Repo) FindByEmail(ctx context.Context, email string) (*User, error) {
	const q = `
		SELECT id, email, password_hash, kyc_level, kyc_status,
		       kyc_submitted_at, kyc_approved_at,
		       COALESCE(kyc_rejected_reason, '') as kyc_rejected_reason,
		       role, created_at, updated_at
		FROM users WHERE email = $1
	`
	u := &User{}
	err := r.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.KycLevel, &u.KycStatus,
		&u.KycSubmittedAt, &u.KycApprovedAt, &u.KycRejectedReason,
		&u.Role, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// FindByID retrieves a user by ID.
func (r *Repo) FindByID(ctx context.Context, id uuid.UUID) (*User, error) {
	const q = `
		SELECT id, email, password_hash, kyc_level, kyc_status,
		       kyc_submitted_at, kyc_approved_at,
		       COALESCE(kyc_rejected_reason, '') as kyc_rejected_reason,
		       role, created_at, updated_at
		FROM users WHERE id = $1
	`
	u := &User{}
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.KycLevel, &u.KycStatus,
		&u.KycSubmittedAt, &u.KycApprovedAt, &u.KycRejectedReason,
		&u.Role, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// UpdateKYCLevel changes a user's KYC level.
func (r *Repo) UpdateKYCLevel(ctx context.Context, id uuid.UUID, level int) error {
	const q = `UPDATE users SET kyc_level = $2, updated_at = NOW() WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, id, level)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// isUniqueViolation returns true if the error is a Postgres unique constraint violation.
func isUniqueViolation(err error) bool {
	// pgx wraps pgconn.PgError; check by code
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
		return true
	}
	return false
}

// CreateKYCSubmission inserts a new KYC submission.
func (r *Repo) CreateKYCSubmission(ctx context.Context, s *KYCSubmission) error {
	const q = `
		INSERT INTO kyc_submissions (user_id, target_level, full_name, id_number, country, doc_front, doc_back, selfie)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, submitted_at
	`
	return r.pool.QueryRow(ctx, q,
		s.UserID, s.TargetLevel, s.FullName, s.IdNumber, s.Country,
		s.DocFront, s.DocBack, s.Selfie,
	).Scan(&s.ID, &s.SubmittedAt)
}

// GetKYCSubmissions returns all KYC submissions for a user.
func (r *Repo) GetKYCSubmissions(ctx context.Context, userID uuid.UUID) ([]*KYCSubmission, error) {
	const q = `
		SELECT id, user_id, target_level, full_name, id_number, country,
		       doc_front, doc_back, selfie, status, submitted_at, reviewed_at, COALESCE(reviewer_note, '') as reviewer_note
		FROM kyc_submissions WHERE user_id = $1 ORDER BY submitted_at DESC
	`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*KYCSubmission{}
	for rows.Next() {
		s := &KYCSubmission{}
		if err := rows.Scan(&s.ID, &s.UserID, &s.TargetLevel, &s.FullName, &s.IdNumber, &s.Country,
			&s.DocFront, &s.DocBack, &s.Selfie, &s.Status, &s.SubmittedAt, &s.ReviewedAt, &s.ReviewerNote); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// GetAllKYCSubmissions returns KYC submissions filtered by status (admin).
// status = "" returns all statuses.
func (r *Repo) GetAllKYCSubmissions(ctx context.Context, status string) ([]*KYCSubmission, error) {
	var rows pgx.Rows
	var err error
	if status == "" {
		const q = `
			SELECT id, user_id, target_level, full_name, id_number, country,
			       doc_front, doc_back, selfie, status, submitted_at, reviewed_at, COALESCE(reviewer_note, '') as reviewer_note
			FROM kyc_submissions ORDER BY submitted_at DESC LIMIT 200
		`
		rows, err = r.pool.Query(ctx, q)
	} else {
		const q = `
			SELECT id, user_id, target_level, full_name, id_number, country,
			       doc_front, doc_back, selfie, status, submitted_at, reviewed_at, COALESCE(reviewer_note, '') as reviewer_note
			FROM kyc_submissions WHERE status = $1 ORDER BY submitted_at DESC LIMIT 200
		`
		rows, err = r.pool.Query(ctx, q, status)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*KYCSubmission{}
	for rows.Next() {
		s := &KYCSubmission{}
		if err := rows.Scan(&s.ID, &s.UserID, &s.TargetLevel, &s.FullName, &s.IdNumber, &s.Country,
			&s.DocFront, &s.DocBack, &s.Selfie, &s.Status, &s.SubmittedAt, &s.ReviewedAt, &s.ReviewerNote); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// GetPendingKYCSubmissions returns all pending submissions (admin).
func (r *Repo) GetPendingKYCSubmissions(ctx context.Context) ([]*KYCSubmission, error) {
	const q = `
		SELECT id, user_id, target_level, full_name, id_number, country,
		       doc_front, doc_back, selfie, status, submitted_at, reviewed_at, COALESCE(reviewer_note, '') as reviewer_note
		FROM kyc_submissions WHERE status = 'PENDING' ORDER BY submitted_at ASC
	`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*KYCSubmission{}
	for rows.Next() {
		s := &KYCSubmission{}
		if err := rows.Scan(&s.ID, &s.UserID, &s.TargetLevel, &s.FullName, &s.IdNumber, &s.Country,
			&s.DocFront, &s.DocBack, &s.Selfie, &s.Status, &s.SubmittedAt, &s.ReviewedAt, &s.ReviewerNote); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// ApproveKYCSubmission marks a submission as approved and updates user kyc_level.
func (r *Repo) ApproveKYCSubmission(ctx context.Context, submissionID uuid.UUID, reviewerNote string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Get submission
	var userID uuid.UUID
	var targetLevel int
	if err := tx.QueryRow(ctx, `SELECT user_id, target_level FROM kyc_submissions WHERE id = $1 AND status = 'PENDING'`, submissionID).Scan(&userID, &targetLevel); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrKYCSubmissionNotFound
		}
		return err
	}

	// Update submission
	if _, err := tx.Exec(ctx, `
		UPDATE kyc_submissions
		SET status = 'APPROVED', reviewed_at = NOW(), reviewer_note = $2
		WHERE id = $1`, submissionID, reviewerNote); err != nil {
		return err
	}

	// Update user kyc_level
	if _, err := tx.Exec(ctx, `
		UPDATE users SET kyc_level = $2, kyc_status = 'APPROVED', kyc_approved_at = NOW(), updated_at = NOW()
		WHERE id = $1`, userID, targetLevel); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// RejectKYCSubmission marks a submission as rejected.
func (r *Repo) RejectKYCSubmission(ctx context.Context, submissionID uuid.UUID, reason string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT user_id FROM kyc_submissions WHERE id = $1 AND status = 'PENDING'`, submissionID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrKYCSubmissionNotFound
		}
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE kyc_submissions
		SET status = 'REJECTED', reviewed_at = NOW(), reviewer_note = $2
		WHERE id = $1`, submissionID, reason); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE users SET kyc_status = 'REJECTED', kyc_rejected_reason = $2, updated_at = NOW()
		WHERE id = $1`, userID, reason); err != nil {
		return err
	}

	return tx.Commit(ctx)
}


// GetFavorites returns the user's favorited market pairs.
func (r *Repo) GetFavorites(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT pair FROM user_favorites WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// CountFavorites returns how many favorites the user has.
func (r *Repo) CountFavorites(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_favorites WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// AddFavorite adds a pair (idempotent, ON CONFLICT DO NOTHING).
func (r *Repo) AddFavorite(ctx context.Context, userID uuid.UUID, pair string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_favorites (user_id, pair) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, userID, pair)
	return err
}

// RemoveFavorite deletes a pair (idempotent).
func (r *Repo) RemoveFavorite(ctx context.Context, userID uuid.UUID, pair string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_favorites WHERE user_id = $1 AND pair = $2`, userID, pair)
	return err
}
