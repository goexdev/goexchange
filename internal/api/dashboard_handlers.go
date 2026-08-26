package api

import (
	"context"
	"net/http"
	"runtime"
	"sort"
	"time"

	"github.com/goexdev/goexchange/internal/chainwatcher"
)

// adminDashboardHandler handles GET /api/v1/admin/dashboard
// Returns a comprehensive dashboard with system health, chain status,
// user stats, and recent activity.
func adminDashboardHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		dashboard := map[string]any{
			"timestamp": time.Now().UTC(),
		}

		dashboard["system"] = getSystemHealth(ctx, d)
		dashboard["chains"] = getChainStatus(ctx, d)
		dashboard["tokens"] = getTokenDistribution(ctx, d)
		dashboard["volume"] = getVolumeStats(ctx, d)
		dashboard["alerts"] = getAlerts(ctx, d)

		writeJSON(w, http.StatusOK, dashboard)
	}
}

// getSystemHealth returns system resource info.
func getSystemHealth(ctx context.Context, d Deps) map[string]any {
	health := map[string]any{
		"goroutines": runtime.NumGoroutine(),
		"go_version": runtime.Version(),
	}

	if d.Pool != nil {
		stat := d.Pool.Stat()
		health["db"] = map[string]any{
			"connected":       true,
			"total_conns":     stat.TotalConns(),
			"acquired_conns":  stat.AcquiredConns(),
			"idle_conns":      stat.IdleConns(),
			"max_conns":       stat.MaxConns(),
			"new_conns_count": stat.NewConnsCount(),
			"acquire_count":   stat.AcquireCount(),
		}
		var dbVersion string
		if err := d.Pool.QueryRow(ctx, "SELECT version()").Scan(&dbVersion); err == nil {
			health["db"].(map[string]any)["version"] = dbVersion
		}
	} else {
		health["db"] = map[string]any{"connected": false}
	}

	if d.Redis != nil {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		pong, err := d.Redis.Ping(pingCtx).Result()
		if err == nil {
			health["redis"] = map[string]any{
				"connected": true,
				"ping":      pong,
			}
			size, _ := d.Redis.DBSize(pingCtx).Result()
			health["redis"].(map[string]any)["key_count"] = size
		} else {
			health["redis"] = map[string]any{
				"connected": false,
				"error":     err.Error(),
			}
		}
	} else {
		health["redis"] = map[string]any{"connected": false}
	}

	if d.VaultClient != nil {
		health["vault"] = map[string]any{
			"connected": true,
		}
	} else {
		health["vault"] = map[string]any{"connected": false}
	}

	return health
}

// getChainStatus returns per-chain health info.
func getChainStatus(ctx context.Context, d Deps) map[string]any {
	result := map[string]any{
		"chains": []map[string]any{},
	}

	if d.ChainRegistry == nil {
		return result
	}

	configs := d.ChainRegistry.Configs()
	chains := make([]map[string]any, 0, len(configs))

	for id, cfg := range configs {
		info := map[string]any{
			"id":       id,
			"family":   cfg.Family,
			"driver":   cfg.Driver,
			"asset":    cfg.Asset,
			"enabled":  cfg.Enabled,
			"min_conf": cfg.MinConf,
		}

		drv, active := d.ChainRegistry.Get(id)
		info["active"] = active

		if active {
			count, err := drv.GetBlockCount(ctx)
			if err == nil {
				info["block_count"] = count
				info["status"] = "healthy"
			} else {
				info["status"] = "error"
				info["error"] = err.Error()
			}
			info["hot_wallet"] = drv.GetHotAddress()
			info["has_signer"] = drv.HasSigner()
		} else {
			if cfg.Enabled {
				info["status"] = "loaded"
			} else {
				info["status"] = "disabled"
			}
		}

		chains = append(chains, info)
	}

	sort.Slice(chains, func(i, j int) bool {
		return chains[i]["id"].(string) < chains[j]["id"].(string)
	})

	result["chains"] = chains
	result["total"] = len(chains)
	result["active"] = len(d.ChainRegistry.List())

	return result
}

