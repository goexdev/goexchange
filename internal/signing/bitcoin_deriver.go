package signing

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"errors"

	"golang.org/x/crypto/ripemd160"

	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/ethereum/go-ethereum/crypto"
)

// BitcoinDeriver implements Deriver for all Bitcoin-family chains (UTXO, secp256k1, base58).
//
// This single implementation works for ALL BTC forks by changing config:
//   BTC:    coin_type=0,  prefix=0x00 (mainnet), 0x6f (testnet)
//   DASH:   coin_type=5,  prefix=0x4c (mainnet), 0x8c (testnet)
//   LTC:    coin_type=2,  prefix=0x30 (mainnet), 0x6f (testnet)
//   DOGE:   coin_type=3,  prefix=0x1e (mainnet), 0x71 (testnet)
//   ZEC:    coin_type=133, prefix=0x1cb8 (mainnet), 0x1d25 (testnet)
//   BCH:    coin_type=145, prefix=0x00 (same as BTC mainnet)
//
// Adding a new fork = config change only. No Go code needed.
type BitcoinDeriver struct {
	coinType uint32
	prefix   byte // P2PKH version byte for base58check
}

// NewBitcoinDeriver creates a deriver for any Bitcoin-family chain.
//
// For Bitcoin mainnet: NewBitcoinDeriver(0, 0x00)
// For DASH mainnet:    NewBitcoinDeriver(5, 0x4c)
// For LTC mainnet:     NewBitcoinDeriver(2, 0x30)
// For testnet:        pass testnet prefix byte
func NewBitcoinDeriver(coinType uint32, prefix byte) *BitcoinDeriver {
	return &BitcoinDeriver{coinType: coinType, prefix: prefix}
}

// NewBitcoinDeriverMainnet returns the deriver for Bitcoin mainnet.
// Convenience constructor for the most common case.
func NewBitcoinDeriverMainnet() *BitcoinDeriver {
	return NewBitcoinDeriver(0, 0x00)
}

func (b *BitcoinDeriver) Family() ChainFamily { return FamilyBitcoin }
func (b *BitcoinDeriver) CoinType() uint32    { return b.coinType }
func (b *BitcoinDeriver) AddressPrefix() byte { return b.prefix }

// DeriveAddress computes a P2PKH address (base58check encoded).
// Standard formula: base58check(ripemd160(sha256(pubkey)))
//
// For DASH: result looks like "Xj6tdsZ8W2..." (starts with 'X')
// For BTC:             result looks like "1A1zP1eP5QG..."
// For LTC:             result looks like "LM2WMpR1..."
func (b *BitcoinDeriver) DeriveAddress(priv *ecdsa.PrivateKey) (string, error) {
	if priv == nil {
		return "", errors.New("nil private key")
	}
	// Get uncompressed public key (65 bytes: 0x04 + 32 + 32)
	pubKey := crypto.FromECDSAPub(&priv.PublicKey)

	// SHA256
	sha := sha256.Sum256(pubKey)
	// RIPEMD160
	ripemd := ripemd160.New()
	ripemd.Write(sha[:])
	hashed := ripemd.Sum(nil) // 20 bytes

	// Base58Check encode with version byte
	return base58.CheckEncode(hashed, b.prefix), nil
}

// SignTransaction produces a 65-byte ECDSA signature (R || S || V).
// V is the recovery id (0 or 1).
//
// Note: V here is NOT EIP-155 style (chainId*2+35). For BTC forks, V is
// just the recovery flag, used by the chain RPC.
func (b *BitcoinDeriver) SignTransaction(priv *ecdsa.PrivateKey, hash []byte) ([]byte, error) {
	if priv == nil {
		return nil, errors.New("nil private key")
	}
	return crypto.Sign(hash, priv)
}