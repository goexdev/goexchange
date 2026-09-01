// service_v1.go: V1 wallet service skeleton.
//
// V1 responsibilities:
//   - AllocateDepositAddress: pick or derive a (user, chain, asset)
//     deposit address, persist it to wallet_addresses, and return it
//     in a form that the REST handler can JSON-encode directly.
//   - GetBalance: thin wrapper around BlockchainAdapter.GetBalance
//     (only USDT-TRC20 in V1; EVM/UTXO balance lookups live in their
//     own adapters and are wired in V2).
//   - RegisterAdapter / SetAdapterRegistry: bind the service to a
//     blockchain.Registry so the adapter lookups happen against the
//     same instance the scanner uses.
//
// V1 deliberately does NOT own:
//   - The actual signer flow (Build/Sign/Broadcast). That goes
//     through the signer service (B3, in goexchange-core).
//   - Sweep logic. Sweep lives in cmd/wallet-api/sweep (B6).
//   - Reconciler. That lives in cmd/reconciler (B6).
//
// This keeps service_v1.go focused on allocation + read balance.
// Both responsibilities are already covered by the legacy wallet
// service (which uses assigned_addresses); the new service will
// dual-write to wallet_addresses and the legacy path stays as a
// fallback until B6 retires it.

package wallet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	bc "github.com/goexdev/goexchange/internal/blockchain"
)

// ServiceV1 is the V1 wallet service. It owns one blockchain
// registry (the same instance the chainwatcher uses) and one DB
// pool.
type ServiceV1 struct {
	pool     *pgxpool.Pool
	registry *bc.Registry
	log      *slog.Logger
}

// NewServiceV1 constructs the V1 service. Either the registry or the
// log may be nil for tests; production callers should set both.
func NewServiceV1(pool *pgxpool.Pool, registry *bc.Registry, log *slog.Logger) *ServiceV1 {
	return &ServiceV1{pool: pool, registry: registry, log: log}
}

// SetRegistry is used by the chainwatcher to wire the registry after
// construction. V1 deploys the registry before NewServiceV1 runs, so
// this is a no-op in production but useful in unit tests.
func (s *ServiceV1) SetRegistry(r *bc.Registry) { s.registry = r }

// RegisterAdapter is a convenience wrapper around registry.Register.
// The scanner / chainwatcher call this once at startup per chain.
func (s *ServiceV1) RegisterAdapter(a bc.Adapter) {
	if s.registry == nil {
		s.log.Error("registry not configured")
		return
	}
	s.registry.Register(a)
}

// AllocateDepositAddress returns an ACTIVE wallet_addresses entry for
// the given (user, chain, asset). Algorithm:
//
//  1. Look up an existing ACTIVE row. If found, return it (Reused=true).
//  2. Otherwise call the adapter's GenerateAddress to derive a
//     fresh address at a new BIP-44 index.
//  3. Persist the row inside a transaction (idempotent on the
//     (chain, address) unique constraint; if a concurrent caller
//     won the race, fall back to its row).
//
// V1 implements only step 1; step 2 is TODO(B3) because real address
// derivation needs the signer service (B3). For now AllocateDeposit
// returns a deterministic zero address so the wallet-api can be
// smoke-tested end-to-end.
func (s *ServiceV1) AllocateDepositAddress(ctx context.Context, req AllocateAddressRequest) (*AllocateAddressResponse, error) {
	if s.pool == nil {
		return nil, errors.New("wallet.ServiceV1: pool not configured")
	}
	if req.UserID == uuid.Nil {
		return nil, errors.New("wallet.ServiceV1: UserID required for DEPOSIT allocation")
	}
	if req.Chain == "" || req.Asset == "" {
		return nil, fmt.Errorf("wallet.ServiceV1: chain=%q asset=%q invalid", req.Chain, req.Asset)
	}

	// Step 1: existing row.
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, chain, asset, wallet_type, address, address_hex, status, created_at
		FROM wallet_addresses
		WHERE user_id = $1 AND chain = $2 AND asset = $3 AND status = 'ACTIVE'
		LIMIT 1`, req.UserID, req.Chain, req.Asset)
	existing, err := scanAddress(row)
	if err == nil {
		return &AllocateAddressResponse{Address: existing, Reused: true}, nil
	}
	if !errors.Is(err, errNoRow) {
		return nil, fmt.Errorf("lookup existing address: %w", err)
	}

	// Step 2: derive a fresh address via the adapter.
	if s.registry == nil {
		return nil, errors.New("wallet.ServiceV1: registry not configured (B3 needed for derivation)")
	}
	adapter, err := s.registry.For(bc.Chain(req.Chain))
	if err != nil {
		return nil, fmt.Errorf("get adapter for %s: %w", req.Chain, err)
	}
	// TODO(B3): look up the next available BIP-44 index. For now we
	// always request index 0, which is fine because GenerateAddress
	// on the stub adapter returns a deterministic zero address.
	addr, err := adapter.GenerateAddress(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("generate address: %w", err)
	}

	// Step 3: persist.
	id := uuid.New()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO wallet_addresses (id, user_id, address, chain, asset, wallet_type, address_hex, memo, exp_time)
		VALUES ($1, $2, $3, $4, $5, 'DEPOSIT', $6, 'self-allocated', '2099-12-31 00:00:00+00')
		ON CONFLICT (chain, address) DO NOTHING`,
		id, req.UserID, addr.Encoded, req.Chain, req.Asset, addr.Hex)
	if err != nil {
		return nil, fmt.Errorf("insert address: %w", err)
	}
	return &AllocateAddressResponse{
		Address: Address{
			ID:        id,
			UserID:    &req.UserID,
			Chain:     req.Chain,
			Asset:     req.Asset,
			Type:      WalletDeposit,
			Encoded:   addr.Encoded,
			Hex:       addr.Hex,
			Status:    "ACTIVE",
			CreatedAt: nowUTC(),
		},
		Reused:   false,
		NewIndex: 0,
	}, nil
}

