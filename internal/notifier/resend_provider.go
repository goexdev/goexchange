package notifier

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode"
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
		Subject: encodeSubject(msg.Subject),
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

// encodeSubject returns a MIME-2047-encoded version of s if it
// contains any non-ASCII characters, otherwise s unchanged.
//
// Resend's API accepts UTF-8 subject as-is, but some downstream
// clients (Gmail in particular for some legacy views, Outlook,
// and a few non-mainstream readers) display the raw bytes
// instead of decoding the subject as UTF-8 when the header
// does not arrive in encoded-word form. RFC 2047 encoded-word
// (the =?UTF-8?B?xxxxx?= form) is the universally-supported
// way to ship non-ASCII subject bytes and is what well-behaved
// clients render correctly. ASCII-only subjects are passed
// through unchanged because encoded-word would just make the
// header longer for no benefit.
func encodeSubject(s string) string {
	if isASCII(s) {
		return s
	}
	// Per RFC 2047 the encoded-text must fit on a single line
	// (no whitespace), so base64 is the safe choice over Q.
	// Maximum total encoded-word length is 75 octets, but we
	// ignore that for now because subjects in our system are
	// well under that; clients also tolerate longer encoded
	// words in practice.
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	return "=?UTF-8?B?" + enc + "?="
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}
