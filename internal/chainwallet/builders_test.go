package chainwallet

import (
	"context"
	"math/big"
	"testing"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/shopspring/decimal"
)

// mockBtcNode is a mock BTC node reader for tests.
type mockBtcNode struct {
	utxos       []BtcUTXO
	height      int64
	getUTXOsErr error
}

func (m *mockBtcNode) GetUTXOs(_ context.Context, _ string) ([]BtcUTXO, error) {
	if m.getUTXOsErr != nil {
		return nil, m.getUTXOsErr
	}
	return m.utxos, nil
}

func (m *mockBtcNode) GetBlockCount(_ context.Context) (int64, error) {
	return m.height, nil
}

// mockEthNode is a mock ETH node reader for tests.
type mockEthNode struct {
	nonce       uint64
	gasPriceWei string
	chainID     string
	getNonceErr error
}

func (m *mockEthNode) GetNonce(_ context.Context, _ string) (uint64, error) {
	if m.getNonceErr != nil {
		return 0, m.getNonceErr
	}
	return m.nonce, nil
}

func (m *mockEthNode) GetGasPrice(_ context.Context) (*big.Int, error) {
	gp := new(big.Int)
	gp.SetString(m.gasPriceWei, 10)
	return gp, nil
}

func (m *mockEthNode) GetChainID() *big.Int {
	cid := new(big.Int)
	cid.SetString(m.chainID, 10)
	return cid
}

func TestBtcBuilderChainName(t *testing.T) {
	b := NewBtcBuilder(&chaincfg.MainNetParams, &mockBtcNode{}, 20)
	if b.ChainName() != "bitcoin" {
		t.Errorf("got %q, want bitcoin", b.ChainName())
	}
}

func TestBtcBuilderNoUTXOs(t *testing.T) {
	mock := &mockBtcNode{utxos: nil}
	b := NewBtcBuilder(&chaincfg.MainNetParams, mock, 20)
	_, err := b.Build(context.Background(), "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", "0.001", "")
	if err == nil {
		t.Error("expected error for no UTXOs")
	}
}

func TestBtcBuilderUTXOSelection(t *testing.T) {
	utxos := []BtcUTXO{
		{TxID: "0000000000000000000000000000000000000000000000000000000000000001", Vout: 0, Value: 100000000},
		{TxID: "0000000000000000000000000000000000000000000000000000000000000002", Vout: 0, Value: 50000000},
	}
	mock := &mockBtcNode{utxos: utxos}
	b := NewBtcBuilder(&chaincfg.MainNetParams, mock, 20)

	selected, total := b.selectUTXOs(utxos, 120000000)
	if selected == nil {
		t.Fatal("expected UTXOs selected")
	}
	if total < 150000000 {
		t.Errorf("total: got %d, want >= 150000000", total)
	}
	if len(selected) < 1 {
		t.Error("expected at least one UTXO")
	}
}

func TestBtcToSatoshi(t *testing.T) {
	d, _ := decimal.NewFromString("0.001")
	sat, _ := btcToSatoshi(d)
	if sat != 100000 {
		t.Errorf("got %d, want 100000", sat)
	}

	d2, _ := decimal.NewFromString("1")
	sat2, _ := btcToSatoshi(d2)
	if sat2 != 100000000 {
		t.Errorf("got %d, want 100000000", sat2)
	}
}

func TestBtcBuilderEstimateSize(t *testing.T) {
	b := NewBtcBuilder(&chaincfg.MainNetParams, &mockBtcNode{}, 20)
	size := b.estimateSize(1, 2)
	if size < 100 {
		t.Errorf("size too small: %d", size)
	}
	if size > 300 {
		t.Errorf("size too large: %d", size)
	}
}

func TestBtcBuilderFeeRate(t *testing.T) {
	b := NewBtcBuilder(&chaincfg.MainNetParams, &mockBtcNode{}, 0)
	if b.feeRate != 20 {
		t.Errorf("default fee rate: got %d, want 20", b.feeRate)
	}

	b2 := NewBtcBuilder(&chaincfg.MainNetParams, &mockBtcNode{}, 50)
	if b2.feeRate != 50 {
		t.Errorf("custom fee rate: got %d, want 50", b2.feeRate)
	}
}

func TestEthBuilderChainName(t *testing.T) {
	mock := &mockEthNode{nonce: 5, gasPriceWei: "20000000000", chainID: "1"}
	b := NewEthBuilder(nil, mock)
	if b.ChainName() != "ethereum" {
		t.Errorf("got %q, want ethereum", b.ChainName())
	}
}

func TestEthBuilderGasPriceWei(t *testing.T) {
	mock := &mockEthNode{gasPriceWei: "20000000000"}
	b := NewEthBuilder(nil, mock)

	gp, err := b.getGasPriceWei(context.Background(), "50")
	if err != nil {
		t.Fatal(err)
	}
	expected := "50000000000"
	if gp.String() != expected {
		t.Errorf("got %s, want %s", gp.String(), expected)
	}
}

func TestEncodeBTCTxHex(t *testing.T) {
	hex := EncodeBTCTxHex(&UnsignedTx{TxData: []byte{0x01, 0x02, 0x03}})
	expected := "010203"
	if hex != expected {
		t.Errorf("got %s, want %s", hex, expected)
	}
}