// getTokenDistribution returns per-token stats from balances table.
func getTokenDistribution(ctx context.Context, d Deps) map[string]any {
	result := map[string]any{
		"tokens": []map[string]any{},
	}

	if d.Pool == nil {
		return result
	}

	rows, err := d.Pool.Query(ctx, `
		SELECT asset, COUNT(DISTINCT user_id) AS holders,
		       COALESCE(SUM(available), 0) AS total_available,
		       COALESCE(SUM(frozen), 0) AS total_frozen
		FROM balances
		GROUP BY asset
		ORDER BY asset
	`)
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	defer rows.Close()

	tokens := []map[string]any{}
	for rows.Next() {
		var asset string
		var holders int
		var available, frozen float64
		if err := rows.Scan(&asset, &holders, &available, &frozen); err == nil {
			tokens = append(tokens, map[string]any{
				"asset":     asset,
				"holders":   holders,
				"available": available,
				"frozen":    frozen,
				"total":     available + frozen,
			})
		}
	}

	result["tokens"] = tokens
	result["count"] = len(tokens)

	return result
}

// getVolumeStats returns 24h volume for deposits/withdrawals.
func getVolumeStats(ctx context.Context, d Deps) map[string]any {
	result := map[string]any{}

	if d.Pool == nil {
		return result
	}

	var depositCount int
	var depositVolume float64
	_ = d.Pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(amount), 0)
		FROM deposits
		WHERE created_at > NOW() - INTERVAL '24 hours'
	`).Scan(&depositCount, &depositVolume)
	result["deposits_24h"] = map[string]any{
		"count":  depositCount,
		"volume": depositVolume,
	}

	var withdrawCount int
	var withdrawVolume float64
	_ = d.Pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(amount), 0)
		FROM withdrawals
		WHERE created_at > NOW() - INTERVAL '24 hours'
	`).Scan(&withdrawCount, &withdrawVolume)
	result["withdrawals_24h"] = map[string]any{
		"count":  withdrawCount,
		"volume": withdrawVolume,
	}

	rows, err := d.Pool.Query(ctx, `
		SELECT status, COUNT(*)
		FROM withdrawals
		WHERE created_at > NOW() - INTERVAL '24 hours'
		GROUP BY status
	`)
	if err == nil {
		statusCounts := map[string]any{}
		for rows.Next() {
			var status string
			var count int
			if err := rows.Scan(&status, &count); err == nil {
				statusCounts[status] = count
			}
		}
		rows.Close()
		result["withdrawals_by_status_24h"] = statusCounts
	}

	rows2, err := d.Pool.Query(ctx, `
		SELECT asset, COALESCE(SUM(amount), 0) AS volume, COUNT(*) AS count
		FROM withdrawals
		WHERE created_at > NOW() - INTERVAL '24 hours'
		GROUP BY asset
		ORDER BY volume DESC
		LIMIT 5
	`)
	if err == nil {
		topTokens := []map[string]any{}
		for rows2.Next() {
			var asset string
			var volume float64
			var count int
			if err := rows2.Scan(&asset, &volume, &count); err == nil {
				topTokens = append(topTokens, map[string]any{
					"asset":  asset,
					"volume": volume,
					"count":  count,
				})
			}
		}
		rows2.Close()
		result["top_withdrawn_tokens_24h"] = topTokens
	}

	return result
}

