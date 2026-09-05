// Package config loads configuration from YAML file + env vars.
package config

import (
	"fmt"
	"os"
	"strconv"
	
	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	Port         int    `yaml:"port"`
	MatcherPort  int    `yaml:"matcher_port"`
	SchedulerPort int   `yaml:"scheduler_port"`
	Env          string `yaml:"env"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type RedisConfig struct {
	Addr string `yaml:"addr"`
}

type JWTConfig struct {
	Secret string        `yaml:"secret"`
	TTL    int           `yaml:"ttl"`  // seconds
}

type ChainWatcherConfig struct {
	Driver         string  `yaml:"driver"` // "mock", "btc", "evm", etc.
	PollIntervalSec int    `yaml:"poll_interval_sec"`
	MinConf        int     `yaml:"min_conf"` // confirmations required (default 6)
	MockIntervalSec int    `yaml:"mock_interval_sec"`
	MockMaxAmountUSD float64 `yaml:"mock_max_amount_usd"`
	HotWalletAddress string `yaml:"hot_wallet_address"`

	// Multi-chain: per-chain configuration
	Chains        map[string]ChainConfig `yaml:"chains"`

	// Markets: explicit list of trading pairs (replaces hardcoded list)
	// Frontend reads these via /api/v1/markets.
	// Admin can enable/disable individual pairs at runtime.
	Pairs         []PairConfig `yaml:"pairs,omitempty"`
}


// PairConfig defines a trading pair in the market.
//
// Pairs are listed in chainwatcher.pairs in config.yaml.
// They can be enabled/disabled at runtime via the admin API.
// Frontend reads them dynamically from /api/v1/markets.
type PairConfig struct {
	Base    string `yaml:"base"`    // e.g. "BTC"
	Quote   string `yaml:"quote"`   // e.g. "USDT"
	Enabled bool   `yaml:"enabled"` // master switch
	// Display
	DisplayName string `yaml:"display_name,omitempty"` // optional human-friendly name
	// Minimum order size
	MinQuantity string `yaml:"min_quantity,omitempty"` // e.g. "0.0001"
	MinPrice    string `yaml:"min_price,omitempty"`    // e.g. "0.01"
	// Tick size (price increment)
	PriceTick string `yaml:"price_tick,omitempty"` // e.g. "0.01"
	QuantityTick string `yaml:"quantity_tick,omitempty"` // e.g. "0.0001"
	// Maker/taker fees (basis points)
	MakerFeeBps int `yaml:"maker_fee_bps,omitempty"` // e.g. 10 = 0.1%
	TakerFeeBps int `yaml:"taker_fee_bps,omitempty"` // e.g. 20 = 0.2%
}

// DerivationConfig configures BIP-44 HD wallet derivation for a chain.
type DerivationConfig struct {
	MnemonicSecret string `yaml:"mnemonic_secret"`
	Path           string `yaml:"path"`
}

// ChainConfig is one chain's driver configuration.
// Each chain is identified by a unique `id` (e.g. "btc", "bsc", "eth", "polygon").
// All behavior is data-driven - no hardcoded mapping in code.
type ChainConfig struct {
	// Identity
	Enabled bool   `yaml:"enabled"`  // master switch (can be toggled at runtime)
	ID      string `yaml:"-"`        // populated from map key (NOT in YAML)

	// Chain definition
	Driver    string `yaml:"driver"`     // "btc", "evm", "mock", etc.
	ChainKind string `yaml:"chain_kind"` // "bitcoin", "evm", "cosmos", "solana"
	Asset     string `yaml:"asset"`      // primary asset symbol

	// Connectivity
	RPCURL  string `yaml:"rpc_url"`
	RPCUser string `yaml:"rpc_user"`
	RPCPass string `yaml:"rpc_pass"`

	// Chain-specific
	ChainID int64 `yaml:"chain_id"` // EVM chain ID (1=ETH, 56=BSC, 97=BSC testnet)

	// Chain family - which Deriver to use
	//   bitcoin: UTXO chains (BTC forks). Same code handles BTC/LTC/DOGE/etc.
	//   evm:     Account-based EVM chains. Same code handles ETH/BSC/Polygon/etc.
	// New families require new code; new chains within a family = config only.
	Family string `yaml:"family"`

	// Bitcoin-family params (only used when Family == "bitcoin")
	CoinType    uint32 `yaml:"coin_type"`     // BIP-44 coin type (0=BTC, 2=LTC, 3=DOGE)
	P2PKHPrefix byte   `yaml:"p2pkh_prefix"`  // P2PKH version byte (0x00=BTC, 0x30=LTC)

	// Confirmations
	MinConf         int `yaml:"min_conf"`
	PollIntervalSec int `yaml:"poll_interval_sec"`

	// Hot wallet (one of these is set)
	HotWallet       string             `yaml:"hot_wallet"`        // direct address (0x...)
	HotWalletSecret string             `yaml:"hot_wallet_secret"` // path in Vault to {address, private_key}
	Derivation      *DerivationConfig  `yaml:"derivation"`         // BIP-44 HD wallet derivation

	// Signing
	Signer          string `yaml:"signer"`
	VaultSecretPath string `yaml:"vault_secret_path"`

	// Display
	DisplayName string `yaml:"display_name"`
	Decimals    int    `yaml:"decimals"`
	ExplorerURL string `yaml:"explorer_url"`

	// Tokens (for EVM chains with ERC20/BEP20 tokens)
	Tokens []TokenConfig `yaml:"tokens"`
}

// TokenConfig defines an ERC20/BEP20 token on a parent chain.
//
// The `json:` tags mirror the YAML field names so the admin
// web UI's ChainToken interface (lowercase `symbol`,
// `contract`, `decimals`, etc.) sees the same shape whether
// it parses the YAML config or the JSON HTTP response. The
// pre-D9 response emitted Go-cased keys (`Symbol`,
// `Contract`, ...) which the web client's lowercase type
// silently dropped, leaving every token with `symbol ===
// undefined` and triggering React's duplicate-key warning
// in the chain list.
type TokenConfig struct {
	Symbol          string `yaml:"symbol" json:"symbol"`
	Contract        string `yaml:"contract" json:"contract"`
	Decimals        int    `yaml:"decimals" json:"decimals"`
	MinConf         int    `yaml:"min_conf" json:"min_conf"`
	VaultSecretPath string `yaml:"vault_secret_path" json:"vault_secret_path,omitempty"`
}

// VaultConfig configures HashiCorp Vault access.
type VaultConfig struct {
	Enabled     bool   `yaml:"enabled"`  // master switch
	Address     string `yaml:"address"`
	Token       string `yaml:"token"`     // for static auth (DEV only)
	AuthMethod  string `yaml:"auth_method"` // "static", "approle", "kubernetes"
	AppRoleID   string `yaml:"app_role_id"` // for approle
	AppSecretID string `yaml:"app_secret_id"` // for approle (can rotate)
	K8sRole     string `yaml:"k8s_role"`  // for kubernetes auth
	CacheTTLSec int    `yaml:"cache_ttl_sec"`
	// Paths to secrets to load at startup
	DBPath      string `yaml:"db_path"`       // e.g. "db/postgres"
	JWTPath     string `yaml:"jwt_path"`      // e.g. "auth/jwt"
}

type Config struct {
	App          AppConfig          `yaml:"app"`
	Database     DatabaseConfig     `yaml:"database"`
	Vault        VaultConfig        `yaml:"vault"`
	Redis        RedisConfig        `yaml:"redis"`
	JWT          JWTConfig          `yaml:"jwt"`
	ChainWatcher ChainWatcherConfig `yaml:"chainwatcher"`
	Matcher      MatcherConfig      `yaml:"matcher"`
	MMBot        MMBotConfig        `yaml:"mmbot"`
	Scheduler    SchedulerConfig    `yaml:"scheduler"`
	Notifier     NotifierConfig     `yaml:"notifier"`
}

type MatcherConfig struct {
	URL string `yaml:"url"` // e.g. http://localhost:8098
}

// MMBotConfig configures the gRPC endpoint for the per-pair
// market-making bot engine running in goexchange-core. The bot
// listens on port 50052 by convention. When the URL is empty
// (e.g. in dev without core), the client falls back to an
// error-returning shim and admin handlers respond with 503.
type MMBotConfig struct {
	URL string `yaml:"url"` // e.g. localhost:50052
}

type SchedulerConfig struct {
	URL string `yaml:"url"` // e.g. http://localhost:8097
}

// EmailSMTPConfig configures SMTP provider (Gmail, MailHog, SES, SendGrid SMTP).
type EmailSMTPConfig struct {
	Host     string `yaml:"host"`     // e.g. "smtp.gmail.com" or "127.0.0.1" (MailHog)
	Port     int    `yaml:"port"`     // 587 (Gmail), 1025 (MailHog), 465 (SSL)
	User     string `yaml:"user"`     // SMTP auth user (empty = no auth)
	Password string `yaml:"password"`
	From     string `yaml:"from"`     // From address
}

// EmailResendConfig configures Resend HTTP API provider.
type EmailResendConfig struct {
	APIKey string `yaml:"api_key"` // Resend API key (re_xxx)
	From   string `yaml:"from"`    // From address
}

// NotifierConfig configures the notifier service.
type NotifierConfig struct {
	WorkerIntervalSec int              `yaml:"worker_interval_sec"` // default 30
	Provider          string           `yaml:"provider"`            // console | smtp | resend
	SMTP              EmailSMTPConfig   `yaml:"smtp"`
	Resend            EmailResendConfig `yaml:"resend"`
	From              string           `yaml:"from"` // default From address
}

// Load reads config from YAML file + env overrides.
func Load(path string) (*Config, error) {
	cfg := defaultConfig()

	// Read from YAML if file exists
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse yaml: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Apply env overrides
	if v := os.Getenv("APP_PORT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cfg.App.Port = i
		}
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		cfg.Redis.Addr = v
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.JWT.Secret = v
	}
	// Vault env overrides. These let the deploy script
	// (deploy-fresh.sh) inject credentials without writing them to
	// config.yaml. VAULT_TOKEN doubles as the static root token AND
	// the approle secret_id — the API picks based on
	// vault.auth_method.
	if v := os.Getenv("VAULT_ADDR"); v != "" {
		cfg.Vault.Address = v
	}
	if v := os.Getenv("VAULT_TOKEN"); v != "" {
		if cfg.Vault.AuthMethod == "approle" {
			cfg.Vault.AppSecretID = v
		} else {
			cfg.Vault.Token = v
		}
	}
	if v := os.Getenv("VAULT_ROLE_ID"); v != "" {
		cfg.Vault.AppRoleID = v
	}
	if v := os.Getenv("VAULT_AUTH_METHOD"); v != "" {
		cfg.Vault.AuthMethod = v
	}

	// Validate
	// jwt.secret can be empty if Vault is enabled (will be loaded from Vault)
	if cfg.JWT.Secret == "" && !cfg.Vault.Enabled {
		return nil, fmt.Errorf("jwt.secret is required (set in config.yaml or JWT_SECRET env)")
	}

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		App: AppConfig{
			Port: 8080,
			Env:  "dev",
		},
		Database: DatabaseConfig{
			URL: "postgres://exchange:exchange@localhost:5432/exchange?sslmode=disable",
		},
		Redis: RedisConfig{
			Addr: "localhost:6379",
		},
		JWT: JWTConfig{
			Secret: "", // must be set
			TTL:    3600, // 1 hour
		},
		ChainWatcher: ChainWatcherConfig{
			MockIntervalSec: 30,
			MockMaxAmountUSD: 100,
		},
	}
}
