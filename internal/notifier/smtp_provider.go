package notifier

import (
	"context"
	"fmt"
	"net/smtp"
	"time"
)

// SMTPConfig configures an SMTP provider.
// Works for Gmail, MailHog, SendGrid SMTP, AWS SES SMTP, etc.
type SMTPConfig struct {
	Host     string        // e.g. "smtp.gmail.com", "127.0.0.1" (MailHog)
	Port     int           // 587 for TLS, 1025 for MailHog, 465 for SSL
	User     string        // SMTP auth user (empty = no auth, e.g. MailHog)
	Password string        // SMTP auth password
	From     string        // From address
	Timeout  time.Duration // SMTP timeout (default 10s)
}

// SMTPProvider sends emails via SMTP.
// Supports Gmail (with app password), MailHog (dev), SendGrid SMTP, AWS SES SMTP.
type SMTPProvider struct {
	cfg SMTPConfig
}

// NewSMTPProvider creates an SMTPProvider.
func NewSMTPProvider(cfg SMTPConfig) *SMTPProvider {
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &SMTPProvider{cfg: cfg}
}

// Name returns the provider name.
func (p *SMTPProvider) Name() string { return "smtp" }

// Send delivers an email via SMTP.
func (p *SMTPProvider) Send(ctx context.Context, msg EmailMessage) error {
	if p.cfg.Host == "" {
		return ErrProviderNotConfigured
	}

	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)

	// Build message
	from := msg.From
	if from == "" {
		from = p.cfg.From
	}
	if from == "" {
		return fmt.Errorf("no From address (set msg.From or cfg.From)")
	}

	headers := msg.Headers
	if headers == nil {
		headers = make(map[string]string)
	}
	headers["From"] = from
	headers["To"] = msg.To
	headers["Subject"] = msg.Subject

	body := msg.Body
	if msg.HTML != "" && body == "" {
		headers["Content-Type"] = "text/html; charset=UTF-8"
		body = msg.HTML
	} else if headers["Content-Type"] == "" {
		headers["Content-Type"] = "text/plain; charset=UTF-8"
	}

	raw := buildSMTPMessage(headers, body)

	// Setup auth (optional)
	var auth smtp.Auth
	if p.cfg.User != "" {
		auth = smtp.PlainAuth("", p.cfg.User, p.cfg.Password, p.cfg.Host)
	}

	// Send with timeout
	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(addr, auth, from, []string{msg.To}, raw)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(p.cfg.Timeout):
		return fmt.Errorf("smtp send timeout after %s", p.cfg.Timeout)
	}
}

// Close is a no-op for SMTPProvider (stateless).
func (p *SMTPProvider) Close() error { return nil }

// buildSMTPMessage constructs an RFC 5322 message.
func buildSMTPMessage(headers map[string]string, body string) []byte {
	var msg []byte
	for k, v := range headers {
		msg = append(msg, []byte(k+": "+v+"\r\n")...)
	}
	msg = append(msg, []byte("\r\n")...)
	msg = append(msg, []byte(body)...)
	return msg
}
