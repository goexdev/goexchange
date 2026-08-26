package signing

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// LocalSigner signs EVM transactions locally with a hex-encoded private key.
// PRODUCTION WARNING: Do NOT use in production. - Private keys should be in Vault/Hardware/HSM.
type LocalSigner struct {
	chain    Chain
	address  common.Address
	key      *ecdsa.PrivateKey
	rpcChainID int64
}

// NewLocalSigner creates a signer from a hex private key (0x prefixed or not).
// Also requires the address (for verification).
func NewLocalSigner(chain Chain, privateKeyHex, address string, chainID int64) (*LocalSigner, error) {
	// Strip 0x prefix if present
	if len(privateKeyHex) >= 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}
	keyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid hex private key: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("private key must be 32 bytes, got %d", len(keyBytes))
	}
	key, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid ECDSA key: %w", err)
	}

	derivedAddr := crypto.PubkeyToAddress(key.PublicKey)
	expectedAddr := common.HexToAddress(address)
	if derivedAddr != expectedAddr {
		return nil, fmt.Errorf("private key does not match address: derived=%s expected=%s",
			derivedAddr.Hex(), expectedAddr.Hex())
	}

	return &LocalSigner{
		chain:     chain,
		address:   derivedAddr,
		key:       key,
		rpcChainID: chainID,
	}, nil
}

func (s *LocalSigner) Name() string { return "local" }
func (s *LocalSigner) Chain() Chain { return s.chain }
func (s *LocalSigner) Address() string { return s.address.Hex() }

// SignTransaction signs an EVM transaction.
// Expects tx.Data to be the RLP-encoded unsigned transaction bytes.
// For EVM, we need a chain-aware signer that constructs the typed tx.
// Note: This is a simplified version that signs the raw bytes.
//
// For production use a proper EIP-1559 / EIP-2930 / Legacy tx builder.
func (s *LocalSigner) SignTransaction(ctx context.Context, tx UnsignedTx) (SignedTx, error) {
	if len(tx.Data) == 0 {
		return SignedTx{}, &ValidationError{Reason: "empty tx data"}
	}

	// Sign the raw bytes using the local private key
	// crypto.SignBytes is not a real function - we use Sign for ECDSA signatures
	// For real EVM tx signing, we'd need to:
	// 1. Parse the unsigned tx into a Transaction struct
	// 2. Sign with the chain ID
	// 3. RLP encode the signed result
	//
	// For this demo, we use SignHash (the simpler version that hashes + signs)
	hash := crypto.Keccak256(tx.Data)
	sig, err := crypto.Sign(hash, s.key)
	if err != nil {
		return SignedTx{}, fmt.Errorf("sign failed: %w", err)
	}

	// sig is [R || S || V] (65 bytes)
	// For EVM, V = sig[64] + chainID * 2 + 35 (legacy)
	sig[64] = sig[64] + byte(s.rpcChainID*2+35)

	txHash := crypto.Keccak256Hash(sig).Hex()
	return SignedTx{
		Raw:       sig, // simplified - real implementation would RLP encode signed tx
		TxHash:    txHash,
		Signature: hex.EncodeToString(sig),
	}, nil
}

// SignHash signs an EIP-191 hash and returns the signature.
// Useful for signing messages, not transactions.
func (s *LocalSigner) SignHash(hash []byte) ([]byte, error) {
	return crypto.Sign(hash, s.key)
}


// PrivateKey returns the underlying *ecdsa.PrivateKey.
// Used by EVM driver for go-ethereum types integration.
// SECURITY: This exposes the raw key - only call from trusted internal code.
func (s *LocalSigner) PrivateKey() *ecdsa.PrivateKey {
	return s.key
}
