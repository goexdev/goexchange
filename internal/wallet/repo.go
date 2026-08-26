package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// Repo provides DB access for balances.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo creates a new Repo.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// GetAll returns all balances for a user (zero balance included if row missing).
//
// Strategy: read all rows from `balances WHERE user_id = $1`, plus all
// currencies from `currencies` table to ensure each user gets a row
// for each supported asset (even if zero).
func (r *Repo) GetAll(ctx context.Context, userID uuid.UUID) (BalanceList, error) {
	const q = `
		SELECT
			c.symbol AS asset,
			COALESCE(b.available, 0) AS available,
			COALESCE(b.frozen,    0) AS frozen
		FROM currencies c
		LEFT JOIN balances b ON b.user_id = $1 AND b.asset = c.symbol
		WHERE c.is_active = TRUE
		ORDER BY c.symbol
	`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out BalanceList
	for rows.Next() {
		var b Balance
		b.UserID = userID
		if err := rows.Scan(&b.Asset, &b.Available, &b.Frozen); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetOne returns balance for one user+asset. Returns zero balance if no row.
func (r *Repo) GetOne(ctx context.Context, userID uuid.UUID, asset string) (*Balance, error) {
	const q = `
		SELECT
			COALESCE(b.available, 0) AS available,
			COALESCE(b.frozen,    0) AS frozen
		FROM currencies c
		LEFT JOIN balances b ON b.user_id = $1 AND b.asset = c.symbol
		WHERE c.symbol = $2 AND c.is_active = TRUE
	`
	b := &Balance{UserID: userID, Asset: asset}
	err := r.pool.QueryRow(ctx, q, userID, asset).Scan(&b.Available, &b.Frozen)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssetNotSupported
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Credit adds amount to a user's available balance.
//
// Inserts the row if it doesn't exist (UPSERT). Always returns nil on success.
// Caller is responsible for validating amount > 0 and asset is active.
func (r *Repo) Credit(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	const q = `
		INSERT INTO balances (user_id, asset, available, frozen, updated_at)
		VALUES ($1, $2, $3, 0, NOW())
		ON CONFLICT (user_id, asset) DO UPDATE
		SET available = balances.available + $3,
		    updated_at = NOW()
	`
	tag, err := r.pool.Exec(ctx, q, userID, asset, amount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("credit returned 0 rows")
	}
	return nil
}

// Freeze moves amount from available to frozen. Atomic: only succeeds if
// available >= amount.
//
// Returns ErrInsufficientBalance if available < amount.
func (r *Repo) Freeze(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	const q = `
		UPDATE balances
		SET available = available - $3,
		    frozen    = frozen + $3,
		    updated_at = NOW()
		WHERE user_id = $1 AND asset = $2 AND available >= $3
	`
	tag, err := r.pool.Exec(ctx, q, userID, asset, amount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either user+asset doesn't exist, or available < amount
		// Distinguish by reading current balance
		b, gerr := r.GetOne(ctx, userID, asset)
		if gerr != nil {
			return gerr
		}
		if b.Available.LessThan(amount) {
			return ErrInsufficientBalance
		}
		return ErrUserNotFound
	}
	return nil
}

// Unfreeze moves amount from frozen back to available. Atomic.
func (r *Repo) Unfreeze(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	const q = `
		UPDATE balances
		SET frozen    = frozen - $3,
		    available = available + $3,
		    updated_at = NOW()
		WHERE user_id = $1 AND asset = $2 AND frozen >= $3
	`
	tag, err := r.pool.Exec(ctx, q, userID, asset, amount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		b, gerr := r.GetOne(ctx, userID, asset)
		if gerr != nil {
			return gerr
		}
		if b.Frozen.LessThan(amount) {
			return ErrInsufficientFrozen
		}
		return ErrUserNotFound
	}
	return nil
}

// DebitFrozen removes amount from frozen (no available change).
// Used during trade settlement: taker's frozen -> maker's available.
func (r *Repo) DebitFrozen(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	const q = `
		UPDATE balances
		SET frozen = frozen - $3,
		    updated_at = NOW()
		WHERE user_id = $1 AND asset = $2 AND frozen >= $3
	`
	tag, err := r.pool.Exec(ctx, q, userID, asset, amount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		b, gerr := r.GetOne(ctx, userID, asset)
		if gerr != nil {
			return gerr
		}
		if b.Frozen.LessThan(amount) {
			return ErrInsufficientFrozen
		}
		return ErrUserNotFound
	}
	return nil
}

// DebitAvailable removes amount from available (used for withdrawals).
func (r *Repo) DebitAvailable(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	const q = `
		UPDATE balances
		SET available = available - $3,
		    updated_at = NOW()
		WHERE user_id = $1 AND asset = $2 AND available >= $3
	`
	tag, err := r.pool.Exec(ctx, q, userID, asset, amount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		b, gerr := r.GetOne(ctx, userID, asset)
		if gerr != nil {
			return gerr
		}
		if b.Available.LessThan(amount) {
			return ErrInsufficientBalance
		}
		return ErrUserNotFound
	}
	return nil
}

// Transfer moves amount between two users atomically (asset + amount).
//
// In a single transaction:
//  1. Debit from sender.available
//  2. Credit to receiver.available (UPSERT)
//
// Returns ErrSameUserTransfer if userID == counterpartyID.
func (r *Repo) Transfer(ctx context.Context, fromID, toID uuid.UUID, asset string, amount decimal.Decimal) error {
	if fromID == toID {
		return ErrSameUserTransfer
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Debit from sender
	const debit = `
		UPDATE balances
		SET available = available - $3,
		    updated_at = NOW()
		WHERE user_id = $1 AND asset = $2 AND available >= $3
	`
	tag, err := tx.Exec(ctx, debit, fromID, asset, amount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInsufficientBalance
	}

	// Credit to receiver
	const credit = `
		INSERT INTO balances (user_id, asset, available, frozen, updated_at)
		VALUES ($1, $2, $3, 0, NOW())
		ON CONFLICT (user_id, asset) DO UPDATE
		SET available = balances.available + $3,
		    updated_at = NOW()
	`
	tag, err = tx.Exec(ctx, credit, toID, asset, amount)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("credit receiver returned 0 rows")
	}

	return tx.Commit(ctx)
}

// SupportedAssets returns all active currency symbols.
func (r *Repo) SupportedAssets(ctx context.Context) ([]string, error) {
	const q = `SELECT symbol FROM currencies WHERE is_active = TRUE ORDER BY symbol`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{} // ensure non-nil
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}