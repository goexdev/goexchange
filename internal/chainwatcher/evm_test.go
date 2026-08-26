package chainwatcher

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

func TestEVMDriver_Name(t *testing.T) {
	d, err := NewEVMDriver("eth", "https://eth.llamarp.com", "ETH", 1, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name() != "eth" {
		t.Errorf("expected name 'eth', got %s", d.Name())
	}
}

func TestEVMDriver_SpawnDeposit_NotSupported(t *testing.T) {
	d, _ := NewEVMDriver("eth", "https://eth.llamarp.com", "ETH", 1, "", nil)
	err := d.SpawnDeposit(context.Background(), "u1", "ETH", "0x", decimal.NewFromInt(1))
	if err == nil {
		t.Error("expected error for SpawnDeposit on EVM driver")
	}
}

func TestEVMDriver_EmptyURL(t *testing.T) {
	_, err := NewEVMDriver("eth", "", "ETH", 1, "", nil)
	if err == nil {
		t.Error("expected error for empty rpc_url")
	}
}

func TestIsValidEVMAddress(t *testing.T) {
	cases := map[string]bool{
		"0x0000000000000000000000000000000000000000": true,
		"0x52908400098527886E0F7030069857D2E4169EE7": true,
		"0x52908400098527886E0F7030069857D2E4169EEZ": false, // invalid hex
		"52908400098527886E0F7030069857D2E4169EE7":   true, // no 0x prefix (EVM is lenient)
		"0x123":                                    false, // too short
		"":                                          false,
	}
	for addr, expected := range cases {
		got := IsValidEVMAddress(addr)
		if got != expected {
			t.Errorf("IsValidEVMAddress(%q) = %v, want %v", addr, got, expected)
		}
	}
}
