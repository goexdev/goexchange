// Package blockchain: common data types.
//
// Address, Block, Transaction, TransferEvent, Balance, BuildResult,
// BroadcastResult and ResourceCost are the value types passed across
// the Adapter boundary. Concrete adapters translate from chain-native
// RPC shapes into these uniform types so the wallet service does not
// have to care which chain it is talking to.
package blockchain

import (
	"math/big"
	"time"
)

// Address is a chain-addressable account identifier. Base58/hex/bech32
// are stored side by side because downstream services (indexer, audit
// log, deposit monitor) frequently need both: hex is what the database
// indexes on (stable, case-insensitive), the encoded form is what users
// see in the UI.
type Address struct {
	// Encoded is the user-facing form: "T..." for TRON, "0x..." for EVM,
	// "bc1..." for native segwit BTC, etc. Always set.
	Encoded string

	// Hex is the lowercase hex form (no "0x" prefix). For BTC it is
	// derived from the script pubkey and may be empty for non-standard
	// scripts. Indexable, case-insensitive.
	Hex string

	// PublicKey is the hex-encoded compressed or uncompressed public
	// key depending on chain convention. Optional; the wallet service
	// uses it only for audit logging.
	PublicKey string
}

// Block is a chain-agnostic block header plus its transactions. Adapters
// populate LogTime from the block's timestamp; if the chain does not
// expose one, the adapter may fall back to the wall-clock time at which
// the block was first observed.
type Block struct {
	Height    uint64
	Hash      string
	Parent    string
	LogTime   time.Time
	TxHashes  []string // TxHashes; the body itself is fetched on demand.
	IsFinal   bool     // true if solidified/confirmed; false for head.
}

// Transaction carries a parsed on-chain transaction plus its parsed
// Transfer events. Adapters populate Events lazily; GetTransaction may
// return a Transaction with nil Events if the caller only needs the
// receipts, and ParseTransferEvents fills them in afterwards.
type Transaction struct {
	Hash          string
	BlockNumber   uint64
	BlockHash     string
	From          string
	Status        TxStatus
	Events         []string // raw log entries; converted by ParseTransferEvents.
	RawResponse   []byte   // original RPC reply, for debugging.
}

// TxStatus is the chain-agnostic outcome of a transaction. "FAILED"
// covers both revert and out-of-gas. "PENDING" means the transaction
// is in the mempool but not yet included in a block.
type TxStatus string

const (
	TxStatusPending TxStatus = "PENDING"
	TxStatusSuccess TxStatus = "SUCCESS"
	TxStatusFailed  TxStatus = "FAILED"
)

// TransferEvent is one row in a transaction's log. The wallet service
// uses this to decide whether a transaction credited a deposit
// address it owns (To matches) and how much to credit.
//
// Amount is always in the asset's smallest denomination (USDT has 6
// decimals on TRON, so 1 USDT = 1_000_000). Callers that need the
// human-readable form must divide by 10^decimals themselves.
type TransferEvent struct {
	// Index is the position of this event within the transaction.
	// Combined with TxHash + Chain it forms the database UNIQUE key.
	Index uint32

	// Contract is the token contract address that emitted the event,
	// in the chain's native encoding. Empty for native-asset transfers.
	Contract string

	From string
	To   string

	// Amount in the asset's smallest unit.
	Amount *big.Int

	// Decimals lets callers convert Amount to a human-readable string
	// without a separate metadata lookup.
	Decimals uint8
}

// Balance is the on-chain balance of an address for a given asset.
// Available is what can be spent; Locked is held by pending
// withdrawals/sweeps. For native assets, Contract is "".
type Balance struct {
	Address   string
	Contract  string
	Symbol    string // "USDT", "TRX", "ETH", ...
	Available *big.Int
	// Locked is meaningful only on chains that distinguish
	// available/locked at the chain level (TRON does not, EVM does
	// not, BTC does not). For chains that do not, the adapter should
	// set Locked = 0 and put everything in Available.
	Locked *big.Int
	Decimals uint8
}

// BuildResult is what BuildTransfer returns: the raw unsigned
// transaction bytes (to be signed by the signer service) and a fee
// estimate so the caller knows whether to top up the hot wallet
// before submitting.
type BuildResult struct {
	RawTx   []byte
	TxHash  string // computed hash of the unsigned tx, useful for logging.
	FeeCost ResourceCost
}

// BroadcastResult distinguishes a successful broadcast from the
// "RPC didn't answer but the transaction may still be in the chain"
// case. The wallet service reacts very differently to the two.
type BroadcastResult struct {
	// TxHash is the hash assigned by the network. Empty when Accepted
	// is false.
	TxHash string

	// Accepted is true if at least one RPC node acknowledged the
	// submission. False means the broadcast definitively failed and
	// should not be retried.
	Accepted bool
}

// ResourceCost describes the on-chain fee that a transaction will
// consume. Kind varies by chain: TRON charges Energy and Bandwidth,
// EVM chains charge gas, BTC charges sat/vB. The wallet service uses
// Resource to decide whether the hot wallet has enough balance to
// broadcast, and to alert when the balance runs low.
type ResourceCost struct {
	Kind   ResourceKind
	Amount *big.Int // energy units / gas / sat - whatever Kind says
	Symbol string   // "SUN", "wei", "sat", etc.
}

// ResourceKind is a hint about which "fuel" the chain charges. The
// value is informational; the wallet service uses it to format alerts
// ("hot wallet below 50,000 Energy") rather than to do math.
type ResourceKind string

const (
	ResourceEnergy    ResourceKind = "ENERGY"    // TRON
	ResourceBandwidth ResourceKind = "BANDWIDTH" // TRON
	ResourceGas       ResourceKind = "GAS"       // EVM (ETH/BSC/Polygon/Arbitrum)
	ResourceSatoshi   ResourceKind = "SAT_VB"    // BTC (sat per vByte)
)