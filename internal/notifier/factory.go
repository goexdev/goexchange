package notifier

import (
	"fmt"
	"log/slog"
)

type ProviderType string

const (
	ProviderConsole ProviderType = "console"
	ProviderSMTP    ProviderType = "smtp"
	ProviderResend  ProviderType = "resend"
)

func NewProvider(typ ProviderType, smtpCfg SMTPConfig, resendCfg ResendConfig, log *slog.Logger) (EmailProvider, error) {
	switch typ {
	case ProviderSMTP:
		if smtpCfg.Host == "" {
			return nil, fmt.Errorf("smtp provider requires host")
		}
		return NewSMTPProvider(smtpCfg), nil
	case ProviderResend:
		if resendCfg.APIKey == "" {
			return nil, fmt.Errorf("resend provider requires api_key")
		}
		return NewResendProvider(resendCfg), nil
	case ProviderConsole, "":
		return NewConsoleProvider(log), nil
	default:
		return nil, fmt.Errorf("unknown provider type: %s", typ)
	}
}
