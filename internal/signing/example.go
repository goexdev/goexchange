package signing

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
)

// SignMessage signs an EIP-191 personal message and returns (signature, address).
// Useful for testing or off-chain authentication.
//
// Example:
//
//	sig, addr, _ := SignMessage(testKey(), "hello")
//	fmt.Println(hex.EncodeToString(sig), addr)
func SignMessage(key *ecdsa.PrivateKey, message string) ([]byte, string, error) {
	hash := crypto.Keccak256([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)))
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		return nil, "", err
	}
	addr := crypto.PubkeyToAddress(key.PublicKey).Hex()
	return sig, addr, nil
}

// VerifyMessage verifies an EIP-191 signature and returns the recovered address.
func VerifyMessage(message string, signature []byte) (string, error) {
	hash := crypto.Keccak256([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)))
	pubKey, err := crypto.SigToPub(hash, signature)
	if err != nil {
		return "", err
	}
	return crypto.PubkeyToAddress(*pubKey).Hex(), nil
}

// Example usage of signing for EIP-191 personal_sign:
//
//	addr := crypto.PubkeyToAddress(key.PublicKey).Hex()
//	sig, _ := SignMessage(key, "Login to GoExchange")
//	// Send sig to server, server verifies with VerifyMessage
//	_ = hex.EncodeToString(sig) // 130-char hex string
//	_ = addr
var _ = hex.EncodeToString
var _ context.Context // keep context import for future use
