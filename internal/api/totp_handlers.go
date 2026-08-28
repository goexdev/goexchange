package api

import (
	"context"
	"time"
	"fmt"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/goexdev/goexchange/internal/audit"
	"github.com/goexdev/goexchange/internal/auth"
	"github.com/goexdev/goexchange/internal/notifier"
)

// 2FA handlers - all require authentication
//
// Endpoints:
// - POST /users/me/2fa/setup     - Generate new secret + QR code URL
// - POST /users/me/2fa/enable    - Verify code + enable 2FA + generate backup codes
// - POST /users/me/2fa/disable   - Verify current code + disable 2FA
// - GET  /users/me/2fa/status    - Check if 2FA is enabled, remaining backup codes
// - POST /users/me/2fa/backup-codes - Generate new backup codes (invalidates old)
// - POST /users/me/2fa/verify    - Verify a code (used during login flow)
// - POST /users/me/2fa/verify-backup - Verify a backup code (used during login)

func totpSetupHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := userUUIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := d.UserSvc.GetUser(r.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get user")
			return
		}

		setup, err := d.TOTPSvc.GenerateSecret(r.Context(), uid, user.Email)
		if err != nil {
			d.Log.Error("totp setup failed", "error", err, "user_id", uid)
			writeError(w, http.StatusInternalServerError, "setup failed")
			return
		}

		auditLogUserAction(d, r, "2fa.setup_initiated", uid, user.Email, true, nil)
		writeJSON(w, http.StatusOK, setup)
	}
}

func totpEnableHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := userUUIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var in struct {
			Code string `json:"code"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if len(in.Code) != 6 {
			writeError(w, http.StatusBadRequest, "code must be 6 digits")
			return
		}

		// Enable (also generates backup codes)
		codes, err := d.TOTPSvc.Enable(r.Context(), uid, in.Code)
		if err != nil {
			auditLogUserAction(d, r, "2fa.enable", uid, "", false, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		auditLogUserAction(d, r, "2fa.enable", uid, "", true, nil)

		// Send notification (background - don't block response)
		if d.Notifier != nil && d.NotifPrefs != nil &&
			d.NotifPrefs.ShouldSend(r.Context(), uid, notifier.Type2FAEnabled) {
			go func() {
				bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer bgCancel()
				ip, ua := getRequestMeta(r)
				if err := d.Notifier.SendNotificationWithEmail(bgCtx, uid, notifier.Type2FAEnabled,
					"Two-Factor Authentication Enabled",
					fmt.Sprintf("2FA has been enabled on your account from IP %s. If this wasn't you, please contact support immediately.", ip),
					map[string]any{
						"event":   "2fa_enabled",
						"ip":      ip,
						"user_agent": ua,
					}, d.NotifPrefs); err != nil {
					d.Log.Warn("2fa enable notification failed", "error", err)
				}
			}()
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":      true,
			"backup_codes": codes, // shown ONCE
			"message":      "2FA enabled successfully. Save these backup codes!",
		})
	}
}

func totpDisableHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := userUUIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var in struct {
			Code string `json:"code"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		if err := d.TOTPSvc.Disable(r.Context(), uid, in.Code); err != nil {
				auditLogUserAction(d, r, "2fa.disable", uid, "", false, err)
				if errors.Is(err, auth.Err2FANotEnabled) {
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}

		auditLogUserAction(d, r, "2fa.disable", uid, "", true, nil)

		// Send notification (disable is critical - user should know!)
		if d.Notifier != nil && d.NotifPrefs != nil &&
			d.NotifPrefs.ShouldSend(r.Context(), uid, notifier.Type2FADisabled) {
			go func() {
				bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer bgCancel()
				ip, ua := getRequestMeta(r)
				if err := d.Notifier.SendNotificationWithEmail(bgCtx, uid, notifier.Type2FADisabled,
					"⚠️ Two-Factor Authentication Disabled",
					fmt.Sprintf("2FA has been DISABLED on your account from IP %s. Your account is now less secure. If this wasn't you, please contact support immediately.", ip),
					map[string]any{
						"event":   "2fa_disabled",
						"ip":      ip,
						"user_agent": ua,
						"severity": "high",
					}, d.NotifPrefs); err != nil {
					d.Log.Warn("2fa disable notification failed", "error", err)
				}
			}()
		}

		writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
	}
}

func totpStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := userUUIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		enabled, err := d.TOTPSvc.IsEnabled(r.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "check failed")
			return
		}

		remaining, _ := d.TOTPSvc.RemainingBackupCodes(r.Context(), uid)

		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":              enabled,
			"backup_codes_remaining": remaining,
		})
	}
}

func totpRegenerateBackupCodesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := userUUIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// Require current TOTP code to regenerate (prevents abuse if session is stolen)
		var in struct {
			Code string `json:"code"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		if err := d.TOTPSvc.VerifyCode(r.Context(), uid, in.Code); err != nil {
			auditLogUserAction(d, r, "2fa.regenerate_backup", uid, "", false, err)
			writeError(w, http.StatusBadRequest, "invalid code")
			return
		}

		codes, err := d.TOTPSvc.GenerateBackupCodes(r.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "generate failed")
			return
		}

		auditLogUserAction(d, r, "2fa.regenerate_backup", uid, "", true, nil)
		writeJSON(w, http.StatusOK, map[string]any{
			"backup_codes": codes,
			"message":      "New backup codes generated. Old codes are now invalid.",
		})
	}
}

// totpVerifyHandler verifies a TOTP code during login (after password is verified).
// Returns success if code is valid.
func totpVerifyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := userUUIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var in struct {
			Code string `json:"code"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		if err := d.TOTPSvc.VerifyCode(r.Context(), uid, in.Code); err != nil {
			auditLogUserAction(d, r, "2fa.verify", uid, "", false, err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		auditLogUserAction(d, r, "2fa.verify", uid, "", true, nil)
		writeJSON(w, http.StatusOK, map[string]bool{"valid": true})
	}
}

// userUUIDFromContext extracts and parses user UUID from context.
func userUUIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	s := userIDFromContext(ctx)
	if s == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// auditLogUserAction is a simpler version of auditLogAdmin for user actions on themselves.
func auditLogUserAction(d Deps, r *http.Request, action string, userID uuid.UUID, email string, success bool, err error) {
	if d.AuditSvc == nil {
		return
	}
	entry := audit.LogEntry{
		Action:      action,
		TargetType:  "user",
		TargetID:    &userID,
		TargetLabel: email,
	}
	if success {
		entry.Status = "success"
	} else {
		entry.Status = "failure"
		if err != nil {
			entry.ErrorMsg = err.Error()
		}
	}
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	if idx := lastIndex(ip, ":"); idx > 0 {
		if ip[0] == '[' {
			ip = ip[1:idx-1]
		} else {
			ip = ip[:idx]
		}
	}
	entry.IP = ip
	entry.UserAgent = r.Header.Get("User-Agent")
	d.AuditSvc.Log(r.Context(), entry)
}

// Verify it's the auth package being imported properly
var _ = auth.Service{}

// getRequestMeta extracts IP and user agent from request for notifications.
func getRequestMeta(r *http.Request) (string, string) {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	if idx := lastIndex(ip, ":"); idx > 0 {
		if ip[0] == '[' {
			ip = ip[1:idx-1]
		} else {
			ip = ip[:idx]
		}
	}
	return ip, r.Header.Get("User-Agent")
}

// totpLoginCompleteHandler handles POST /api/v1/auth/2fa/complete
//
// Called after loginHandler returns requires_2fa=true.
// Client sends {temp_token, code} where code is TOTP or backup code.
//
// Returns full JWT token + user info.
func totpLoginCompleteHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			TempToken string `json:"temp_token"`
			Code       string `json:"code"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if in.TempToken == "" || in.Code == "" {
			writeError(w, http.StatusBadRequest, "missing temp_token or code")
			return
		}

		// Verify temp token (must have scope=2fa_login)
		claims, err := d.AuthSvc.VerifyToken(in.TempToken)
		if err != nil {
			d.Log.Warn("2fa login: invalid temp token", "error", err)
			writeError(w, http.StatusUnauthorized, "invalid or expired temp token")
			return
		}
		if claims.Scope != "2fa_login" {
			d.Log.Warn("2fa login: wrong scope", "scope", claims.Scope)
			writeError(w, http.StatusUnauthorized, "invalid token scope")
			return
		}

		uid, err := uuid.Parse(claims.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid user in token")
			return
		}

		// Try TOTP code first, then backup code
		verified := false
		usedBackup := false

		if err := d.TOTPSvc.VerifyCode(r.Context(), uid, in.Code); err == nil {
			verified = true
		} else {
			// Try backup code
			ok, err := d.TOTPSvc.UseBackupCode(r.Context(), uid, in.Code)
			if err != nil {
				auditLogUserAction(d, r, "2fa.login_failed", uid, "", false, err)
				writeError(w, http.StatusUnauthorized, "invalid 2FA code")
				return
			}
			if ok {
				verified = true
				usedBackup = true
			}
		}

		if !verified {
			auditLogUserAction(d, r, "2fa.login_failed", uid, "", false, errors.New("invalid code"))
			writeError(w, http.StatusUnauthorized, "invalid 2FA code")
			return
		}

		// Get user
		u, err := d.UserSvc.GetUser(r.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "user lookup failed")
			return
		}

		// Issue full token
		token, err := d.AuthSvc.IssueToken(u.ID.String())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Audit log
		auditLogUserAction(d, r, "2fa.login_success", uid, u.Email, true, nil)
		if usedBackup {
			auditLogUserAction(d, r, "2fa.backup_code_used", uid, u.Email, true, nil)

			// CRITICAL: Notify user when backup code is used (account may be compromised)
			if d.Notifier != nil && d.NotifPrefs != nil &&
				d.NotifPrefs.ShouldSend(r.Context(), uid, notifier.Type2FABackupUsed) {
				go func() {
					bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer bgCancel()
					ip, ua := getRequestMeta(r)

					// Count remaining backup codes
					remaining, _ := d.TOTPSvc.RemainingBackupCodes(bgCtx, uid)
					body := fmt.Sprintf("A backup code was used to log in from IP %s. You have %d backup code(s) remaining.", ip, remaining)
					if remaining == 0 {
						body += " You have NO backup codes left - generate new ones in Settings."
					}

					_ = d.Notifier.SendNotificationWithEmail(bgCtx, uid, notifier.Type2FABackupUsed,
						"🔑 Backup Code Used for Login",
						body,
						map[string]any{
							"event":            "2fa_backup_used",
							"ip":               ip,
							"user_agent":       ua,
							"remaining_codes":  remaining,
						}, d.NotifPrefs)
				}()
			}
		}

		// Always notify on 2FA login success (helps detect unauthorized access)
		if d.Notifier != nil && d.NotifPrefs != nil && !usedBackup &&
			d.NotifPrefs.ShouldSend(r.Context(), uid, notifier.Type2FALoginSuccess) {
			go func() {
				bgCtx, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer bgCancel()
				ip, _ := getRequestMeta(r)
				_ = d.Notifier.SendNotificationWithEmail(bgCtx, uid, notifier.Type2FALoginSuccess,
					"2FA Login Successful",
					fmt.Sprintf("Login with 2FA from IP %s.", ip),
					map[string]any{
						"event": "2fa_login",
						"ip":    ip,
					}, d.NotifPrefs)
			}()
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"token": token,
			"user":  u.ToPublic(),
			"used_backup_code": usedBackup,
		})
	}
}

// getNotifPrefsHandler handles GET /users/me/notif-prefs
// Returns the user's notification preferences.
func getNotifPrefsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := userUUIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		prefs, err := d.NotifPrefs.Get(r.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, prefs)
	}
}

// patchNotifPrefsHandler handles PATCH /users/me/notif-prefs
// Updates user's notification preferences (only whitelisted fields).
func patchNotifPrefsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := userUUIDFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var updates map[string]bool
		if err := decodeJSON(r, &updates); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		// Whitelist allowed fields
		allowedFields := map[string]bool{
			"notify_2fa_enabled":       true,
			"notify_2fa_disabled":      true,
			"notify_2fa_backup_used":   true,
			"notify_2fa_failed":        true,
			"notify_2fa_login_success": true,
			"notify_login":             true,
			"notify_withdrawal":        true,
			"notify_large_withdraw":    true,
			// Email preferences (separate from in-app)
			"email_2fa_enabled":        true,
			"email_2fa_disabled":       true,
			"email_2fa_backup_used":    true,
			"email_2fa_failed":         true,
			"email_2fa_login_success":  true,
			"email_login":              true,
			"email_withdrawal":         true,
			"email_large_withdraw":     true,
		}

		filtered := make(map[string]bool)
		for k, v := range updates {
			if allowedFields[k] {
				filtered[k] = v
			}
		}

		prefs, err := d.NotifPrefs.Update(r.Context(), uid, filtered)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, prefs)
	}
}
