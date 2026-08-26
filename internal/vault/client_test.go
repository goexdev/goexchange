package vault

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_ConfigValidation(t *testing.T) {
	cases := []struct {
		name string
		addr string
		auth AuthMethod
	}{
		{"no address", "", NewStaticTokenAuth("token")},
		{"no auth", "http://x", nil},
	}
	for _, c := range cases {
		_, err := NewClient(c.addr, c.auth)
		if err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

func TestClient_Health(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/sys/health" {
			w.WriteHeader(200)
			w.Write([]byte(`{"initialized":true,"sealed":false}`))
		} else {
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	c, _ := NewClient(server.URL, NewStaticTokenAuth("test-token"))
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestClient_Health_Unreachable(t *testing.T) {
	c, _ := NewClient("http://127.0.0.1:1", NewStaticTokenAuth("test-token")) // invalid port
	err := c.Health(context.Background())
	if err == nil {
		t.Error("expected error for unreachable vault")
	}
}

func TestClient_GetSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"data":{"password":"secret123","user":"admin"}}}`))
	}))
	defer server.Close()

	c, _ := NewClient(server.URL, NewStaticTokenAuth("test-token"))
	data, err := c.GetSecret(context.Background(), "db/postgres")
	if err != nil {
		t.Fatal(err)
	}
	if data["password"] != "secret123" {
		t.Errorf("expected 'secret123', got %q", data["password"])
	}
	if data["user"] != "admin" {
		t.Errorf("expected 'admin', got %q", data["user"])
	}
}

func TestClient_GetSecret_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"errors":["not found"]}`))
	}))
	defer server.Close()

	c, _ := NewClient(server.URL, NewStaticTokenAuth("test-token"))
	_, err := c.GetSecret(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for 404")
	}
}

func TestClient_PutSecret(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("X-Vault-Token") == "" {
			t.Error("missing token header")
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	c, _ := NewClient(server.URL, NewStaticTokenAuth("test-token"))
	if err := c.PutSecret(context.Background(), "test/key", map[string]string{
		"foo": "bar",
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("server not called")
	}
}

func TestClient_CacheTTL(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Write([]byte(`{"data":{"data":{"key":"value1"}}}`))
	}))
	defer server.Close()

	c, _ := NewClient(server.URL, NewStaticTokenAuth("test-token"))
	c.SetCacheTTL(1 * time.Hour) // long cache

	//1 call to fetch
	_, _ = c.GetSecret(context.Background(), "test/path")
	//2nd call should be cached
	_, _ = c.GetSecret(context.Background(), "test/path")

	if callCount != 1 {
		t.Errorf("expected 1 call (cached), got %d", callCount)
	}

	// Invalidate and re-fetch
	c.InvalidateCache("test/path")
	_, _ = c.GetSecret(context.Background(), "test/path")

	if callCount != 2 {
		t.Errorf("expected 2 calls after invalidation, got %d", callCount)
	}
}

func TestClient_GetValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"data":{"key1":"value1","key2":"value2"}}}`))
	}))
	defer server.Close()

	c, _ := NewClient(server.URL, NewStaticTokenAuth("test-token"))

	v, err := c.GetValue(context.Background(), "test", "key1")
	if err != nil {
		t.Fatal(err)
	}
	if v != "value1" {
		t.Errorf("expected value1, got %q", v)
	}

	// Missing key
	_, err = c.GetValue(context.Background(), "test", "missing")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestClient_StripPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/path/key" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{"data":{"data":{}}}`))
	}))
	defer server.Close()

	c, _ := NewClient(server.URL, NewStaticTokenAuth("test-token"))
	// Should strip "secret/" prefix
	_, _ = c.GetSecret(context.Background(), "secret/path/key")
	_, _ = c.GetSecret(context.Background(), "path/key")
}
