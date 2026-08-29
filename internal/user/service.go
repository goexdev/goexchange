package user

import (
	"fmt"
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Service is the user business logic layer.
type Service struct {
	repo *Repo
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewService creates a new user service.
func NewService(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{repo: NewRepo(pool), pool: pool, log: log}
}

// NewServiceWithRepo creates a service with a custom repo (for testing).
func NewServiceWithRepo(repo *Repo, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log}
}

// RegisterInput contains the data needed to register a user.
type RegisterInput struct {
	Email    string
	Password string
}

// Register creates a new user. Hashes password with bcrypt.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*User, error) {
	if err := validateEmail(in.Email); err != nil {
		return nil, err
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	u := &User{
		ID:           uuid.New(),
		Email:        strings.ToLower(strings.TrimSpace(in.Email)),
		PasswordHash: string(hash),
		KycLevel:     0, // L0 = default, no KYC required
		KycStatus:    "NONE",
		Role:         "user", // NEW-M3: explicit default so downstream auth/UI never sees an empty role
	}

	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}
	s.log.Info("user registered", "id", u.ID, "email", u.Email)
	return u, nil
}

// LoginInput contains credentials.
type LoginInput struct {
	Email    string
	Password string
}

// Login authenticates a user by email + password. Returns the user on success.
func (s *Service) Login(ctx context.Context, in LoginInput) (*User, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	u, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	s.log.Info("user logged in", "id", u.ID, "email", u.Email)
	return u, nil
}

// GetUser retrieves a user by ID.
func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	return s.repo.FindByID(ctx, id)
}

// UpdateKYCLevel sets a user's KYC level (admin operation).
func (s *Service) UpdateKYCLevel(ctx context.Context, id uuid.UUID, level int) error {
	if level < 0 || level > 2 {
		return errors.New("kyc level must be 0, 1, or 2")
	}
	return s.repo.UpdateKYCLevel(ctx, id, level)
}

// validateEmail checks email format. Returns ErrInvalidEmail on failure.
func validateEmail(email string) error {
	if email == "" {
		return ErrInvalidEmail
	}
	_, err := mail.ParseAddress(email)
	if err != nil {
		return ErrInvalidEmail
	}
	return nil
}

// validatePassword checks password strength. Returns ErrWeakPassword on failure.
func validatePassword(pw string) error {
	if len(pw) < 8 {
		return ErrWeakPassword
	}
	return nil
}

// SubmitKYCInput contains the data needed to submit a KYC upgrade.
type SubmitKYCInput struct {
	TargetLevel int    `json:"target_level"` // 1 or 2
	FullName    string `json:"full_name"`
	IdNumber    string `json:"id_number"`
	Country     string `json:"country"`
	DocFront    string `json:"doc_front"`
	DocBack     string `json:"doc_back"`
	Selfie      string `json:"selfie"`
}

