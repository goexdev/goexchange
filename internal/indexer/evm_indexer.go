package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/goexdev/goexchange/internal/chainwatcher"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EVMIndexer polls EVM chains for ERC20 Transfer events to the hot wallet.
// On detection, credits the user balance and records the transfer.
type EVMIndexer struct {
	pool    *pgxpool.Pool
	log     *slog.Logger
	mu      sync.Mutex
	drivers map[string]chainwatcher.Driver // chain_id -> driver
	cfg     map[string]chainConfig            // chain_id -> config
}

type chainConfig struct {
	HotWallet string
	RPCURL    string
	ChainID   int64
}

// NewEVMIndexer creates an EVM indexer service.
func NewEVMIndexer(pool *pgxpool.Pool, log *slog.Logger) *EVMIndexer {
	return &EVMIndexer{
		pool:    pool,
		log:     log,
		drivers: make(map[string]chainwatcher.Driver),
		cfg:     make(map[string]chainConfig),
	}
}

// AddChain registers a chain to be indexed.
func (i *EVMIndexer) AddChain(chainID string, drv chainwatcher.Driver, hotWallet, rpcURL string, evmChainID int64) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.drivers[chainID] = drv
	i.cfg[chainID] = chainConfig{
		HotWallet: hotWallet,
		RPCURL:    rpcURL,
		ChainID:   evmChainID,
	}
}

// Run starts the indexer loop. Polls each chain every interval.
// Cancellable via ctx.
func (i *EVMIndexer) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	// Initial scan immediately
	i.scanAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			i.scanAll(ctx)
		}
	}
}

// scanAll runs a scan for every registered chain.
func (i *EVMIndexer) scanAll(ctx context.Context) {
	i.mu.Lock()
	chains := make([]string, 0, len(i.cfg))
	for c := range i.cfg {
		chains = append(chains, c)
	}
	i.mu.Unlock()

	for _, chainID := range chains {
		if err := i.scanChain(ctx, chainID); err != nil {
			i.log.Warn("chain indexer error", "chain", chainID, "error", err)
		}
	}
}

// scanChain scans one chain for new Transfer events.
func (i *EVMIndexer) scanChain(ctx context.Context, chainID string) error {
	i.mu.Lock()
	drv, ok := i.drivers[chainID]
	cfg := i.cfg[chainID]
	i.mu.Unlock()
	if !ok {
		return fmt.Errorf("chain %s not registered", chainID)
	}

	evmDrv, ok := drv.(*chainwatcher.EVMDriver)
	if !ok {
		i.log.Debug("chain is not EVM, skipping indexer", "chain", chainID)
		return nil
	}

	lastBlock, err := i.getLastBlock(ctx, chainID)
	if err != nil {
		return fmt.Errorf("get last block: %w", err)
	}

	currentBlock, err := i.getCurrentBlock(ctx, evmDrv)
	if err != nil {
		return fmt.Errorf("get current block: %w", err)
	}
	if currentBlock <= lastBlock {
		return nil
	}

	// For first run (lastBlock == 0), start from a recent block to avoid
	// "history pruned" errors on public RPCs
	startBlock := lastBlock + 1
	if lastBlock == 0 {
		// Start 1000 blocks behind current to handle pruned history
		if currentBlock > 1000 {
			startBlock = currentBlock - 1000
		}
	}
	const maxBatch = 2000
	toBlock := startBlock + maxBatch
	if toBlock > currentBlock {
		toBlock = currentBlock
	}

	transfers, err := evmDrv.ScanLogs(ctx, startBlock, toBlock, cfg.HotWallet)
	if err != nil {
		return fmt.Errorf("scan logs: %w", err)
	}

	if len(transfers) > 0 {
		i.log.Info("found transfers", "chain", chainID, "from", lastBlock+1, "to", toBlock, "count", len(transfers))
	}

	for _, t := range transfers {
		asset := i.assetForToken(chainID, t.Token)
		if asset == "" {
			i.log.Debug("unknown token, skipping", "chain", chainID, "token", t.Token.Hex())
			continue
		}
		_, err := i.pool.Exec(ctx, `
			INSERT INTO evm_indexed_transfers
			(chain_id, tx_hash, log_index, block_number, token_address, from_address, to_address, amount, asset)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (chain_id, tx_hash, log_index) DO NOTHING
		`, chainID, t.TxHash, t.LogIndex, t.BlockNumber,
			t.Token.Hex(), t.From.Hex(), t.To.Hex(),
			t.Amount.String(), asset)
		if err != nil {
			i.log.Warn("insert transfer failed", "err", err)
		}
	}

	_, err = i.pool.Exec(ctx, `
		INSERT INTO evm_indexer_state (chain_id, last_block, hot_wallet, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (chain_id) DO UPDATE SET last_block = $2, updated_at = NOW()
	`, chainID, toBlock, cfg.HotWallet)
	if err != nil {
		return fmt.Errorf("update last block: %w", err)
	}

	return nil
}

