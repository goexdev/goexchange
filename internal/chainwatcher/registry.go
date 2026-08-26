package chainwatcher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/goexdev/goexchange/internal/config"
)

// ChainRegistry holds per-chain drivers with thread-safe add/remove/update.
// Supports hot-reload: config changes can add/remove/replace drivers at runtime.
type ChainRegistry struct {
	mu      sync.RWMutex
	drivers map[string]Driver // chain_id (e.g. "btc", "bsc") -> Driver
	configs map[string]config.ChainConfig
	deps    DriverDeps
	ctx     context.Context
	cancel  context.CancelFunc
	log     *slog.Logger
}

func NewChainRegistry(deps DriverDeps, log *slog.Logger) *ChainRegistry {
	ctx, cancel := context.WithCancel(context.Background())
	return &ChainRegistry{
		drivers: make(map[string]Driver),
		configs: make(map[string]config.ChainConfig),
		deps:    deps,
		ctx:     ctx,
		cancel:  cancel,
		log:     log,
	}
}

// Get returns the driver for a chain_id.
func (r *ChainRegistry) Get(chainID string) (Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.drivers[chainID]
	return d, ok
}

// GetForAsset returns the driver for an asset by looking up which chain handles it.
func (r *ChainRegistry) GetForAsset(asset string) (Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for chainID, cfg := range r.configs {
		if cfg.Asset == asset {
			d, ok := r.drivers[chainID]
			return d, ok
		}
		for _, tok := range cfg.Tokens {
			if tok.Symbol == asset {
				d, ok := r.drivers[chainID]
				return d, ok
			}
		}
	}
	return nil, false
}

// GetTokenForAsset returns the token config for an ERC20/BEP-20 token.
// Returns nil if the asset is not a token (e.g. native BNB/ETH).
func (r *ChainRegistry) GetTokenForAsset(asset string) *config.TokenConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, cfg := range r.configs {
		for i := range cfg.Tokens {
			if cfg.Tokens[i].Symbol == asset {
				return &cfg.Tokens[i]
			}
		}
	}
	return nil
}

// ChainIDForAsset returns the chain_id that handles the given asset.
func (r *ChainRegistry) ChainIDForAsset(asset string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for chainID, cfg := range r.configs {
		if cfg.Asset == asset {
			return chainID
		}
		for _, tok := range cfg.Tokens {
			if tok.Symbol == asset {
				return chainID
			}
		}
	}
	return ""
}

// List returns all active chain IDs.
func (r *ChainRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.drivers))
	for id := range r.drivers {
		ids = append(ids, id)
	}
	return ids
}

// Configs returns a copy of all chain configs.
func (r *ChainRegistry) Configs() map[string]config.ChainConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]config.ChainConfig, len(r.configs))
	for k, v := range r.configs {
		out[k] = v
	}
	return out
}

// SyncFromConfig reconciles registry state with a new config map.
func (r *ChainRegistry) SyncFromConfig(ctx context.Context, newConfigs map[string]config.ChainConfig) ([]string, error) {
	for id, cfg := range newConfigs {
		cfg.ID = id
		newConfigs[id] = cfg
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var changes []string

	// Remove chains that are no longer in config or disabled
	for chainID := range r.drivers {
		newCfg, exists := newConfigs[chainID]
		if !exists {
			changes = append(changes, fmt.Sprintf("removed chain %s (not in config)", chainID))
			delete(r.drivers, chainID)
			delete(r.configs, chainID)
			continue
		}
		if !newCfg.Enabled {
			changes = append(changes, fmt.Sprintf("disabled chain %s (enabled=false)", chainID))
			delete(r.drivers, chainID)
			r.configs[chainID] = newCfg
			continue
		}
	}

	// Add new or update changed chains
	for chainID, newCfg := range newConfigs {
		if !newCfg.Enabled {
			continue
		}
		oldCfg, existed := r.configs[chainID]
		if !existed {
			drv, err := BuildDriver(ctx, newCfg, r.deps)
			if err != nil {
				r.log.Error("failed to build driver", "chain", chainID, "error", err)
				changes = append(changes, fmt.Sprintf("FAILED build %s: %v", chainID, err))
				continue
			}
			r.drivers[chainID] = drv
			r.configs[chainID] = newCfg
			changes = append(changes, fmt.Sprintf("added chain %s (driver=%s)", chainID, newCfg.Driver))
			continue
		}
		if configChanged(oldCfg, newCfg) {
			drv, err := BuildDriver(ctx, newCfg, r.deps)
			if err != nil {
				r.log.Error("failed to rebuild driver", "chain", chainID, "error", err)
				changes = append(changes, fmt.Sprintf("FAILED rebuild %s: %v", chainID, err))
				continue
			}
			r.drivers[chainID] = drv
			r.configs[chainID] = newCfg
			changes = append(changes, fmt.Sprintf("updated chain %s", chainID))
		}
	}

	return changes, nil
}

// SetEnabled toggles a single chain at runtime (admin API).
func (r *ChainRegistry) SetEnabled(chainID string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg, exists := r.configs[chainID]
	if !exists {
		return fmt.Errorf("chain %s not found", chainID)
	}
	if cfg.Enabled == enabled {
		return nil
	}
	cfg.Enabled = enabled
	r.configs[chainID] = cfg

	if enabled {
		drv, err := BuildDriver(r.ctx, cfg, r.deps)
		if err != nil {
			return err
		}
		r.drivers[chainID] = drv
	} else {
		delete(r.drivers, chainID)
	}
	return nil
}

// AddChain adds a new chain at runtime.
func (r *ChainRegistry) AddChain(cfg config.ChainConfig) error {
	if cfg.ID == "" {
		return fmt.Errorf("chain id required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.drivers[cfg.ID]; exists {
		return fmt.Errorf("chain %s already exists", cfg.ID)
	}
	cfg.Enabled = true
	drv, err := BuildDriver(r.ctx, cfg, r.deps)
	if err != nil {
		return err
	}
	r.drivers[cfg.ID] = drv
	r.configs[cfg.ID] = cfg
	return nil
}

// RemoveChain removes a chain at runtime.
func (r *ChainRegistry) RemoveChain(chainID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.drivers[chainID]; !exists {
		return fmt.Errorf("chain %s not found", chainID)
	}
	delete(r.drivers, chainID)
	delete(r.configs, chainID)
	return nil
}

// Close stops the registry.
func (r *ChainRegistry) Close() {
	r.cancel()
}

func configChanged(old, new config.ChainConfig) bool {
	return old.Driver != new.Driver ||
		old.RPCURL != new.RPCURL ||
		old.RPCUser != new.RPCUser ||
		old.RPCPass != new.RPCPass ||
		old.Asset != new.Asset ||
		old.MinConf != new.MinConf ||
		old.ChainID != new.ChainID ||
		old.HotWallet != new.HotWallet ||
		old.Signer != new.Signer ||
		old.VaultSecretPath != new.VaultSecretPath
}