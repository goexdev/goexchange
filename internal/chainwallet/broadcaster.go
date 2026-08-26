package chainwallet

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

// NodeBroadcaster broadcasts to a node-proxy server.
type NodeBroadcaster struct {
	nodes map[string]*NodeEndpoint // chain name -> endpoint
	hc    *http.Client
	secret []byte
}

// NodeEndpoint configures a single node endpoint.
type NodeEndpoint struct {
	URL       string // e.g. "https://btc-node-1.internal:8443/broadcast"
	CertFile  string
	KeyFile   string
	CACertFile string
}

// BroadcastRequest is the body sent to node-proxy /broadcast.
type BroadcastRequest struct {
	SignedTx string `json:"signed_tx"`
}

// BroadcastResponse is the response from node-proxy.
type BroadcastResponse struct {
	TxHash string `json:"tx_hash"`
	Error  string `json:"error,omitempty"`
}

// NewNodeBroadcaster creates a node broadcaster.
func NewNodeBroadcaster(secret string) (*NodeBroadcaster, error) {
	// Use a default TLS config that allows opting per-endpoint
	return &NodeBroadcaster{
		nodes:  make(map[string]*NodeEndpoint),
		hc:     &http.Client{Timeout: 30 * time.Second},
		secret: []byte(secret),
	}, nil
}

// RegisterNode adds a node endpoint.
func (b *NodeBroadcaster) RegisterNode(chain string, ep NodeEndpoint) {
	b.nodes[chain] = &ep
}

// Broadcast sends a signed tx to the node-proxy.
func (b *NodeBroadcaster) Broadcast(ctx context.Context, chain, signedHex string) (string, error) {
	ep, ok := b.nodes[chain]
	if !ok {
		return "", fmt.Errorf("no node endpoint for chain %s", chain)
	}

	// Build request
	body, _ := json.Marshal(BroadcastRequest{SignedTx: signedHex})
	ts := time.Now().UTC().Format(time.RFC3339)
	sig := b.sign(ts, body)

	// Load certs if specified
	tr := http.DefaultTransport
	if ep.CertFile != "" && ep.KeyFile != "" && ep.CACertFile != "" {
		cert, err := tls.LoadX509KeyPair(ep.CertFile, ep.KeyFile)
		if err != nil {
			return "", fmt.Errorf("load cert: %w", err)
		}
		caData, err := os.ReadFile(ep.CACertFile)
		if err != nil {
			return "", fmt.Errorf("read CA: %w", err)
		}
		caPool := x509.NewCertPool()
		caPool.AppendCertsFromPEM(caData)

		tr = &http.Transport{
			TLSClientConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
				RootCAs:      caPool,
				MinVersion:   tls.VersionTLS13,
			},
		}
	}

	client := &http.Client{Timeout: 30 * time.Second, Transport: tr}

	req, err := http.NewRequestWithContext(ctx, "POST", ep.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", sig)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("broadcast HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var br BroadcastResponse
	if err := json.Unmarshal(respBody, &br); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if br.Error != "" {
		return "", fmt.Errorf("broadcast error: %s", br.Error)
	}
	return br.TxHash, nil
}

func (b *NodeBroadcaster) sign(ts string, body []byte) string {
	mac := hmac.New(sha256.New, b.secret)
	mac.Write([]byte(ts))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
