// Package main is the goexchange API binary.
//
// Runs the HTTP API server only. Talks to:
// - Postgres (direct)
// - Matcher binary (via HTTP for /orders)
// - Scheduler binary (via HTTP for admin endpoints)
package main

import (
	"github.com/goexdev/goexchange/internal/apikeys"
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/goexdev/goexchange/internal/api"
	"github.com/goexdev/goexchange/internal/auth"
	"github.com/goexdev/goexchange/internal/chainwatcher"
	signerclient "github.com/goexdev/goexchange/internal/signer/client"
	"github.com/goexdev/goexchange/internal/indexer"
	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/db"
	"github.com/goexdev/goexchange/internal/logger"
	"github.com/goexdev/goexchange/internal/marketdata"
	"github.com/goexdev/goexchange/internal/audit"
	"github.com/goexdev/goexchange/internal/signing"
	"github.com/goexdev/goexchange/internal/notifier"
	"github.com/goexdev/goexchange/internal/notifier/templates"
	"github.com/goexdev/goexchange/internal/risk"
	"github.com/goexdev/goexchange/internal/matching"
	"github.com/goexdev/goexchange/internal/mmbot"
	"github.com/goexdev/goexchange/internal/trading"
	"github.com/goexdev/goexchange/internal/user"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/goexdev/goexchange/internal/trigger"
	"github.com/goexdev/goexchange/internal/analytics"
	"github.com/goexdev/goexchange/internal/uploads"
	"github.com/goexdev/goexchange/internal/vault"
	"github.com/goexdev/goexchange/internal/vaultinit"
	"github.com/goexdev/goexchange/internal/wallet"
)

//go:embed migrations
var migrationsFS embed.FS


// signingChainFromString maps chain_id to signing.Chain enum.
func signingChainFromString(chainID string) signing.Chain {
	switch chainID {
	case "eth":
		return signing.ChainETH
	case "bsc":
		return signing.ChainBSC
	case "btc":
		return signing.ChainBTC
	default:
		return signing.Chain(chainID)
	}
}

// localSignerAdapter wraps LocalSigner to expose chainwatcher.TxSigner interface.
// We use LocalSigner (not VaultSigner) because VaultSigner just wraps LocalSigner
// internally - using LocalSigner directly avoids double-wrapping.
type localSignerAdapter struct {
	signer *signing.LocalSigner
}

func (a *localSignerAdapter) Name() string                            { return a.signer.Name() }
func (a *localSignerAdapter) Chain() string                           { return string(a.signer.Chain()) }
func (a *localSignerAdapter) Address() string                          { return a.signer.Address() }
func (a *localSignerAdapter) SignTransaction(ctx context.Context, data []byte) ([]byte, string, error) {
	return a.signer.Sign(ctx, data)
}
// buildChainDrivers creates a map of chain_id -> Driver based on config.
// Only enabled chains are included.
// If vaultClient is set, chains with signer=vault get a VaultSigner.
func buildChainDrivers(cfg config.ChainWatcherConfig, vaultCfg config.VaultConfig, vaultClient *vault.Client, log *slog.Logger) map[string]chainwatcher.Driver {
	drivers := make(map[string]chainwatcher.Driver)
	for chainID, chainCfg := range cfg.Chains {
		if !chainCfg.Enabled {
			continue
		}
		// Build signer if configured
		var signer chainwatcher.TxSigner
		switch chainCfg.Signer {
		case "vault":
			if vaultClient == nil {
				log.Warn("chain wants vault signer but vault is not configured", "chain", chainID)
			} else {
				// Read private key from Vault using the client (which has AppRole token)
				privKey, err := vaultClient.GetValue(context.Background(), chainCfg.VaultSecretPath, "private_key")
				if err != nil {
					log.Error("vault signer init failed (cannot read key)", "chain", chainID, "error", err)
				} else {
					address, _ := vaultClient.GetValue(context.Background(), chainCfg.VaultSecretPath, "address")
					ls, err := signing.NewLocalSigner(signingChainFromString(chainID), privKey, address, chainCfg.ChainID)
					if err != nil {
						log.Error("local signer init failed", "chain", chainID, "error", err)
					} else {
						signer = &localSignerAdapter{signer: ls}
						log.Info("vault signer configured (via Vault client)", "chain", chainID, "address", address)
					}
				}
			}
		}

		var drv chainwatcher.Driver
		switch chainCfg.Driver {
		case "btc":
			btcDrv, err := chainwatcher.NewBTCDriver(chainCfg.RPCURL, chainCfg.RPCUser, chainCfg.RPCPass, chainID, chainCfg.HotWallet)
			if err != nil {
				log.Error("btc driver init failed", "chain", chainID, "error", err)
				continue
			}
			drv = btcDrv
		case "evm":
			evmDrv, err := chainwatcher.NewEVMDriver(chainID, chainCfg.RPCURL, chainCfg.Asset, chainCfg.ChainID, chainCfg.HotWallet, signer)
			if err != nil {
				log.Error("evm driver init failed", "chain", chainID, "error", err)
				continue
			}
			drv = evmDrv
		default:
			log.Warn("unknown chain driver", "chain", chainID, "driver", chainCfg.Driver)
		}
		if drv != nil {
			drivers[chainID] = drv
			log.Info("chain driver initialized", "chain", chainID, "driver", chainCfg.Driver, "asset", chainCfg.Asset)
		}
	}
	return drivers
}


