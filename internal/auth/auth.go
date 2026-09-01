// Package auth handles JWT issue/verify.
package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Service manages JWT.
type Service struct {
	secret []byte
	ttl    time.Duration
}

// NewService creates a new auth service.
func NewService(secret string, ttl time.Duration) *Service {
	return &Service{secret: []byte(secret), ttl: ttl}
}

// Claims is a JWT claims struct.
type Claims struct {
	UserID string `json:"uid"`
	Role   string `json:"role,omitempty"` // optional: "user" (default) | "admin"
	Scope  string `json:"scope,omitempty"` // optional: "2fa_login" for temp tokens
	jwt.RegisteredClaims
}

// IssueToken issues a JWT for a user. Role is optional; when
// empty the claim is omitted and downstream authorization
// treats the token as a regular user token. Callers that issue
// admin tokens should pass role="admin".
func (s *Service) IssueToken(userID, role string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// IssueTempToken issues a short-lived token with limited scope.
// Used for the second step of 2FA login: after password is verified,
// user provides this token + 2FA code to get the full token.
//
// The temp token:
// - Has TTL of 5 minutes
// - Has scope="2fa_login" (cannot be used for other APIs)
// - Cannot be used to make API calls except /2fa/login endpoint
func (s *Service) IssueTempToken(userID string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Scope:  "2fa_login",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// VerifyToken verifies a JWT.
func (s *Service) VerifyToken(token string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := tok.Claims.(*Claims); ok {
		return claims, nil
	}
	return nil, jwt.ErrTokenInvalidClaims
}
