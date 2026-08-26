package api

import (
	"net/http"
	"time"
)

// adminFeeStatsHandler handles GET /api/v1/admin/fee-stats
// Returns aggregated fee statistics per asset for the dashboard.
func adminFeeStatsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const totalQ = `
			SELECT asset,
			       COUNT(*) FILTER (WHERE fee > 0) as withdrawal_count,
			       COALESCE(SUM(fee), 0)::text as total_fee,
			       COALESCE(SUM(amount), 0)::text as total_volume,
			       COALESCE(SUM(receive_amount), 0)::text as total_received
			FROM withdrawals
			WHERE status IN ('BROADCAST', 'DONE', 'CONFIRMED', 'FAILED', 'PENDING', 'HOLD')
			GROUP BY asset
			ORDER BY SUM(fee) DESC
		`
		rows, err := d.Pool.Query(r.Context(), totalQ)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()

		type AssetStats struct {
			Asset           string `json:"asset"`
			WithdrawalCount int    `json:"withdrawal_count"`
			TotalFee        string `json:"total_fee"`
			TotalVolume     string `json:"total_volume"`
			TotalReceived   string `json:"total_received"`
		}

		assets := []AssetStats{}
		for rows.Next() {
			var a AssetStats
			if err := rows.Scan(&a.Asset, &a.WithdrawalCount, &a.TotalFee, &a.TotalVolume, &a.TotalReceived); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			assets = append(assets, a)
		}

		var grandTotalFee, grandTotalVolume string
		_ = d.Pool.QueryRow(r.Context(), `
			SELECT COALESCE(SUM(fee), 0)::text, COALESCE(SUM(amount), 0)::text
			FROM withdrawals
			WHERE status IN ('BROADCAST', 'DONE', 'CONFIRMED', 'FAILED', 'PENDING', 'HOLD')
		`).Scan(&grandTotalFee, &grandTotalVolume)

		const dailyQ = `
			SELECT DATE(created_at) as day,
			       COUNT(*) as count,
			       COALESCE(SUM(fee), 0)::text as daily_fee,
			       COALESCE(SUM(amount), 0)::text as daily_volume
			FROM withdrawals
			WHERE created_at >= NOW() - INTERVAL '30 days'
			  AND status IN ('BROADCAST', 'DONE', 'CONFIRMED')
			GROUP BY DATE(created_at)
			ORDER BY day DESC
			LIMIT 30
		`
		rows2, err := d.Pool.Query(r.Context(), dailyQ)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows2.Close()

		type DailyStats struct {
			Day         time.Time
			Count       int
			DailyFee    string
			DailyVolume string
		}
		type DailyOut struct {
			Day         string `json:"day"`
			Count       int    `json:"count"`
			DailyFee    string `json:"daily_fee"`
			DailyVolume string `json:"daily_volume"`
		}
		daily := []DailyStats{}
		for rows2.Next() {
			var dd DailyStats
			if err := rows2.Scan(&dd.Day, &dd.Count, &dd.DailyFee, &dd.DailyVolume); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			daily = append(daily, dd)
		}
		dailyOut := []DailyOut{}
		for _, dd := range daily {
			dailyOut = append(dailyOut, DailyOut{
				Day:         dd.Day.Format("2006-01-02"),
				Count:       dd.Count,
				DailyFee:    dd.DailyFee,
				DailyVolume: dd.DailyVolume,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"by_asset":           assets,
			"grand_total_fee":    grandTotalFee,
			"grand_total_volume": grandTotalVolume,
			"daily":              dailyOut,
		})
	}
}