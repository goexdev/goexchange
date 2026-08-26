package notifier

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNotifierPackage_Loads verifies the package compiles.
func TestNotifierPackage_Loads(t *testing.T) {
	assert.NotNil(t, NewService)
}

func TestSMTPConfig_Defaults(t *testing.T) {
	cfg := SMTPConfig{
		Host:     "smtp.gmail.com",
		Port:     587,
		User:     "user",
		Password: "pass",
		From:     "noreply@test.local",
	}
	assert.Equal(t, "smtp.gmail.com", cfg.Host)
	assert.Equal(t, 587, cfg.Port)
}

func TestNewSMTPProvider_DefaultsPort(t *testing.T) {
	p := NewSMTPProvider(SMTPConfig{Host: "test", Port: 0})
	assert.Equal(t, 587, p.cfg.Port, "should default to port 587")

	p2 := NewSMTPProvider(SMTPConfig{Host: "test", Port: 1025, Timeout: 0})
	assert.Equal(t, 10*time.Second, p2.cfg.Timeout, "should default to 10s timeout")
}

func TestNotificationConstants(t *testing.T) {
	// Verify all type constants are defined
	assert.Equal(t, "KYC_APPROVED", TypeKYCApproved)
	assert.Equal(t, "KYC_REJECTED", TypeKYCRejected)
	assert.Equal(t, "WITHDRAWAL_HELD", TypeWithdrawalHeld)
	assert.Equal(t, "WITHDRAWAL_DONE", TypeWithdrawalDone)
	assert.Equal(t, "LARGE_WITHDRAW", TypeLargeWithdraw)
	assert.Equal(t, "LOGIN_RISK", TypeLoginRisk)
}

func TestNewProvider_SelectsCorrectly(t *testing.T) {
	log := newTestLogger()

	// Console
	p, err := NewProvider(ProviderConsole, SMTPConfig{}, ResendConfig{}, log)
	require.NoError(t, err)
	assert.Equal(t, "console", p.Name())

	// Empty defaults to console
	p, err = NewProvider("", SMTPConfig{}, ResendConfig{}, log)
	require.NoError(t, err)
	assert.Equal(t, "console", p.Name())

	// SMTP
	p, err = NewProvider(ProviderSMTP, SMTPConfig{Host: "smtp.test.com", Port: 587}, ResendConfig{}, log)
	require.NoError(t, err)
	assert.Equal(t, "smtp", p.Name())

	// SMTP without host should error
	_, err = NewProvider(ProviderSMTP, SMTPConfig{}, ResendConfig{}, log)
	assert.Error(t, err)

	// Resend
	p, err = NewProvider(ProviderResend, SMTPConfig{}, ResendConfig{APIKey: "re_test"}, log)
	require.NoError(t, err)
	assert.Equal(t, "resend", p.Name())

	// Resend without key should error
	_, err = NewProvider(ProviderResend, SMTPConfig{}, ResendConfig{}, log)
	assert.Error(t, err)

	// Unknown
	_, err = NewProvider("unknown", SMTPConfig{}, ResendConfig{}, log)
	assert.Error(t, err)
}


func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}