// Package signing provides transaction signing for blockchain operations.
//
// Abstraction layer over different signing backends:
//   - LocalSigner: signs locally with private key (DEV only)
//   - VaultSigner: signs via HashiCorp Vault (production)
//
// For BTC, the actual signing can be delegated to Bitcoin Core's wallet
// via signrawtransactionwithwallet RPC. For EVM, we sign locally with
// ECDSA and broadcast via eth_sendRawTransaction.
package signing

import (
	"context"
)

// Chain identifies which blockchain we're signing for.
type Chain string

const (
	ChainBTC  Chain = "btc"
	ChainETH  Chain = "eth"
	ChainBSC  Chain = "bsc"
	ChainSol  Chain = "sol"
)

// Signer is the interface for signing raw transactions.
type Signer interface {
	// Name returns the signer backend (e.g. "local", "vault").
	Name() string
	// Chain returns which chain this signer signs for.
	Chain() Chain
	// Address returns the signer's public address (hot wallet).
	Address() string
	// SignTransaction signs a transaction and returns the raw bytes to broadcast.
	// For BTC: hex of the signed raw transaction
	// For EVM: hex of signed RLP-encoded transaction
	SignTransaction(ctx context.Context, tx UnsignedTx) (SignedTx, error)
}

// UnsignedTx represents a transaction to sign.
type UnsignedTx struct {
	// Chain-specific raw data
	Data []byte // raw unsigned tx bytes (RLP for EVM, PSBT for BTC)
	// Metadata for signing
	Meta TxMeta
}

// TxMeta is chain-specific metadata.
type TxMeta struct {
	From     string // sender address
	To       string // recipient address
	Amount   string // human-readable amount
	Asset    string // asset symbol (BTC, ETH, etc.)
	Nonce    int64  // for EVM
	GasLimit uint64 // for EVM
	GasPrice string // for EVM (wei)
	ChainID  int64  // for EVM
}

// SignedTx is the result of signing.
type SignedTx struct {
	Raw       []byte // raw signed bytes (hex)
	TxHash    string // expected tx hash (may be empty if not computable)
	Signature string // signature string (debug only, never log this in prod!)
}

// ValidationError indicates a signing request is malformed.
type ValidationError struct{ Reason string }

func (e *ValidationError) Error() string { return "signing validation: " + e.Reason }
