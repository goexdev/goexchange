package nodeconn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)


func TestExtractMethod(t *testing.T) {
	// Helper test
	body := []byte(`{"jsonrpc":"2.0","id":"0","method":"get_info","params":{}}`)
	var req struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	if req.Method != "get_info" {
		t.Errorf("got %q want get_info", req.Method)
	}
}

func TestSignHMAC(t *testing.T) {
	secret := "test-secret"
	ts := "2024-01-01T00:00:00Z"
	body := []byte(`{"method":"test"}`)

	sig1 := signHMAC(secret, ts, body)
	sig2 := signHMAC(secret, ts, body)
	if sig1 != sig2 {
		t.Error("signHMAC not deterministic")
	}

	sig3 := signHMAC(secret, "different", body)
	if sig1 == sig3 {
		t.Error("signHMAC should differ for different ts")
	}
}

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.nodes == nil {
		t.Error("nodes map not initialized")
	}
}

func TestManagerCallUnknown(t *testing.T) {
	m := NewManager()
	_, err := m.Call(context.Background(), "unknown", "get_info", nil)
	if err == nil {
		t.Error("expected error for unknown node")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestManagerHealthCheck(t *testing.T) {
	m := NewManager()
	results := m.HealthCheck(context.Background())
	if results == nil {
		t.Error("expected non-nil results")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
