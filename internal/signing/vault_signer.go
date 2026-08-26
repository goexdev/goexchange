package signing

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// VaultConfig configures Vault access.
type VaultConfig struct {
	// Address is the Vault server URL (e.g. "https://vault.example.com:8200")
	Address string
	// Token is the Vault auth token (DEV only - use AppRole/K8s in production)
	Token string
	// SecretPath is the KV v2 path to the hot wallet key (e.g. "secret/eth/hot-wallet")
	SecretPath string
	// Chain identifies which blockchain
	Chain Chain
	// CacheTTL is how long to cache the private key in memory (default 5m)
	CacheTTL time.Duration
}

// VaultSigner fetches private keys from HashiCorp Vault and signs transactions.
//
// Production deployment:
//   - Run Vault in HA mode (3+ servers)
//   - Use AppRole or K8s auth (NOT static tokens)
//   - Audit backend enabled (logs every read)
//   - Auto-unseal via AWS KMS / GCP CKMS / Azure Key Vault
//
// For local dev: vault server -dev -dev-root-token-id=dev-root-token
type VaultSigner struct {
	cfg     VaultConfig
	client  *http.Client
	mu      sync.Mutex
	cached  *cachedKey
}

// cachedKey is an in-memory cache of the decrypted key.
type cachedKey struct {
	privateKey []byte
	address    string
	chainID    int64
	loadedAt   time.Time
}

// NewVaultSigner creates a Vault-backed signer.
func NewVaultSigner(cfg VaultConfig) (*VaultSigner, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("vault address required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("vault token required (use AppRole/K8s in production)")
	}
	if cfg.SecretPath == "" {
		return nil, fmt.Errorf("secret path required")
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	return &VaultSigner{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (s *VaultSigner) Name() string { return "vault" }
func (s *VaultSigner) Chain() Chain { return s.cfg.Chain }

// Address returns the hot wallet address. Fetches from Vault if not cached.
func (s *VaultSigner) Address() string {
	key, err := s.loadKey()
	if err != nil {
		return ""
	}
	return key.address
}

// SignHash signs a 32-byte hash using the Vault-stored private key.
// For transaction signing, you should use SignTransaction with proper EIP-1559/Legacy support.
func (s *VaultSigner) SignHash(hash []byte) ([]byte, error) {
	key, err := s.loadKey()
	if err != nil {
		return nil, fmt.Errorf("load key: %w", err)
	}

	// Use go-ethereum's SignHash
	privKey, err := crypto.ToECDSA(key.privateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}
	return crypto.Sign(hash, privKey)
}

// SignTransaction signs a transaction. Note: full tx construction needs
// EIP-1559/EIP-2930 support — for now this signs the hash of tx.Data.
func (s *VaultSigner) SignTransaction(ctx context.Context, tx UnsignedTx) (SignedTx, error) {
	if len(tx.Data) == 0 {
		return SignedTx{}, &ValidationError{Reason: "empty tx data"}
	}
	key, err := s.loadKey()
	if err != nil {
		return SignedTx{}, fmt.Errorf("load key: %w", err)
	}
	hash := crypto.Keccak256(tx.Data)
	sig, err := s.SignHash(hash)
	if err != nil {
		return SignedTx{}, err
	}
	sig[64] = sig[64] + byte(key.chainID*2+35)
	return SignedTx{
		Raw:       sig,
		TxHash:    crypto.Keccak256Hash(sig).Hex(),
		Signature: hex.EncodeToString(sig),
	}, nil
}

// loadKey fetches the private key from Vault. Caches for CacheTTL.
func (s *VaultSigner) loadKey() (*cachedKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil && time.Since(s.cached.loadedAt) < s.cfg.CacheTTL {
		return s.cached, nil
	}

	key, err := s.fetchFromVault()
	if err != nil {
		return nil, err
	}
	s.cached = key
	return key, nil
}

// fetchFromVault calls Vault KV v2 API to retrieve the secret.
func (s *VaultSigner) fetchFromVault() (*cachedKey, error) {
	url := fmt.Sprintf("%s/v1/%s/data/%s",
		strings.TrimRight(s.cfg.Address, "/"),
		"secret",
		strings.TrimPrefix(s.cfg.SecretPath, "secret/"))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", s.cfg.Token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vault returned %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	privHex, _ := result.Data.Data["private_key"]
	addr, _ := result.Data.Data["address"]
	chainIDStr, _ := result.Data.Data["chain_id"]

	if privHex == "" {
		return nil, fmt.Errorf("private_key not found in vault secret")
	}

	// Strip 0x prefix
	if strings.HasPrefix(privHex, "0x") {
		privHex = privHex[2:]
	}
	privBytes, err := hex.DecodeString(privHex)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(privBytes) != 32 {
		return nil, fmt.Errorf("private key must be 32 bytes, got %d", len(privBytes))
	}

	chainID := int64(0)
	if chainIDStr != "" {
		v, _ := strconv.ParseInt(chainIDStr, 10, 64); chainID = v

	}

	return &cachedKey{
		privateKey: privBytes,
		address:    addr,
		chainID:    chainID,
		loadedAt:   time.Now(),
	}, nil
}

// InvalidateCache forces reload on next access (useful after key rotation).
func (s *VaultSigner) InvalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = nil
}