// getAlerts returns recent issues that need attention.
func getAlerts(ctx context.Context, d Deps) map[string]any {
	result := map[string]any{
		"items": []map[string]any{},
	}

	if d.Pool == nil {
		return result
	}

	alerts := []map[string]any{}

	var failedWithdraws int
	_ = d.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM withdrawals
		WHERE status = 'FAILED'
		AND created_at > NOW() - INTERVAL '24 hours'
	`).Scan(&failedWithdraws)
	if failedWithdraws > 0 {
		alerts = append(alerts, map[string]any{
			"level":  "warning",
			"title":  "Failed withdrawals (24h)",
			"count":  failedWithdraws,
			"action": "View admin withdrawals",
		})
	}

	var heldWithdraws int
	_ = d.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM withdrawals WHERE risk_hold = true
	`).Scan(&heldWithdraws)
	if heldWithdraws > 0 {
		alerts = append(alerts, map[string]any{
			"level":  "info",
			"title":  "Withdrawals on risk hold",
			"count":  heldWithdraws,
			"action": "Review held withdrawals",
		})
	}

	var pendingKYC int
	_ = d.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM kyc_submissions WHERE status = 'PENDING'
	`).Scan(&pendingKYC)
	if pendingKYC > 0 {
		alerts = append(alerts, map[string]any{
			"level":  "info",
			"title":  "Pending KYC submissions",
			"count":  pendingKYC,
			"action": "Review KYC queue",
		})
	}

	if d.ChainRegistry != nil {
		disabled := 0
		for _, cfg := range d.ChainRegistry.Configs() {
			if !cfg.Enabled {
				disabled++
			}
		}
		if disabled > 0 {
			alerts = append(alerts, map[string]any{
				"level":  "warning",
				"title":  "Disabled chains",
				"count":  disabled,
				"action": "Review chain configuration",
			})
		}
	}

	result["items"] = alerts
	result["count"] = len(alerts)
	result["has_warnings"] = false
	for _, a := range alerts {
		if a["level"] == "warning" || a["level"] == "error" {
			result["has_warnings"] = true
			break
		}
	}

	return result
}

// DashboardChartsHandler handles GET /api/v1/admin/dashboard/charts
// Returns time-series data for charts:
//   - user_signups: last 30 days daily count
//   - volume_hourly: last 24 hours hourly deposits + withdrawals
//   - token_distribution: pie chart of token balances
//   - withdrawal_status: pie of withdrawal statuses (24h)
func dashboardChartsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		result := map[string]any{}

		// 1. User signups - last 30 days, daily buckets
		signupRows, err := d.Pool.Query(ctx,
			`SELECT
			  date_trunc('day', created_at) AS day,
			  COUNT(*) AS count
			FROM users
			WHERE created_at >= NOW() - INTERVAL '30 days'
			GROUP BY day
			ORDER BY day ASC`,
		)
		if err == nil {
			defer signupRows.Close()
			signups := []map[string]any{}
			for signupRows.Next() {
				var t time.Time
				var c int
				if err := signupRows.Scan(&t, &c); err == nil {
					signups = append(signups, map[string]any{
						"day":   t.Format("2006-01-02"),
						"count": c,
					})
				}
			}
			result["user_signups"] = signups
		}

		// 2. Volume hourly - last 24h, hourly buckets
		volRows, err := d.Pool.Query(ctx,
			`SELECT
			  date_trunc('hour', created_at) AS hour,
			  type,
			  COUNT(*) AS count,
			  COALESCE(SUM(amount), 0) AS volume
			FROM (
			  SELECT 'deposit' AS type, created_at, amount FROM deposits
			  WHERE created_at >= NOW() - INTERVAL '24 hours' AND status IN ('CONFIRMED', 'CREDITED')
			  UNION ALL
			  SELECT 'withdrawal' AS type, created_at, amount FROM withdrawals
			  WHERE created_at >= NOW() - INTERVAL '24 hours'
			) AS combined
			GROUP BY hour, type
			ORDER BY hour ASC`,
		)
		if err == nil {
			defer volRows.Close()
			hours := []map[string]any{}
			for volRows.Next() {
				var t time.Time
				var typ string
				var cnt int
				var vol float64
				if err := volRows.Scan(&t, &typ, &cnt, &vol); err == nil {
					hours = append(hours, map[string]any{
						"hour":   t.Format("2006-01-02T15:04"),
						"type":   typ,
						"count":  cnt,
						"volume": vol,
					})
				}
			}
			result["volume_hourly"] = hours
		}

		// 3. Token distribution - current top balances
		tokRows, err := d.Pool.Query(ctx,
			`SELECT asset,
			  COUNT(*) AS holders,
			  COALESCE(SUM(available), 0) + COALESCE(SUM(frozen), 0) AS total
			FROM balances
			WHERE (available + frozen) > 0
			GROUP BY asset
			ORDER BY total DESC
			LIMIT 10`,
		)
		if err == nil {
			defer tokRows.Close()
			tokens := []map[string]any{}
			for tokRows.Next() {
				var asset string
				var holders int
				var total float64
				if err := tokRows.Scan(&asset, &holders, &total); err == nil {
					tokens = append(tokens, map[string]any{
						"asset":   asset,
						"holders": holders,
						"total":   total,
					})
				}
			}
			result["token_distribution"] = tokens
		}

		// 4. Withdrawal status breakdown (24h)
		wsRows, err := d.Pool.Query(ctx,
			`SELECT status, COUNT(*) AS count
			FROM withdrawals
			WHERE created_at >= NOW() - INTERVAL '24 hours'
			GROUP BY status`,
		)
		if err == nil {
			defer wsRows.Close()
			statuses := []map[string]any{}
			for wsRows.Next() {
				var status string
				var cnt int
				if err := wsRows.Scan(&status, &cnt); err == nil {
					statuses = append(statuses, map[string]any{
						"status": status,
						"count":  cnt,
					})
				}
			}
			result["withdrawal_statuses"] = statuses
		}

		writeJSON(w, http.StatusOK, result)
	}
}

// Type checks (these may be needed if imports are used elsewhere)
var _ = chainwatcher.Driver(nil)
