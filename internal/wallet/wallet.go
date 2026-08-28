// Package wallet handles user balances: query, credit, freeze, debit.
//
// Architecture:
// - Single writer per asset row (this service)
// - Atomic ops via SQL UPDATE...WHERE (no read-modify-write race)
// - All operations use sql.NullString + shpspring decimal for NUMERIC
package wallet

import (
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Balance represents a user's balance for one asset.
type Balance struct {
	UserID    uuid.UUID       `json:"-"`         // omitted from JSON — leaks account id (M9 from the 2026-08-28 audit)
	Asset     string          `json:"asset"`
	Available decimal.Decimal `json:"available"`
	Frozen    decimal.Decimal `json:"frozen"`
}

// Total = available + frozen.
func (b *Balance) Total() decimal.Decimal {
	return b.Available.Add(b.Frozen)
}

// IsZero returns true if available + frozen are both zero.
func (b *Balance) IsZero() bool {
	return b.Available.IsZero() && b.Frozen.IsZero()
}

// BalanceList is a list of balances for one user.
type BalanceList []Balance

// Filter returns balances where total > 0.
func (bl BalanceList) Filter() BalanceList {
	var out BalanceList
	for _, b := range bl {
		if !b.IsZero() {
			out = append(out, b)
		}
	}
	return out
}

// Errors.
var (
	ErrInsufficientBalance = errors.New("insufficient available balance")
	ErrInsufficientFrozen  = errors.New("insufficient frozen balance")
	ErrUserNotFound        = errors.New("user not found or has no wallets")
	ErrNegativeAmount      = errors.New("amount must be positive")
	ErrAssetNotSupported   = errors.New("asset not supported")
	ErrSameUserTransfer    = errors.New("cannot transfer to self")
)