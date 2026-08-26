package api

import (
	"fmt"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/goexdev/goexchange/internal/audit"
	"github.com/goexdev/goexchange/internal/user"
)

// auditLogAdmin writes an audit log entry for an admin action.
// adminUserID is from request context (set by authMiddleware).
func auditLogAdmin(d Deps, r *http.Request, entry audit.LogEntry) {
	if d.AuditSvc == nil {
		return
	}
	adminID := userIDFromContextUUID(r.Context())
	if adminID != uuid.Nil {
		entry.AdminUserID = &adminID
		if u, err := d.UserSvc.GetUser(r.Context(), adminID); err == nil && u != nil {
			entry.AdminEmail = u.Email
		}
	}
	if entry.IP == "" {
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		// Strip port: "1.2.3.4:5678" → "1.2.3.4", "[::1]:5678" → "::1"
		if idx := strings.LastIndex(ip, ":"); idx > 0 {
			if ip[0] == '[' {
				ip = ip[1:idx-1] // strip [...]
			} else {
				ip = ip[:idx]
			}
		}
		entry.IP = ip
	}
	if entry.UserAgent == "" {
		entry.UserAgent = r.Header.Get("User-Agent")
	}
	d.AuditSvc.Log(r.Context(), entry)
}

// auditLogFail writes a failure audit log entry (status="failure").
// Use this for any error return path in admin handlers.
func auditLogFail(d Deps, r *http.Request, action, targetType string, targetID *uuid.UUID, targetLabel string, details map[string]any, err error) {
	auditLogAdmin(d, r, audit.LogEntry{
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		TargetLabel: targetLabel,
		Details:     details,
		Status:      "failure",
		ErrorMsg:    err.Error(),
	})
}


// adminListUsersHandler handles GET /api/v1/admin/users
// Query params: limit, offset, search (email/id), role, kyc_level, kyc_status
func adminListUsersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		opts := user.ListUsersOpts{
			Limit:  50,
			Search: q.Get("search"),
			Role:   q.Get("role"),
			KycSt:  q.Get("kyc_status"),
		}
		if l := q.Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
				opts.Limit = n
			}
		}
		if o := q.Get("offset"); o != "" {
			if n, err := strconv.Atoi(o); err == nil && n >= 0 {
				opts.Offset = n
			}
		}
		if k := q.Get("kyc_level"); k != "" {
			if n, err := strconv.Atoi(k); err == nil {
				opts.KycLvl = n
			}
		}
		users, err := d.UserSvc.ListUsersOpts(r.Context(), opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		total, err := d.UserSvc.CountUsers(r.Context(), opts)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]*user.PublicUser, len(users))
		for i, u := range users {
			out[i] = u.ToPublic()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"users":  out,
			"total":  total,
			"limit":  opts.Limit,
			"offset": opts.Offset,
		})
	}
}

// adminGetUserHandler handles GET /api/v1/admin/users/{id}
func adminGetUserHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		u, err := d.UserSvc.GetUser(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeJSON(w, http.StatusOK, u.ToPublic())
	}
}

// adminSetUserRoleHandler handles POST /api/v1/admin/users/{id}/role
func adminSetUserRoleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
		auditLogFail(d, r, "user.set_role", "user", nil, idStr, nil, err)
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var body struct {
			Role string `json:"role"`
		}
		if err := decodeJSON(r, &body); err != nil {
		auditLogFail(d, r, "user.set_role", "user", &id, body.Role, map[string]any{"step": "decode_json"}, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Role != "user" && body.Role != "admin" {
		auditLogFail(d, r, "user.set_role", "user", &id, body.Role, map[string]any{"attempted_role": body.Role}, errors.New("invalid role"))
			writeError(w, http.StatusBadRequest, "role must be 'user' or 'admin'")
			return
		}
		if err := d.UserSvc.SetUserRole(r.Context(), id, body.Role); err != nil {
		auditLogFail(d, r, "user.set_role", "user", &id, body.Role, map[string]any{"attempted_role": body.Role}, err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		auditLogAdmin(d, r, audit.LogEntry{
			Action:      "user.set_role",
			TargetType:  "user",
			TargetID:    &id,
			TargetLabel: body.Role,
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "role": body.Role})
	}
}

// adminListWithdrawalsHandler handles GET /api/v1/admin/withdrawals
func adminListWithdrawalsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		wd, err := d.ChainWatcherSvc.ListAllWithdrawals(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, wd)
	}
}

// adminListDepositsHandler handles GET /api/v1/admin/deposits
func adminListDepositsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		deps, err := d.ChainWatcherSvc.ListAllDeposits(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, deps)
	}
}

// adminListOrdersHandler handles GET /api/v1/admin/orders
func adminListOrdersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		orders, err := d.TradingSvc.ListAllOrders(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, orders)
	}
}