func (i *EVMIndexer) getLastBlock(ctx context.Context, chainID string) (uint64, error) {
	var lastBlock uint64
	err := i.pool.QueryRow(ctx, `SELECT last_block FROM evm_indexer_state WHERE chain_id = $1`, chainID).Scan(&lastBlock)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return lastBlock, nil
}

func (i *EVMIndexer) getCurrentBlock(ctx context.Context, drv *chainwatcher.EVMDriver) (uint64, error) {
	height, err := drv.GetBlockCount(ctx)
	if err != nil {
		return 0, err
	}
	return uint64(height), nil
}

// assetForToken maps a token contract address to an asset symbol.
func (i *EVMIndexer) assetForToken(chainID string, tokenAddr common.Address) string {
	if tokenAddr.Hex() == "0x0000000000000000000000000000000000000000" {
		switch chainID {
		case "eth", "arbitrum", "optimism", "base":
			return "ETH"
		case "bsc":
			return "BNB"
		case "polygon":
			return "MATIC"
		}
		return ""
	}
	tokenMap := map[string]map[string]string{
		"eth": {
			"0xdac17f958d2ee523a2206206994597c13d831ec7": "USDT",
			"0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48": "USDC",
			"0x6b175474e89094c44da98b954eedeac495271d0f": "DAI",
		},
		"bsc": {
			"0x337610d27c682e347c9cdc60d17240a4dae5bbda": "USDT",
			"0x64544969ed7eb3032d3bef8b4dbd6e7dd2c6f6e5": "USDC",
			"0xed24fc36d5ee211c25de6bfc1b6db6ff1c6d63d8": "BUSD",
			"0x3db4cb3a991c854d457f66a745949fd43da55d6b": "DAI",
		},
		"polygon": {
			"0x3c499c542cef5e3811e1192ce70d8cc03d5c3359": "USDC",
			"0xc2132d05d31c914a87c6611c10748aeb04b58e8f": "USDT",
			"0x8f3cf7ad23cd3cadbd9735aff958023239c6a063": "DAI",
		},
		"arbitrum": {
			"0xaf88d065e77c8cc2239327c5edb3a432268e5831": "USDC",
			"0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9": "USDT",
			"0xda10009cbd5d07dd0cecc66161fc93d7c9000da1": "DAI",
		},
		"optimism": {
			"0x0b2c639c533813f4aa9d7837caf62653d097ff85": "USDC",
			"0x94b008aa51179c9b73a1f4d8f73b95a4c8fd7b97": "USDT",
			"0xda10009cbd5d07dd0cecc66161fc93d7c9000da1": "DAI",
		},
		"base": {
			"0x833589fcd6edb6e08f4c7c32d4f71b54bda02913": "USDC",
			"0x50c5725949a6f0c72e6c4a641f24049f91723e658": "DAI",
		},
	}
	if m, ok := tokenMap[chainID]; ok {
		addr := strings.ToLower(tokenAddr.Hex())
		if asset, ok := m[addr]; ok {
			return asset
		}
	}
	return ""
}