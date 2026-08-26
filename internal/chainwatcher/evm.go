package chainwatcher

import (
	"strconv"
	"crypto/ecdsa"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/shopspring/decimal"
)

// EVMDriver implements Driver for EVM-compatible chains (Ethereum, BSC, Polygon, etc.).
// Uses standard Ethereum JSON-RPC over HTTP.
type EVMDriver struct {
	name       string // "eth" or "bsc"
	rpcURL     string
	chainID    int64
	asset      string // "ETH" or "BNB"
	hotAddr    string // hot wallet address
	client     *http.Client
	mu         sync.Mutex
	signer     TxSigner    // optional - if set, SendToAddress uses it
	privKey    *ecdsa.PrivateKey // optional - if set, uses go-ethereum for proper EIP-155 signing
}

// TxSigner is the interface drivers use to sign transactions.
// Defined here to avoid circular imports with internal/signing.
type TxSigner interface {
	Name() string
	Chain() string
	Address() string
	SignTransaction(ctx context.Context, data []byte) (signed []byte, txHash string, err error)
}

func NewEVMDriver(name, rpcURL, asset string, chainID int64, hotAddr string, signer TxSigner) (*EVMDriver, error) {
	if rpcURL == "" {
		return nil, fmt.Errorf("rpc_url required for EVM driver")
	}
	return &EVMDriver{
		name:    name,
		rpcURL:  rpcURL,
		chainID: chainID,
		asset:   asset,
		hotAddr: hotAddr,
		signer:  signer,
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (d *EVMDriver) Name() string { return d.name }

// rpcCall makes an Ethereum JSON-RPC call.
func (d *EVMDriver) rpcCall(ctx context.Context, method string, params []interface{}) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", d.rpcURL, strings.NewReader(string(body)))
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
	var result struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w (body: %s)", err, string(respBody))
	}
	if result.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", result.Error.Code, result.Error.Message)
	}
	return result.Result, nil
}

func (d *EVMDriver) GetBlockCount(ctx context.Context) (int64, error) {
	r, err := d.rpcCall(ctx, "eth_blockNumber", nil)
	if err != nil {
		return 0, err
	}
	var hex string
	if err := json.Unmarshal(r, &hex); err != nil {
		return 0, err
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(hex, "0x"), 16)
	if !ok {
		return 0, fmt.Errorf("invalid block number: %s", hex)
	}
	return n.Int64(), nil
}

// GenerateAddress creates a new address derived from a deterministic seed.
// In production, this should be a real HD wallet (BIP44).
// For demo, returns a placeholder address.
func (d *EVMDriver) GenerateAddress(ctx context.Context) (string, error) {
	// For real production: derive from BIP44 HD wallet
	// For demo: use a fixed test address pattern
	return "0x" + strings.Repeat("0", 40), nil
}

// GetReceived returns balance (incl. pending) for an address.
// Note: EVM doesn't have separate "received" - it's just balance.
func (d *EVMDriver) GetReceived(ctx context.Context, address string) (decimal.Decimal, error) {
	return d.GetReceivedConfirmed(ctx, address, 0)
}

// GetReceivedConfirmed returns balance with >= minConf confirmations.
func (d *EVMDriver) GetReceivedConfirmed(ctx context.Context, address string, minConf int) (decimal.Decimal, error) {
	r, err := d.rpcCall(ctx, "eth_getBalance", []interface{}{address, fmt.Sprintf("0x%x", minConf)})
	if err != nil {
		return decimal.Zero, err
	}
	var hex string
	if err := json.Unmarshal(r, &hex); err != nil {
		return decimal.Zero, err
	}
	wei, ok := new(big.Int).SetString(strings.TrimPrefix(hex, "0x"), 16)
	if !ok {
		return decimal.Zero, fmt.Errorf("invalid balance: %s", hex)
	}
	// wei -> ether: divide by 1e18
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	ether := new(big.Float).Quo(new(big.Float).SetInt(wei), new(big.Float).SetInt(divisor))
	f, _ := ether.Float64()
	return decimal.NewFromFloat(f), nil
}

