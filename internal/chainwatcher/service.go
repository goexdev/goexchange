// Package chainwatcher - service orchestration.
//
// Polls watched addresses every PollIntervalSec, detects new deposits,
// credits user wallets after MinConf confirmations.
package chainwatcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/user"
	"github.com/goexdev/goexchange/internal/risk"
	"github.com/goexdev/goexchange/internal/chainwallet"
	"github.com/goexdev/goexchange/internal/notifier"
	"github.com/goexdev/goexchange/internal/wallet"
)

var (
	ErrInvalidAsset   = errors.New("invalid asset")
	ErrInvalidAmount  = errors.New("amount must be positive")
	ErrDepositNotFound = errors.New("deposit not found")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrWithdrawalFailed = errors.New("withdrawal failed")
)

// Service handles chain watching.
type Service struct {
	pool     *pgxpool.Pool
	wallet   *wallet.Service
	userSvc  *user.Service
	notifier *notifier.Service
	log      *slog.Logger
	cfg      config.ChainWatcherConfig

	mu            sync.Mutex
	driver        Driver           // default chain driver (fallback)
	registry      *ChainRegistry   // per-chain drivers (hot-reloadable)
	depositsCount int

	// chainWallet handles build → sign → broadcast via signer service.
	// Nil in dev mode.
	chainWallet *chainwallet.Manager

	// State tracking for polled addresses (in-memory).
	muState sync.Mutex
	state   map[string]*addressState // address -> last seen state
}

// addressState tracks the last known state of a watched address.
type addressState struct {
	lastAmount  decimal.Decimal
	lastTxHash  string
	userID      uuid.UUID
}

// New creates a chainwatcher service.
//
// driverType: "mock" or chain-specific (btc, bsc, eth, etc.)
func New(pool *pgxpool.Pool, wallet *wallet.Service, userSvc *user.Service, notifier *notifier.Service, log *slog.Logger, cfg config.ChainWatcherConfig, driverType string) *Service {
	var drv Driver
	switch driverType {
	case "mock":
		drv = NewMockDriver(cfg)
	default:
		// Unknown driver type - use mock as fallback
		drv = NewMockDriver(cfg)
	}
	return &Service{
		pool:     pool,
		wallet:   wallet,
		userSvc:  userSvc,
		notifier: notifier,
		log:      log,
		cfg:      cfg,
		driver:   drv,
		state:    make(map[string]*addressState),
	}
}

// NewMultiChain creates a Service with a MultiChainDriver that supports multiple chains.
// Each chain has its own driver implementation.
func NewMultiChain(pool *pgxpool.Pool, wallet *wallet.Service, userSvc *user.Service, notifier *notifier.Service, log *slog.Logger, cfg config.ChainWatcherConfig, drivers map[string]Driver) *Service {
	multi := NewMultiChainDriver()
	for chainID, drv := range drivers {
		multi.Register(chainID, drv)
	}
	return &Service{
		pool:     pool,
		wallet:   wallet,
		userSvc:  userSvc,
		notifier: notifier,
		log:      log,
		cfg:      cfg,
		driver:   multi,
		state:    make(map[string]*addressState),
	}
}


// GetHotAddress returns the hot wallet address (delegates to driver).
func (s *Service) GetHotAddress() string {
	if s.driver == nil {
		return ""
	}
	return s.driver.GetHotAddress()
}

// HasSigner returns true if this chain driver supports real signed withdrawals.
func (s *Service) HasSigner() bool {
	if s.driver == nil {
		return false
	}
	return s.driver.HasSigner()
}

// Start begins the watch loop.
func (s *Service) Start(ctx context.Context) {
	s.log.Info("chainwatcher started", "driver", s.driver.Name(), "interval_sec", s.cfg.PollIntervalSec)
	go s.runLoop(ctx)
}

// WatchAddress adds an address to the watch list.
// (No-op for poll-based drivers; the run loop picks it up from DB.)
func (s *Service) WatchAddress(_ context.Context, address string) {
	s.muState.Lock()
	defer s.muState.Unlock()
	if _, ok := s.state[address]; !ok {
		s.state[address] = &addressState{}
	}
}

// runLoop polls watched addresses every PollIntervalSec.
func (s *Service) runLoop(ctx context.Context) {
	if s.cfg.PollIntervalSec <= 0 {
		s.log.Info("polling disabled (interval=0)")
		return
	}
	ticker := time.NewTicker(time.Duration(s.cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.log.Info("chainwatcher stopped")
			return
		case <-ticker.C:
			if err := s.pollDeposits(ctx); err != nil {
				s.log.Warn("poll deposits failed", "error", err)
			}
			if err := s.PollWithdrawals(ctx); err != nil {
				s.log.Warn("poll withdrawals failed", "error", err)
			}
		}
	}
}

// pollDeposits fetches deposits for all watched addresses.
func (s *Service) pollDeposits(ctx context.Context) error {
	// Get all assigned addresses from DB
	rows, err := s.pool.Query(ctx, `
		SELECT a.address, a.user_id
		FROM assigned_addresses a
		JOIN users u ON a.user_id = u.id
		JOIN chains c ON a.chain = c.name
		WHERE c.driver = $1
		LIMIT 1000
	`, s.driver.Name())
	if err != nil {
		return err
	}
	defer rows.Close()

	var watched []struct {
		Address string
		UserID  uuid.UUID
		Asset   string
	}
	for rows.Next() {
		var addr string
		var uid uuid.UUID
		var asset string
		if err := rows.Scan(&addr, &uid, &asset); err != nil {
			return err
		}
		watched = append(watched, struct {
			Address string
			UserID  uuid.UUID
			Asset   string
		}{addr, uid, asset})
	}

	s.log.Info("polling watched addresses", "count", len(watched), "driver", s.driver.Name())

	for _, w := range watched {
		if err := s.pollOne(ctx, w.Address, w.UserID, w.Asset); err != nil {
			s.log.Warn("poll address failed", "address", w.Address, "error", err)
		}
	}
	return nil
}

