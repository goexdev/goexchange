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

		d.Log.Info("KYC document uploaded",
			"user_id", userID,
			"type", docType,
			"path", rel,
			"mime", savedType,
			"size", header.Size,
		)

		writeJSON(w, http.StatusOK, map[string]any{
			"path":         rel,
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
