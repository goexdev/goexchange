// Package nodeconn provides secure connections to remote blockchain nodes.
//
// In production, each chain (BTC, ETH, etc.) runs on its own VPS
// with the node-proxy service. This package manages the connection from
// the exchange's chainwatcher to those node-proxy servers.
package nodeconn

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
	"sync"
	"sync/atomic"
	"time"
)

// NodeConfig configures a single remote node connection.
type NodeConfig struct {
	Name        string        // e.g. "btc-mainnet"
	Type        string        // "bitcoin" | "ethereum" | etc
	URL         string        // e.g. "https://btc-node-1.internal:8443"
	CertFile    string        // Client cert for mTLS
	KeyFile     string        // Client key for mTLS
	CACertFile  string        // CA cert for server verification
	APISecret   string        // Shared secret for HMAC signing
	Timeout     time.Duration // Request timeout
	MaxRetries  int           // Max retries before failover
	Failover    string        // Backup URL
	HealthCheck string        // Method to call for health
}

// Node represents a single remote node connection.
type Node struct {
	cfg     NodeConfig
	hc      *http.Client
	healthy atomic.Bool
	mu      sync.Mutex
	stats   NodeStats
}

// NodeStats tracks call statistics.
type NodeStats struct {
	TotalCalls  uint64
	FailedCalls uint64
	LastError   string
	LastSuccess time.Time
}

// Manager manages multiple node connections.
type Manager struct {
	mu    sync.RWMutex
	nodes map[string]*Node
}

// NewManager creates a new node connection manager.
func NewManager() *Manager {
	return &Manager{nodes: make(map[string]*Node)}
}

// Register adds a node configuration.
func (m *Manager) Register(cfg NodeConfig) error {
	node, err := newNode(cfg)
	if err != nil {
		return fmt.Errorf("create node %s: %w", cfg.Name, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[cfg.Name] = node
	return nil
}

// Get returns a node by name.
func (m *Manager) Get(name string) (*Node, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.nodes[name]
	return n, ok
}

// Call invokes an RPC method on the named node with automatic failover.
func (m *Manager) Call(ctx context.Context, nodeName, method string, params interface{}) (json.RawMessage, error) {
	node, ok := m.Get(nodeName)
	if !ok {
		return nil, fmt.Errorf("node %s not registered", nodeName)
	}
	return node.Call(ctx, method, params)
}

// HealthCheck pings all nodes.
func (m *Manager) HealthCheck(ctx context.Context) map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]bool)
	for name, node := range m.nodes {
		results[name] = node.HealthCheck(ctx)
	}
	return results
}

// newNode creates a Node from config.
func newNode(cfg NodeConfig) (*Node, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 2
	}
	if cfg.HealthCheck == "" {
		cfg.HealthCheck = "get_info"
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

	hc := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			TLSClientConfig:       tlsConfig,
			MaxIdleConns:          10,
			MaxIdleConnsPerHost:   5,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: cfg.Timeout,
		},
	}

	return &Node{cfg: cfg, hc: hc}, nil
}

// Call invokes an RPC method on the node.
func (n *Node) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Build request body
	body, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "0",
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}

	// Sign with HMAC
	ts := time.Now().UTC().Format(time.RFC3339)
	sig := signHMAC(n.cfg.APISecret, ts, body)

	// Try primary, then failover
	urls := []string{n.cfg.URL}
	if n.cfg.Failover != "" {
		urls = append(urls, n.cfg.Failover)
	}

	var lastErr error
	for _, url := range urls {
		atomic.AddUint64(&n.stats.TotalCalls, 1)
		result, err := n.callURL(ctx, url, body, ts, sig)
		if err == nil {
			n.healthy.Store(true)
			n.stats.LastSuccess = time.Now()
			return result, nil
		}
		lastErr = err
		atomic.AddUint64(&n.stats.FailedCalls, 1)
		n.stats.LastError = err.Error()
	}

	n.healthy.Store(false)
	return nil, fmt.Errorf("all nodes failed for %s: %w", method, lastErr)
}

func (n *Node) callURL(ctx context.Context, url string, body []byte, ts, sig string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", sig)

	resp, err := n.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

// HealthCheck pings the node.
func (n *Node) HealthCheck(ctx context.Context) bool {
	_, err := n.Call(ctx, n.cfg.HealthCheck, nil)
	if err == nil {
		n.healthy.Store(true)
	} else {
		n.healthy.Store(false)
	}
	return err == nil
}

// IsHealthy returns the cached health state.
func (n *Node) IsHealthy() bool {
	return n.healthy.Load()
}

// Stats returns call statistics.
func (n *Node) Stats() NodeStats {
	return n.stats
}

// signHMAC computes HMAC-SHA256 of timestamp + body.
func signHMAC(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