// pollOne checks one address for new deposits using ListTransactions.
// Each new tx is recorded with its REAL on-chain tx_hash, preventing duplicates.
// Only credits deposits after minConf confirmations (default 6).
func (s *Service) pollOne(ctx context.Context, address string, userID uuid.UUID, asset string) error {
	// Get all confirmed receive transactions (>= minConf)
	txs, err := s.driver.ListTransactions(ctx, address, s.cfg.MinConf)
	if err != nil {
		return err
	}

	// Get/create state for this address
	s.muState.Lock()
	state, ok := s.state[address]
	if !ok {
		state = &addressState{userID: userID}
		s.state[address] = state
	}
	s.muState.Unlock()

	// Process each transaction - recordDeposit uses ON CONFLICT (chain, tx_hash) DO NOTHING
	// so re-running this loop is idempotent.
	for _, tx := range txs {
		// Check if we've already seen this tx (in-memory check for fast path)
		s.muState.Lock()
		alreadySeen := state.lastTxHash == tx.TxHash
		s.muState.Unlock()
		if alreadySeen {
			continue
		}

		s.log.Info("new deposit detected",
			"address", address,
			"user_id", userID,
			"amount", tx.Amount.String(),
			"tx_hash", tx.TxHash,
			"block", tx.BlockHeight,
			"confirmations", tx.Confirmations,
		)

		// Update state with latest tx hash (best-effort; DB UNIQUE is the real guarantee)
		s.muState.Lock()
		state.lastTxHash = tx.TxHash
		state.lastAmount = state.lastAmount.Add(tx.Amount)
		state.userID = userID
		s.muState.Unlock()

		// Record deposit (real tx_hash; UNIQUE constraint prevents duplicates)
		if _, err := s.recordDeposit(ctx, userID, asset, tx.TxHash, tx.Amount, address); err != nil {
			s.log.Warn("recordDeposit failed", "tx_hash", tx.TxHash, "error", err)
		}
	}

	return nil
}

// recordDeposit persists and credits a new deposit. Returns the deposit.
func (s *Service) recordDeposit(ctx context.Context, userID uuid.UUID, asset, txHash string, amount decimal.Decimal, toAddress string) (*Deposit, error) {
	chain := s.driver.Name() // e.g. "mock", "btc", "bsc"
	// ON CONFLICT (chain, to_address, amount) WHERE tx_hash LIKE 'poll-%'
	// prevents duplicate synthetic records from old processes / restarts
	const q = `
		INSERT INTO deposits (id, user_id, asset, amount, tx_hash, to_address, chain, confirmations, status, created_at, confirmed_at, credited_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, 6, $7, NOW(), NOW(), NOW())
		ON CONFLICT (chain, tx_hash) DO NOTHING
		RETURNING id, created_at
	`
	var depositID uuid.UUID
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, q, userID, asset, amount, txHash, toAddress, chain, "CREDITED").Scan(&depositID, &createdAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			s.log.Debug("deposit already recorded (dedup hit)",
				"user_id", userID, "tx_hash", txHash, "chain", chain)
			return nil, nil
		}
		return nil, fmt.Errorf("insert deposit: %w", err)
	}

	// Credit wallet
	if err := s.wallet.Credit(ctx, userID, asset, amount); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.depositsCount++
	s.mu.Unlock()

	s.log.Info("deposit credited",
		"user_id", userID,
		"asset", asset,
		"amount", amount.String(),
		"tx_hash", txHash,
	)

	// Trigger: deposit confirmed notification (only for real chain)
	// Notify user of confirmed deposit (any chain)
	s.notifier.SendNotification(ctx, userID, "DEPOSIT_CONFIRMED",
		"Deposit Confirmed",
		fmt.Sprintf("Your deposit of %s %s has been confirmed.", amount.String(), asset),
		map[string]any{"tx_hash": txHash, "amount": amount.String(), "asset": asset})

	return &Deposit{
		ID:            depositID,
		UserID:        userID,
		Asset:         asset,
		Amount:        amount,
		TxHash:        txHash,
		ToAddress:     toAddress,
		Chain:         chain,
		Confirmations: 6,
		Status:        "CREDITED",
		CreatedAt:     createdAt,
	}, nil
}

// SpawnDeposit triggers a mock deposit (mock driver only).
func (s *Service) SpawnDeposit(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) (*Deposit, error) {
	txHash := RandomTxHash()
	// Check for duplicate (mock: service generates hash, so duplicates are rare)
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM deposits WHERE tx_hash = $1)`, txHash).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("deposit with tx_hash already exists")
	}
	if err := s.driver.SpawnDeposit(ctx, userID.String(), asset, txHash, amount); err != nil {
		return nil, err
	}
	// Pick a random address (mock)
	end := 24
	if len(txHash) < end {
		end = len(txHash)
	}
	if end < 10 {
		end = 10
	}
	addr := "mock" + txHash[10:end]
	deposit, err := s.recordDeposit(ctx, userID, asset, txHash, amount, addr)
	if err != nil {
		return nil, err
	}
	return deposit, nil
}

// ListDeposits returns deposits for a user.
func (s *Service) ListDeposits(ctx context.Context, userID uuid.UUID, limit int) ([]*Deposit, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	out := []*Deposit{}
	const q = `
		SELECT id, user_id, asset, amount, tx_hash, COALESCE(from_address, ''), to_address,
		       chain, confirmations, status, created_at
		FROM deposits
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		d := &Deposit{}
		if err := rows.Scan(
			&d.ID, &d.UserID, &d.Asset, &d.Amount, &d.TxHash, &d.FromAddress, &d.Address,
			&d.Chain, &d.Confirmations, &d.Status, &d.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDeposit returns one deposit.
func (s *Service) GetDeposit(ctx context.Context, id uuid.UUID) (*Deposit, error) {
	d := &Deposit{}
	const q = `
		SELECT id, user_id, asset, amount, tx_hash, COALESCE(from_address, ''), to_address,
		       chain, confirmations, status, created_at
		FROM deposits
		WHERE id = $1
	`
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&d.ID, &d.UserID, &d.Asset, &d.Amount, &d.TxHash, &d.FromAddress, &d.Address,
		&d.Chain, &d.Confirmations, &d.Status, &d.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDepositNotFound
		}
		return nil, err
	}
	return d, nil
}