// adminStatsHandler handles GET /api/v1/admin/stats
func adminStatsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := d.UserSvc.GetAdminStats(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Add more stats
		wdStats, _ := d.ChainWatcherSvc.GetWithdrawStats(r.Context())
		depStats, _ := d.ChainWatcherSvc.GetDepositStats(r.Context())
		orderStats, _ := d.TradingSvc.GetOrderStats(r.Context())
		for k, v := range wdStats {
			stats[k] = v
		}
		for k, v := range depStats {
			stats[k] = v
		}
		for k, v := range orderStats {
			stats[k] = v
		}
		writeJSON(w, http.StatusOK, stats)
	}
}


// adminListHeldWithdrawalsHandler handles GET /api/v1/admin/withdrawals/held
func adminListHeldWithdrawalsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wd, err := d.ChainWatcherSvc.ListHeldWithdrawals(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, wd)
	}
}

// adminApproveHeldHandler handles POST /api/v1/admin/withdrawals/{id}/approve-hold
func adminApproveHeldHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
		auditLogFail(d, r, "withdrawal.approve_hold", "withdrawal", nil, idStr, nil, err)
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := d.ChainWatcherSvc.ApproveHeldWithdrawal(r.Context(), id); err != nil {
		auditLogFail(d, r, "withdrawal.approve_hold", "withdrawal", &id, idStr, nil, err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
	}
}

// adminRejectHeldHandler handles POST /api/v1/admin/withdrawals/{id}/reject-hold
func adminRejectHeldHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
		auditLogFail(d, r, "withdrawal.reject_hold", "withdrawal", nil, idStr, nil, err)
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		_ = decodeJSON(r, &body)
		if err := d.ChainWatcherSvc.RejectHeldWithdrawal(r.Context(), id, body.Reason); err != nil {
		auditLogFail(d, r, "withdrawal.reject_hold", "withdrawal", &id, idStr, map[string]any{"reason": body.Reason}, err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
	}
}

// adminSetUserPasswordHandler handles POST /api/v1/admin/users/{id}/password
func adminSetUserPasswordHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
		auditLogFail(d, r, "user.reset_password", "user", nil, idStr, nil, err)
		auditLogFail(d, r, "user.reset_password", "user", nil, idStr, nil, err)
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var body struct {
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &body); err != nil {
		auditLogFail(d, r, "user.reset_password", "user", &id, idStr, nil, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Password == "" {
		auditLogFail(d, r, "user.reset_password", "user", &id, idStr, map[string]any{"reason": "empty password"}, errors.New("password required"))
			writeError(w, http.StatusBadRequest, "password required")
			return
		}
		if err := d.UserSvc.SetUserPassword(r.Context(), id, body.Password); err != nil {
		auditLogFail(d, r, "user.reset_password", "user", &id, idStr, nil, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		d.Log.Info("admin reset password", "user_id", id, "by", userIDFromContext(r.Context()))
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// adminListRiskEventsHandler handles GET /api/v1/admin/risk-events
func adminListRiskEventsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		events, err := d.RiskSvc.ListRiskEvents(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}

// adminGetUserRiskHandler handles GET /api/v1/admin/users/{id}/risk
func adminGetUserRiskHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		score, err := d.RiskSvc.GetUserRiskScore(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"user_id": id,
			"risk_score": score,
		})
	}
}


// adminHotWalletHandler returns hot wallet addresses per chain from driver + vault.
// Useful for verifying Vault integration.
func adminHotWalletHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chainID := r.URL.Query().Get("chain")
		if chainID == "" {
			writeError(w, http.StatusBadRequest, "chain query param required")
			return
		}
		if d.ChainRegistry == nil {
			writeError(w, http.StatusServiceUnavailable, "chain registry not initialized")
			return
		}
		drv, ok := d.ChainRegistry.Get(chainID)
		if !ok {
			writeError(w, http.StatusNotFound, "chain not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"chain":       chainID,
			"hot_address": drv.GetHotAddress(),
			"has_signer":  fmt.Sprintf("%t", drv.HasSigner()),
		})
	}
}

// adminListAuditLogsHandler handles GET /api/v1/admin/audit-logs.
// Returns audit log entries with optional filters.
func adminListAuditLogsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.AuditSvc == nil {
			writeError(w, http.StatusServiceUnavailable, "audit service not initialized")
			return
		}
		f := audit.QueryFilter{}
		if v := r.URL.Query().Get("action"); v != "" {
			f.Action = v
		}
		if v := r.URL.Query().Get("target_type"); v != "" {
			f.TargetType = v
		}
		if v := r.URL.Query().Get("admin_id"); v != "" {
			if id, err := uuid.Parse(v); err == nil {
				f.AdminUserID = &id
			}
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				f.Limit = n
			}
		}
		entries, err := d.AuditSvc.Query(r.Context(), f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, entries)
	}
}
// adminVaultHealthHandler returns Vault connection status.
// Helps operators verify the integration is healthy.
func adminVaultHealthHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if d.VaultClient == nil {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"enabled":false,"status":"vault not configured"}`))
			return
		}
		err := d.VaultClient.Health(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(fmt.Sprintf(`{"enabled":true,"status":"unhealthy","error":%q}`, err.Error())))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"enabled":true,"status":"healthy"}`))
	}
}
