package api

import (
	"net/http"
)

// publicListCurrenciesHandler handles GET /api/v1/currencies
// Returns all active currencies with their withdraw fee info (for users to see fees).
func publicListCurrenciesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const q = `
			SELECT symbol, name, precision, min_withdraw, max_withdraw,
			       withdraw_fee_flat, withdraw_fee_percent, withdraw_fee_min
			FROM currencies WHERE is_active = true ORDER BY symbol
		`
		rows, err := d.Pool.Query(r.Context(), q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()
		type Currency struct {
			Symbol             string `json:"symbol"`
			Name               string `json:"name"`
			Precision          int    `json:"precision"`
			MinWithdraw        string `json:"min_withdraw"`
			MaxWithdraw        string `json:"max_withdraw"`
			WithdrawFeeFlat    string `json:"withdraw_fee_flat"`
			WithdrawFeePercent string `json:"withdraw_fee_percent"`
			WithdrawFeeMin     string `json:"withdraw_fee_min"`
		}
		out := []Currency{}
		for rows.Next() {
			var c Currency
			if err := rows.Scan(&c.Symbol, &c.Name, &c.Precision,
				&c.MinWithdraw, &c.MaxWithdraw,
				&c.WithdrawFeeFlat, &c.WithdrawFeePercent, &c.WithdrawFeeMin); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			out = append(out, c)
		}
		writeJSON(w, http.StatusOK, out)
	}
}