// DepositsCount returns the total deposits since startup.
func (s *Service) DepositsCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.depositsCount
}

// Withdrawal is the DB representation.
type Withdrawal struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Asset         string
	Amount        decimal.Decimal
	Fee           decimal.Decimal
	ReceiveAmount decimal.Decimal
	DestAddress   string
	TxHash        string
	Chain         string
	Status        string // PENDING, BROADCAST, DONE, FAILED
	Confirmations int
	ErrorMsg      string
	CreatedAt     time.Time
	SentAt        *time.Time
	ConfirmedAt   *time.Time
	RiskScore     int
	RiskHold      bool
}

// WithdrawWithSigner is the production withdrawal flow that uses the
// signer service for signing and node-proxy for broadcasting.
//
// This is the recommended flow for production deployments where:
//   - Private keys are in Vault (not on the exchange server)
//   - Node servers are in separate subnets (read-only + broadcast only)
//
// Note: an older Withdraw() function existed with an INSERT that passed
// 12 parameters against a 10-placeholder SQL string. That function was
// dead code (no callers — the API handler uses WithdrawWithSigner below)
// and has been removed (Bug #22 fix).
//
// computeFee returns the withdrawal fee for an asset amount.
// Formula: max(flat, amount * percent), then max(fee, min)
func (s *Service) computeFee(ctx context.Context, asset string, amount decimal.Decimal) (decimal.Decimal, error) {
	var feeFlat, feePercent, feeMin decimal.Decimal
	err := s.pool.QueryRow(ctx, `
		SELECT withdraw_fee_flat, withdraw_fee_percent, withdraw_fee_min
		FROM currencies WHERE symbol = $1 AND is_active = true
	`, asset).Scan(&feeFlat, &feePercent, &feeMin)
	if err != nil {
		// Fallback: no fee if currency not found
		return decimal.Zero, nil
	}
	feeFromPercent := amount.Mul(feePercent)
	fee := decimal.Max(feeFlat, feeFromPercent)
	fee = decimal.Max(fee, feeMin)
	return fee, nil
}

