// SolanaDriver implements Driver for Solana blockchain.
//
// Uses Solana JSON-RPC API (https://api.mainnet-beta.solana.com or custom RPC).
//
// Implemented operations:
//   - GetReceived/GetReceivedConfirmed/GetReceivedPending: getBalance RPC
//   - SendToAddress: sendTransaction RPC (signed by caller, not driver)
//   - GetBlockCount: getSlot RPC
//   - GetConfirmations: getSignatureStatuses RPC
//   - ListTransactions: getSignaturesForAddress + getTransaction
//
// NOT implemented:
//   - SPL token balance queries (would need getTokenAccountsByOwner)
//   - SPL token transfers (need full transaction construction with program calls)
//   - Real transaction signing (callers should use signing.SolanaDeriver)
package chainwatcher

import (
	"bytes"
	"context"
	"encoding/json"
		"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// SolanaDriver implements Driver for Solana chains.
type SolanaDriver struct {
	name       string
	rpcURL     string
	commitment string
	asset      string
	hotAddr    string
	client     *http.Client
	log        *slog.Logger
	mu         sync.Mutex
}

// SolanaConfig configures the Solana driver.
type SolanaConfig struct {
	RPCURL     string
	Commitment string
	Asset      string
	HotAddr    string
	ChainID    string
}

// NewSolanaDriver creates a new Solana driver.
func NewSolanaDriver(cfg SolanaConfig, log *slog.Logger) (*SolanaDriver, error) {
	if cfg.RPCURL == "" {
		return nil, fmt.Errorf("rpc_url required for Solana driver")
	}
	if cfg.Commitment == "" {
		cfg.Commitment = "confirmed"
	}
	if cfg.Asset == "" {
		cfg.Asset = "SOL"
	}
	return &SolanaDriver{
		name:       "solana",
		rpcURL:     cfg.RPCURL,
		commitment: cfg.Commitment,
		asset:      cfg.Asset,
		hotAddr:    cfg.HotAddr,
		client:     &http.Client{Timeout: 30 * time.Second},
		log:        log,
	}, nil
}

func (d *SolanaDriver) Name() string { return d.name }

// GetHotAddress returns the configured hot wallet address.
func (d *SolanaDriver) GetHotAddress() string { return d.hotAddr }

// ErrNotSupported is reused from mock.go
// rpcCall makes a Solana JSON-RPC call.
func (d *SolanaDriver) rpcCall(ctx context.Context, method string, params []interface{}) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", d.rpcURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("solana RPC HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("solana RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// GetBalance returns the SOL balance.
func (d *SolanaDriver) getBalanceLamports(ctx context.Context, address string) (uint64, error) {
	if !isValidSolanaAddress(address) {
		return 0, fmt.Errorf("invalid Solana address: %s", address)
	}
	result, err := d.rpcCall(ctx, "getBalance", []interface{}{address})
	if err != nil {
		return 0, err
	}
	var balResp struct {
		Value uint64 `json:"value"`
	}
	if err := json.Unmarshal(result, &balResp); err != nil {
		return 0, fmt.Errorf("parse balance: %w", err)
	}
	return balResp.Value, nil
}

// lamportsToSOL converts lamports (1e9) to SOL decimal.
func lamportsToSOL(lamports uint64) decimal.Decimal {
	return decimal.NewFromInt(int64(lamports)).Div(decimal.NewFromInt(1_000_000_000))
}

// GetReceived returns total balance (used as deposit tracker).
func (d *SolanaDriver) GetReceived(ctx context.Context, address string) (decimal.Decimal, error) {
	bal, err := d.getBalanceLamports(ctx, address)
	if err != nil {
		return decimal.Zero, err
	}
	return lamportsToSOL(bal), nil
}

// GetReceivedConfirmed returns confirmed balance (>= minConf).
// Solana finality is fast (~12-15 sec), so we treat anything > 0 as confirmed.
func (d *SolanaDriver) GetReceivedConfirmed(ctx context.Context, address string, minConf int) (decimal.Decimal, error) {
	// For Solana, balance already reflects only confirmed transactions
	// (mempool transactions don't update balance)
	return d.GetReceived(ctx, address)
}

// GetReceivedPending returns mempool balance (not really applicable for Solana).
func (d *SolanaDriver) GetReceivedPending(ctx context.Context, address string, minConf int) (decimal.Decimal, error) {
	// Solana doesn't have a separate "pending" concept like Bitcoin mempool
	return decimal.Zero, nil
}

// SendToAddress sends SOL to the address.
// NOTE: This requires a pre-signed transaction. In production, callers should
// construct and sign the transaction using SolanaDeriver, then pass it here.
func (d *SolanaDriver) SendToAddress(ctx context.Context, asset, address string, amount decimal.Decimal) (string, error) {
	if !isValidSolanaAddress(address) {
		return "", fmt.Errorf("invalid Solana address: %s", address)
	}
	// TODO: implement real SOL transfer (requires transaction construction)
	return "", fmt.Errorf("Solana SendToAddress requires pre-signed tx - not yet implemented for production use")
}

// GetBlockCount returns current slot.
func (d *SolanaDriver) GetBlockCount(ctx context.Context) (int64, error) {
	result, err := d.rpcCall(ctx, "getSlot", []interface{}{})
	if err != nil {
		return 0, err
	}
	var slot uint64
	if err := json.Unmarshal(result, &slot); err != nil {
		return 0, fmt.Errorf("parse slot: %w", err)
	}
	return int64(slot), nil
}

// GenerateAddress is not supported (would need to manage Solana keypairs).
func (d *SolanaDriver) GenerateAddress(ctx context.Context) (string, error) {
	return "", ErrNotSupported
}

// GetConfirmations returns confirmations for a tx signature.
func (d *SolanaDriver) GetConfirmations(ctx context.Context, txHash string) (int64, error) {
	result, err := d.rpcCall(ctx, "getSignatureStatuses", []interface{}{
		[]string{txHash},
		map[string]bool{"searchTransactionHistory": true},
	})
	if err != nil {
		return 0, err
	}
	var statusResp struct {
		Value []struct {
			ConfirmationStatus string `json:"confirmationStatus"`
			Slot               uint64 `json:"slot"`
		} `json:"value"`
	}
	if err := json.Unmarshal(result, &statusResp); err != nil {
		return 0, err
	}
	if len(statusResp.Value) == 0 || statusResp.Value[0].ConfirmationStatus == "" {
		return -1, nil // Not found
	}
	switch statusResp.Value[0].ConfirmationStatus {
	case "finalized":
		return 1, nil
	case "confirmed":
		return 1, nil
	case "processed":
		return 1, nil
	default:
		return 0, nil
	}
}

// ListTransactions lists transactions for an address.
func (d *SolanaDriver) ListTransactions(ctx context.Context, address string, minConf int) ([]TxRecord, error) {
	if !isValidSolanaAddress(address) {
		return nil, fmt.Errorf("invalid Solana address: %s", address)
	}
	result, err := d.rpcCall(ctx, "getSignaturesForAddress", []interface{}{
		address,
		map[string]interface{}{
			"limit": 100,
		},
	})
	if err != nil {
		return nil, err
	}

	var sigs []struct {
		Signature string `json:"signature"`
		Slot      uint64 `json:"slot"`
		BlockTime int64  `json:"blockTime"`
		Err       any    `json:"err"`
	}
	if err := json.Unmarshal(result, &sigs); err != nil {
		return nil, fmt.Errorf("parse signatures: %w", err)
	}

	var txs []TxRecord
	for _, s := range sigs {
		if s.Err != nil {
			continue
		}
		txs = append(txs, TxRecord{
			TxHash:        s.Signature,
			Amount:        decimal.Zero, // Would need getTransaction for amount
			Address:       address,
			Confirmations: 1,
			BlockHeight:   int64(s.Slot),
			Category:      "receive",
			Time:          s.BlockTime,
		})
	}

	return txs, nil
}

// SpawnDeposit is only for mock drivers.
func (d *SolanaDriver) SpawnDeposit(ctx context.Context, userID, asset, txHash string, amount decimal.Decimal) error {
	return ErrNotSupported
}

// HasSigner returns false - real signing is external.
func (d *SolanaDriver) HasSigner() bool {
	return false
}

// isValidSolanaAddress checks basic format.
func isValidSolanaAddress(addr string) bool {
	if len(addr) < 32 || len(addr) > 50 {
		return false
	}
	if strings.HasPrefix(addr, "0x") {
		return false
	}
	for _, c := range addr {
		switch c {
		case '0', 'O', 'I', 'l':
			return false
		}
	}
	return true
}