// initVault initializes the Vault client and overrides config with secrets.
// Returns nil if disabled or unavailable (with warning).
//
// This is a copy of the same helper in cmd/api/main.go, kept
// here because the scheduler is a separate binary and does not
// link against api. They diverge only in logging context.
func initVault(cfg *config.Config, ctx context.Context, log *slog.Logger) (*vault.Client, error) {
	if !cfg.Vault.Enabled {
		log.Info("vault disabled, using config.yaml for all secrets")
		return nil, nil
	}
	if cfg.Vault.Address == "" {
		return nil, fmt.Errorf("vault.enabled=true but address is empty")
	}
	// Token only required for static auth method
	if cfg.Vault.AuthMethod == "" || cfg.Vault.AuthMethod == "static" {
		if cfg.Vault.Token == "" {
			return nil, fmt.Errorf("vault.auth_method=static but token is empty")
		}
	}

	// Build auth method based on cfg
	var auth vault.AuthMethod
	var err error
	switch cfg.Vault.AuthMethod {
	case "approle":
		auth, err = vault.NewAppRoleAuth(cfg.Vault.Address, cfg.Vault.AppRoleID, cfg.Vault.AppSecretID)
		if err != nil {
			return nil, fmt.Errorf("create approle auth: %w", err)
		}
		log.Info("vault auth: approle", "role_id", cfg.Vault.AppRoleID)
	case "kubernetes":
		auth, err = vault.NewKubernetesAuth(cfg.Vault.Address, cfg.Vault.K8sRole)
		if err != nil {
			return nil, fmt.Errorf("create k8s auth: %w", err)
		}
		log.Info("vault auth: kubernetes", "role", cfg.Vault.K8sRole)
	default:
		auth = vault.NewStaticTokenAuth(cfg.Vault.Token)
		log.Warn("vault auth: static token (DEV ONLY)")
	}
	if auth == nil {
		return nil, fmt.Errorf("no vault auth method configured")
	}
	c, err := vault.NewClient(cfg.Vault.Address, auth)
	if err != nil {
		return nil, fmt.Errorf("create vault client: %w", err)
	}
	if err := c.Health(ctx); err != nil {
		return nil, fmt.Errorf("vault health check failed: %w", err)
	}

	cacheTTL := time.Duration(cfg.Vault.CacheTTLSec) * time.Second
	if cacheTTL > 0 {
		c.SetCacheTTL(cacheTTL)
	}
	log.Info("vault connected", "address", cfg.Vault.Address, "cache_ttl", cacheTTL)

	// Load DB credentials from Vault and override cfg.Database.URL
	if cfg.Vault.DBPath != "" {
		data, err := c.GetSecret(ctx, cfg.Vault.DBPath)
		if err != nil {
			return nil, fmt.Errorf("load db secret: %w", err)
		}
		password := data["password"]
		user := data["user"]
		host := data["host"]
		port := data["port"]
		database := data["database"]
		if password != "" {
			cfg.Database.URL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
				user, password, host, port, database)
			log.Info("db password loaded from vault", "path", cfg.Vault.DBPath)
		}
	}

	// Load JWT secret from Vault and override cfg.JWT.Secret
	if cfg.Vault.JWTPath != "" {
		data, err := c.GetSecret(ctx, cfg.Vault.JWTPath)
		if err != nil {
			return nil, fmt.Errorf("load jwt secret: %w", err)
		}
		secret := data["secret"]
		if secret != "" {
			cfg.JWT.Secret = secret
			log.Info("jwt secret loaded from vault", "path", cfg.Vault.JWTPath)
		}
	}

	// Override notifier SMTP/Resend if secrets exist
	if cfg.Notifier.Provider == "smtp" {
		data, err := c.GetSecret(ctx, "notifier/smtp")
		if err == nil && data["password"] != "" {
			cfg.Notifier.SMTP.Password = data["password"]
			log.Info("smtp password loaded from vault")
		}
	}
	if cfg.Notifier.Provider == "resend" {
		data, err := c.GetSecret(ctx, "notifier/resend")
		if err == nil && data["api_key"] != "" {
			cfg.Notifier.Resend.APIKey = data["api_key"]
			log.Info("resend api key loaded from vault")
		}
	}

	return c, nil
}


