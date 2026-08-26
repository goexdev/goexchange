// Package vault provides HashiCorp Vault integration for goexchange.
//
// All sensitive secrets (DB passwords, JWT signing keys, API keys, hot wallet
// private keys) should be stored in Vault and loaded at startup.
//
// Production deployment:
//   - Run Vault in HA mode (3+ servers, Raft backend)
//   - Use AppRole / K8s auth (NOT static tokens in production)
//   - Auto-unseal via AWS KMS / GCP CKMS / Azure Key Vault
//   - Enable audit backend (file/syslog/grpc)
//
// For dev: vault server -dev -dev-root-token-id=dev-root-token
package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client is a Vault KV v2 client.
// Uses an AuthMethod to obtain tokens (static token, AppRole, Kubernetes).
type Client struct {
	address string
	auth    AuthMethod
	namespace string // optional enterprise namespace
	client    *http.Client

	// In-memory cache: secret path -> cached value
	mu       sync.RWMutex
	cache    map[string]cachedSecret
	cacheTTL time.Duration
}

type cachedSecret struct {
	data     map[string]string
	loadedAt time.Time
}

// NewClient creates a new Vault client with the given auth method.
func NewClient(address string, auth AuthMethod) (*Client, error) {
	if address == "" {
		return nil, fmt.Errorf("vault address required")
	}
	if auth == nil {
		return nil, fmt.Errorf("auth method required")
	}
	return &Client{
		auth:     auth,
		address:  strings.TrimRight(address, "/"),
		client:   &http.Client{Timeout: 10 * time.Second},
		cache:    make(map[string]cachedSecret),
		cacheTTL: 5 * time.Minute, // default
	}, nil
}

// SetCacheTTL overrides the default cache TTL.
func (c *Client) SetCacheTTL(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cacheTTL = ttl
}

// Health checks if Vault is reachable.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET",
		c.address+"/v1/sys/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("vault unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault unhealthy: %s - body %s", resp.Status, string(body))
	}
	return nil
}

// GetSecret fetches a secret from Vault KV v2.
// Returns map of key=value pairs in the secret.
//
// Example: vault kv put secret/db password=secret123
//          client.GetSecret(ctx, "db") -> {"password": "secret123"}
func (c *Client) GetSecret(ctx context.Context, secretPath string) (map[string]string, error) {
	secretPath = strings.TrimPrefix(secretPath, "secret/")

	// Check cache first
	c.mu.RLock()
	cached, ok := c.cache[secretPath]
	c.mu.RUnlock()
	if ok && time.Since(cached.loadedAt) < c.cacheTTL {
		return cached.data, nil
	}

	// Fetch from Vault
	token, err := c.auth.GetToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("get auth token: %w", err)
	}
	url := fmt.Sprintf("%s/v1/secret/data/%s", c.address, secretPath)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("secret not found: %s", secretPath)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("vault returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if result.Data.Data == nil {
		return nil, fmt.Errorf("secret %s has no data", secretPath)
	}

	// Cache
	c.mu.Lock()
	c.cache[secretPath] = cachedSecret{
		data:     result.Data.Data,
		loadedAt: time.Now(),
	}
	c.mu.Unlock()

	return result.Data.Data, nil
}

// GetValue fetches a single value from a secret.
// Convenience wrapper around GetSecret.
func (c *Client) GetValue(ctx context.Context, secretPath, key string) (string, error) {
	data, err := c.GetSecret(ctx, secretPath)
	if err != nil {
		return "", err
	}
	v, ok := data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %q", key, secretPath)
	}
	return v, nil
}

// PutSecret writes a secret to Vault KV v2.
// Used by admin tools / migrations / setup scripts.
func (c *Client) PutSecret(ctx context.Context, secretPath string, data map[string]string) error {
	secretPath = strings.TrimPrefix(secretPath, "secret/")
	url := fmt.Sprintf("%s/v1/secret/data/%s", c.address, secretPath)

	payload, err := json.Marshal(map[string]interface{}{
		"data": data,
	})
	if err != nil {
		return err
	}
	token, err := c.auth.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("get auth token: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("vault write failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault write returned %d: %s", resp.StatusCode, string(body))
	}

	// Invalidate cache
	c.mu.Lock()
	delete(c.cache, secretPath)
	c.mu.Unlock()

	return nil
}

// InvalidateCache forces reload of a secret.
func (c *Client) InvalidateCache(secretPath string) {
	secretPath = strings.TrimPrefix(secretPath, "secret/")
	c.mu.Lock()
	delete(c.cache, secretPath)
	c.mu.Unlock()
}

// InvalidateAllCache clears all cached secrets (for key rotation).
func (c *Client) InvalidateAllCache() {
	c.mu.Lock()
	c.cache = make(map[string]cachedSecret)
	c.mu.Unlock()
}
