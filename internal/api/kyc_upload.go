package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/goexdev/goexchange/internal/uploads"
)

// kycUploadHandler handles POST /api/v1/users/me/kyc/upload
// Accepts multipart/form-data with a "file" and a "type" field (front|back|selfie).
// Returns the relative file path which the client should then submit via KYC form.
func kycUploadHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContextUUID(r.Context())
		if userID == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// Limit request body to MaxFileSize + headers overhead
		r.Body = http.MaxBytesReader(w, r.Body, uploads.MaxFileSize+4096)

		if err := r.ParseMultipartForm(6 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart form or file too large")
			return
		}

		docType := r.FormValue("type")
		if docType != "front" && docType != "back" && docType != "selfie" {
			writeError(w, http.StatusBadRequest, "type must be front, back, or selfie")
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing file field")
			return
		}
		defer file.Close()

		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		rel, savedType, err := d.UploadStore.Save(uploads.CategoryKYC, contentType, file)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}

			// NEW-H1: record the (user, file) ownership so kycUserFileHandler
			// can authorize later downloads. Without this table, an attacker
			// who guessed a 32-char hex filename could pull the bytes from
			// the auth'd endpoint.
			if _, err := d.Pool.Exec(r.Context(),
				`INSERT INTO kyc_files (user_id, file_path) VALUES ($1, $2)
				 ON CONFLICT (file_path) DO UPDATE SET user_id = EXCLUDED.user_id`,
				userID, rel,
			); err != nil {
				// Best-effort: remove the orphan file before surfacing the error.
				if abs, perr := d.UploadStore.Path(rel); perr == nil {
					_ = os.Remove(abs)
				}
				d.Log.Error("kyc_files ownership record failed", "error", err, "user_id", userID, "path", rel)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}

		d.Log.Info("KYC document uploaded",
			"user_id", userID,
			"type", docType,
			"path", rel,
			"mime", savedType,
			"size", header.Size,
		)

		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		host := r.Host
		if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
			host = fwd
		}
		// NEW-L1: return a full URL so the SPA can render the image
		// without needing to guess the origin / scheme / port.
		publicURL := scheme + "://" + host + "/api/v1/users/me/kyc/files/" + filepath.Base(rel)

		writeJSON(w, http.StatusOK, map[string]any{
			"path":         rel,
			"url":          publicURL,
			"content_type": savedType,
			"doc_type":     docType,
		})
	}
}

// kycAdminDownloadHandler handles GET /api/v1/admin/kyc/{submission_id}/{doc_type}
// Admin only - serves KYC documents for review.
func kycAdminDownloadHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		submissionIDStr := chi.URLParam(r, "id")
		docType := chi.URLParam(r, "type")

		if docType != "front" && docType != "back" && docType != "selfie" {
			writeError(w, http.StatusBadRequest, "invalid doc type")
			return
		}

		submissionID, err := uuid.Parse(submissionIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}

		// Lookup file path
		var filePath *string
		err = d.Pool.QueryRow(r.Context(),
			`SELECT CASE WHEN $2 = 'front' THEN doc_front_path
			             WHEN $2 = 'back' THEN doc_back_path
			             WHEN $2 = 'selfie' THEN selfie_path
			        END
			 FROM kyc_submissions WHERE id = $1`,
			submissionID, docType).Scan(&filePath)
		if err != nil {
			writeError(w, http.StatusNotFound, "submission not found")
			return
		}
		if filePath == nil || *filePath == "" {
			writeError(w, http.StatusNotFound, "file not uploaded")
			return
		}

		abs, err := d.UploadStore.Path(*filePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "invalid file path")
			return
		}

		// Open and serve file
		f, err := os.Open(abs)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not on disk")
			return
		}
		defer f.Close()

		// Set content type from extension
		ext := filepath.Ext(abs)
		switch ext {
		case ".jpg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".webp":
			w.Header().Set("Content-Type", "image/webp")
		case ".pdf":
			w.Header().Set("Content-Type", "application/pdf")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}

		// Log access
		d.Log.Info("KYC document accessed",
			"admin", userIDFromContext(r.Context()),
			"submission_id", submissionID,
			"doc_type", docType,
			"path", *filePath,
		)

		http.ServeFile(w, r, abs)
	}
}

// kycUserFileHandler serves a KYC document for the *owner* of the file.
// URL: GET /api/v1/users/me/kyc/files/{file}
//
// Only the authenticated user can download their own KYC file. Any
// other user (or anonymous) gets 403 — even if they know the filename.
// This replaces the old anonymous /kyc/<hash>.png static serve which
// leaked any KYC document the URL was guessed for (NEW-H1 from the
// 2026-08-28 v0.3 audit).
//
// We don't persist (user_id, file) associations in the database yet;
// while the file name is a 32-char random hex it is unguessable, but
// defence-in-depth adds an ownership check via the most recent upload
// row in kyc_submissions.
func kycUserFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContextUUID(r.Context())
		if userID == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		filename := chi.URLParam(r, "file")
		if filename == "" {
			writeError(w, http.StatusBadRequest, "missing file name")
			return
		}
		// The filename is "<32-char hex>.<ext>" — defence-in-depth
		// against path traversal by rejecting anything that isn't
		// word characters and a single dot.
		if !safeKycFilename(filename) {
			writeError(w, http.StatusBadRequest, "invalid file name")
			return
		}

		// Ownership check: this filename must appear in one of the
		// authenticated user's kyc_submissions rows. We accept any
		// matching row (any submission they own) so re-uploads don't
		// invalidate older URLs.
		var owned bool
		err := d.Pool.QueryRow(r.Context(),
			`SELECT EXISTS(
				SELECT 1 FROM kyc_files
				WHERE file_path = $1 AND user_id = $2
			)`,
			"kyc/"+filename, userID,
		).Scan(&owned)
		if err != nil {
			d.Log.Error("kyc ownership check failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !owned {
			writeError(w, http.StatusForbidden, "not your file")
			return
		}

		abs, err := d.UploadStore.Path("kyc/" + filename)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "invalid path")
			return
		}
		f, err := os.Open(abs)
		if err != nil {
			writeError(w, http.StatusNotFound, "file not on disk")
			return
		}
		defer f.Close()

		ext := filepath.Ext(abs)
		switch ext {
		case ".jpg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".webp":
			w.Header().Set("Content-Type", "image/webp")
		case ".pdf":
			w.Header().Set("Content-Type", "application/pdf")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		d.Log.Info("KYC document served to owner",
			"user_id", userID, "file", filename,
		)
		http.ServeFile(w, r, abs)
	}
}

// safeKycFilename restricts the path component to the character set
// used by UploadStore.randomName (hex digits) + one dot + an extension.
// No slashes, no `..`, no path traversal.
func safeKycFilename(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if (c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	// at most one dot
	dot := 0
	for _, c := range s {
		if c == '.' {
			dot++
		}
	}
	return dot <= 1
}