// SubmitKYC creates a new KYC submission.
func (s *Service) SubmitKYC(ctx context.Context, userID uuid.UUID, in SubmitKYCInput) (*KYCSubmission, error) {
	if in.TargetLevel < 1 || in.TargetLevel > 2 {
		return nil, errors.New("target level must be 1 or 2")
	}
	u, err := s.repo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.KycLevel >= in.TargetLevel {
		return nil, errors.New("already at this kyc level")
	}
	if u.KycStatus == "PENDING" {
		return nil, errors.New("kyc submission already pending")
	}
	sub := &KYCSubmission{
		UserID:      userID,
		TargetLevel: in.TargetLevel,
		FullName:    in.FullName,
		IdNumber:    in.IdNumber,
		Country:     in.Country,
		DocFront:    in.DocFront,
		DocBack:     in.DocBack,
		Selfie:      in.Selfie,
		Status:      "PENDING",
	}
	if err := s.repo.CreateKYCSubmission(ctx, sub); err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET kyc_status = 'PENDING', kyc_submitted_at = NOW(), updated_at = NOW() WHERE id = $1`, userID); err != nil {
		return nil, err
	}
	s.log.Info("kyc submitted", "user_id", userID, "target_level", in.TargetLevel)
	return sub, nil
}

// ApproveKYC approves a KYC submission (admin).
func (s *Service) ApproveKYC(ctx context.Context, submissionID uuid.UUID, reviewerNote string) error {
	if err := s.repo.ApproveKYCSubmission(ctx, submissionID, reviewerNote); err != nil {
		return err
	}
	s.log.Info("kyc approved", "submission_id", submissionID)
	return nil
}

// RejectKYC rejects a KYC submission (admin).
func (s *Service) RejectKYC(ctx context.Context, submissionID uuid.UUID, reason string) error {
	if err := s.repo.RejectKYCSubmission(ctx, submissionID, reason); err != nil {
		return err
	}
	s.log.Info("kyc rejected", "submission_id", submissionID, "reason", reason)
	return nil
}

// ListKYCSubmissions returns all submissions for a user.
func (s *Service) ListKYCSubmissions(ctx context.Context, userID uuid.UUID) ([]*KYCSubmission, error) {
	return s.repo.GetKYCSubmissions(ctx, userID)
}

// ListPendingKYCSubmissions returns all pending submissions (admin).
func (s *Service) ListPendingKYCSubmissions(ctx context.Context) ([]*KYCSubmission, error) {
	return s.repo.GetPendingKYCSubmissions(ctx)
}

// ListAllKYCSubmissions returns KYC submissions filtered by status (admin).
// status = "" returns all submissions.
func (s *Service) ListAllKYCSubmissions(ctx context.Context, status string) ([]*KYCSubmission, error) {
	return s.repo.GetAllKYCSubmissions(ctx, status)
}


// ListUsers returns the most recent N users (admin only).
// ListUsersOpts provides filtering and pagination options for ListUsers.
type ListUsersOpts struct {
	Limit  int
	Offset int
	Search string // matches email or id
	Role   string // filter by role (admin/user)
	KycLvl int    // filter by KYC level (0=all, 1, 2)
	KycSt  string // filter by KYC status
}

// ListUsers returns a list of users matching the given options.
func (s *Service) ListUsers(ctx context.Context, limit int) ([]*User, error) {
	return s.ListUsersOpts(ctx, ListUsersOpts{Limit: limit})
}

// ListUsersOpts is the full version of ListUsers with filters.
func (s *Service) ListUsersOpts(ctx context.Context, opts ListUsersOpts) ([]*User, error) {
	if opts.Limit <= 0 || opts.Limit > 200 {
		opts.Limit = 50
	}

	// Build query with optional filters
	q := `SELECT id, email, password_hash, kyc_level, kyc_status,
	       kyc_submitted_at, kyc_approved_at,
	       COALESCE(kyc_rejected_reason, ''),
	       role, created_at, updated_at
		FROM users WHERE 1=1`
	args := []interface{}{}
	argN := 0
	if opts.Search != "" {
		argN++
		q += ` AND (email ILIKE $` + fmt.Sprint(argN) + ` OR id::text = $` + fmt.Sprint(argN) + `)`
		args = append(args, "%"+opts.Search+"%")
	}
	if opts.Role != "" {
		argN++
		q += ` AND role = $` + fmt.Sprint(argN)
		args = append(args, opts.Role)
	}
	if opts.KycLvl > 0 {
		argN++
		q += ` AND kyc_level = $` + fmt.Sprint(argN)
		args = append(args, opts.KycLvl)
	}
	if opts.KycSt != "" {
		argN++
		q += ` AND kyc_status = $` + fmt.Sprint(argN)
		args = append(args, opts.KycSt)
	}
	q += ` ORDER BY created_at DESC`
	argN++
	q += ` LIMIT $` + fmt.Sprint(argN)
	args = append(args, opts.Limit)
	if opts.Offset > 0 {
		argN++
		q += ` OFFSET $` + fmt.Sprint(argN)
		args = append(args, opts.Offset)
	}

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*User{}
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.KycLevel, &u.KycStatus,
			&u.KycSubmittedAt, &u.KycApprovedAt, &u.KycRejectedReason, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// SetUserRole changes a user's role (admin only).
func (s *Service) SetUserRole(ctx context.Context, id uuid.UUID, role string) error {
	const q = `UPDATE users SET role = $2, updated_at = NOW() WHERE id = $1`
	_, err := s.pool.Exec(ctx, q, id, role)
	return err
}

// CountUsers returns total user count, optionally filtered.
func (s *Service) CountUsers(ctx context.Context, opts ListUsersOpts) (int, error) {
	q := `SELECT COUNT(*) FROM users WHERE 1=1`
	args := []interface{}{}
	argN := 0
	if opts.Search != "" {
		argN++
		q += ` AND (email ILIKE $` + fmt.Sprint(argN) + ` OR id::text = $` + fmt.Sprint(argN) + `)`
		args = append(args, "%"+opts.Search+"%")
	}
	if opts.Role != "" {
		argN++
		q += ` AND role = $` + fmt.Sprint(argN)
		args = append(args, opts.Role)
	}
	if opts.KycLvl > 0 {
		argN++
		q += ` AND kyc_level = $` + fmt.Sprint(argN)
		args = append(args, opts.KycLvl)
	}
	if opts.KycSt != "" {
		argN++
		q += ` AND kyc_status = $` + fmt.Sprint(argN)
		args = append(args, opts.KycSt)
	}
	var count int
	err := s.pool.QueryRow(ctx, q, args...).Scan(&count)
	return count, err
}

// GetAdminStats returns system-wide stats.
func (s *Service) GetAdminStats(ctx context.Context) (map[string]interface{}, error) {
	stats := map[string]interface{}{}
	var totalUsers, admins, l0Users, l1Users, l2Users int
	err := s.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) AS total,
		  COUNT(*) FILTER (WHERE role = 'admin') AS admins,
		  COUNT(*) FILTER (WHERE kyc_level = 0) AS l0,
		  COUNT(*) FILTER (WHERE kyc_level = 1) AS l1,
		  COUNT(*) FILTER (WHERE kyc_level = 2) AS l2
		FROM users
	`).Scan(&totalUsers, &admins, &l0Users, &l1Users, &l2Users)
	if err != nil {
		return nil, err
	}
	stats["total_users"] = totalUsers
	stats["admin_users"] = admins
	stats["l0_users"] = l0Users
	stats["l1_users"] = l1Users
	stats["l2_users"] = l2Users

	var pendingKYC int
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM kyc_submissions WHERE status = 'PENDING'`).Scan(&pendingKYC)
	stats["pending_kyc"] = pendingKYC

	return stats, nil
}


// IsLockedOut returns true if the email has too many failed attempts recently.
func (s *Service) IsLockedOut(ctx context.Context, email string, maxFailures int, windowMins int) (bool, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM login_attempts
		WHERE email = $1 AND success = false AND timestamp > NOW() - ($2 || ' minutes')::interval
	`, email, fmt.Sprintf("%d", windowMins)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count >= maxFailures, nil
}

