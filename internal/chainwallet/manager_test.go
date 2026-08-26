package chainwallet

import (
	"context"
	"testing"
)

func TestManager(t *testing.T) {
	// Test that the manager can be created
	m := NewManager(NewLocalSigner())
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.builders == nil {
		t.Error("builders not initialized")
	}
}


func TestLocalSigner(t *testing.T) {
	s := NewLocalSigner()
	signed, err := s.Sign(context.Background(), "bitcoin", []byte("raw_tx_data"), "wd-123", "user-456")
	if err != nil {
		t.Fatal(err)
	}
	if signed.SignedHex != "raw_tx_data" {
		t.Errorf("expected raw_tx_data, got %q", signed.SignedHex)
	}
	if signed.PubKey != "LOCAL_SIGNER" {
		t.Errorf("expected LOCAL_SIGNER, got %q", signed.PubKey)
	}
}

func TestNodeBroadcasterNoEndpoint(t *testing.T) {
	b, err := NewNodeBroadcaster("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Broadcast(context.Background(), "bitcoin", "deadbeef")
	if err == nil {
		t.Error("expected error for no endpoint")
	}
}

func TestRegisterBuilder(t *testing.T) {
	m := NewManager(NewLocalSigner())

	// Register a mock builder
	m.RegisterBuilder(&mockBuilder{chain: "test"})

	_, ok := m.builders["test"]
	if !ok {
		t.Error("builder not registered")
	}
}

type mockBuilder struct {
	chain string
}

func (b *mockBuilder) ChainName() string { return b.chain }

func (b *mockBuilder) Build(_ context.Context, _, _, _ string) (*UnsignedTx, error) {
	return &UnsignedTx{
		Chain:  b.chain,
		TxData: []byte("mock_tx_data"),
	}, nil
}
