package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// adminListCurrenciesHandler handles GET /api/v1/admin/currencies
// Returns all currencies with their withdraw fee settings.
func adminListCurrenciesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const q = `
			SELECT symbol, name, precision, is_active, min_withdraw, max_withdraw,
			       withdraw_fee_flat, withdraw_fee_percent, withdraw_fee_min
			FROM currencies ORDER BY symbol
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
			IsActive           bool   `json:"is_active"`
			MinWithdraw        string `json:"min_withdraw"`
			MaxWithdraw        string `json:"max_withdraw"`
			WithdrawFeeFlat    string `json:"withdraw_fee_flat"`
			WithdrawFeePercent string `json:"withdraw_fee_percent"`
			WithdrawFeeMin     string `json:"withdraw_fee_min"`
			UpdatedAt          string `json:"updated_at"`
		}
		out := []Currency{}
		for rows.Next() {
			var c Currency
			if err := rows.Scan(&c.Symbol, &c.Name, &c.Precision, &c.IsActive,
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

// adminUpdateCurrencyHandler handles PUT /api/v1/admin/currencies/{symbol}
// Updates currency settings including withdraw fees.
func adminUpdateCurrencyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		symbol := chi.URLParam(r, "symbol")
		if symbol == "" {
			writeError(w, http.StatusBadRequest, "symbol required")
			return
		}

		var in struct {
			MinWithdraw        string `json:"min_withdraw"`
			MaxWithdraw        string `json:"max_withdraw"`
			WithdrawFeeFlat    string `json:"withdraw_fee_flat"`
			WithdrawFeePercent string `json:"withdraw_fee_percent"`
			WithdrawFeeMin     string `json:"withdraw_fee_min"`
			IsActive           *bool  `json:"is_active"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
			return
		}

		setClauses := []string{}
		args := []any{}
		argIdx := 1
		if in.MinWithdraw != "" {
			setClauses = append(setClauses, "min_withdraw = $"+strconv.Itoa(argIdx))
			args = append(args, in.MinWithdraw)
			argIdx++
		}
		if in.MaxWithdraw != "" {
			setClauses = append(setClauses, "max_withdraw = $"+strconv.Itoa(argIdx))
			args = append(args, in.MaxWithdraw)
			argIdx++
		}
		if in.WithdrawFeeFlat != "" {
			setClauses = append(setClauses, "withdraw_fee_flat = $"+strconv.Itoa(argIdx))
			args = append(args, in.WithdrawFeeFlat)
			argIdx++
		}
		if in.WithdrawFeePercent != "" {
			setClauses = append(setClauses, "withdraw_fee_percent = $"+strconv.Itoa(argIdx))
			args = append(args, in.WithdrawFeePercent)
			argIdx++
		}
		if in.WithdrawFeeMin != "" {
			setClauses = append(setClauses, "withdraw_fee_min = $"+strconv.Itoa(argIdx))
			args = append(args, in.WithdrawFeeMin)
			argIdx++
		}
		if in.IsActive != nil {
			setClauses = append(setClauses, "is_active = $"+strconv.Itoa(argIdx))
			args = append(args, *in.IsActive)
			argIdx++
		}
		if len(setClauses) == 0 {
			writeError(w, http.StatusBadRequest, "no fields to update")
			return
		}
		setClauses = append(setClauses, "updated_at = NOW()")
		args = append(args, symbol)
		q := "UPDATE currencies SET "
		for i, c := range setClauses {
			if i > 0 {
				q += ", "
			}
			q += c
		}
		q += " WHERE symbol = $" + strconv.Itoa(argIdx)

		_, err := d.Pool.Exec(r.Context(), q, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "symbol": symbol})
	}
}