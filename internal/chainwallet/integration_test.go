// Integration test for the full withdrawal flow.
package chainwallet

import (
	"github.com/btcsuite/btcd/chaincfg"
	"context"
	"testing"
)

// TestFullBTCFlowMocked tests BTC flow with mocks.
func TestFullBTCFlowMocked(t *testing.T) {
	mock := &mockBtcNode{
		utxos: []BtcUTXO{
			{TxID: "0000000000000000000000000000000000000000000000000000000000000001", Vout: 0, Value: 100000000},
		},
	}
	btcBuilder := NewBtcBuilder(&chaincfg.TestNet3Params, mock, 20)
	btcBuilder.SetHotAddress("tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx")

	mgr := NewManager(&mockSigner{
		signFunc: func(_ context.Context, _ string, txData []byte, _, _ string) (*SignedTx, error) {
			return &SignedTx{
				SignedHex: "mock_signed_" + string(txData),
				TxHash:    "mock_btc_tx_hash",
				PubKey:    "mock_btc_addr",
			}, nil
		},
	})
	mgr.RegisterBuilder(btcBuilder)
	mgr.RegisterBroadcaster(&btcMockBroadcaster{})

	_, err := mgr.Send(context.Background(), "bitcoin", "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx", "0.001", "", "wd-btc-1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
}

// TestFullETHFlowMocked tests ETH flow with mocks.
func TestFullETHFlowMocked(t *testing.T) {
	mock := &mockEthNode{
		nonce:       5,
		gasPriceWei: "20000000000",
		chainID:     "1",
	}
	ethBuilder := NewEthBuilder(nil, mock)
	ethBuilder.SetHotAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f0fA0A")

	mgr := NewManager(&mockSigner{
		signFunc: func(_ context.Context, _ string, txData []byte, _, _ string) (*SignedTx, error) {
			return &SignedTx{
				SignedHex: "0xmock" + string(txData),
				TxHash:    "0xmocketh",
				PubKey:    "0xmockaddr",
			}, nil
		},
	})
	mgr.RegisterBuilder(ethBuilder)
	mgr.RegisterBroadcaster(&ethMockBroadcaster{})

	_, err := mgr.Send(context.Background(), "ethereum", "0x742d35Cc6634C0532925a3b844Bc9e7595f0fA0A", "0.1", "", "wd-eth-1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
}

// Mock helper types

type mockSigner struct {
	signFunc func(ctx context.Context, chain string, txData []byte, withdrawalID, userID string) (*SignedTx, error)
}

func (m *mockSigner) Sign(ctx context.Context, chain string, txData []byte, withdrawalID, userID string) (*SignedTx, error) {
	if m.signFunc != nil {
		return m.signFunc(ctx, chain, txData, withdrawalID, userID)
	}
	return &SignedTx{SignedHex: string(txData), TxHash: "default_mock"}, nil
}

type mockBroadcaster struct {
	calls []broadcastCall
}

type broadcastCall struct {
	chain     string
	signedHex string
}

type btcMockBroadcaster struct{}

func (b *btcMockBroadcaster) Broadcast(_ context.Context, _, _ string) (string, error) {
	return "mock_btc_broadcast_tx_hash", nil
}

type ethMockBroadcaster struct{}

func (b *ethMockBroadcaster) Broadcast(_ context.Context, _, _ string) (string, error) {
	return "0xmockethbroadcast", nil
}