func (s *Service) WithdrawWithSigner(ctx context.Context, userID uuid.UUID, asset, toAddress string, amount decimal.Decimal) (*Withdrawal, error) {
	if !amount.IsPositive() {
		return nil, ErrInvalidAmount
	}
	// 0. Check KYC withdrawal limit
	if err := s.WithdrawLimitCheck(ctx, userID, amount); err != nil {
		return nil, err
	}
	// 0b. Compute fee (from currencies table - admin-configurable)
	fee, err := s.computeFee(ctx, asset, amount)
	if err != nil {
		return nil, err
	}
	receiveAmount := amount.Sub(fee)
	if receiveAmount.IsNegative() {
		return nil, fmt.Errorf("withdrawal amount %s less than fee %s", amount.String(), fee.String())
	}
	// 0c. Check user has enough balance for amount + fee
	totalDebit := amount.Add(fee)
	var avail decimal.Decimal
	err = s.pool.QueryRow(ctx, `SELECT available FROM balances WHERE user_id = $1 AND asset = $2`, userID, asset).Scan(&avail)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("insufficient balance: no %s wallet found", asset)
	}
	if err != nil {
		return nil, err
	}
	if avail.LessThan(totalDebit) {
		return nil, fmt.Errorf("insufficient balance: need %s (amount %s + fee %s), have %s", totalDebit.String(), amount.String(), fee.String(), avail.String())
	}
	// 1. Compute risk score
	riskScore := s.computeWithdrawRisk(ctx, userID, amount, toAddress)
	riskAction := risk.ActionForScore(riskScore)
	// 2. Insert withdrawal record
	status := "PENDING"
	if riskAction == risk.ActionHold {
		status = "HOLD"
	} else if riskAction == risk.ActionBlock {
		return nil, fmt.Errorf("withdrawal blocked by risk control (score: %d): %w", riskScore, ErrWithdrawalBlocked)
	}
	riskHold := riskAction == risk.ActionHold
	w := &Withdrawal{
		ID:            uuid.New(),
		UserID:        userID,
		Asset:         asset,
		Amount:        amount,
		Fee:           fee,
		ReceiveAmount: receiveAmount,
		DestAddress:   toAddress,
		Chain:         s.chainForAsset(asset),
		Status:        status,
		RiskScore:     riskScore,
		RiskHold:      riskHold,
		CreatedAt:     time.Now().UTC(),
	}
	const insertQ = `
		INSERT INTO withdrawals (id, user_id, asset, amount, fee, receive_amount, dest_address, chain, status, risk_score, risk_hold, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err = s.pool.Exec(ctx, insertQ, w.ID, w.UserID, w.Asset, w.Amount, w.Fee, w.ReceiveAmount, w.DestAddress, w.Chain, w.Status, riskScore, riskHold, w.CreatedAt)
	if err != nil {
		return nil, err
	}
	// 3. If on HOLD, skip debit + broadcast
	if riskHold {
		s.log.Info("withdrawal on hold pending admin review", "withdrawal_id", w.ID, "risk_score", riskScore)
		return w, nil
	}
	// 4. Debit user balance (atomic)
	if err := s.wallet.DebitAvailable(ctx, userID, asset, amount); err != nil {
		s.failWithdrawal(ctx, w.ID, err.Error())
		return nil, err
	}
	// 5. Use chainWallet (signer + broadcaster) for the actual send
	if s.chainWallet == nil {
		errMsg := "chainWallet not configured (signer service unavailable)"
		_ = s.wallet.Credit(ctx, userID, asset, amount)
		s.failWithdrawal(ctx, w.ID, errMsg)
		return nil, fmt.Errorf("%s", errMsg)
	}
	chain := s.chainForAsset(asset)
	chain = normalizeChainName(chain)
	txHash, err := s.chainWallet.Send(ctx, chain, toAddress, amount.String(), "", w.ID.String(), userID.String())
	if err != nil {
		_ = s.wallet.Credit(ctx, userID, asset, amount)
		s.failWithdrawal(ctx, w.ID, err.Error())
		return nil, err
	}
	// 6. Update to BROADCAST
	sentAt := time.Now().UTC()
	_, err = s.pool.Exec(ctx, `
		UPDATE withdrawals SET tx_hash = $1, status = 'BROADCAST', sent_at = $2
		WHERE id = $3
	`, txHash, sentAt, w.ID)
	if err != nil {
		s.log.Warn("failed to update withdrawal", "error", err)
	}
	w.TxHash = txHash
	w.Status = "BROADCAST"
	w.SentAt = &sentAt

	// Trigger: large withdrawal notification
	if amount.GreaterThan(decimal.NewFromInt(500)) {
		s.notifier.SendNotification(ctx, userID, notifier.TypeLargeWithdraw,
			"Large Withdrawal Detected",
			fmt.Sprintf("Withdrawal of %s %s to %s is being broadcast.", amount.String(), asset, toAddress[:10]),
			map[string]any{"withdrawal_id": w.ID.String(), "amount": amount.String(), "asset": asset})
	}
	return w, nil
}

// normalizeChainName converts a chain ID to the canonical name used by signer.
// e.g. "bsc" -> "ethereum", "bitcoin" -> "bitcoin"
func normalizeChainName(chain string) string {
	switch chain {
	case "bsc", "polygon", "avalanche", "arbitrum", "optimism", "base":
		return "ethereum"
	case "bitcoin", "litecoin", "dogecoin":
		return "bitcoin"
	}
	return chain
}

func (s *Service) failWithdrawal(ctx context.Context, id uuid.UUID, errMsg string) {
	_, _ = s.pool.Exec(ctx, `UPDATE withdrawals SET status = 'FAILED', error_msg = $1 WHERE id = $2`, errMsg, id)

	// Trigger: withdrawal failed notification
	var userID uuid.UUID
	var amount, asset string
	_ = s.pool.QueryRow(ctx, `SELECT user_id, asset, amount::text FROM withdrawals WHERE id = $1`, id).Scan(&userID, &asset, &amount)
	if userID != uuid.Nil {
		_ = s.notifier.SendNotification(ctx, userID, "WITHDRAWAL_FAILED",
			"Withdrawal Failed",
			fmt.Sprintf("Your withdrawal of %s %s failed: %s", amount, asset, errMsg),
			map[string]any{"withdrawal_id": id.String(), "error": errMsg})
	}
}

// ListWithdrawals returns user's withdrawal history.
func (s *Service) ListWithdrawals(ctx context.Context, userID uuid.UUID, limit int) ([]*Withdrawal, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	out := []*Withdrawal{}
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, asset, amount, dest_address, COALESCE(tx_hash, ''), chain,
		       status, confirmations, COALESCE(error_msg, ''), created_at, sent_at, confirmed_at
		FROM withdrawals
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		w := &Withdrawal{}
		if err := rows.Scan(&w.ID, &w.UserID, &w.Asset, &w.Amount, &w.DestAddress, &w.TxHash, &w.Chain,
			&w.Status, &w.Confirmations, &w.ErrorMsg, &w.CreatedAt, &w.SentAt, &w.ConfirmedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Driver returns the underlying driver (for health check).
func (s *Service) DriverName() string {
	return s.driver.Name()
}

// GetDepositAddress returns user's deposit address for asset.
// Returns existing assigned address if any.
// Otherwise generates a new one via the right chain's driver.
func (s *Service) GetDepositAddress(ctx context.Context, userID uuid.UUID, asset string) (string, error) {
	// Check existing
	var addr string
	chain := s.chainForAsset(asset)
	err := s.pool.QueryRow(ctx, `
		SELECT address FROM assigned_addresses
		WHERE user_id = $1 AND chain = $2
		LIMIT 1
	`, userID, chain).Scan(&addr)
	if err == nil {
		return addr, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	// Generate new
	return s.AllocateAddress(ctx, userID, asset)
}

// AllocateAddress generates a new address via driver and persists.
func (s *Service) AllocateAddress(ctx context.Context, userID uuid.UUID, asset string) (string, error) {
	drv := s.driverForAsset(asset)
	if drv == nil {
		return "", fmt.Errorf("no driver for asset %s", asset)
	}
	addr, err := drv.GenerateAddress(ctx)
	if err != nil {
		return "", fmt.Errorf("generate address: %w", err)
	}
	chain := s.chainForAsset(asset)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO assigned_addresses (user_id, address, chain, asset, exp_time, memo)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, addr, chain, asset, "2099-12-31 00:00:00+00", "self-allocated")
	if err != nil {
		return "", err
	}
	s.log.Info("address allocated",
		"user_id", userID,
		"asset", asset,
		"chain", chain,
		"address", addr,
	)
	return addr, nil
}

// chainForAsset returns the chain_id for an asset.
// Data-driven via the ChainRegistry. Falls back to lowercase(asset) for legacy.
func (s *Service) chainForAsset(asset string) string {
	if s.registry != nil {
		if id := s.registry.ChainIDForAsset(asset); id != "" {
			return id
		}
	}
	// Legacy fallback (deprecated, kept for tests)
	return strings.ToLower(asset)
}

// PendingDeposit shows deposits that are in mempool (0-5 conf).
type PendingDeposit struct {
	Address     string          `json:"address"`
	Asset       string          `json:"asset"`
	Confirmed   decimal.Decimal `json:"confirmed"`    // >= minConf
	Pending     decimal.Decimal `json:"pending"`      // 0 < conf < minConf
	Total       decimal.Decimal `json:"total"`        // confirmed + pending
	MinConf     int             `json:"min_conf"`
	BlockHeight int64           `json:"block_height"` // current block
}

// PendingTx represents one pending on-chain transaction with its current
// confirmation count. UI uses this to show per-tx status (separate from
// aggregate per-address balances).
type PendingTx struct {
	TxHash        string          `json:"tx_hash"`
	Address       string          `json:"address"`
	Asset         string          `json:"asset"`
	Amount        decimal.Decimal `json:"amount"`
	Confirmations int64           `json:"confirmations"` // 0 if in mempool, increases per block
	MinConf       int             `json:"min_conf"`      // threshold (e.g. 6)
	BlockHeight   int64           `json:"block_height"`  // block where confirmed, -1 if mempool
	Time          int64           `json:"time"`          // unix timestamp from chain
	Status        string          `json:"status"`        // "mempool" or "confirming"
}

// GetPendingDeposits returns all watched addresses with confirmed + pending balance.
// Used by UI to show "X {asset} incoming (3 conf)".
func (s *Service) GetPendingDeposits(ctx context.Context, userID uuid.UUID) ([]*PendingDeposit, error) {
	out := []*PendingDeposit{}
	minConf := s.cfg.MinConf
	if minConf == 0 {
		minConf = 6
	}
	rows, err := s.pool.Query(ctx, `
		SELECT a.address, COALESCE(a.asset, '')
		FROM assigned_addresses a
		WHERE a.user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var addr, asset string
		if err := rows.Scan(&addr, &asset); err != nil {
			return nil, err
		}
		confirmed, err := s.driver.GetReceivedConfirmed(ctx, addr, minConf)
		if err != nil {
			continue
		}
		pending, err := s.driver.GetReceivedPending(ctx, addr, minConf)
		if err != nil {
			continue
		}
		total := confirmed.Add(pending)
		blockHeight, _ := s.driver.GetBlockCount(ctx)
		out = append(out, &PendingDeposit{
			Address:     addr,
			Asset:       asset,
			Confirmed:   confirmed,
			Pending:     pending,
			Total:       total,
			MinConf:     minConf,
			BlockHeight: blockHeight,
		})
	}
	return out, nil
}

