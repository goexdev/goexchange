package user

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/google/uuid"
)

// User represents a registered user.
type User struct {
	ID            uuid.UUID
	Email         string
	PasswordHash  string
	KycLevel      int        // 0=L0 (default, no KYC), 1=L1, 2=L2
	KycStatus     string     // NONE, PENDING, APPROVED, REJECTED
	KycSubmittedAt  *time.Time
	KycApprovedAt   *time.Time
	KycRejectedReason string
	Role           string // "user" or "admin"
	EmailVerified bool   // gates login (must be true to issue JWT)
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Public view of a user (no password hash).
type PublicUser struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	KycLevel    int       `json:"kyc_level"`
	KycStatus   string    `json:"kyc_status"`
	KycLimitUSDT string   `json:"kyc_limit_usdt"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

// ToPublic converts a User to its public representation (no password hash).
func (u *User) ToPublic() *PublicUser {
	limit := WithdrawLimitByKYC[u.KycLevel]
	return &PublicUser{
		ID:          u.ID,
		Email:       u.Email,
		KycLevel:    u.KycLevel,
		KycStatus:   u.KycStatus,
		KycLimitUSDT: limit.String(),
		Role:        u.Role,
		CreatedAt:   u.CreatedAt,
	}
}

// WithdrawLimitByKYC maps KYC level to daily USDT withdrawal limit.
//
// L0 (default, no KYC): 1000 USDT/day
// L1 (basic KYC):         10000 USDT/day
// L2 (full KYC):          100000 USDT/day
var WithdrawLimitByKYC = map[int]decimal.Decimal{
	0: decimal.NewFromInt(1000),
	1: decimal.NewFromInt(10000),
	2: decimal.NewFromInt(100000),
}

// Errors returned by the user service.
var (
	ErrEmailTaken        = errors.New("email already registered")
	ErrUserNotFound      = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidEmail      = errors.New("invalid email format")
	ErrWeakPassword      = errors.New("password too weak (min 8 chars)")
	ErrInvalidKYCLevel   = errors.New("invalid kyc level (must be 0, 1, or 2)")
)


// KYCSubmission represents a user's KYC upgrade request.
type KYCSubmission struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	TargetLevel  int     // 1=L1, 2=L2
	FullName     string
	IdNumber     string
	Country      string
	DocFront     string
	DocBack      string
	Selfie       string
	Status       string  // PENDING, APPROVED, REJECTED
	SubmittedAt  time.Time
	ReviewedAt   *time.Time
	ReviewerNote string
}

// Errors for KYC submissions.
var (
	ErrKYCSubmissionNotFound = errors.New("kyc submission not found")
)
