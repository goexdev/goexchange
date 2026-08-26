package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ResendConfig configures the Resend provider.
type ResendConfig struct {
	APIKey string // Resend API key (re_xxx)
	From   string // From address (must be verified domain)
	URL    string // API endpoint (default https://api.resend.com/emails)
}

// ResendProvider sends emails via Resend HTTP API.
// https://resend.com/docs/api-reference/emails/send-email
type ResendProvider struct {
	cfg  ResendConfig
	http *http.Client
}

// NewResendProvider creates a ResendProvider.
func NewResendProvider(cfg ResendConfig) *ResendProvider {
	if cfg.URL == "" {
		cfg.URL = "https://api.resend.com/emails"
	}
	return &ResendProvider{
		cfg:  cfg,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *ResendProvider) Name() string { return "resend" }

func (p *ResendProvider) Send(ctx context.Context, msg EmailMessage) error {
	if p.cfg.APIKey == "" {
		return ErrProviderNotConfigured
	}

	from := msg.From
	if from == "" {
		from = p.cfg.From
	}
	if from == "" {
		return fmt.Errorf("no From address")
	}

	body := resendRequest{
		From:    from,
		To:      []string{msg.To},
		Subject: msg.Subject,
	}
	if msg.HTML != "" {
		body.HTML = msg.HTML
	} else {
		body.Text = msg.Body
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("resend api error: status %d, body: %s", resp.StatusCode, string(respBody))
}

func (p *ResendProvider) Close() error { return nil }

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	HTML    string   `json:"html,omitempty"`
}
