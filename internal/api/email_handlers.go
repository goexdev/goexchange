// Email verification + password reset handlers.
//
// Public surface (no auth required for these — they are exactly
// the moments when the user has lost/forgotten their credentials):
//
//   GET  /api/v1/auth/verify-email?token=...     confirm signup
//   POST /api/v1/auth/forgot-password            request reset link
//   POST /api/v1/auth/reset-password             consume reset link
//   POST /api/v1/auth/resend-verification        re-issue verify link
//
// On top of these, /users/register and /users/login were updated:
//   - register no longer issues a JWT until the user verifies.
//   - login refuses to issue a JWT unless the user is verified, and
//     returns {requires_email_verification: true} instead so the
//     web UI can route to a "verify your email" page.

package api

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/goexdev/goexchange/internal/notifier/templates"
	"github.com/goexdev/goexchange/internal/user"
	"github.com/google/uuid"
)

// =========================================================================
// Public URL helpers
// =========================================================================

// publicBaseURL is the absolute origin (scheme + host) that appears
// in transactional emails. It is read once from the PUBLIC_URL env
// var (set by deploy-fresh.sh) and falls back to https://goexchange.top.
//
// The link is built as:  <PUBLIC_URL>/<path>?token=<token>
// so a tester copying it into a real browser follows the same flow
// as the email recipient.
func publicBaseURL() string {
	if u := os.Getenv("PUBLIC_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://goexchange.top"
}

// shortLang picks a supported locale from the request's Accept-Language
// header, defaulting to "en" when no match. We keep the parser small
// and explicit instead of pulling in golang.org/x/text — only two
// languages ship today, and we can grow this list deliberately.
func shortLang(acceptLang string) string {
	if acceptLang == "" {
		return "en"
	}
	lower := strings.ToLower(acceptLang)
	switch {
	case strings.HasPrefix(lower, "zh"):
		return "zh"
	default:
		return "en"
	}
}

// sendVerifyEmail generates a token, persists it, and queues an
// email. Used by both the post-register path and the
// resend-verification handler. The HTTP request is passed in so we
// can pull the Accept-Language header for locale selection; the
// context comes from r.Context() for cancel-correctness.
func sendVerifyEmail(d Deps, r *http.Request, uid uuid.UUID, email string) {
	ctx := r.Context()
	plaintext, exp, err := d.UserSvc.CreateVerifyToken(ctx, uid)
	if err != nil {
		d.Log.Warn("create verify token failed", "user_id", uid, "error", err)
		return
	}
	url := publicBaseURL() + "/verify-email?token=" + plaintext
	subject, html, text, err := templates.Render(templates.KindVerifyEmail, templates.Params{
		Lang:      shortLang(r.Header.Get("Accept-Language")),
		Email:     email,
		ActionURL: url,
		TTL:       int(time.Until(exp).Minutes()),
	})
	if err != nil {
		d.Log.Warn("render verify template failed", "error", err)
		return
	}
	if err := d.Notifier.SendHTML(ctx, email, subject, html, text); err != nil {
		d.Log.Warn("queue verify email failed", "user_id", uid, "error", err)
	}
}

// =========================================================================
// Handlers
// =========================================================================

// verifyEmailHandler: GET /api/v1/auth/verify-email?token=...
func verifyEmailHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token")
		if tok == "" {
			writeError(w, http.StatusBadRequest, "missing token")
			return
		}
		uid, err := d.UserSvc.ConsumeVerifyToken(r.Context(), tok)
		if err != nil {
			switch {
			case errors.Is(err, user.ErrTokenNotFound):
				writeError(w, http.StatusBadRequest, "invalid token")
			case errors.Is(err, user.ErrTokenExpired):
				writeError(w, http.StatusGone, "token expired or already used")
			default:
				d.Log.Warn("verify email failed", "error", err)
				writeError(w, http.StatusInternalServerError, "verify failed")
			}
			return
		}
		// Issue the JWT now that the user is verified.
		token, err := d.AuthSvc.IssueToken(uid.String())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"verified":  true,
			"user_id":   uid.String(),
			"token":     token,
			"message":   "Email verified. You are now signed in.",
		})
	}
}

// forgotPasswordHandler: POST /api/v1/auth/forgot-password
// Body: { "email": "..." }
// Always returns 200 to avoid email enumeration — but if the email
// matches a real account, an email is queued.
func forgotPasswordHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "email required")
			return
		}
		plaintext, uid, exp, err := d.UserSvc.CreateResetTokenForEmail(r.Context(), req.Email)
		if err != nil {
			// Unknown email — still 200, no email queued. The
			// operator sees nothing in logs because this is
			// expected (people mistype addresses).
			writeJSON(w, http.StatusOK, map[string]string{"message": "if the email exists, a reset link has been sent"})
			return
		}
		url := publicBaseURL() + "/reset-password?token=" + plaintext
		subject, html, text, err := templates.Render(templates.KindResetPwd, templates.Params{
			Lang:      shortLang(r.Header.Get("Accept-Language")),
			Email:     req.Email,
			ActionURL: url,
			TTL:       int(time.Until(exp).Minutes()),
		})
		if err != nil {
			d.Log.Warn("render reset template failed", "error", err)
		} else if err := d.Notifier.SendHTML(r.Context(), req.Email, subject, html, text); err != nil {
			d.Log.Warn("queue reset email failed", "user_id", uid, "error", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "if the email exists, a reset link has been sent"})
	}
}

// resetPasswordHandler: POST /api/v1/auth/reset-password
// Body: { "token": "...", "password": "..." }
func resetPasswordHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if req.Token == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "token and password required")
			return
		}
		uid, err := d.UserSvc.ResetPasswordWithToken(r.Context(), req.Token, req.Password)
		if err != nil {
			switch {
			case errors.Is(err, user.ErrWeakPassword):
				writeError(w, http.StatusBadRequest, "password too weak")
			case errors.Is(err, user.ErrTokenNotFound):
				writeError(w, http.StatusBadRequest, "invalid token")
			case errors.Is(err, user.ErrTokenExpired):
				writeError(w, http.StatusGone, "token expired or already used")
			default:
				d.Log.Warn("reset password failed", "error", err)
				writeError(w, http.StatusInternalServerError, "reset failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"user_id": uid.String(),
			"message": "password updated",
		})
	}
}

// resendVerificationHandler: POST /api/v1/auth/resend-verification
// Body: { "email": "..." }
// Same anti-enumeration stance as forgot-password: 200 either way.
// Only re-issues a link if the email belongs to an unverified user.
func resendVerificationHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email string `json:"email"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		if req.Email == "" {
			writeError(w, http.StatusBadRequest, "email required")
			return
		}
		u, err := d.UserSvc.FindUserByEmail(r.Context(), req.Email)
		if err == nil && u != nil && !u.EmailVerified {
			sendVerifyEmail(d, r, u.ID, u.Email)
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "if the email exists and is unverified, a new link has been sent"})
	}
}

