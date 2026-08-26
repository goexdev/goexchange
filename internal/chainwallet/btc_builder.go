// Package chainwallet - Bitcoin transaction builder.
package chainwallet

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/shopspring/decimal"
)

// BtcBuilder builds Bitcoin transactions.
type BtcBuilder struct {
	network *chaincfg.Params
	nodeRPC BtcNodeReader
	feeRate int64
	hotAddr string
}

// BtcNodeReader reads Bitcoin blockchain data via node-proxy.
type BtcNodeReader interface {
	GetUTXOs(ctx context.Context, address string) ([]BtcUTXO, error)
	GetBlockCount(ctx context.Context) (int64, error)
}

// BtcUTXO is an unspent Bitcoin transaction output.
type BtcUTXO struct {
	TxID  string
	Vout  uint32
	Value int64
}

// NewBtcBuilder creates a BTC tx builder.
func NewBtcBuilder(network *chaincfg.Params, nodeRPC BtcNodeReader, feeRate int64) *BtcBuilder {
	if feeRate == 0 {
		feeRate = 20
	}
	return &BtcBuilder{network: network, nodeRPC: nodeRPC, feeRate: feeRate}
}

// SetHotAddress sets the hot wallet address.
func (b *BtcBuilder) SetHotAddress(addr string) { b.hotAddr = addr }

// ChainName returns "bitcoin".
func (b *BtcBuilder) ChainName() string { return "bitcoin" }

// Build constructs an unsigned Bitcoin transaction.
func (b *BtcBuilder) Build(ctx context.Context, toAddress, amount, feeRate string) (*UnsignedTx, error) {
	amountDec, err := decimal.NewFromString(amount)
	if err != nil {
		return nil, fmt.Errorf("parse amount: %w", err)
	}
	if !amountDec.IsPositive() {
		return nil, fmt.Errorf("amount must be positive")
	}

	sourceAddr := b.hotAddr
	if sourceAddr == "" {
		sourceAddr, err = b.getHotAddress(ctx)
		if err != nil {
			return nil, fmt.Errorf("get hot address: %w", err)
		}
	}

	toAddr, err := btcutil.DecodeAddress(toAddress, b.network)
	if err != nil {
		return nil, fmt.Errorf("invalid destination address: %w", err)
	}
	fromAddr, err := btcutil.DecodeAddress(sourceAddr, b.network)
	if err != nil {
		return nil, fmt.Errorf("invalid source address: %w", err)
	}

	utxos, err := b.nodeRPC.GetUTXOs(ctx, sourceAddr)
	if err != nil {
		return nil, fmt.Errorf("get UTXOs: %w", err)
	}
	if len(utxos) == 0 {
		return nil, fmt.Errorf("no UTXOs for address %s", sourceAddr)
	}

	amountSat, err := btcToSatoshi(amountDec)
	if err != nil {
		return nil, fmt.Errorf("convert amount: %w", err)
	}

	selected, totalIn := b.selectUTXOs(utxos, amountSat)
	if selected == nil {
		return nil, fmt.Errorf("insufficient funds: need %d sat, have %d sat", amountSat, totalIn)
	}

	fee := int64(b.estimateSize(len(selected), 2)) * b.feeRate

	outputs := []*wire.TxOut{
		{Value: amountSat, PkScript: b.p2pkhScript(toAddr)},
	}
	change := totalIn - amountSat - fee
	if change > 546 {
		outputs = append(outputs, &wire.TxOut{
			Value: change,
			PkScript: b.p2pkhScript(fromAddr),
		})
	}

	tx := wire.NewMsgTx(wire.TxVersion)
	for _, u := range selected {
		txid, err := chainhash.NewHashFromStr(u.TxID)
		if err != nil {
			return nil, fmt.Errorf("invalid txid %s: %w", u.TxID, err)
		}
		txIn := wire.NewTxIn(wire.NewOutPoint(txid, u.Vout), nil, nil)
		txIn.Sequence = 0xffffffff
		tx.AddTxIn(txIn)
	}
	for _, out := range outputs {
		tx.AddTxOut(out)
	}

	var buf []byte
	w := newBytesWriter(&buf)
	if err := tx.Serialize(w); err != nil {
		return nil, fmt.Errorf("serialize tx: %w", err)
	}

	return &UnsignedTx{
		Chain:  "bitcoin",
		TxData: buf,
		TxHash: tx.TxHash().String(),
		Fee:    fmt.Sprintf("%d", fee),
	}, nil
}

func (b *BtcBuilder) getHotAddress(_ context.Context) (string, error) {
	return "", fmt.Errorf("hot address not configured")
}

func (b *BtcBuilder) selectUTXOs(utxos []BtcUTXO, targetAmount int64) ([]BtcUTXO, int64) {
	sorted := make([]BtcUTXO, len(utxos))
	copy(sorted, utxos)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].Value < sorted[j].Value {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var selected []BtcUTXO
	var total int64
	for _, u := range sorted {
		selected = append(selected, u)
		total += u.Value
		txSize := b.estimateSize(len(selected), 2)
		feeNeeded := int64(txSize) * b.feeRate
		if total >= targetAmount+feeNeeded {
			return selected, total
		}
	}
	return nil, total
}

func (b *BtcBuilder) estimateSize(numInputs, numOutputs int) int {
	return numInputs*148 + numOutputs*34 + 10
}

func (b *BtcBuilder) p2pkhScript(addr btcutil.Address) []byte {
	if script := addr.ScriptAddress(); script != nil {
		return script
	}
	return nil
}

func btcToSatoshi(d decimal.Decimal) (int64, error) {
	sat := d.Mul(decimal.NewFromInt(100000000))
	return sat.IntPart(), nil
}

type bytesWriter struct {
	buf *[]byte
}

func newBytesWriter(buf *[]byte) *bytesWriter {
	return &bytesWriter{buf: buf}
}

func (b *bytesWriter) Write(p []byte) (int, error) {
	*b.buf = append(*b.buf, p...)
	return len(p), nil
}

// EncodeBTCTxHex is a helper to encode the unsigned tx as hex.
func EncodeBTCTxHex(tx *UnsignedTx) string {
	return hex.EncodeToString(tx.TxData)
}
