// Package wallet - bootstrap helpers.
//
// BootstrapNewUser is a dev/test-only faucet that gives new users 10000 USDT.
// It is now DISABLED by default (M0 era testnet behavior).
// To enable: set environment variable GOEXCHANGE_ENABLE_FAUCET=true
package wallet

import (
	"context"
	"os"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// DefaultFaucetAmount is the initial testnet USDT grant for new users.
// Only applied if GOEXCHANGE_ENABLE_FAUCET=true.
const DefaultFaucetAmount = "10000"

// BootstrapNewUser initializes balances for a new user.
// Gives 10000 USDT only if GOEXCHANGE_ENABLE_FAUCET env var is set.
// In production, new users start with 0 balance (deposit funds first).
func (s *Service) BootstrapNewUser(ctx context.Context, userID uuid.UUID) error {
	if os.Getenv("GOEXCHANGE_ENABLE_FAUCET") != "true" {
		s.log.Info("faucet disabled (new user starts with 0 balance)", "user_id", userID)
		return nil
	}

	amt, err := decimal.NewFromString(DefaultFaucetAmount)
	if err != nil {
		return err
	}
	if err := s.Credit(ctx, userID, "USDT", amt); err != nil {
		return err
	}
	s.log.Info("faucet enabled: bootstrapped new user", "user_id", userID, "amount", amt.String(), "asset", "USDT")
	return nil
}