// GetReceivedPending returns mempool balance. For EVM, we use 0 vs >=1.
// Returns current - confirmed.
func (d *EVMDriver) GetReceivedPending(ctx context.Context, address string, minConf int) (decimal.Decimal, error) {
	pending, err := d.GetReceivedConfirmed(ctx, address, 0)
	if err != nil {
		return decimal.Zero, err
	}
	confirmed, err := d.GetReceivedConfirmed(ctx, address, minConf)
	if err != nil {
		return decimal.Zero, err
	}
	return pending.Sub(confirmed), nil
}

// SendToAddress sends native token (ETH/BNB). Returns tx hash.
// If signer is set, builds + signs tx locally, then broadcasts.
// If no signer, returns error (must be configured).
func (d *EVMDriver) SendToAddress(ctx context.Context, asset, address string, amount decimal.Decimal) (string, error) {
	if d.signer == nil {
		return "", fmt.Errorf("EVM driver %s has no signer configured", d.name)
	}
	return d.signAndBroadcast(ctx, address, amount)
}


// getBaseFee returns the base fee of the latest block (EIP-1559).
// Returns 0 if chain doesn't use EIP-1559 (pre-London fork).
func (d *EVMDriver) getBaseFee(ctx context.Context) (*big.Int, error) {
	resp, err := d.rpcCall(ctx, "eth_getBlockByNumber", []interface{}{"latest", false})
	if err != nil {
		return nil, fmt.Errorf("get block: %w", err)
	}
	var block struct {
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	if err := json.Unmarshal(resp, &block); err != nil {
		return nil, fmt.Errorf("parse block: %w", err)
	}
	if block.BaseFeePerGas == "" {
		// No baseFee = pre-EIP-1559 chain
		return big.NewInt(0), nil
	}
	baseFee, ok := new(big.Int).SetString(strings.TrimPrefix(block.BaseFeePerGas, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("parse baseFee: %s", block.BaseFeePerGas)
	}
	return baseFee, nil
}

// getMaxPriorityFee returns the suggested max priority fee per gas.
// Falls back to 1 gwei if the RPC doesn't support the method.
func (d *EVMDriver) getMaxPriorityFee(ctx context.Context) (*big.Int, error) {
	resp, err := d.rpcCall(ctx, "eth_maxPriorityFeePerGas", nil)
	if err != nil {
		// Fallback: 1 gwei
		return big.NewInt(1000000000), nil
	}
	var hexStr string
	if err := json.Unmarshal(resp, &hexStr); err != nil {
		return big.NewInt(1000000000), nil
	}
	fee, ok := new(big.Int).SetString(strings.TrimPrefix(hexStr, "0x"), 16)
	if !ok {
		return big.NewInt(1000000000), nil
	}
	return fee, nil
}

// ERC20 Transfer event signature
var erc20TransferEventTopic = common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

// IndexedTransfer represents an ERC20 Transfer event.
type IndexedTransfer struct {
	TxHash      string
	LogIndex    int
	BlockNumber uint64
	From        common.Address
	To          common.Address
	Amount      *big.Int
	Token       common.Address
}

// ScanLogs scans blocks for ERC20 Transfer events to destAddress.
func (d *EVMDriver) ScanLogs(ctx context.Context, fromBlock, toBlock uint64, destAddress string) ([]IndexedTransfer, error) {
	dest := common.HexToAddress(destAddress)
	filter := map[string]interface{}{
		"fromBlock": fmt.Sprintf("0x%x", fromBlock),
		"toBlock":   fmt.Sprintf("0x%x", toBlock),
		"address":   []string{}, // any contract
		"topics": [][]string{
			{erc20TransferEventTopic.Hex()},
			{},
			{fmt.Sprintf("0x%064x", dest)},
		},
	}
	resp, err := d.rpcCall(ctx, "eth_getLogs", []interface{}{filter})
	if err != nil {
		return nil, fmt.Errorf("get logs: %w", err)
	}
	var logs []struct {
		TransactionHash string   `json:"transactionHash"`
		LogIndex        string   `json:"logIndex"`
		BlockNumber     string   `json:"blockNumber"`
		Address         string   `json:"address"`
		Topics          []string `json:"topics"`
		Data            string   `json:"data"`
	}
	if err := json.Unmarshal(resp, &logs); err != nil {
		return nil, fmt.Errorf("parse logs: %w", err)
	}
	var transfers []IndexedTransfer
	for _, l := range logs {
		if len(l.Topics) < 3 {
			continue
		}
		to := common.BytesToAddress(common.HexToHash(l.Topics[2]).Bytes())
		if to != dest {
			continue
		}
		amount := new(big.Int).SetBytes(common.FromHex(l.Data))
		blockNum, _ := strconv.ParseUint(strings.TrimPrefix(l.BlockNumber, "0x"), 16, 64)
		logIdx, _ := strconv.Atoi(strings.TrimPrefix(l.LogIndex, "0x"))
		fromAddr := common.BytesToAddress(common.HexToHash(l.Topics[1]).Bytes())
		transfers = append(transfers, IndexedTransfer{
			TxHash:      l.TransactionHash,
			LogIndex:    logIdx,
			BlockNumber: blockNum,
			From:        fromAddr,
			To:          to,
			Amount:      amount,
			Token:       common.HexToAddress(l.Address),
		})
	}
	return transfers, nil
}

// supportsEIP1559 returns true if chain supports EIP-1559 (baseFee != 0).
func (d *EVMDriver) supportsEIP1559(ctx context.Context) bool {
	baseFee, err := d.getBaseFee(ctx)
	if err != nil {
		return false
	}
	return baseFee.Sign() > 0
}

// signAndBroadcast builds a real EIP-1559 or EIP-1559 tx, signs it locally, and broadcasts via eth_sendRawTransaction.
// Returns the tx hash.
//
// This is a simplified tx builder. Production should use:
//   - go-ethereum/core/types for proper EIP-1559/EIP-2930 construction
//   - eth/gasprice for fee estimation
//   - eth/accounts for HD wallet signing
func (d *EVMDriver) signAndBroadcast(ctx context.Context, toAddress string, amount decimal.Decimal) (string, error) {
	if !common.IsHexAddress(toAddress) {
		return "", fmt.Errorf("invalid destination address: %s", toAddress)
	}
	amountWei := amount.Mul(decimal.NewFromInt(1e18)).BigInt()
	if amountWei.Sign() <= 0 {
		return "", fmt.Errorf("amount must be positive")
	}

	// Get nonce for sender
	fromAddr := d.signer.Address()
	nonce, err := d.getUint(ctx, "eth_getTransactionCount",
		[]interface{}{fromAddr, "pending"})
	if err != nil {
		return "", fmt.Errorf("get nonce: %w", err)
	}

	// Get gas price
	gasPrice, err := d.getUint(ctx, "eth_gasPrice", nil)
	if err != nil {
		return "", fmt.Errorf("get gas price: %w", err)
	}

	// Build transaction - EIP-1559 (DynamicFeeTx) if supported, else legacy.
	// LatestSignerForChainID auto-picks London vs EIP-155 signer based on chainID.
	gasLimit := uint64(21000)
	chainID := big.NewInt(d.chainID)

	var tx *types.Transaction
	if d.supportsEIP1559(ctx) {
		// EIP-1559 (Ethereum, Optimism, Arbitrum, Base, Polygon)
		baseFee, err := d.getBaseFee(ctx)
		if err != nil {
			return "", fmt.Errorf("get base fee: %w", err)
		}
		tipCap, err := d.getMaxPriorityFee(ctx)
		if err != nil {
			return "", fmt.Errorf("get priority fee: %w", err)
		}
		// maxFeePerGas = baseFee * 2 + tipCap (buffer for next block base fee increase)
		maxFeePerGas := new(big.Int).Add(
			new(big.Int).Mul(baseFee, big.NewInt(2)),
			tipCap,
		)
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce.Uint64(),
			GasTipCap: tipCap,
			GasFeeCap: maxFeePerGas,
			Gas:       gasLimit,
			To:        func() *common.Address { addr := common.HexToAddress(toAddress); return &addr }(),
			Value:     amountWei,
			Data:      nil,
		})
	} else {
		// Legacy (BSC, pre-EIP-1559 chains)
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce.Uint64(),
			GasPrice: gasPrice,
			Gas:      gasLimit,
			To:       func() *common.Address { addr := common.HexToAddress(toAddress); return &addr }(),
			Value:    amountWei,
			Data:      nil,
		})
	}

	// Sign with go-ethereum types (proper EIP-155)
	if d.privKey == nil {
		return "", fmt.Errorf("EVM driver %s missing privKey (should be set by factory)", d.name)
	}
	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, d.privKey)
	if err != nil {
		return "", fmt.Errorf("sign tx: %w", err)
	}

	// Marshal signed tx to RLP bytes
	signedBytes, err := signedTx.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("marshal tx: %w", err)
	}

	// Broadcast via eth_sendRawTransaction
	rawHex := "0x" + hex.EncodeToString(signedBytes)
	resp, err := d.rpcCall(ctx, "eth_sendRawTransaction", []interface{}{rawHex})
	if err != nil {
		return "", fmt.Errorf("send tx: %w", err)
	}
	var txHash string
	if err := json.Unmarshal(resp, &txHash); err != nil {
		return "", fmt.Errorf("parse send result: %w", err)
	}
	return txHash, nil
}