// ClearLoginAttempts clears failed attempts for an email on successful login.
func (s *Service) ClearLoginAttempts(ctx context.Context, email string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM login_attempts WHERE email = $1 AND success = false`, email)
	return err
}

// SetUserPassword updates a user's password (admin operation).
func (s *Service) SetUserPassword(ctx context.Context, id uuid.UUID, password string) error {
	if len(password) < 8 {
		return errors.New("password too short (min 8 chars)")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1`, id, string(hash))
	return err
}

// AddressBookEntry is a saved withdrawal address for a user.
type AddressBookEntry struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	Asset       string     `json:"asset"`
	Address     string     `json:"address"`
	Label       string     `json:"label"`
	Whitelisted bool       `json:"whitelisted"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// AddAddressInput contains the data needed to add an address.
type AddAddressInput struct {
	Asset       string
	Address     string
	Label       string
	Whitelisted bool
}

// UpdateAddressInput contains fields that can be updated.
type UpdateAddressInput struct {
	Label      *string
	Whitelisted *bool
}

// ListAddresses returns all saved addresses for a user.
func (s *Service) ListAddresses(ctx context.Context, userID uuid.UUID) ([]AddressBookEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, asset, address, COALESCE(label, ''), whitelisted, last_used_at, created_at, updated_at
		 FROM withdrawal_addresses WHERE user_id = $1 ORDER BY last_used_at DESC NULLS LAST, created_at DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AddressBookEntry{}
	for rows.Next() {
		var e AddressBookEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Asset, &e.Address, &e.Label, &e.Whitelisted, &e.LastUsedAt, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// AddAddress saves a new address to the user's address book.
func (s *Service) AddAddress(ctx context.Context, userID uuid.UUID, in AddAddressInput) (*AddressBookEntry, error) {
	in.Asset = strings.ToUpper(strings.TrimSpace(in.Asset))
	in.Address = strings.TrimSpace(in.Address)
	in.Label = strings.TrimSpace(in.Label)
	if in.Asset == "" || in.Address == "" {
		return nil, errors.New("asset and address required")
	}
	if len(in.Label) > 64 {
		in.Label = in.Label[:64]
	}
	var e AddressBookEntry
	err := s.pool.QueryRow(ctx,
		`INSERT INTO withdrawal_addresses (user_id, asset, address, label, whitelisted)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, user_id, asset, address, label, whitelisted, last_used_at, created_at, updated_at`,
		userID, in.Asset, in.Address, in.Label, in.Whitelisted,
	).Scan(&e.ID, &e.UserID, &e.Asset, &e.Address, &e.Label, &e.Whitelisted, &e.LastUsedAt, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// DeleteAddress removes a saved address.
func (s *Service) DeleteAddress(ctx context.Context, userID, addressID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM withdrawal_addresses WHERE id = $1 AND user_id = $2`,
		addressID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("address not found")
	}
	return nil
}

