package apikeys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Key represents an API key for programmatic access.
type Key struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	Name       string     `json:"name"`
	KeyID      string     `json:"key_id"`     // public identifier (e.g. "gk_live_a1b2c3")
	KeyHash    string     `json:"-"`          // never expose hash
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Revoked    bool       `json:"revoked"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Scopes
const (
	ScopeRead      = "read"
	ScopeTrade     = "trade"
	ScopeWithdraw  = "withdraw"
)

// Errors
var (
	ErrKeyNotFound = errors.New("api key not found")
)

// Service manages API keys.
type Service struct {
	pool *pgxpool.Pool
}

// NewService creates a new API keys service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Generate creates a new API key for the given user.
// Returns the key, the plaintext secret (only shown once), and error.
// Format: gk_<env>_<random 32 hex chars>
// Example: gk_live_a1b2c3d4e5f6g7h8i1b2c3d4e5f6g7h8
func (s *Service) Generate(ctx context.Context, userID uuid.UUID, name string, scopes []string, expiresAt *time.Time) (*Key, string, error) {
	if name == "" {
		return nil, "", fmt.Errorf("name required")
	}
	if len(scopes) == 0 {
		scopes = []string{ScopeRead}
	}

	// Generate the secret
	secretBytes := make([]byte, 16)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, "", fmt.Errorf("rand failed: %w", err)
	}
	secret := hex.EncodeToString(secretBytes)

	// Format: gk_live_<random>
	keyID := "gk_live_" + secret[:8] // public prefix
	fullKey := keyID + "_" + secret[8:]

	// Hash for storage
	hash, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("bcrypt: %w", err)
	}

	key := &Key{
		ID:        uuid.New(),
		UserID:    userID,
		Name:      name,
		KeyID:     keyID,
		KeyHash:   string(hash),
		Scopes:    scopes,
		ExpiresAt: expiresAt,
		Revoked:   false,
		CreatedAt: time.Now(),
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO api_keys (id, user_id, name, key_id, key_hash, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		key.ID, key.UserID, key.Name, key.KeyID, key.KeyHash, key.Scopes, key.ExpiresAt,
	)
	if err != nil {
		return nil, "", err
	}

	return key, fullKey, nil
}

// List returns all API keys for a user.
// Plaintext secret is never returned.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]*Key, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, name, key_id, key_hash, scopes,
		        last_used_at, expires_at, revoked, created_at
		 FROM api_keys
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Key{}
	for rows.Next() {
		k := &Key{}
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyID, &k.KeyHash,
			&k.Scopes, &k.LastUsedAt, &k.ExpiresAt, &k.Revoked, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

// Revoke marks a key as revoked.
// Revoked keys cannot be used for auth.
func (s *Service) Revoke(ctx context.Context, userID, keyID uuid.UUID) error {
	result, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked = true WHERE id = $1 AND user_id = $2`,
		keyID, userID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// Authenticate verifies an API key and returns the associated user ID.
// Returns nil key if invalid or revoked.
func (s *Service) Authenticate(ctx context.Context, fullKey string) (*Key, error) {
	// Extract the public key_id (gk_live_<8 hex>)
	if !strings.HasPrefix(fullKey, "gk_") {
		return nil, fmt.Errorf("invalid key format")
	}

	// Find the prefix that matches
	parts := strings.SplitN(fullKey, "_", 4)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid key format")
	}
	keyID := parts[0] + "_" + parts[1] + "_" + parts[2] // "gk_live_xxxxxxxx"

	k := &Key{}
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, name, key_id, key_hash, scopes,
		        last_used_at, expires_at, revoked, created_at
		 FROM api_keys
		 WHERE key_id = $1 AND NOT revoked`,
		keyID,
	)
	if err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyID, &k.KeyHash,
		&k.Scopes, &k.LastUsedAt, &k.ExpiresAt, &k.Revoked, &k.CreatedAt); err != nil {
		return nil, ErrKeyNotFound
	}

	// Check expiry
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("key expired")
	}

	// Verify bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(k.KeyHash), []byte(fullKey)); err != nil {
		return nil, fmt.Errorf("invalid key")
	}

	// Update last_used_at (async, don't fail)
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.pool.Exec(bgCtx,
			`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, k.ID,
		)
	}()

	return k, nil
}
