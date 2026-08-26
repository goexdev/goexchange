package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// AuthMethod is the interface for Vault authentication methods.
//
// Implementations:
//   - StaticTokenAuth: simple token (DEV only)
//   - AppRoleAuth: role_id + secret_id login (recommended for production)
//   - KubernetesAuth: K8s ServiceAccount JWT login
type AuthMethod interface {
	Name() string
	GetToken(ctx context.Context) (string, error)
}

// StaticTokenAuth uses a fixed token (DEV/test only).
// NEVER use in production - use AppRole or Kubernetes auth.
type StaticTokenAuth struct {
	token string
}

func NewStaticTokenAuth(token string) *StaticTokenAuth {
	return &StaticTokenAuth{token: token}
}

func (a *StaticTokenAuth) Name() string { return "static" }
func (a *StaticTokenAuth) GetToken(_ context.Context) (string, error) {
	return a.token, nil
}

// AppRoleAuth uses HashiCorp AppRole auth with role_id + secret_id.
// Recommended for production. Supports automatic token renewal.
//
// Setup (one-time):
//
//	vault auth enable approle
//	vault write auth/approle/role/<role_name> token_policies="goexchange" token_ttl=1h
//
// Then read role_id and secret_id:
//
//	vault read auth/approle/role/<role_name>/role-id
//	vault write -f auth/approle/role/<role_name>/secret-id
type AppRoleAuth struct {
	address  string
	roleID   string
	secretID string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	tokenTTL  time.Duration
}

func NewAppRoleAuth(address, roleID, secretID string) (*AppRoleAuth, error) {
	if address == "" {
		return nil, fmt.Errorf("vault address required")
	}
	if roleID == "" {
		return nil, fmt.Errorf("role_id required")
	}
	if secretID == "" {
		return nil, fmt.Errorf("secret_id required")
	}
	return &AppRoleAuth{
		address:  strings.TrimRight(address, "/"),
		roleID:   roleID,
		secretID: secretID,
		tokenTTL: 1 * time.Hour,
	}, nil
}

func (a *AppRoleAuth) Name() string { return "approle" }

// GetToken returns a valid token, logging in fresh if needed.
func (a *AppRoleAuth) GetToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return cached if still valid (with 5min buffer)
	if a.token != "" && time.Until(a.expiresAt) > 5*time.Minute {
		return a.token, nil
	}

	// Login: POST /v1/auth/approle/login
	body, _ := json.Marshal(map[string]string{
		"role_id":   a.roleID,
		"secret_id": a.secretID,
	})
	req, err := http.NewRequestWithContext(ctx, "POST",
		a.address+"/v1/auth/approle/login",
		strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("approle login failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("approle login: %s - %s", resp.Status, string(respBody))
	}

	var loginResp struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(respBody, &loginResp); err != nil {
		return "", fmt.Errorf("decode approle response: %w", err)
	}

	a.token = loginResp.Auth.ClientToken
	ttl := a.tokenTTL
	if loginResp.Auth.LeaseDuration > 0 {
		ttl = time.Duration(loginResp.Auth.LeaseDuration) * time.Second
	}
	a.expiresAt = time.Now().Add(ttl)

	return a.token, nil
}

// SetSecretID updates the secret_id (for rotation).
func (a *AppRoleAuth) SetSecretID(secretID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.secretID = secretID
	a.token = "" // force refresh
}

// KubernetesAuth uses Kubernetes service account JWT for Vault auth.
// Recommended for K8s deployments - no static secrets to manage.
//
// Setup (one-time):
//
//	vault auth enable kubernetes
//	vault write auth/kubernetes/config ... 
//	vault write auth/kubernetes/role/<role_name> ... policies=goexchange
//
// Then mount the SA JWT at /var/run/secrets/kubernetes.io/serviceaccount/token
type KubernetesAuth struct {
	address string
	role    string
	jwtPath string
}

func NewKubernetesAuth(address, role string) (*KubernetesAuth, error) {
	if address == "" {
		return nil, fmt.Errorf("vault address required")
	}
	if role == "" {
		return nil, fmt.Errorf("k8s role name required")
	}
	return &KubernetesAuth{
		address: strings.TrimRight(address, "/"),
		role:    role,
		jwtPath: "/var/run/secrets/kubernetes.io/serviceaccount/token",
	}, nil
}

func (a *KubernetesAuth) Name() string { return "kubernetes" }

func (a *KubernetesAuth) GetToken(ctx context.Context) (string, error) {
	jwt, err := os.ReadFile(a.jwtPath)
	if err != nil {
		return "", fmt.Errorf("read k8s SA JWT: %w", err)
	}

	body, _ := json.Marshal(map[string]string{
		"role": string(jwt),
	})
	req, err := http.NewRequestWithContext(ctx, "POST",
		a.address+"/v1/auth/kubernetes/login",
		strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("k8s login failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("k8s login: %s - %s", resp.Status, string(respBody))
	}

	var loginResp struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(respBody, &loginResp); err != nil {
		return "", fmt.Errorf("decode k8s response: %w", err)
	}

	return loginResp.Auth.ClientToken, nil
}
