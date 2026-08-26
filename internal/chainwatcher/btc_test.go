package chainwatcher

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

// Mock BTC RPC server for testing
func TestBTCDriver_Name(t *testing.T) {
	d, err := NewBTCDriver("http://127.0.0.1:18443", "user", "pass", "btc", "")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name() != "btc" {
		t.Errorf("expected name 'btc', got %s", d.Name())
	}
}

func TestBTCDriver_SpawnDeposit_NotSupported(t *testing.T) {
	d, _ := NewBTCDriver("http://127.0.0.1:18443", "user", "pass", "btc", "")
	err := d.SpawnDeposit(context.Background(), "u1", "BTC", "tx", decimal.NewFromInt(1))
	if err == nil {
		t.Error("expected error for SpawnDeposit on BTC driver")
	}
}

func TestBTCDriver_EmptyURL(t *testing.T) {
	_, err := NewBTCDriver("", "", "", "btc", "")
	if err == nil {
		t.Error("expected error for empty rpc_url")
	}
}