// triggerMonitor runs the trigger order background worker.
func triggerMonitor(mds *marketdata.Service, ts *trading.Service, tr *trigger.Service) {
	pairs := []struct{ base, quote string }{
		{"BTC", "USDT"}, {"BNB", "USDT"}, {"SOL", "USDT"}, {"USDC", "USDT"},
	}
	getPrices := func(ctx context.Context) (map[string]decimal.Decimal, error) {
		prices := make(map[string]decimal.Decimal)
		for _, p := range pairs {
			t, err := mds.GetTicker(ctx, p.base, p.quote)
			if err != nil || t.Last == "" {
				continue
			}
			if d, err := decimal.NewFromString(t.Last); err == nil {
				prices[p.base+"_"+p.quote] = d
			}
		}
		return prices, nil
	}
	placeOrder := func(ctx context.Context, userID uuid.UUID, pair, side string, qty decimal.Decimal) (uuid.UUID, error) {
		req := trading.PlaceOrderInput{
			UserID:   userID,
			Pair:     pair,
			Side:     side,
			Type:     string(matching.TypeMarket),
			Quantity: qty,
		}
		result, err := ts.PlaceOrder(ctx, req)
		if err != nil {
			return uuid.Nil, err
		}
		return result.OrderID, nil
	}
	tr.RunMonitor(context.Background(), getPrices, placeOrder, 5*time.Second)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logger.New(cfg.App.Env)
	slog.SetDefault(log)
	log.Info("starting goexchange-api", "env", cfg.App.Env, "port", cfg.App.Port)

	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer initCancel()

	// Initialize Vault client (if enabled) and load secrets.
	// The scheduler binary uses the same helper from
	// internal/vaultinit so both binaries share the same
	// secret-loading logic and there is no copy drift.
	vaultClient, err := vaultinit.Init(cfg, initCtx, log)
	if err != nil {
		return fmt.Errorf("init vault: %w", err)
	}
	defer func() {
		if vaultClient != nil {
			vaultClient.InvalidateAllCache()
		}
	}()

	pool, err := db.Connect(initCtx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	// Build services (no chainwatcher - that's in scheduler)
	userSvc := user.NewService(pool, log)
	walletSvc := wallet.NewService(pool, log)

	// Signer daemon (closed-source): if SIGNER_ADDR is set (compose
	// wires it to "signer:50061"; dev runs on host wire it to
	// "127.0.0.1:50061"), dial it and hand the client to the V1
	// wallet service. A nil signer is fine: AllocateDepositAddress
	// falls back to the registry adapter (which also returns a
	// placeholder address until B5 wires real derivation).
	signerAddr := os.Getenv("SIGNER_ADDR")
	if signerAddr == "" {
		signerAddr = "127.0.0.1:50061"
	}
	signerClient, err := signerclient.NewClient(initCtx, signerAddr)
	if err != nil {
		log.Warn("signer dial failed; AllocateDepositAddress will use registry fallback",
			"addr", signerAddr, "error", err)
	} else {
		defer signerClient.Close()
		ok, vaultStatus, _, herr := signerClient.Health(initCtx)
		if herr != nil || !ok {
			log.Warn("signer reports unhealthy",
				"addr", signerAddr, "vault_status", vaultStatus, "error", herr)
		} else {
			log.Info("signer connected", "addr", signerAddr, "vault_status", vaultStatus)
		}
	}

	// Matching engine runs in matcher binary; we use a client here
	matchingClient := matching.NewClient(cfg.Matcher.URL, log)

// MMBot client (per-pair market-making bot in core).
// Empty URL means "bot engine not deployed in this env"; the
// client still returns a non-nil interface that always errors,
// so admin handlers respond with HTTP 503 instead of nil-deref
// panicking. Same pattern as the matcher client: failures are
// surfaced to the caller, never swallowed.
mmBotClient := mmbot.NewGRPCClient(cfg.MMBot.URL, log.With("component", "mmbot"))
	tradingSvc := trading.NewService(pool, matching.NewOrderSourceAdapter(matchingClient), walletSvc, log)
	marketDataSvc := marketdata.NewService(marketdata.NewMatcherAdapter(matchingClient), log)
	// Seed the marketdata pair cache from the trading service (which
	// reads trading_pairs from the DB at startup). Falling back to
	// cfg.ChainWatcher.Pairs kept the cache empty in production where
	// that section is not present in config.yaml.
	var pc []config.PairConfig
	for _, ep := range tradingSvc.EnabledPairs() {
		pc = append(pc, config.PairConfig{Base: ep.Base, Quote: ep.Quote, Enabled: ep.Enabled})
	}
	if pc == nil {
		pc = cfg.ChainWatcher.Pairs
	}
	marketDataSvc.SetPairs(pc)

	// Notifier setup (must be before chainwatcher.New which uses it)
	nlog := log.With("component", "notifier")

	// Externalise email templates to config/email/ so a copy fix
	// at 3am does not require a rebuild. The package writes a copy
	// of the embedded defaults on first boot so a fresh deploy still
	// sends mail; operators edit the on-disk files and reload via
	// kill -HUP or by waiting for the file watcher.
	if err := templates.Init(); err != nil {
		return fmt.Errorf("init email templates: %w", err)
	}
	ok, fail := templates.Stats()
	log.Info("email templates initialised", "dir", templates.LocaleFileDir, "ok_reloads", ok, "failed_reloads", fail)

	nprovider, err := notifier.NewProvider(

		notifier.ProviderType(cfg.Notifier.Provider),

		notifier.SMTPConfig{Host: cfg.Notifier.SMTP.Host, Port: cfg.Notifier.SMTP.Port, User: cfg.Notifier.SMTP.User, Password: cfg.Notifier.SMTP.Password, From: cfg.Notifier.SMTP.From},

		notifier.ResendConfig{APIKey: cfg.Notifier.Resend.APIKey, From: cfg.Notifier.Resend.From},

		nlog,

	)
	if err != nil {
		return fmt.Errorf("init notifier provider: %w", err)
	}
	auditSvc := audit.NewService(pool, log.With("component", "audit"))
	notifierSvc := notifier.NewService(pool, nprovider, cfg.Notifier.From, nlog)
	notifPrefsSvc := notifier.NewPrefsService(pool)

	defer notifierSvc.Close()


	// Build multi-chain driver (each chain has its own driver + RPC)
	// Register signer builder so factory.go can construct LocalSigner for EVM chains.
	// Wires chainwatcher's TxSigner interface to the actual signing.LocalSigner.
	chainwatcher.RegisterSignerBuilder(func(cfg config.ChainConfig, privKey, expectedAddr string) (chainwatcher.TxSigner, error) {
		ls, err := signing.NewLocalSigner(
			signing.ChainFromString(cfg.ID), // "bsc" -> ChainBSC
			privKey,
			expectedAddr,
			cfg.ChainID,
		)
		if err != nil {
			return nil, err
		}
		return &signing.LocalSignerAdapter{Signer: ls}, nil
	})

	// Build ChainRegistry (hot-reloadable multi-chain driver manager).
	// All chain drivers are stored here; Service.driverForAsset looks them up by asset.
	deps := chainwatcher.DriverDeps{
		VaultClient:  vaultClient,
		Logger:       log,
	}
	registry := chainwatcher.NewChainRegistry(deps, log)
	changes, err := registry.SyncFromConfig(context.Background(), cfg.ChainWatcher.Chains)
	if err != nil {
		log.Warn("chain registry sync failed", "error", err)
	}
	for _, c := range changes {
		log.Info("chain registry change", "change", c)
	}

	var chainWatcher *chainwatcher.Service
	// Build drivers map from registry (chain_id -> driver)
	driversMap := make(map[string]chainwatcher.Driver)
	for _, id := range registry.List() {
		if drv, ok := registry.Get(id); ok {
			driversMap[id] = drv
		}
	}
	chainWatcher = chainwatcher.NewMultiChain(pool, walletSvc, userSvc, notifierSvc, log, cfg.ChainWatcher, driversMap)
	chainWatcher.SetRegistry(registry)
	if len(driversMap) == 0 {
		log.Warn("no chain drivers active - chainWatcher will not poll deposits")
	}
	authSvc := auth.NewService(cfg.JWT.Secret, time.Duration(cfg.JWT.TTL)*time.Second)

	// TOTP service - uses JWT secret for encrypting TOTP secrets at rest
	// For production, use a separate encryption key (e.g., from Vault)
	totpSvc, err := auth.NewTOTPService(pool, cfg.JWT.Secret+"-2fa-encryption-key-padding", "goexchange")
	if err != nil {
		log.Error("create totp service failed", "error", err)
		os.Exit(1)
	}
	notifHub := api.NewNotifWSHub(notifierSvc, authSvc, log.With("component", "ws-notif"))
	marketHub := api.NewMarketWSHub(marketDataSvc.Events(), log.With("component", "ws-market"))

	// Ticker poller: background goroutine that periodically fetches
	// tickers from matching engine and publishes to WS clients
	tickerPoller := marketdata.NewTickerPoller(marketDataSvc, marketdata.NewMatcherAdapter(matchingClient), log.With("component", "ticker-poller"), 2*time.Second)
	go tickerPoller.Run(context.Background())
	riskSvc := risk.New(pool, log)


	// HTTP router
	// Trigger orders service (stop loss / take profit)
	triggerSvc := trigger.NewService(pool, log)
	analyticsSvc := analytics.NewService(pool)


	// Trigger order monitor - checks pending triggers against current price every 5s
	go triggerMonitor(marketDataSvc, tradingSvc, triggerSvc)

	uploadStore, err := uploads.New("/root/goexchange/uploads")
	if err != nil {
		log.Error("create upload store", "error", err)
		os.Exit(1)
	}
	router := api.NewRouter(api.Deps{
		Log:             log,
		Pool:            pool,
		UserSvc:         userSvc,
		WalletSvc:       walletSvc,
		TradingSvc:      tradingSvc,
		MarketDataSvc:   marketDataSvc,
		ChainWatcherSvc: chainWatcher,
		AuthSvc:         authSvc,
		TOTPSvc:         totpSvc,
		RiskSvc:         riskSvc,
		Notifier:        notifierSvc,
		NotifWSHub:       notifHub,
		MarketWSHub:      marketHub,
		AuditSvc:        auditSvc,
		NotifPrefs:      notifPrefsSvc,
		VaultClient:     vaultClient,
		APIKeys:         apikeys.NewService(pool),
		ChainRegistry:  registry,
		UploadStore:    uploadStore,
		TriggerSvc:      triggerSvc,
		AnalyticsSvc:    analyticsSvc,
		ConfigPath:     "config.yaml",
		MMBotClient:     mmBotClient,
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.App.Port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Graceful shutdown
	ctxBg, cancelBg := context.WithCancel(context.Background())
	_ = ctxBg
	defer cancelBg()

	chainWatcher.Start(ctxBg)

	// Stream forwarder: subscribe to matching-engine gRPC stream and
	// publish trade events onto the marketdata EventBus so the WS hub
	// pushes them out to clients. Runs in its own goroutine; exits
	// when ctxBg is canceled (see MATCHING_LICENSE_DESIGN §5.3).
	streamFwd := marketdata.NewStreamForwarder(matchingClient, marketDataSvc.Events(), log.With("component", "stream-fwd"))
	go func() {
		if err := streamFwd.Run(ctxBg); err != nil {
			log.Error("stream forwarder exited with error", "error", err)
		}
	}()

	// Start EVM indexer
	evmIndexer := indexer.NewEVMIndexer(pool, log)
	for chainID, drv := range driversMap {
		evmDrv, ok := drv.(*chainwatcher.EVMDriver)
		if !ok {
			continue
		}
		cfg, exists := cfg.ChainWatcher.Chains[chainID]
		if !exists {
			continue
		}
		_ = evmDrv // we use it for type check
		evmIndexer.AddChain(chainID, drv, cfg.HotWallet, cfg.RPCURL, cfg.ChainID)
	}
	go evmIndexer.Run(ctxBg, 30*time.Second)

	// Start notifier outbox worker. Picks up rows inserted via
	// notifier.Send / SendHTML and dispatches them through the
	// configured provider (Resend in prod, console/smtp in dev).
	// Without this goroutine the outbox accumulates PENDING rows
	// forever — that is a real bug, see commit history.
	go notifierSvc.RunWorker(ctxBg, 15*time.Second)

	// Start config file watcher for hot-reload.
	// When config.yaml changes, debounced re-read + apply to registry.
	configWatcher := config.NewWatcher("config.yaml", log, func(newCfg *config.Config) error {
		// Apply new chain config to registry
		changes, err := registry.SyncFromConfig(context.Background(), newCfg.ChainWatcher.Chains)
		if err != nil {
			return err
		}
		for _, c := range changes {
			log.Info("hot-reload applied", "change", c)
		}
		return nil
	})
	if err := configWatcher.Start(ctxBg); err != nil {
		log.Warn("config watcher start failed", "error", err)
	} else {
		defer configWatcher.Stop()
	}

	go func() {
		log.Info("HTTP server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("HTTP server failed", "error", err)
			cancelBg()
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("api shutdown signal received")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown failed", "error", err)
	}
	log.Info("api shutdown complete")
	return nil
}
