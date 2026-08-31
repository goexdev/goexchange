package api

import (
	"fmt"
	"strings"
	"context"
	"time"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/goexdev/goexchange/internal/audit"
	"github.com/goexdev/goexchange/internal/notifier"
	"github.com/goexdev/goexchange/internal/user"
)

// ctxKey is the type for context keys (unexported to prevent collision).
type ctxKey int

const (
	userIDKey ctxKey = iota
)

// withUserID stores a user ID in the context.
func withUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// userIDFromContext returns the user ID from the context.
func userIDFromContext(ctx context.Context) string {
	s, _ := ctx.Value(userIDKey).(string)
	return s
}

// userIDFromContextUUID returns the user ID as uuid.UUID.
func userIDFromContextUUID(ctx context.Context) uuid.UUID {
	s, _ := ctx.Value(userIDKey).(string)
	id, _ := uuid.Parse(s)
	return id
}

// registerRequest is the JSON body for POST /users/register.
type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func registerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
			return
		}
		u, err := d.UserSvc.Register(r.Context(), user.RegisterInput{
			Email:    req.Email,
			Password: req.Password,
		})
		if err != nil {
			status := "failure"
			if err == user.ErrEmailTaken {
				status = "warning" // Not really a failure, but worth recording
			}
			auditLogAdmin(d, r, audit.LogEntry{
				Action:      "user.register",
				TargetType:  "user",
				TargetLabel: req.Email,
				Details:     map[string]any{"error_type": err.Error()},
				Status:      status,
				ErrorMsg:    err.Error(),
			})
			switch err {
			case user.ErrInvalidEmail:
				writeError(w, http.StatusBadRequest, "invalid email")
			case user.ErrWeakPassword:
				writeError(w, http.StatusBadRequest, "password too weak")
			case user.ErrEmailTaken:
				writeError(w, http.StatusConflict, "email already registered")
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		// Success
		registerUID := u.ID
		auditLogAdmin(d, r, audit.LogEntry{
			Action:      "user.register",
			TargetType:  "user",
			AdminUserID: &registerUID,
			AdminEmail:  u.Email,
			TargetID:    &u.ID,
			TargetLabel: u.Email,
		})
		// Auto-bootstrap wallet (10000 USDT)
		if err := d.WalletSvc.BootstrapNewUser(r.Context(), u.ID); err != nil {
			d.Log.Warn("bootstrap wallet failed", "user_id", u.ID, "error", err)
		}
		// Generate a verify-token, queue the email, and tell the
		// caller to check their inbox. We deliberately do NOT
		// issue a JWT here — the user must click the link before
		// they can log in. This is the new flow shipped by
		// migration 0028.
		sendVerifyEmail(d, r, u.ID, u.Email)
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"user":                        u.ToPublic(),
			"requires_email_verification": true,
			"message":                     "Account created. Check your email to verify.",
		})
	}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}


func loginHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		ip := stripPort(r.RemoteAddr)
		ua := r.UserAgent()

		// Check lockout
		if locked, _ := d.UserSvc.IsLockedOut(r.Context(), req.Email, 5, 15); locked {
			_ = d.RiskSvc.RecordLogin(r.Context(), req.Email, nil, ip, ua, false, "locked_out")
			auditLogAdmin(d, r, audit.LogEntry{
				Action:      "user.login",
				TargetType:  "user",
				TargetLabel: req.Email,
				Details:     map[string]any{"reason": "locked_out"},
				Status:      "failure",
				ErrorMsg:    "too many failed attempts, account locked",
			})
			writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
			return
		}

		u, err := d.UserSvc.Login(r.Context(), user.LoginInput{
			Email:    req.Email,
			Password: req.Password,
		})
		if err != nil {
			reason := "invalid_credentials"
			if err != user.ErrInvalidCredentials {
				reason = err.Error()
			}
			_ = d.RiskSvc.RecordLogin(r.Context(), req.Email, nil, ip, ua, false, reason)
			auditLogAdmin(d, r, audit.LogEntry{
				Action:      "user.login",
				TargetType:  "user",
				TargetLabel: req.Email,
				Details:     map[string]any{"reason": reason},
				Status:      "failure",
				ErrorMsg:    reason,
			})
			if err == user.ErrInvalidCredentials {
				writeError(w, http.StatusUnauthorized, "invalid email or password")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Success
		_ = d.RiskSvc.RecordLogin(r.Context(), req.Email, &u.ID, ip, ua, true, "")
		_ = d.RiskSvc.RecordKnownIP(r.Context(), u.ID, ip)
		uid := u.ID
		auditLogAdmin(d, r, audit.LogEntry{
			Action:      "user.login",
			TargetType:  "user",
			AdminUserID: &uid,
			AdminEmail:  u.Email,
			TargetID:    &u.ID,
			TargetLabel: u.Email,
		})

		// Compute risk score (internal use only — never expose to client)
		score, _ := d.RiskSvc.ComputeLoginScore(r.Context(), u.ID, req.Email, ip)
		_ = d.UserSvc.ClearLoginAttempts(r.Context(), req.Email)
		if score != nil {
			_ = d.RiskSvc.UpdateUserRiskScore(r.Context(), u.ID, score.Score)
			_ = d.RiskSvc.RecordEvent(r.Context(), u.ID, "LOGIN", score, map[string]interface{}{
				"ip": ip, "user_agent": ua,
			})
		}

		// Gate: a user must have verified their email before we
		// issue any token. Same generic message regardless of
		// reason (unverified vs. unknown email vs. wrong password)
		// so an attacker cannot enumerate accounts by status.
		if !u.EmailVerified {
			auditLogAdmin(d, r, audit.LogEntry{
				Action:      "user.login.unverified",
				TargetType:  "user",
				AdminUserID: &uid,
				AdminEmail:  u.Email,
				TargetID:    &u.ID,
				TargetLabel: u.Email,
				Details:     map[string]any{"reason": "email_not_verified"},
			})
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"requires_email_verification": true,
				"message": "verify your email before signing in. Check your inbox or use resend-verification.",
			})
			return
		}

		// Check if user has 2FA enabled
		twoFAEnabled, err := d.TOTPSvc.IsEnabled(r.Context(), u.ID)
		if err != nil {
			d.Log.Warn("2fa check failed", "error", err, "user_id", u.ID)
			// Continue with normal login (fail open for availability)
			twoFAEnabled = false
		}

		if twoFAEnabled {
			// Issue temp token (5 min, scope=2fa_login)
			tempToken, err := d.AuthSvc.IssueTempToken(u.ID.String())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			auditLogAdmin(d, r, audit.LogEntry{
				Action:      "user.login.2fa_required",
				TargetType:  "user",
				TargetID:    &u.ID,
				TargetLabel: u.Email,
			})
			// risk_score / risk_factors intentionally NOT in the response —
			// exposing them lets an attacker tune behaviour to slip past
			// the controls (H3 from the 2026-08-28 audit).
			resp := map[string]interface{}{
				"requires_2fa": true,
				"temp_token":   tempToken,
				"user":         u.ToPublic(),
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		// No 2FA - issue full token
		token, err := d.AuthSvc.IssueToken(u.ID.String())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp := map[string]interface{}{
			"user":  u.ToPublic(),
			"token": token,
		}

		// Trigger: risky login notification (high risk).
		// risk_score / factors are intentionally NOT exposed to the client —
		// the score is still used here for the in-app + email notification.
		if score != nil && score.Score >= 20 {
			go func(uid uuid.UUID, sc int, factors map[string]int) {
				bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer bgCancel()
				d.Notifier.SendNotification(bgCtx, uid, notifier.TypeLoginRisk,
					"Unusual Login Activity",
					fmt.Sprintf("We detected a login with elevated risk (score: %d). If this wasn't you, please change your password.", sc),
					map[string]any{"risk_score": sc, "factors": factors})
			}(u.ID, score.Score, score.Factors)
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func meHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := d.UserSvc.GetUser(r.Context(), userIDFromContextUUID(r.Context()))
		if err != nil {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeJSON(w, http.StatusOK, u.ToPublic())
	}
}

// submitKYCHandler handles POST /api/v1/users/me/kyc
func submitKYCHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in user.SubmitKYCInput
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sub, err := d.UserSvc.SubmitKYC(r.Context(), userIDFromContextUUID(r.Context()), in)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, sub)
	}
}