// UpdateAddress updates a saved address.
func (s *Service) UpdateAddress(ctx context.Context, userID, addressID uuid.UUID, in UpdateAddressInput) (*AddressBookEntry, error) {
	if in.Label != nil {
		l := strings.TrimSpace(*in.Label)
		if len(l) > 64 {
			l = l[:64]
		}
		_, err := s.pool.Exec(ctx,
			`UPDATE withdrawal_addresses SET label = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`,
			l, addressID, userID)
		if err != nil {
			return nil, err
		}
	}
	if in.Whitelisted != nil {
		_, err := s.pool.Exec(ctx,
			`UPDATE withdrawal_addresses SET whitelisted = $1, updated_at = NOW() WHERE id = $2 AND user_id = $3`,
			*in.Whitelisted, addressID, userID)
		if err != nil {
			return nil, err
		}
	}
	var e AddressBookEntry
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, asset, address, label, whitelisted, last_used_at, created_at, updated_at
		 FROM withdrawal_addresses WHERE id = $1 AND user_id = $2`,
		addressID, userID,
	).Scan(&e.ID, &e.UserID, &e.Asset, &e.Address, &e.Label, &e.Whitelisted, &e.LastUsedAt, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// MarkAddressUsed updates last_used_at to NOW() (called after successful withdrawal).
func (s *Service) MarkAddressUsed(ctx context.Context, userID uuid.UUID, asset, address string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE withdrawal_addresses SET last_used_at = NOW() WHERE user_id = $1 AND asset = $2 AND address = $3`,
		userID, asset, address)
	return err
}


// GetFavorites returns the user's favorited market pairs.
func (s *Service) GetFavorites(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.repo.GetFavorites(ctx, userID)
}

// MaxFavoritesPerUser is the hard cap to prevent abuse.
const MaxFavoritesPerUser = 100

// AddFavorite adds a pair to user's favorites (idempotent).
func (s *Service) AddFavorite(ctx context.Context, userID uuid.UUID, pair string) error {
	// Enforce max favorites per user to prevent DoS
	count, err := s.repo.CountFavorites(ctx, userID)
	if err != nil {
		return err
	}
	if count >= MaxFavoritesPerUser {
		return fmt.Errorf("max favorites reached (%d)", MaxFavoritesPerUser)
	}
	return s.repo.AddFavorite(ctx, userID, pair)
}

// RemoveFavorite removes a pair from user's favorites.
func (s *Service) RemoveFavorite(ctx context.Context, userID uuid.UUID, pair string) error {
	return s.repo.RemoveFavorite(ctx, userID, pair)
}
