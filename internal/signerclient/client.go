// Package signerclient provides the client for the signer service.
//
// The signer service is the only component with access to private keys.
// This client is used by the exchange API to:
//  1. Get addresses for deposits
//  2. Sign withdrawal transactions
//  3. Get public keys
//
// All requests are authenticated with mTLS + HMAC.
package signerclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Config configures the signer client.
type Config struct {
	URL         string        // e.g. "https://signer.signer.internal:8443"
	CertFile    string        // mTLS client cert
	KeyFile     string        // mTLS client key
	CACertFile  string        // CA cert for server verification
	APISecret   string        // Shared secret for HMAC
	Timeout     time.Duration // Request timeout (default: 30s)
}

// Client is the signer HTTP client.
type Client struct {
	cfg    Config
	hc     *http.Client
	secret []byte
}

// New creates a new signer client.
func New(cfg Config) (*Client, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	// Load client cert
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	// Load CA
	caData, err := os.ReadFile(cfg.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA: %w", err)
	}
	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caData)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS13,
	}

	return &Client{
		cfg: cfg,
		hc: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
		secret: []byte(cfg.APISecret),
	}, nil
}

// AddressResponse is the response from /address/{chain}.
type AddressResponse struct {
	Address string `json:"address"`
	Chain   string `json:"chain"`
}

// SignRequest is the request body for /sign.
type SignRequest struct {
	Chain       string          `json:"chain"`
	TxData      json.RawMessage `json:"tx_data"`
	WithdrawalID string          `json:"withdrawal_id,omitempty"`
	UserID      string          `json:"user_id,omitempty"`
}

// SignResponse is the response from /sign.
type SignResponse struct {
	SignedTx string `json:"signed_tx"`
	TxHash   string `json:"tx_hash"`
	PubKey   string `json:"pub_key"`
}

// GetAddress returns the deposit address for the given chain.
func (c *Client) GetAddress(ctx context.Context, chain string) (string, error) {
	resp, err := c.doRequest(ctx, "GET", "/address/"+chain, nil, nil)
	if err != nil {
		return "", err
	}

	var addr AddressResponse
	if err := json.Unmarshal(resp, &addr); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return addr.Address, nil
}

// Sign signs a transaction for the given chain.
func (c *Client) Sign(ctx context.Context, req SignRequest) (*SignResponse, error) {
	// Build tx_data from chain-specific data
	// For BTC: {inputs: [...], outputs: [...]}
	// For ETH: {to, value, gas, gasPrice, nonce, chainId}
	// For future chains.], mixin, fee}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(ctx, "POST", "/sign", body, &req)
	if err != nil {
		return nil, err
	}

	var sr SignResponse
	if err := json.Unmarshal(resp, &sr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &sr, nil
}

// Health checks the signer service.
func (c *Client) Health(ctx context.Context) error {
	_, err := c.doRequest(ctx, "GET", "/health", nil, nil)
	return err
}

// doRequest makes an authenticated request to the signer.
func (c *Client) doRequest(ctx context.Context, method, path string, body []byte, signReq *SignRequest) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.URL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Add HMAC signature
	ts := time.Now().UTC().Format(time.RFC3339)
	sig := c.sign(ts, body)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", sig)

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("signer HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return io.ReadAll(resp.Body)
}

// sign computes HMAC-SHA256 of timestamp + body.
func (c *Client) sign(ts string, body []byte) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(ts))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