// getUint extracts a hex result from JSON-RPC and converts to *big.Int.
func (d *EVMDriver) getUint(ctx context.Context, method string, params []interface{}) (*big.Int, error) {
	resp, err := d.rpcCall(ctx, method, params)
	if err != nil {
		return nil, err
	}
	var hexStr string
	if err := json.Unmarshal(resp, &hexStr); err != nil {
		return nil, fmt.Errorf("parse RPC result: %w", err)
	}
	val, ok := new(big.Int).SetString(strings.TrimPrefix(hexStr, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("invalid hex: %s", hexStr)
	}
	return val, nil
}


// IsValidEVMAddress validates an Ethereum address.
func IsValidEVMAddress(addr string) bool {
	return common.IsHexAddress(addr)
}

// GetConfirmations for a tx: blockNumber - tx.blockNumber + 1, or 0 if pending.
func (d *EVMDriver) GetConfirmations(ctx context.Context, txHash string) (int64, error) {
	if !strings.HasPrefix(txHash, "0x") {
		return -1, fmt.Errorf("invalid tx hash")
	}
	r, err := d.rpcCall(ctx, "eth_getTransactionReceipt", []interface{}{txHash})
	if err != nil {
		return -1, err
	}
	// null = pending
	if string(r) == "null" || len(r) == 0 {
		return 0, nil
	}
	var receipt struct {
		BlockNumber string `json:"blockNumber"`
	}
	if err := json.Unmarshal(r, &receipt); err != nil {
		return -1, err
	}
	if receipt.BlockNumber == "" {
		return 0, nil
	}
	blockNum, _ := new(big.Int).SetString(strings.TrimPrefix(receipt.BlockNumber, "0x"), 16)
	head, err := d.GetBlockCount(ctx)
	if err != nil {
		return -1, err
	}
	confs := head - blockNum.Int64() + 1
	if confs < 0 {
		confs = 0
	}
	return confs, nil
}

// ListTransactions for an address using eth_getTransactionByBlockHashAndIndex or scan blocks.
// For demo, returns empty list. Production would need a scan or use a token tracker.
func (d *EVMDriver) ListTransactions(ctx context.Context, address string, minConf int) ([]TxRecord, error) {
	// Note: Ethereum doesn't have a simple "list transactions for address" RPC.
	// Production solutions:
	// 1. Use Etherscan/BSCScan API
	// 2. Index blocks locally (full node)
	// 3. Subscribe to logs via WebSocket
	// For demo, return empty
	return []TxRecord{}, nil
}

func (d *EVMDriver) SpawnDeposit(ctx context.Context, userID, asset, txHash string, amount decimal.Decimal) error {
	return fmt.Errorf("spawn not supported by real EVM driver")
}

func (d *EVMDriver) HasSigner() bool { return d.signer != nil }
func (d *EVMDriver) GetHotAddress() string { return d.hotAddr }

