package notifier

import (
	"context"
	"log/slog"
)

// ConsoleProvider logs emails to stdout instead of actually sending them.
// Useful for dev/test environments where no email server is available.
type ConsoleProvider struct {
	log *slog.Logger
}

// NewConsoleProvider creates a ConsoleProvider.
func NewConsoleProvider(log *slog.Logger) *ConsoleProvider {
	return &ConsoleProvider{log: log}
}

// Name returns the provider name.
func (p *ConsoleProvider) Name() string { return "console" }

// Send logs the email message.
func (p *ConsoleProvider) Send(ctx context.Context, msg EmailMessage) error {
	p.log.Info("email (console mode - not actually sent)",
		"from", msg.From,
		"to", msg.To,
		"subject", msg.Subject,
		"body", msg.Body,
	)
	return nil
}

// Close is a no-op for ConsoleProvider.
func (p *ConsoleProvider) Close() error { return nil }
