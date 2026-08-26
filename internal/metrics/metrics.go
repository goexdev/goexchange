// Package metrics provides Prometheus metrics for goexchange.
//
// Metrics are organized into categories:
//   - HTTP metrics (request rate, latency, status codes)
//   - Business metrics (orders, deposits, withdrawals)
//   - Chain metrics (per-chain block height, latency)
//   - WebSocket metrics (connections, events)
//   - System metrics (goroutines, memory, DB pool)
//
// All metrics are registered with the default Prometheus registry.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP metrics
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_http_requests_total",
			Help: "Total HTTP requests handled by the API server",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "goexchange_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		},
		[]string{"method", "path"},
	)

	// Business metrics
	OrdersPlacedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_orders_placed_total",
			Help: "Total orders placed",
		},
		[]string{"pair", "side"},
	)

	OrdersFilledTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_orders_filled_total",
			Help: "Total orders fully filled",
		},
		[]string{"pair", "side"},
	)

	TradesExecutedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_trades_executed_total",
			Help: "Total trades executed",
		},
		[]string{"pair"},
	)

	TradeVolumeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_trade_volume_total",
			Help: "Total trade volume (in base asset units)",
		},
		[]string{"pair", "side"},
	)

	DepositsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_deposits_total",
			Help: "Total deposits detected",
		},
		[]string{"chain", "asset"},
	)

	WithdrawalsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_withdrawals_total",
			Help: "Total withdrawals",
		},
		[]string{"chain", "asset", "status"},
	)

	// Chain metrics
	ChainBlockHeight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goexchange_chain_block_height",
			Help: "Current block height of each chain node",
		},
		[]string{"chain"},
	)

	ChainRPCLatency = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goexchange_chain_rpc_latency_seconds",
			Help: "RPC latency to each chain node",
		},
		[]string{"chain", "method"},
	)

	ChainWatcherStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goexchange_chain_watcher_status",
			Help: "Chain watcher status (1=healthy, 0=down)",
		},
		[]string{"chain"},
	)

	ChainDepositsPolled = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_chain_deposits_polled_total",
			Help: "Total deposit polls by chain",
		},
		[]string{"chain"},
	)

	// WebSocket metrics
	WSConnectionsActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goexchange_ws_connections_active",
			Help: "Active WebSocket connections by endpoint",
		},
		[]string{"endpoint"},
	)

	WSEventsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_ws_events_total",
			Help: "Total WebSocket events sent",
		},
		[]string{"endpoint", "type"},
	)

	// Market data metrics
	MarketPairsEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "goexchange_market_pairs_enabled",
			Help: "Number of enabled trading pairs",
		},
	)

	MarketPairsTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "goexchange_market_pairs_total",
			Help: "Total number of trading pairs",
		},
	)

	TickerUpdatesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "goexchange_ticker_updates_total",
			Help: "Total ticker updates published",
		},
	)

	// System metrics (Go runtime - auto-collected by promhttp)
	// DB pool metrics
	DBConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "goexchange_db_connections_active",
			Help: "Active database connections",
		},
	)

	DBConnectionsIdle = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "goexchange_db_connections_idle",
			Help: "Idle database connections",
		},
	)

	DBConnectionsTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "goexchange_db_connections_total",
			Help: "Total database connections",
		},
	)

	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "goexchange_db_query_duration_seconds",
			Help:    "Database query duration",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		},
		[]string{"operation"},
	)

	// Auth metrics
	LoginAttemptsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goexchange_login_attempts_total",
			Help: "Total login attempts",
		},
		[]string{"success"},
	)

	TwoFactorChallenges = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "goexchange_2fa_challenges_total",
			Help: "Total 2FA challenges",
		},
	)

	// Risk metrics
	RiskScoreBlocked = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "goexchange_risk_blocked_total",
			Help: "Total operations blocked by risk engine",
		},
	)

	RiskScoreHeld = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "goexchange_risk_held_total",
			Help: "Total operations held by risk engine",
		},
	)
)