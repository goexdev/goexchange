package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// TOTPService manages TOTP-based 2FA for users.
type TOTPService struct {
	pool          *pgxpool.Pool
	appSecret     []byte
	backupCodeN   int
	issuer        string
}

type TOTPSetup struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

func NewTOTPService(pool *pgxpool.Pool, appSecret string, issuer string) (*TOTPService, error) {
	if len(appSecret) < 32 {
		return nil, errors.New("app secret must be at least 32 bytes")
	}
	return &TOTPService{
		pool:        pool,
		appSecret:   []byte(appSecret[:32]),
		backupCodeN: 8,
		issuer:      issuer,
	}, nil
}

func (s *TOTPService) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.appSecret)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *TOTPService) decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.appSecret)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// GenerateSecret creates a new TOTP secret (not yet enabled).
func (s *TOTPService) GenerateSecret(ctx context.Context, userID uuid.UUID, accountName string) (*TOTPSetup, error) {
	secretBytes := make([]byte, 20)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, fmt.Errorf("generate random: %w", err)
	}

	encrypted, err := s.encrypt(secretBytes)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO user_totp (user_id, secret_encrypted, enabled)
		 VALUES ($1, $2, false)
		 ON CONFLICT (user_id) DO UPDATE
		 SET secret_encrypted = $2, enabled = false, updated_at = NOW()`,
		userID, encrypted)
	if err != nil {
		return nil, fmt.Errorf("store secret: %w", err)
	}

	base32Secret := base32.StdEncoding.EncodeToString(secretBytes)
	otpAuthURL := fmt.Sprintf(
		"otpauth://totp/%s:%s?secret=%s&issuer=%s",
		s.issuer, accountName, base32Secret, s.issuer,
	)

	return &TOTPSetup{
		Secret:     base32Secret,
		OTPAuthURL: otpAuthURL,
	}, nil
}

// VerifyCode checks if a TOTP code is valid for the user.
func (s *TOTPService) VerifyCode(ctx context.Context, userID uuid.UUID, code string) error {
	var encrypted []byte
	err := s.pool.QueryRow(ctx,
		`SELECT secret_encrypted FROM user_totp WHERE user_id = $1`,
		userID).Scan(&encrypted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("2FA not set up")
		}
		return err
	}

	secretBytes, err := s.decrypt(encrypted)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	secretBase32 := base32.StdEncoding.EncodeToString(secretBytes)

	valid, err := totp.ValidateCustom(code, secretBase32, time.Now(), totp.ValidateOpts{
		Period:    30,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if !valid {
		return errors.New("invalid code")
	}
	return nil
}

// Enable marks 2FA as enabled and returns generated backup codes.
func (s *TOTPService) Enable(ctx context.Context, userID uuid.UUID, code string) ([]string, error) {
	if err := s.VerifyCode(ctx, userID, code); err != nil {
		return nil, err
	}

	_, err := s.pool.Exec(ctx,
		`UPDATE user_totp SET enabled = true, updated_at = NOW() WHERE user_id = $1`,
		userID)
	if err != nil {
		return nil, err
	}

	codes, err := s.GenerateBackupCodes(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("generate backup codes: %w", err)
	}
	return codes, nil
}

// Disable turns off 2FA (requires current code).
func (s *TOTPService) Disable(ctx context.Context, userID uuid.UUID, currentCode string) error {
	if err := s.VerifyCode(ctx, userID, currentCode); err != nil {
		return errors.New("invalid code: cannot disable 2FA")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM user_totp WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_backup_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsEnabled returns whether 2FA is enabled for a user.
func (s *TOTPService) IsEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	var enabled bool
	err := s.pool.QueryRow(ctx,
		`SELECT enabled FROM user_totp WHERE user_id = $1`,
		userID).Scan(&enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return enabled, nil
}

// GenerateBackupCodes creates 8 single-use backup codes.
func (s *TOTPService) GenerateBackupCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM user_backup_codes WHERE user_id = $1 AND used = false`,
		userID); err != nil {
		return nil, err
	}

	codes := make([]string, 0, s.backupCodeN)
	for i := 0; i < s.backupCodeN; i++ {
		codeBytes := make([]byte, 8)
		if _, err := rand.Read(codeBytes); err != nil {
			return nil, err
		}
		code := base64.RawURLEncoding.EncodeToString(codeBytes)[:10]

		hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}

		if _, err := s.pool.Exec(ctx,
			`INSERT INTO user_backup_codes (user_id, code_hash) VALUES ($1, $2)`,
			userID, string(hash)); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// UseBackupCode marks a backup code as used if valid.
func (s *TOTPService) UseBackupCode(ctx context.Context, userID uuid.UUID, code string) (bool, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, code_hash FROM user_backup_codes WHERE user_id = $1 AND used = false`,
		userID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var codeHash string
		if err := rows.Scan(&id, &codeHash); err != nil {
			return false, err
		}
		if bcrypt.CompareHashAndPassword([]byte(codeHash), []byte(code)) == nil {
			if _, err := s.pool.Exec(ctx,
				`UPDATE user_backup_codes SET used = true, used_at = NOW() WHERE id = $1`,
				id); err != nil {
				return true, err
			}
			return true, nil
		}
	}
	return false, nil
}

// RemainingBackupCodes counts unused backup codes.
func (s *TOTPService) RemainingBackupCodes(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_backup_codes WHERE user_id = $1 AND used = false`,
		userID).Scan(&count)
	return count, err
}
