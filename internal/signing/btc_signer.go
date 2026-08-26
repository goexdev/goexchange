package signing

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BitcoinCoreSigner signs BTC transactions using Bitcoin Core's wallet RPC.
// Bitcoin Core holds the private keys in wallet.dat and signs via:
//   signrawtransactionwithwallet "rawhex"
//   sendrawtransaction "signedhex"
//
// This is the simplest and most secure approach for BTC:
// - Keys never leave the Bitcoin Core node
// - Can be replaced with HSM/Vault later via same interface
type BitcoinCoreSigner struct {
	chain    Chain
	rpcURL   string
	user     string
	password string
	address  string // hot wallet address (loaded from getwalletinfo or config)
	client   *http.Client
}

func NewBitcoinCoreSigner(rpcURL, user, password, address string) (*BitcoinCoreSigner, error) {
	if rpcURL == "" {
		return nil, fmt.Errorf("rpc_url required")
	}
	return &BitcoinCoreSigner{
		chain:    ChainBTC,
		rpcURL:   rpcURL,
		user:     user,
		password: password,
		address:  address,
		client:   &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (s *BitcoinCoreSigner) Name() string { return "bitcoin-core-wallet" }
func (s *BitcoinCoreSigner) Chain() Chain { return s.chain }
func (s *BitcoinCoreSigner) Address() string { return s.address }

// SignTransaction signs a BTC transaction using Bitcoin Core wallet.
// Expects tx.Data to be the hex-encoded unsigned raw transaction.
func (s *BitcoinCoreSigner) SignTransaction(ctx context.Context, tx UnsignedTx) (SignedTx, error) {
	if len(tx.Data) == 0 {
		return SignedTx{}, &ValidationError{Reason: "empty tx data"}
	}

	rawHex := hex.EncodeToString(tx.Data)
	signedHex, err := s.signRawTransaction(ctx, rawHex)
	if err != nil {
		return SignedTx{}, fmt.Errorf("signrawtransactionwithwallet failed: %w", err)
	}

	signedBytes, err := hex.DecodeString(signedHex)
	if err != nil {
		return SignedTx{}, fmt.Errorf("decode signed hex: %w", err)
	}

	// Note: signedHex is NOT the tx hash - it's the raw tx bytes
	// To get the tx hash, we'd need to sendrawtransaction and read back,
	// or compute it locally from the signed bytes.
	return SignedTx{
		Raw:    signedBytes,
		TxHash: "", // not computed here; populated after broadcast
	}, nil
}

// signRawTransaction calls Bitcoin Core's signrawtransactionwithwallet RPC.
func (s *BitcoinCoreSigner) signRawTransaction(ctx context.Context, rawHex string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "1.0", "id": "goexchange", "method": "signrawtransactionwithwallet",
		"params": []interface{}{rawHex},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", s.rpcURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.user != "" {
		req.SetBasicAuth(s.user, s.password)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result struct {
		Result struct {
			Hex        string `json:"hex"`
			Complete   bool   `json:"complete"`
			Signatures struct {
				Txid string `json:"txid"`
			} `json:"signatures"`
		} `json:"result"`
		Error interface{} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal: %w (body: %s)", err, string(respBody))
	}
	if result.Error != nil {
		return "", fmt.Errorf("rpc error: %v", result.Error)
	}
	if !result.Result.Complete {
		return "", fmt.Errorf("transaction not completely signed (some inputs unsigned)")
	}
	if result.Result.Hex == "" {
		return "", fmt.Errorf("empty signed hex returned")
	}
	return result.Result.Hex, nil
}

// SendSignedTransaction broadcasts a signed transaction and returns the tx hash.
// Uses Bitcoin Core's sendrawtransaction RPC.
func (s *BitcoinCoreSigner) SendSignedTransaction(ctx context.Context, signedHex string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "1.0", "id": "goexchange", "method": "sendrawtransaction",
		"params": []interface{}{signedHex},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", s.rpcURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.user != "" {
		req.SetBasicAuth(s.user, s.password)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var result struct {
		Result string      `json:"result"`
		Error  interface{} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("rpc error: %v", result.Error)
	}
	return result.Result, nil
}

// CreateRawTransaction creates an unsigned BTC transaction.
// This is a helper that calls createrawtransaction RPC.
// In production this, would be replaced by PSBT construction.
func (s *BitcoinCoreSigner) CreateRawTransaction(ctx context.Context, outputs []interface{}, lockTime int64) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "1.0", "id": "goexchange", "method": "createrawtransaction",
		"params": []interface{}{[]interface{}{}, outputs, lockTime},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", s.rpcURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.user != "" {
		req.SetBasicAuth(s.user, s.password)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Result string `json:"result"`
		Error  interface{} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("rpc error: %v", result.Error)
	}
	return result.Result, nil
}

// fundRawTransaction adds inputs (UTXOs) to a raw transaction.
// This uses Bitcoin Core's wallet to select UTXOs and fund the tx.
func (s *BitcoinCoreSigner) fundRawTransaction(ctx context.Context, rawHex string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "1.0", "id": "goexchange", "method": "fundrawtransaction",
		"params": []interface{}{rawHex},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", s.rpcURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.user != "" {
		req.SetBasicAuth(s.user, s.password)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Result struct {
			Hex       string  `json:"hex"`
			Fee       float64 `json:"fee"`
			ChangePos int     `json:"changepos"`
		} `json:"result"`
		Error interface{} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("rpc error: %v", result.Error)
	}
	return result.Result.Hex, nil
}
