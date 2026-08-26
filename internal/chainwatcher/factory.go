package chainwatcher

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"sync"

	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/signing"
)

// DriverConstructor creates a Driver from ChainConfig.
// Different chain kinds use different constructors.
// Registered globally in init() or in main.go.
type DriverConstructor func(ctx context.Context, cfg config.ChainConfig, deps DriverDeps) (Driver, error)

// DriverDeps provides dependencies drivers need at construction time.
type DriverDeps struct {
	VaultClient  VaultKeyReader
	Logger       *slog.Logger
	SignerConfig SignerConfig
}

// VaultKeyReader is the subset of vault.Client we need for driver signing.
type VaultKeyReader interface {
	GetValue(ctx context.Context, path, key string) (string, error)
}

// SignerConfig is passed to driver constructors so they can build signers.
type SignerConfig struct {
	VaultAddress string
	VaultToken   string
}

// driverFactory is a global registry mapping driver type name to constructor.
type driverFactory struct {
	mu           sync.RWMutex
	constructors map[string]DriverConstructor
}

var factory = &driverFactory{
	constructors: make(map[string]DriverConstructor),
}

// RegisterDriver adds a driver constructor to the global factory.
func RegisterDriver(name string, ctor DriverConstructor) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	factory.constructors[name] = ctor
}

// BuildDriver constructs a driver for the given config using the registered factory.
func BuildDriver(ctx context.Context, cfg config.ChainConfig, deps DriverDeps) (Driver, error) {
	factory.mu.RLock()
	ctor, ok := factory.constructors[cfg.Driver]
	factory.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown driver type: %s (registered: %v)", cfg.Driver, factory.DriverTypes())
	}
	return ctor(ctx, cfg, deps)
}

// DriverTypes returns all registered driver type names.
func (f *driverFactory) DriverTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	types := make([]string, 0, len(f.constructors))
	for k := range f.constructors {
		types = append(types, k)
	}
	return types
}

// Register built-in drivers. Called automatically on package import.
func init() {
	RegisterDriver("btc", buildBTCDriver)
	RegisterDriver("evm", buildEVMDriver)
	RegisterDriver("solana", buildSolanaDriver)
	RegisterDriver("mock", buildMockDriver)
}

func buildSolanaDriver(ctx context.Context, cfg config.ChainConfig, deps DriverDeps) (Driver, error) {
	return NewSolanaDriver(SolanaConfig{
		RPCURL:     cfg.RPCURL,
		Commitment: "confirmed",
		Asset:      cfg.Asset,
		HotAddr:    cfg.HotWallet,
		ChainID:    cfg.ID,
	}, deps.Logger)
}

func buildBTCDriver(ctx context.Context, cfg config.ChainConfig, deps DriverDeps) (Driver, error) {
	return NewBTCDriver(cfg.RPCURL, cfg.RPCUser, cfg.RPCPass, cfg.ID, cfg.HotWallet)
}

func buildEVMDriver(ctx context.Context, cfg config.ChainConfig, deps DriverDeps) (Driver, error) {
	var signer TxSigner
	var privKey *ecdsa.PrivateKey
	hotWallet := cfg.HotWallet

	// HD wallet path: derive key from mnemonic
	if cfg.Derivation != nil && deps.VaultClient != nil {
		derivation, derivedAddr, err := deriveHDSigner(ctx, cfg, deps)
		if err != nil {
			return nil, fmt.Errorf("HD derivation failed: %w", err)
		}
		privKey = derivation
		hotWallet = derivedAddr
		// Build a LocalSigner from the derived key for the high-level interface
		privKeyHex := fmt.Sprintf("%x", privKey.D.Bytes())
		if ls, lsErr := buildLocalSigner(cfg, privKeyHex, derivedAddr); lsErr == nil {
			signer = ls
		}
	} else if cfg.Signer == "vault" && deps.VaultClient != nil {
		// Standard path: read key from Vault
		privKeyHex, err := deps.VaultClient.GetValue(ctx, cfg.VaultSecretPath, "private_key")
		if err == nil {
			if deps.VaultClient == nil {
			return nil, fmt.Errorf("vault disabled and no address fallback configured")
		}
		address, _ := deps.VaultClient.GetValue(ctx, cfg.VaultSecretPath, "address")
			if ls, lsErr := buildLocalSigner(cfg, privKeyHex, address); lsErr == nil {
				signer = ls
				// Extract the underlying private key for go-ethereum signing
				if lsa, ok := ls.(interface{ UnderlyingKey() *ecdsa.PrivateKey }); ok {
					privKey = lsa.UnderlyingKey()
				}
			}
		}
	}

	drv, err := NewEVMDriver(cfg.ID, cfg.RPCURL, cfg.Asset, cfg.ChainID, hotWallet, signer)
	if err != nil {
		return nil, err
	}
	// Inject privKey for proper go-ethereum signing
	if privKey != nil {
		drv.privKey = privKey
	}
	return drv, nil
}

// deriveHDSigner reads the BIP-39 mnemonic from Vault, derives the child key
// at the chain's configured path, and returns the *ecdsa.PrivateKey and address.
//
// This allows a single mnemonic to control hot wallets for multiple chains
// (each with a different BIP-44 derivation path).
func deriveHDSigner(ctx context.Context, cfg config.ChainConfig, deps DriverDeps) (*ecdsa.PrivateKey, string, error) {
	if cfg.Derivation == nil {
		return nil, "", fmt.Errorf("no derivation config")
	}
	mnemonicPath := cfg.Derivation.MnemonicSecret
	derivationPath := cfg.Derivation.Path

	// Read mnemonic from Vault (or config fallback if vault disabled)
	if deps.VaultClient == nil {
		return nil, "", fmt.Errorf("vault disabled and no mnemonic fallback configured")
	}
	mnemonic, err := deps.VaultClient.GetValue(ctx, mnemonicPath, "mnemonic")
	if err != nil {
		return nil, "", fmt.Errorf("read mnemonic from %s: %w", mnemonicPath, err)
	}

	// Create HD signer
	hd, err := signing.NewHDSignerFromMnemonic(mnemonic)
	if err != nil {
		return nil, "", fmt.Errorf("create HD signer: %w", err)
	}

	// Derive child key
	child, err := hd.Derive(derivationPath)
	if err != nil {
		return nil, "", fmt.Errorf("derive path %s: %w", derivationPath, err)
	}

	// Get private key
	privKey, err := child.PrivateKey()
	if err != nil {
		return nil, "", fmt.Errorf("get private key: %w", err)
	}

	// Get derived address
	addr, err := child.PublicAddress()
	if err != nil {
		return nil, "", fmt.Errorf("get address: %w", err)
	}

	return privKey, addr, nil
}

func buildMockDriver(ctx context.Context, cfg config.ChainConfig, deps DriverDeps) (Driver, error) {
	return &stubDriver{name: cfg.ID, asset: cfg.Asset, hotAddr: cfg.HotWallet}, nil
}

func buildLocalSigner(cfg config.ChainConfig, privKey, expectedAddr string) (TxSigner, error) {
	// LocalSigner factory: main.go can override via RegisterSignerBuilder
	signer, err := signerBuilder(cfg, privKey, expectedAddr)
	return signer, err
}

// SignerBuilder creates a TxSigner for a chain.
type SignerBuilder func(cfg config.ChainConfig, privKey, expectedAddr string) (TxSigner, error)

var signerBuilder SignerBuilder

// RegisterSignerBuilder sets the global signer factory.
// Called from main.go to wire LocalSigner/VaultSigner construction.
func RegisterSignerBuilder(b SignerBuilder) {
	signerBuilder = b
}