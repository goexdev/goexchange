// Package uploads handles file storage for KYC documents and other binary data.
//
// Files are stored on local filesystem under /root/goexchange/uploads/<category>/.
// In production this should be replaced with S3-compatible storage.
//
// Security:
// - Files are served via authenticated endpoints only
// - Path traversal prevention (only allow UUIDs as filenames)
// - MIME type validation
// - Size limits per category
package uploads

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// CategoryKYC stores KYC verification documents
	CategoryKYC = "kyc"

	// MaxFileSize is the maximum allowed file size (5 MB)
	MaxFileSize = 5 * 1024 * 1024

	// MaxImageSize is the maximum image dimensions (pixels)
	MaxImageSize = 4096
)

// AllowedMimeTypes per category.
// Maps category -> allowed MIME types.
var AllowedMimeTypes = map[string][]string{
	CategoryKYC: {
		"image/jpeg",
		"image/png",
		"image/webp",
		"application/pdf",
	},
}

// Store handles file storage operations.
type Store struct {
	BaseDir string
}

// New creates a new Store rooted at baseDir.
func New(baseDir string) (*Store, error) {
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("create base dir: %w", err)
	}
	for _, cat := range []string{CategoryKYC} {
		if err := os.MkdirAll(filepath.Join(baseDir, cat), 0700); err != nil {
			return nil, fmt.Errorf("create category dir %s: %w", cat, err)
		}
	}
	return &Store{BaseDir: baseDir}, nil
}

// Save stores data under category with a random filename.
// Returns the relative path (e.g. "kyc/abc123.jpg") and MIME type.
func (s *Store) Save(category string, contentType string, data io.Reader) (string, string, error) {
	if !isValidCategory(category) {
		return "", "", fmt.Errorf("invalid category: %s", category)
	}

	allowed, ok := AllowedMimeTypes[category]
	if !ok {
		return "", "", fmt.Errorf("no mime types configured for category: %s", category)
	}

	if !contains(allowed, contentType) {
		return "", "", fmt.Errorf("content type %q not allowed for category %q", contentType, category)
	}

	ext, err := extensionForContentType(contentType)
	if err != nil {
		return "", "", err
	}

	name, err := randomName()
	if err != nil {
		return "", "", err
	}
	filename := name + ext

	path := filepath.Join(s.BaseDir, category, filename)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return "", "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	limited := io.LimitReader(data, MaxFileSize+1)
	written, err := io.Copy(f, limited)
	if err != nil {
		_ = os.Remove(path)
		return "", "", fmt.Errorf("write file: %w", err)
	}
	if written > MaxFileSize {
		_ = os.Remove(path)
		return "", "", fmt.Errorf("file too large: max %d bytes", MaxFileSize)
	}

	rel := filepath.ToSlash(filepath.Join(category, filename))
	return rel, contentType, nil
}

// Path returns the absolute filesystem path for a relative path.
// Returns an error if the path attempts traversal.
func (s *Store) Path(rel string) (string, error) {
	if strings.Contains(rel, "..") {
		return "", errors.New("invalid path")
	}
	abs := filepath.Join(s.BaseDir, rel)
	cleaned := filepath.Clean(abs)
	if !strings.HasPrefix(cleaned, s.BaseDir) {
		return "", errors.New("path escapes base directory")
	}
	return cleaned, nil
}

// Delete removes a file by relative path.
func (s *Store) Delete(rel string) error {
	abs, err := s.Path(rel)
	if err != nil {
		return err
	}
	return os.Remove(abs)
}

// FileInfo describes a stored file.
type FileInfo struct {
	Path        string    `json:"path"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
}

// Stat returns information about a stored file.
func (s *Store) Stat(rel string) (*FileInfo, error) {
	abs, err := s.Path(rel)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	return &FileInfo{
		Path:        rel,
		ContentType: mime.TypeByExtension(filepath.Ext(rel)),
		Size:        st.Size(),
		ModTime:     st.ModTime(),
	}, nil
}

// ----- helpers -----

func extensionForContentType(ct string) (string, error) {
	switch ct {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/webp":
		return ".webp", nil
	case "application/pdf":
		return ".pdf", nil
	}
	return "", fmt.Errorf("no extension for %q", ct)
}

func randomName() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isValidCategory(c string) bool {
	for _, valid := range []string{CategoryKYC} {
		if c == valid {
			return true
		}
	}
	return false
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}