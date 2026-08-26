package wallet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// Service is the wallet business logic layer.
type Service struct {
	repo *Repo
	log  *slog.Logger
}

// NewService creates a new wallet service.
func NewService(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{repo: NewRepo(pool), log: log}
}

// GetAll returns all balances for a user.
func (s *Service) GetAll(ctx context.Context, userID uuid.UUID) (BalanceList, error) {
	return s.repo.GetAll(ctx, userID)
}

// GetOne returns balance for one user+asset.
func (s *Service) GetOne(ctx context.Context, userID uuid.UUID, asset string) (*Balance, error) {
	return s.repo.GetOne(ctx, userID, asset)
}

// Credit adds amount to user's available balance. Caller validates amount > 0.
func (s *Service) Credit(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return ErrNegativeAmount
	}
	if err := s.repo.Credit(ctx, userID, asset, amount); err != nil {
		return fmt.Errorf("credit: %w", err)
	}
	s.log.Info("credit", "user_id", userID, "asset", asset, "amount", amount.String())
	return nil
}

// Freeze moves amount from available to frozen.
func (s *Service) Freeze(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return ErrNegativeAmount
	}
	if err := s.repo.Freeze(ctx, userID, asset, amount); err != nil {
		if errors.Is(err, ErrInsufficientBalance) {
			return err
		}
		if errors.Is(err, ErrAssetNotSupported) {
			return err
		}
		return fmt.Errorf("freeze: %w", err)
	}
	s.log.Info("freeze", "user_id", userID, "asset", asset, "amount", amount.String())
	return nil
}

// Unfreeze moves amount from frozen back to available.
func (s *Service) Unfreeze(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return ErrNegativeAmount
	}
	if err := s.repo.Unfreeze(ctx, userID, asset, amount); err != nil {
		return fmt.Errorf("unfreeze: %w", err)
	}
	s.log.Info("unfreeze", "user_id", userID, "asset", asset, "amount", amount.String())
	return nil
}

// DebitFrozen removes amount from frozen (settle).
func (s *Service) DebitFrozen(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return ErrNegativeAmount
	}
	if err := s.repo.DebitFrozen(ctx, userID, asset, amount); err != nil {
		return fmt.Errorf("debit_frozen: %w", err)
	}
	s.log.Info("debit_frozen", "user_id", userID, "asset", asset, "amount", amount.String())
	return nil
}

// DebitAvailable removes amount from available (withdraw).
func (s *Service) DebitAvailable(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return ErrNegativeAmount
	}
	if err := s.repo.DebitAvailable(ctx, userID, asset, amount); err != nil {
		return fmt.Errorf("debit_available: %w", err)
	}
	s.log.Info("debit_available", "user_id", userID, "asset", asset, "amount", amount.String())
	return nil
}

// Transfer moves amount from one user to another atomically.
func (s *Service) Transfer(ctx context.Context, fromID, toID uuid.UUID, asset string, amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return ErrNegativeAmount
	}
	if err := s.repo.Transfer(ctx, fromID, toID, asset, amount); err != nil {
		return fmt.Errorf("transfer: %w", err)
	}
	s.log.Info("transfer", "from", fromID, "to", toID, "asset", asset, "amount", amount.String())
	return nil
}

// SupportedAssets returns the list of active currency symbols.
func (s *Service) SupportedAssets(ctx context.Context) ([]string, error) {
	return s.repo.SupportedAssets(ctx)
}