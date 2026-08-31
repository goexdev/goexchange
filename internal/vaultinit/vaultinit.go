// Package vaultinit exposes the vault-init helper used by both
// the API binary and the scheduler binary. The two binaries
// share config.yaml + .env but otherwise live in different
// cmd trees and cannot share a private helper without copying.
// Putting the helper here lets both binaries import a single
// copy that is tested in isolation.
//
// The function mirrors what cmd/api/main.go did before this
// package was extracted; we keep the behaviour identical
// (same log fields, same override order) so the API binary
// does not change behaviour.
package vaultinit

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/vault"
)

// Init initializes the Vault client and overrides the cfg with
// secrets loaded from Vault. Returns nil when vault is disabled
// (cfg.Vault.Enabled == false). When returned non-nil, the
// caller is responsible for calling Close.
func Init(cfg *config.Config, ctx context.Context, log *slog.Logger) (*vault.Client, error) {
	if !cfg.Vault.Enabled {
		log.Info("vault disabled, using config.yaml for all secrets")
		return nil, nil
	}
	if cfg.Vault.Address == "" {
		return nil, fmt.Errorf("vault.enabled=true but address is empty")
	}
	if cfg.Vault.AuthMethod == "" || cfg.Vault.AuthMethod == "static" {
		if cfg.Vault.Token == "" {
			return nil, fmt.Errorf("vault.auth_method=static but token is empty")
		}
	}

	var auth vault.AuthMethod
	var err error
	switch cfg.Vault.AuthMethod {
	case "approle":
		auth, err = vault.NewAppRoleAuth(cfg.Vault.Address, cfg.Vault.AppRoleID, cfg.Vault.AppSecretID)
		if err != nil {
			return nil, fmt.Errorf("create approle auth: %w", err)
		}
		log.Info("vault auth: approle", "role_id", cfg.Vault.AppRoleID)
	case "kubernetes":
		auth, err = vault.NewKubernetesAuth(cfg.Vault.Address, cfg.Vault.K8sRole)
		if err != nil {
			return nil, fmt.Errorf("create k8s auth: %w", err)
		}
		log.Info("vault auth: kubernetes", "role", cfg.Vault.K8sRole)
	default:
		auth = vault.NewStaticTokenAuth(cfg.Vault.Token)
		log.Warn("vault auth: static token (DEV ONLY)")
	}
	if auth == nil {
		return nil, fmt.Errorf("no vault auth method configured")
	}

	c, err := vault.NewClient(cfg.Vault.Address, auth)
	if err != nil {
		return nil, fmt.Errorf("create vault client: %w", err)
	}

	// Override DB password if a path is configured.
	if cfg.Vault.DBPath != "" {
		data, err := c.GetSecret(ctx, cfg.Vault.DBPath)
		if err != nil {
			return nil, fmt.Errorf("load db secret: %w", err)
		}
		user := data["user"]
		password := data["password"]
		host := data["host"]
		port := data["port"]
		database := data["database"]
		if password != "" {
			cfg.Database.URL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
				user, password, host, port, database)
			log.Info("db password loaded from vault", "path", cfg.Vault.DBPath)
		}
	}

	// Override JWT secret if a path is configured.
	if cfg.Vault.JWTPath != "" {
		data, err := c.GetSecret(ctx, cfg.Vault.JWTPath)
		if err != nil {
			return nil, fmt.Errorf("load jwt secret: %w", err)
		}
		if s := data["secret"]; s != "" {
			cfg.JWT.Secret = s
			log.Info("jwt secret loaded from vault", "path", cfg.Vault.JWTPath)
		}
	}

	// Override Resend API key for the notifier if configured.
	// Both API and scheduler read this so the email provider
	// works the same way in both binaries.
	if cfg.Notifier.Provider == "resend" {
		data, err := c.GetSecret(ctx, "notifier/resend")
		if err == nil && data["api_key"] != "" {
			cfg.Notifier.Resend.APIKey = data["api_key"]
			log.Info("resend api key loaded from vault")
		}
	}

	log.Info("vault connected", "address", cfg.Vault.Address, "cache_ttl", cfg.Vault.CacheTTLSec)
	return c, nil
}
