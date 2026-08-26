package notifier

import (
	"context"
	"errors"
)

// EmailMessage represents a single email to send.
type EmailMessage struct {
	From    string            // sender (e.g. "noreply@goexchange.local")
	To      string            // recipient
	Subject string            // email subject
	Body    string            // plain text body
	HTML    string            // optional HTML body
	Headers map[string]string // optional extra headers
}

// EmailProvider abstracts over different email sending backends.
//
// Implementations:
//   - ConsoleProvider: logs to stdout (dev mode)
//   - SMTPProvider: uses net/smtp (works for Gmail, MailHog, any SMTP server)
//   - ResendProvider: uses Resend HTTP API (https://resend.com)
//
// To add a new provider:
//  1. Implement this interface
//  2. Register in NewProvider() factory below
//  3. Add config to internal/config/config.go
type EmailProvider interface {
	// Name returns the provider name (for logging).
	Name() string

	// Send delivers a single email. Returns error on failure.
	// Provider should implement retries internally if desired.
	Send(ctx context.Context, msg EmailMessage) error

	// Close releases resources (e.g. SMTP connection pool).
	Close() error
}

// ErrProviderNotConfigured indicates the provider is missing required config.
var ErrProviderNotConfigured = errors.New("email provider not configured")

// ErrUnsupportedMessage indicates the message format is not supported.
var ErrUnsupportedMessage = errors.New("unsupported message format")
