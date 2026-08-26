package chainwatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// BTCDriver implements Driver for Bitcoin Core (works with BTC testnet/regtest).
// Uses the Bitcoin Core RPC interface for native Bitcoin.
type BTCDriver struct {
	name     string
	rpcURL   string
	user     string
	password string
	client   *http.Client
	chainID  string // "btc" or "btc-testnet"
	asset    string // "BTC"
	hotAddr  string // hot wallet address (used for withdrawals)
	mu       sync.Mutex
}

func NewBTCDriver(rpcURL, user, password, chainID, hotAddr string) (*BTCDriver, error) {
	if rpcURL == "" {
		return nil, fmt.Errorf("rpc_url required for BTC driver")
	}
	return &BTCDriver{
		name:     "btc",
		rpcURL:   rpcURL,
		user:     user,
		password: password,
		client:   &http.Client{Timeout: 10 * time.Second},
		chainID:  chainID,
		asset:    "BTC",
		hotAddr:  hotAddr,
	}, nil
}

func (d *BTCDriver) Name() string { return d.name }

// rpcCall makes a Bitcoin Core JSON-RPC call.
// BTC uses Bitcoin Core RPC.
func (d *BTCDriver) rpcCall(ctx context.Context, method string, params []interface{}) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "1.0", "id": "goexchange", "method": method, "params": params,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", d.rpcURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.user != "" {
		req.SetBasicAuth(d.user, d.password)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Result json.RawMessage `json:"result"`
		Error  interface{}     `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Error != nil {
		return nil, fmt.Errorf("rpc error: %v", result.Error)
	}
	return result.Result, nil
}

func (d *BTCDriver) GetBlockCount(ctx context.Context) (int64, error) {
	r, err := d.rpcCall(ctx, "getblockcount", nil)
	if err != nil {
		return 0, err
	}
	var n int64
	if err := json.Unmarshal(r, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func (d *BTCDriver) GenerateAddress(ctx context.Context) (string, error) {
	r, err := d.rpcCall(ctx, "getnewaddress", nil)
	if err != nil {
		return "", err
	}
	var addr string
	if err := json.Unmarshal(r, &addr); err != nil {
		return "", err
	}
	return addr, nil
}

// GetReceived returns how much BTC an address has received (incl. mempool).
func (d *BTCDriver) GetReceived(ctx context.Context, address string) (decimal.Decimal, error) {
	r, err := d.rpcCall(ctx, "getreceivedbyaddress", []interface{}{address, 0})
	if err != nil {
		return decimal.Zero, err
	}
	var v float64
	if err := json.Unmarshal(r, &v); err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(v), nil
}

// GetReceivedConfirmed returns balance with >= minConf confirmations.
func (d *BTCDriver) GetReceivedConfirmed(ctx context.Context, address string, minConf int) (decimal.Decimal, error) {
	r, err := d.rpcCall(ctx, "getreceivedbyaddress", []interface{}{address, minConf})
	if err != nil {
		return decimal.Zero, err
	}
	var v float64
	if err := json.Unmarshal(r, &v); err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(v), nil
}

// GetReceivedPending returns mempool balance (received but < minConf).
func (d *BTCDriver) GetReceivedPending(ctx context.Context, address string, minConf int) (decimal.Decimal, error) {
	total, err := d.GetReceived(ctx, address)
	if err != nil {
		return decimal.Zero, err
	}
	confirmed, err := d.GetReceivedConfirmed(ctx, address, minConf)
	if err != nil {
		return decimal.Zero, err
	}
	return total.Sub(confirmed), nil
}

func (d *BTCDriver) SendToAddress(ctx context.Context, asset, address string, amount decimal.Decimal) (string, error) {
	if asset != "BTC" {
		return "", fmt.Errorf("btc driver only supports BTC asset, got %s", asset)
	}
	r, err := d.rpcCall(ctx, "sendtoaddress", []interface{}{address, amount.String()})
	if err != nil {
		return "", err
	}
	var txid string
	if err := json.Unmarshal(r, &txid); err != nil {
		return "", err
	}
	return txid, nil
}

func (d *BTCDriver) GetConfirmations(ctx context.Context, txHash string) (int64, error) {
	r, err := d.rpcCall(ctx, "gettransaction", []interface{}{txHash})
	if err != nil {
		return -1, err
	}
	var tx struct {
		Confirmations int64 `json:"confirmations"`
	}
	if err := json.Unmarshal(r, &tx); err != nil {
		return -1, err
	}
	return tx.Confirmations, nil
}

// ListTransactions returns receive txs for an address with >= minConf confirmations.
// Uses listtransactions RPC.
func (d *BTCDriver) ListTransactions(ctx context.Context, address string, minConf int) ([]TxRecord, error) {
	// Get last 100 receive txs
	r, err := d.rpcCall(ctx, "listtransactions", []interface{}{"*", 100, 0, true})
	if err != nil {
		return nil, err
	}
	var rawTxs []map[string]interface{}
	if err := json.Unmarshal(r, &rawTxs); err != nil {
		return nil, err
	}

	var records []TxRecord
	for _, tx := range rawTxs {
		category, _ := tx["category"].(string)
		if category != "receive" {
			continue
		}
		txAddress, _ := tx["address"].(string)
		if txAddress != address {
			continue
		}
		confF, _ := tx["confirmations"].(float64)
		conf := int64(confF)
		if conf < int64(minConf) {
			continue
		}
		amountF, _ := tx["amount"].(float64)
		amount := decimal.NewFromFloat(amountF)
		txid, _ := tx["txid"].(string)
		blockHeightF, _ := tx["blockheight"].(float64)
		blockHeight := int64(blockHeightF)
		timeF, _ := tx["time"].(float64)
		timeUnix := int64(timeF)

		records = append(records, TxRecord{
			TxHash:        txid,
			Amount:        amount,
			Address:       address,
			Confirmations: conf,
			BlockHeight:   blockHeight,
			Category:      category,
			Time:          timeUnix,
		})
	}
	return records, nil
}

func (d *BTCDriver) SpawnDeposit(ctx context.Context, userID, asset, txHash string, amount decimal.Decimal) error {
	return fmt.Errorf("spawn not supported by real BTC driver")
}

func (d *BTCDriver) HasSigner() bool { return false } // requires Bitcoin Core wallet setup
func (d *BTCDriver) GetHotAddress() string { return d.hotAddr }