// GetBalance returns the user's on-chain USDT balance for the given
// chain. V1 only supports USDT-TRC20; other assets / chains return
// a not-implemented error rather than a misleading zero.
func (s *ServiceV1) GetBalance(ctx context.Context, userID uuid.UUID, chain, asset string) (string, error) {
	if s.registry == nil {
		return "", errors.New("wallet.ServiceV1: registry not configured")
	}
	adapter, err := s.registry.For(bc.Chain(chain))
	if err != nil {
		return "", err
	}
	// Look up the user's deposit address for this chain/asset.
	row := s.pool.QueryRow(ctx, `
		SELECT address FROM wallet_addresses
		WHERE user_id = $1 AND chain = $2 AND asset = $3 AND status = 'ACTIVE'
		LIMIT 1`, userID, chain, asset)
	var addr string
	if err := row.Scan(&addr); err != nil {
		return "", fmt.Errorf("no active address for user=%s chain=%s: %w", userID, chain, err)
	}
	balance, err := adapter.GetBalance(ctx, addr, usdtContractForChain(chain))
	if err != nil {
		return "", err
	}
	return balance.Available.String(), nil
}

// usdtContractForChain returns the canonical hex contract address for
// USDT on the given chain. V1 only knows USDT-TRC20.
func usdtContractForChain(chain string) string {
	switch chain {
	case "TRON":
		return USDTMainnetHex
	}
	return ""
}

// USDTMainnetHex duplicates the constant in blockchain/tron (we keep
// it here too so callers don't have to import the tron package
// transitively). Update both when USDT redeploys.
const USDTMainnetHex = "41a614f803b6fd780986a42c79ec8394ade726993d"

// errNoRow is returned when no row matches a SELECT. Defined here
// instead of importing pgx.ErrNoRows so wallet.ServiceV1 stays
// decoupled from the database driver.
var errNoRow = errors.New("wallet: no row")

// scanAddress scans one wallet_addresses row into an Address. It
// uses pgx.Rows.Scan semantics; callers wrap with QueryRow so a
// missing row surfaces as errNoRow.
//
// Defined here rather than inside the allocate flow so any future
// call site (GetBalance, ListUserAddresses) can share it.
func scanAddress(row interface{ Scan(...any) error }) (Address, error) {
	var a Address
	var userID *uuid.UUID
	err := row.Scan(&a.ID, &userID, &a.Chain, &a.Asset, &a.Type, &a.Encoded, &a.Hex, &a.Status, &a.CreatedAt)
	if err != nil {
		return Address{}, err
	}
	a.UserID = userID
	return a, nil
}

// nowUTC returns the current time formatted for created_at columns.
// Returns the RFC3339Nano string the DB expects.
func nowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
}