// listKYCHandler handles GET /api/v1/users/me/kyc
func listKYCHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subs, err := d.UserSvc.ListKYCSubmissions(r.Context(), userIDFromContextUUID(r.Context()))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, subs)
	}
}

// getKycLimitHandler handles GET /api/v1/users/me/limit
func getKycLimitHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := d.UserSvc.GetUser(r.Context(), userIDFromContextUUID(r.Context()))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		limit := user.WithdrawLimitByKYC[u.KycLevel]
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"kyc_level":  u.KycLevel,
			"kyc_status": u.KycStatus,
			"limit_usdt": limit.String(),
		})
	}
}

// adminListPendingKYCHandler handles GET /api/v1/admin/kyc/pending
func adminListPendingKYCHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subs, err := d.UserSvc.ListPendingKYCSubmissions(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, subs)
	}
}

// adminListAllKYCHandler handles GET /api/v1/admin/kyc?status=...
func adminListAllKYCHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		subs, err := d.UserSvc.ListAllKYCSubmissions(r.Context(), status)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, subs)
	}
}

// adminApproveKYCHandler handles POST /api/v1/admin/kyc/{id}/approve
func adminApproveKYCHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
		auditLogFail(d, r, "kyc.approve", "kyc", nil, idStr, nil, err)
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var body struct {
			Note string `json:"note"`
		}
		_ = decodeJSON(r, &body)
		if err := d.UserSvc.ApproveKYC(r.Context(), id, body.Note); err != nil {
		auditLogFail(d, r, "kyc.approve", "kyc", &id, idStr, map[string]any{"note": body.Note}, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if userID, err := getKYCUserID(r.Context(), d, id); err == nil {
			d.Notifier.SendNotification(r.Context(), userID, "KYC_APPROVED",
				"KYC Approved - Level 1 Verified",
				"Your KYC has been approved. Your daily withdrawal limit is now 10000 USDT.",
				map[string]any{"kyc_id": id})
		}
		auditLogAdmin(d, r, audit.LogEntry{
			Action:      "kyc.approve",
			TargetType:  "kyc",
			TargetID:    &id,
			TargetLabel: id.String(),
			Details:     map[string]any{"note": body.Note},
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
	}
}

// adminRejectKYCHandler handles POST /api/v1/admin/kyc/{id}/reject
func adminRejectKYCHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
		auditLogFail(d, r, "kyc.reject", "kyc", nil, idStr, nil, err)
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if err := decodeJSON(r, &body); err != nil {
		auditLogFail(d, r, "kyc.reject", "kyc", &id, idStr, map[string]any{"reason": body.Reason}, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := d.UserSvc.RejectKYC(r.Context(), id, body.Reason); err != nil {
		auditLogFail(d, r, "kyc.reject", "kyc", &id, idStr, map[string]any{"reason": body.Reason}, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if userID, err := getKYCUserID(r.Context(), d, id); err == nil {
			d.Notifier.SendNotification(r.Context(), userID, "KYC_REJECTED",
				"KYC Rejected",
				fmt.Sprintf("Your KYC was rejected: %s. Please contact support.", body.Reason),
				map[string]any{"kyc_id": id})
		}
		auditLogAdmin(d, r, audit.LogEntry{
			Action:      "kyc.reject",
			TargetType:  "kyc",
			TargetID:    &id,
			TargetLabel: id.String(),
			Details:     map[string]any{"reason": body.Reason},
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
	}
}
// Force recompile


// stripPort removes ":port" from "host:port" addresses.
// Handles IPv4 (1.2.3.4:5678) and IPv6 ([::1]:5678 or [::1]:5678).
func stripPort(addr string) string {
	if addr == "" {
		return ""
	}
	// IPv6 with brackets: [::1]:5678 → ::1
	if strings.HasPrefix(addr, "[") {
		end := strings.Index(addr, "]")
		if end > 0 {
			return addr[1:end]
		}
	}
	// IPv4 or unbracketed IPv6: strip last colon
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// getKYCUserID returns the user_id for a KYC submission.
func getKYCUserID(ctx context.Context, d Deps, kycID uuid.UUID) (uuid.UUID, error) {
	var userID uuid.UUID
	err := d.Pool.QueryRow(ctx, `SELECT user_id FROM kyc_submissions WHERE id = $1`, kycID).Scan(&userID)
	return userID, err
}