// ImportDepositsFromChain scans all user addresses and imports any
// unrecorded confirmed deposits from the chain. Useful for reconciliation
// after the old poll-XXX bug cleanup.
// Returns list of imported deposit IDs.
func (s *Service) ImportDepositsFromChain(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	imported := []uuid.UUID{}

	minConf := s.cfg.MinConf
	if minConf == 0 {
		minConf = 6
	}

	// Get user addresses
	rows, err := s.pool.Query(ctx, `
		SELECT a.address, COALESCE(a.asset, 'BTC')
		FROM assigned_addresses a
		WHERE a.user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var addresses []struct {
		Address string
		Asset   string
	}
	for rows.Next() {
		var addr, asset string
		if err := rows.Scan(&addr, &asset); err != nil {
			return nil, err
		}
		addresses = append(addresses, struct {
			Address string
			Asset   string
		}{addr, asset})
	}

	// For each address, get all confirmed txs and import unrecorded ones
	for _, a := range addresses {
		txs, err := s.driver.ListTransactions(ctx, a.Address, minConf)
		if err != nil {
			continue
		}
		for _, tx := range txs {
			if tx.Category != "receive" {
				continue
			}
			// Check if this tx already recorded (by tx_hash)
			var existingUserID uuid.UUID
			var existingToAddr string
			err := s.pool.QueryRow(ctx,
				`SELECT user_id, to_address FROM deposits WHERE chain = $1 AND tx_hash = $2`,
				s.driver.Name(), tx.TxHash).Scan(&existingUserID, &existingToAddr)
			switch err {
			case pgx.ErrNoRows:
				deposit, err := s.recordDeposit(ctx, userID, a.Asset, tx.TxHash, tx.Amount, a.Address)
				if err != nil {
					s.log.Warn("import deposit failed", "tx_hash", tx.TxHash, "error", err)
					continue
				}
				if deposit != nil {
					imported = append(imported, deposit.ID)
				}
			case nil:
				if existingUserID != userID || existingToAddr != a.Address {
					_, err := s.pool.Exec(ctx,
						`UPDATE deposits SET user_id = $1, to_address = $2 WHERE chain = $3 AND tx_hash = $4`,
						userID, a.Address, s.driver.Name(), tx.TxHash)
					if err != nil {
						s.log.Warn("re-assign deposit failed", "tx_hash", tx.TxHash, "error", err)
						continue
					}
					s.log.Info("re-assigned deposit", "tx_hash", tx.TxHash, "from", existingUserID, "to", userID)
					imported = append(imported, uuid.Nil)
				}
			}
		}
	}

	return imported, nil
}

// GetPendingTxs returns per-tx pending transactions for a user.
// Each entry is one on-chain transaction with its current confirmation count.
// Returns ALL receive txs with < minConf confirmations (mempool + confirming).
// Use this for "show each pending tx separately with live confirmations".
func (s *Service) GetPendingTxs(ctx context.Context, userID uuid.UUID) ([]*PendingTx, error) {
	out := []*PendingTx{}
	minConf := s.cfg.MinConf
	if minConf == 0 {
		minConf = 6
	}

	// Get all assigned addresses for user
	rows, err := s.pool.Query(ctx, `
		SELECT a.address, COALESCE(a.asset, 'BTC')
		FROM assigned_addresses a
		WHERE a.user_id = $1
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var addresses []struct {
		Address string
		Asset   string
	}
	for rows.Next() {
		var addr, asset string
		if err := rows.Scan(&addr, &asset); err != nil {
			return nil, err
		}
		addresses = append(addresses, struct {
			Address string
			Asset   string
		}{addr, asset})
	}

	// For each address, get all receive txs (any conf), then filter to < minConf
	for _, a := range addresses {
		// Use a high max-conf to get all txs, then filter
		// ListTransactions with minConf=0 returns all receive txs (mempool + confirmed)
		txs, err := s.driver.ListTransactions(ctx, a.Address, 0)
		if err != nil {
			continue
		}

		for _, tx := range txs {
			if tx.Category != "receive" {
				continue
			}

			// Check if this tx was already credited (in deposits table)
			var exists bool
			err := s.pool.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM deposits WHERE chain = $1 AND tx_hash = $2)
			`, s.driver.Name(), tx.TxHash).Scan(&exists)
			if err != nil {
				continue
			}
			if exists {
				continue // already credited, not pending
			}

			// Only show if < minConf
			if tx.Confirmations >= int64(minConf) {
				continue
			}

			status := "mempool"
			if tx.Confirmations > 0 {
				status = "confirming"
			}

			out = append(out, &PendingTx{
				TxHash:        tx.TxHash,
				Address:       tx.Address,
				Asset:         a.Asset,
				Amount:        tx.Amount,
				Confirmations: tx.Confirmations,
				MinConf:       minConf,
				BlockHeight:   tx.BlockHeight,
				Time:          tx.Time,
				Status:        status,
			})
		}
	}

	return out, nil
}

// PollWithdrawals checks BROADCAST withdrawals for confirmations and marks DONE.
//
// Run in the main loop after pollDeposits. Updates withdrawal status:
//   - BROADCAST + >= minConf → DONE (confirmed on chain)
//   - BROADCAST + < minConf → stays BROADCAST (waiting)
//   - BROADCAST + tx not found → stays BROADCAST (might be reorged)
func (s *Service) PollWithdrawals(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tx_hash, chain
		FROM withdrawals
		WHERE status = 'BROADCAST' AND tx_hash IS NOT NULL AND tx_hash != ''
		LIMIT 50
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	minConf := s.cfg.MinConf
	s.log.Info("poll withdrawals started", "minConf", s.cfg.MinConf)
	if minConf == 0 {
		minConf = 6
	}

	for rows.Next() {
		var id uuid.UUID
		var txHash, chain string
		if err := rows.Scan(&id, &txHash, &chain); err != nil {
			return err
		}
		s.log.Info("checking withdrawal", "id", id, "tx_hash", txHash)
		confs, err := s.driver.GetConfirmations(ctx, txHash)
		if err != nil {
			s.log.Warn("get confirmations failed", "id", id, "tx_hash", txHash, "error", err)
			continue
		}
		s.log.Info("got confirmations", "id", id, "confs", confs, "minConf", minConf)
		if confs >= int64(minConf) {
			_, err := s.pool.Exec(ctx, `
				UPDATE withdrawals
				SET status = 'DONE', confirmations = $1, confirmed_at = NOW()
				WHERE id = $2 AND status = 'BROADCAST'
			`, confs, id)
			if err != nil {
				s.log.Warn("update withdrawal status failed", "id", id, "error", err)
				continue
			}
			s.log.Info("withdrawal confirmed",
				"id", id,
				"tx_hash", txHash,
				"confirmations", confs,
			)
			// Trigger: withdrawal DONE notification
			var userID uuid.UUID
			var amount, asset string
			_ = s.pool.QueryRow(ctx,
				`SELECT user_id, amount::text, asset FROM withdrawals WHERE id = $1`,
				id).Scan(&userID, &amount, &asset)
			if userID != uuid.Nil {
				s.notifier.SendNotification(ctx, userID, notifier.TypeWithdrawalDone,
					"Withdrawal Confirmed",
					fmt.Sprintf("Your withdrawal of %s %s has been confirmed on-chain.", amount, asset),
					map[string]any{"withdrawal_id": id.String(), "tx_hash": txHash})
			}
		} else {
			// Update confirmation count for UI display
			_, _ = s.pool.Exec(ctx, `
				UPDATE withdrawals SET confirmations = $1 WHERE id = $2 AND status = 'BROADCAST'
			`, confs, id)
		}
	}
	return nil
}



// DailyWithdrawalUsage returns the total USDT-equivalent withdrawn today (UTC).
func (s *Service) DailyWithdrawalUsage(ctx context.Context, userID uuid.UUID) (decimal.Decimal, error) {
	var total decimal.Decimal
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM withdrawals
		WHERE user_id = $1
		  AND status IN ('PENDING', 'BROADCAST', 'DONE')
		  AND created_at >= date_trunc('day', NOW() AT TIME ZONE 'UTC')
	`, userID).Scan(&total)
	return total, err
}

// WithdrawLimitCheck verifies the requested amount is within the user's daily limit.
// Returns ErrWithdrawLimitExceeded if the limit would be exceeded.
func (s *Service) WithdrawLimitCheck(ctx context.Context, userID uuid.UUID, amount decimal.Decimal) error {
	u, err := s.userSvc.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	limit, ok := user.WithdrawLimitByKYC[u.KycLevel]
	if !ok {
		return fmt.Errorf("unknown kyc level %d", u.KycLevel)
	}
	used, err := s.DailyWithdrawalUsage(ctx, userID)
	if err != nil {
		return err
	}
	if used.Add(amount).GreaterThan(limit) {
		return fmt.Errorf("daily withdrawal limit exceeded for kyc level L%d (limit: %s USDT, used: %s USDT, requested: %s USDT): %w",
			u.KycLevel, limit.String(), used.String(), amount.String(), ErrWithdrawLimitExceeded)
	}
	return nil
}

// ErrWithdrawLimitExceeded is returned when the daily withdrawal limit is exceeded.
var ErrWithdrawLimitExceeded = errors.New("withdrawal limit exceeded")


// ListAllWithdrawals returns the most recent N withdrawals (admin only).
func (s *Service) ListAllWithdrawals(ctx context.Context, limit int) ([]*Withdrawal, error) {
	const q = `
		SELECT w.id, w.user_id, w.asset, w.amount, w.dest_address, w.chain,
		       COALESCE(w.tx_hash, '') AS tx_hash, w.status, COALESCE(w.error_msg, '') AS error_msg, w.created_at, w.sent_at, w.confirmed_at
		FROM withdrawals w ORDER BY w.created_at DESC LIMIT $1
	`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Withdrawal{}
	for rows.Next() {
		w := &Withdrawal{}
		if err := rows.Scan(&w.ID, &w.UserID, &w.Asset, &w.Amount, &w.DestAddress, &w.Chain,
			&w.TxHash, &w.Status, &w.ErrorMsg, &w.CreatedAt, &w.SentAt, &w.ConfirmedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// ListAllDeposits returns the most recent N deposits (admin only).
func (s *Service) ListAllDeposits(ctx context.Context, limit int) ([]*Deposit, error) {
	const q = `
		SELECT id, user_id, chain, asset, address, from_address, amount, tx_hash, confirmations, status, created_at, credited_at
		FROM deposits ORDER BY created_at DESC LIMIT $1
	`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Deposit{}
	for rows.Next() {
		d := &Deposit{}
		if err := rows.Scan(&d.ID, &d.UserID, &d.Chain, &d.Asset, &d.Address, &d.FromAddress,
			&d.Amount, &d.TxHash, &d.Confirmations, &d.Status, &d.CreatedAt, &d.CreditedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// GetWithdrawStats returns withdrawal stats.
func (s *Service) GetWithdrawStats(ctx context.Context) (map[string]interface{}, error) {
	stats := map[string]interface{}{}
	var total, pending, broadcast, done, failed float64
	var totalAmount string
	err := s.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) AS total,
		  COUNT(*) FILTER (WHERE status = 'PENDING') AS pending,
		  COUNT(*) FILTER (WHERE status = 'BROADCAST') AS broadcast,
		  COUNT(*) FILTER (WHERE status = 'DONE') AS done,
		  COUNT(*) FILTER (WHERE status = 'FAILED') AS failed,
		  COALESCE(SUM(amount), 0) AS total_amount
		FROM withdrawals
	`).Scan(&total, &pending, &broadcast, &done, &failed, &totalAmount)
	if err != nil {
		return nil, err
	}
	stats["total_withdrawals"] = total
	stats["pending_withdrawals"] = pending
	stats["broadcast_withdrawals"] = broadcast
	stats["done_withdrawals"] = done
	stats["failed_withdrawals"] = failed
	stats["total_withdrawal_amount"] = totalAmount
	return stats, nil
}

// GetDepositStats returns deposit stats.
func (s *Service) GetDepositStats(ctx context.Context) (map[string]interface{}, error) {
	stats := map[string]interface{}{}
	var total, pending, done float64
	var totalAmount string
	err := s.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) AS total,
		  COUNT(*) FILTER (WHERE status = 'PENDING') AS pending,
		  COUNT(*) FILTER (WHERE status = 'DONE') AS done,
		  COALESCE(SUM(amount), 0) AS total_amount
		FROM deposits
	`).Scan(&total, &pending, &done, &totalAmount)
	if err != nil {
		return nil, err
	}
	stats["total_deposits"] = total
	stats["pending_deposits"] = pending
	stats["done_deposits"] = done
	stats["total_deposit_amount"] = totalAmount
	return stats, nil
}


// computeWithdrawRisk returns risk score for withdrawal.
func (s *Service) computeWithdrawRisk(ctx context.Context, userID uuid.UUID, amount decimal.Decimal, destAddress string) int {
	score := 0

	// Factor 1: Account age
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `SELECT created_at FROM users WHERE id = $1`, userID).Scan(&createdAt)
	if err == nil {
		age := time.Since(createdAt)
		if age < 24*time.Hour {
			score += 15
		} else if age < 7*24*time.Hour {
			score += 8
		}
	}

	// Factor 2: Amount ratio vs total deposits
	var totalDeposits decimal.Decimal
	_ = s.pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM deposits WHERE user_id = $1 AND status = 'CREDITED'`, userID).Scan(&totalDeposits)
	if totalDeposits.IsPositive() {
		ratio := amount.Div(totalDeposits)
		if ratio.GreaterThan(decimal.NewFromFloat(0.5)) {
			score += 20
		} else if ratio.GreaterThan(decimal.NewFromFloat(0.2)) {
			score += 10
		}
	}

	// Factor 3: New destination
	var sentBefore int
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM withdrawals WHERE user_id = $1 AND dest_address = $2 AND status IN ('DONE', 'BROADCAST')`, userID, destAddress).Scan(&sentBefore)
	if sentBefore == 0 {
		score += 10
	}

	// Factor 4: Unusual hour (2-6am UTC)
	hour := time.Now().UTC().Hour()
	if hour >= 2 && hour < 6 {
		score += 10
	}

	// Factor 5: Failed login attempts last 24h
	var email string
	_ = s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	var failed int
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM login_attempts WHERE email = $1 AND success = false AND timestamp > NOW() - INTERVAL '24 hours'`, email).Scan(&failed)
	if failed > 0 {
		pts := failed * 3
		if pts > 25 {
			pts = 25
		}
		score += pts
	}

	s.log.Info("withdraw risk score", "user_id", userID, "score", score, "amount", amount.String())
	return score
}

// ErrWithdrawalBlocked is returned when a withdrawal is blocked by risk control.
var ErrWithdrawalBlocked = errors.New("withdrawal blocked by risk control")


// ListHeldWithdrawals returns withdrawals with status=HOLD.
func (s *Service) ListHeldWithdrawals(ctx context.Context) ([]*Withdrawal, error) {
	const q = `
		SELECT w.id, w.user_id, w.asset, w.amount, w.dest_address, w.chain,
		       COALESCE(w.tx_hash, '') AS tx_hash, w.status, COALESCE(w.error_msg, '') AS error_msg,
		       w.created_at, w.sent_at, w.confirmed_at, w.risk_score, w.risk_hold
		FROM withdrawals w WHERE w.status = 'HOLD' ORDER BY w.created_at DESC
	`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Withdrawal{}
	for rows.Next() {
		w := &Withdrawal{}
		if err := rows.Scan(&w.ID, &w.UserID, &w.Asset, &w.Amount, &w.DestAddress, &w.Chain,
			&w.TxHash, &w.Status, &w.ErrorMsg, &w.CreatedAt, &w.SentAt, &w.ConfirmedAt, &w.RiskScore, &w.RiskHold); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// ApproveHeldWithdrawal approves a held withdrawal and broadcasts it.
func (s *Service) ApproveHeldWithdrawal(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	var asset, toAddress string
	var amount decimal.Decimal
	if err := tx.QueryRow(ctx, `
		SELECT user_id, asset, dest_address, amount
		FROM withdrawals WHERE id = $1 AND status = 'HOLD'
	`, id).Scan(&userID, &asset, &toAddress, &amount); err != nil {
		return fmt.Errorf("withdrawal not found or not on hold: %w", err)
	}

	// Send via driver
	txHash, err := s.driver.SendToAddress(ctx, asset, toAddress, amount)
	if err != nil {
		// Refund + mark FAILED
		_ = s.wallet.Credit(ctx, userID, asset, amount)
		s.failWithdrawal(ctx, id, err.Error())
		return err
	}

	sentAt := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE withdrawals SET tx_hash = $1, status = 'BROADCAST', risk_hold = FALSE, sent_at = $2
		WHERE id = $3
	`, txHash, sentAt, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RejectHeldWithdrawal rejects a held withdrawal and refunds.
func (s *Service) RejectHeldWithdrawal(ctx context.Context, id uuid.UUID, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID uuid.UUID
	var asset string
	var amount decimal.Decimal
	if err := tx.QueryRow(ctx, `
		SELECT user_id, asset, amount FROM withdrawals WHERE id = $1 AND status = 'HOLD'
	`, id).Scan(&userID, &asset, &amount); err != nil {
		return fmt.Errorf("withdrawal not found or not on hold: %w", err)
	}

	// Refund
	if err := s.wallet.Credit(ctx, userID, asset, amount); err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE withdrawals SET status = 'REJECTED', error_msg = $2, risk_hold = FALSE
		WHERE id = $1
	`, id, "rejected by admin: "+reason)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// driverForAsset returns the right driver for the asset via the registry.
// Falls back to s.driver if registry not initialized.
func (s *Service) driverForAsset(asset string) Driver {
	if s.registry != nil {
		if drv, ok := s.registry.GetForAsset(asset); ok {
			return drv
		}
	}
	return s.driver
}

// SetRegistry wires the chain registry into the service.
// Required for per-asset driver lookup (multi-chain withdrawal routing).
func (s *Service) SetRegistry(r *ChainRegistry) {
	s.registry = r
}

// GetRegistry returns the service's chain registry (or nil if not set).
func (s *Service) GetRegistry() *ChainRegistry {
	return s.registry
}


// getTokenConfig returns the token config for an asset (e.g. USDT on BSC).
// Returns nil if asset is not a token (native BNB/ETH).
func (s *Service) getTokenConfig(asset string) *config.TokenConfig {
	if s.registry == nil {
		return nil
	}
	return s.registry.GetTokenForAsset(asset)
}

// sendTokenWithdrawal handles ERC20/BEP-20 token withdrawals.
// It type-asserts the driver to *EVMDriver and calls sendERC20 with the token contract.
func (s *Service) sendTokenWithdrawal(ctx context.Context, drv Driver, tokenCfg *config.TokenConfig, toAddress string, amount decimal.Decimal) (string, error) {
	evmDrv, ok := drv.(*EVMDriver)
	if !ok {
		return "", fmt.Errorf("driver %s does not support ERC20 transfers (only EVM)", drv.Name())
	}
	// Convert decimal amount to token's smallest unit (e.g. 18 decimals)
	tokenAmount := amountToBigInt(amount, tokenCfg.Decimals)
	return evmDrv.sendERC20(ctx, tokenCfg.Contract, toAddress, tokenAmount)
}
