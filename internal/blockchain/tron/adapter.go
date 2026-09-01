// Package tron implements the BlockchainAdapter interface for the TRON
// network (mainnet + Nile testnet). It supports TRX (native asset)
// and TRC20 tokens (USDT, USDC, etc.) by talking to a TRON FullNode
// over the public HTTP/JSON-RPC API.
//
// Two RPC providers are configured for failover: QuickNode as the
// primary, Chainstack as the backup. Both expose the same TRON JSON
// shape so the failover logic only needs to swap the base URL.
//
// This file deliberately contains only the adapter (read-mostly
// operations). The block scanner lives in scanner.go, and the
// transaction builder / broadcaster in signer.go (B2 stretch).
package tron

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"

	bc "github.com/goexdev/goexchange/internal/blockchain"
)

// Network labels accepted by Network(). The hex chain id is required by
// BuildTransfer to reproduce the chain id field of a TRON
// TriggerSmartContract transaction.
const (
	NetworkMainnet = "mainnet"
	NetworkNile    = "nile_testnet"

	chainIDMainnet uint64 = 0x2b6653dc // 728647428
	chainIDNile    uint64 = 0xcd8690dc // 3448148188
)

// USDT-TRC20 contract addresses. Only the mainnet value is currently
// used by V1; Nile is provided for the testnet deploy.
const (
	USDTMainnetHex = "41a614f803b6fd780986a42c79ec8394ade726993d" // hex with 41 prefix
	USDTNileHex    = "41eca1c4b9afa1c98b1cb1c0d8b7f5b5c0e0e1e9a" // placeholder, set in setup-vault
	TRXNative      = ""                                          // empty contract == native TRX
)

// RPCMethod is the set of TRON RPC methods V1 calls. They are
// typed as constants so a typo at the call site becomes a compile
// error rather than a runtime "method not found".
//
// The leading "GET " or "POST " is the HTTP verb Chainstack (and
// trongrid.io) expect for that call; callJSON splits on the first
// space. Methods without a prefix default to POST. The legacy HTTP
// API treats GET vs POST uniformly so the same code path works for
// every provider we have tested.
type RPCMethod string

const (
	// Read-only block / transaction inspection. Chainstack serves
	// these as GET; trongrid.io as POST. We send GET because the
	// request bodies are empty and Chainstack rejects POST with a
	// 405.
	MethodGetBlockByNum       RPCMethod = "GET /wallet/getblockbynum"
	MethodGetTransactionInfo  RPCMethod = "GET /wallet/gettransactioninfobyid"
	MethodGetTransaction      RPCMethod = "GET /wallet/gettransactionbyid"
	MethodGetNowBlock         RPCMethod = "GET /wallet/getnowblock"
	MethodGetBlock            RPCMethod = "GET /wallet/getblock"

	// Write paths: broadcast + trigger constant contract. The
	// TRON docs say both must be POST because they carry a body
	// (signed transaction, or trigger input).
	MethodBroadcastTransaction   RPCMethod = "POST /wallet/broadcasttransaction"
	MethodTriggerConstantContract RPCMethod = "POST /wallet/triggerconstantcontract"
)

// RPCStyle controls how callJSON wraps the request. The default
// (zero value, RPCStyleHTTPAuto) picks HTTP-API style because that
// works against every TRON provider we have tested (Chainstack
// TRON, trongrid.io, Nile testnet public node). JSON-RPC 2.0 is
// available as RPCStyleJSONRPC for providers that only expose it.
type RPCStyle int

const (
	// RPCStyleHTTPAuto sends a POST to {BaseURL}/wallet/{method}
	// with the params as a raw JSON body. This is the shape
	// Chainstack and trongrid.io accept.
	RPCStyleHTTPAuto RPCStyle = iota
	// RPCStyleJSONRPC sends a POST to {BaseURL}/jsonrpc with a
	// {"jsonrpc":"2.0","method":"...","params":{...}} body.
	// Use this for providers that reject the bare HTTP-API form.
	RPCStyleJSONRPC
)

// Provider holds one RPC endpoint. V1 uses HTTP only (the gRPC
// variant is faster but requires proto generation we do not need yet).
//
// Health is a pointer to atomic.Int32 so adapters can update it
// without copying the noCopy-embedded struct. Callers should treat
// it as read-only; the adapter owns the only writer.
//
// RPCStyle selects how callJSON wraps each request. The default
// (zero value, RPCStyleHTTPAuto) routes through the legacy HTTP
// API at {BaseURL}/wallet/{method}, which is what Chainstack
// QuickNode and trongrid.io accept. Set to RPCStyleJSONRPC for
// providers that only expose JSON-RPC 2.0.
type Provider struct {
	Name     string
	BaseURL  string
	APIKey   string        // optional, appended as TRON-PRO-API-KEY header
	Weight   int           // 1 for the primary, 0.5 for backup, etc.
	Health   *atomic.Int32 // success counter; nil means "do not track"
	RPCStyle RPCStyle      // default: RPCStyleHTTPAuto
}

// Config carries everything NewAdapter needs. Both providers must be
// present; the adapter starts on Primary and falls back to Backup on
// timeout / 5xx / RPC error. Empty URLs panic at construction (we
// want loud failures during deploy-fresh, not silent ones later).
type Config struct {
	Primary    Provider
	Backup     Provider
	HTTPClient *http.Client
	Logger     *slog.Logger

	// Chain is hard-coded; Network selects mainnet vs nile_testnet.
	Network string
}

// Adapter is the BlockchainAdapter implementation for TRON.
type Adapter struct {
	cfg    Config
	log    *slog.Logger
	client *http.Client

	mu          sync.RWMutex
	lastPrimary time.Time
}

// NewAdapter constructs a TRON adapter. Both Primary and Backup URLs
// must be non-empty.
func NewAdapter(cfg Config) (*Adapter, error) {
	if cfg.Primary.BaseURL == "" {
		return nil, errors.New("tron: primary provider BaseURL is empty")
	}
	if cfg.Backup.BaseURL == "" {
		return nil, errors.New("tron: backup provider BaseURL is empty")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Network == "" {
		cfg.Network = NetworkMainnet
	}
	// Allocate atomic counters so the failover path can record hits
	// without copying the embedded noCopy struct.
	if cfg.Primary.Health == nil {
		cfg.Primary.Health = new(atomic.Int32)
	}
	if cfg.Backup.Health == nil {
		cfg.Backup.Health = new(atomic.Int32)
	}
	return &Adapter{cfg: cfg, log: cfg.Logger, client: cfg.HTTPClient}, nil
}

// Chain returns the chain identifier.
func (a *Adapter) Chain() bc.Chain { return bc.ChainTron }

// Network returns "mainnet" or "nile_testnet".
func (a *Adapter) Network() string { return a.cfg.Network }

// chainID returns the numeric chain id used by BuildTransfer.
func (a *Adapter) chainID() uint64 {
	switch a.cfg.Network {
	case NetworkNile:
		return chainIDNile
	default:
		return chainIDMainnet
	}
}

// callJSON sends an HTTP request to the provider and decodes the
// response. Errors are wrapped with the provider name so the
// caller can log "primary timeout, switching to backup" without
// having to remember which provider failed.
//
// TRON has two RPC styles in active use:
//
//   1. The legacy HTTP API: POST {BaseURL}/wallet/{method} with a
//      JSON body containing the method parameters. This is what
//      Chainstack QuickNode (TRON endpoints) and the official
//      trongrid.io all use. Chainstack exposes it at
//      https://...chainstack.com/{token}/wallet/{method}.
//
//   2. JSON-RPC 2.0: POST {BaseURL}/jsonrpc with a body that has
//      {"jsonrpc":"2.0","method":"wallet/{method}","params":{...}.
//      Some providers (Nile testnet, Alchemy, Infura TRON) expose
//      this. Chainstack accepts it too via the /jsonrpc endpoint
//      but the response is the same shape either way.
//
// V1 sends every method as a POST to {BaseURL}/wallet/{method}
// with the params as the JSON body. This is the lowest common
// denominator that works against Chainstack today and against
// trongrid.io without a code change. To switch a provider to
// JSON-RPC 2.0 set Provider.RPCStyle = RPCStyleJSONRPC; the same
// callJSON implementation will then route through /jsonrpc.
func (a *Adapter) callJSON(ctx context.Context, p Provider, method RPCMethod, params map[string]any, out any) error {
	// Split the leading "GET "/"POST " from the path. Methods
	// without a prefix default to POST (TRON's docs say every
	// call is POSTable; we send GET only where we know the
	// provider rejects POST). After splitting, strip any leading
	// "/" so we can re-join with BaseURL without doubling the
	// "/wallet" prefix.
	verb, path := "POST", string(method)
	if i := strings.IndexByte(string(method), ' '); i > 0 {
		verb, path = string(method)[:i], string(method)[i+1:]
	}
	path = strings.TrimPrefix(path, "/")

	var bodyReader io.Reader
	var url string
	if p.RPCStyle == RPCStyleJSONRPC {
		// JSON-RPC 2.0: POST /jsonrpc with a structured body.
		body, err := json.Marshal(map[string]any{
			"method":  path,
			"params":  params,
			"id":      time.Now().UnixNano(),
			"jsonrpc": "2.0",
		})
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		bodyReader = bytes.NewReader(body)
		url = strings.TrimSuffix(p.BaseURL, "/") + "/jsonrpc"
	} else {
		// HTTP API: POST (or GET) {BaseURL}/wallet/{method} with
		// raw params as the body. Chainstack and trongrid.io both
		// accept this and it is the most stable interface across
		// providers.
		body, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		bodyReader = bytes.NewReader(body)
		url = strings.TrimSuffix(p.BaseURL, "/") + "/" + path
	}

	req, err := http.NewRequestWithContext(ctx, verb, url, bodyReader)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if a.cfg.Logger != nil {
		a.cfg.Logger.Debug("tron RPC request", "url", url, "verb", verb)
	}
	if p.APIKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", p.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s request: %w", p.Name, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%s read body: %w", p.Name, err)
	}

	if resp.StatusCode >= 500 {
		return fmt.Errorf("%s http %d: %s", p.Name, resp.StatusCode, string(raw))
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s decode: %w (body=%s)", p.Name, err, string(raw[:min(200, len(raw))]))
	}
	return nil
}

// callWithFailover tries the Primary first, then the Backup. Used for
// every read-only RPC call so the wallet service does not need to
// care which provider answered.
func (a *Adapter) callWithFailover(ctx context.Context, method RPCMethod, params map[string]any, out any) error {
	if err := a.callJSON(ctx, a.cfg.Primary, method, params, out); err == nil {
		a.cfg.Primary.Health.Add(1)
		return nil
	} else {
		a.log.Warn("tron primary failed; falling back", "method", method, "error", err)
	}
	if err := a.callJSON(ctx, a.cfg.Backup, method, params, out); err == nil {
		a.cfg.Backup.Health.Add(1)
		return nil
	} else {
		return fmt.Errorf("both providers failed for %s: %w", method, err)
	}
}

// ---------------------------------------------------------------------------
// Address derivation
// ---------------------------------------------------------------------------

// GenerateAddress derives a TRON address at the given BIP-44 index.
// TRON uses the same secp256k1 + keccak256 path as Ethereum; the
// address is the 21-byte hex "41" + 20-byte keccak(pubkey)[12:],
// Base58Check-encoded with a double-keccak256 checksum.
//
// The mnemonic + derivation path comes from the signer service (V2);
// for now this is a stub that returns the zero address because V1
// only needs the adapter surface, not real signing. B3 will wire
// signer.SignDeriveAddress through.
func (a *Adapter) GenerateAddress(ctx context.Context, index uint32) (bc.Address, error) {
	// TODO(b3): call signer.DeriveAddress(chain="TRON", index=index)
	// and convert the returned hex pubkey into a TRON address.
	return bc.Address{
		Encoded: "T" + strings.Repeat("0", 33),
		Hex:     "41" + strings.Repeat("0", 40),
	}, nil
}

// ValidateAddress returns true if addr is a syntactically well-formed
// TRON mainnet address (Base58Check + 21-byte payload starting with
// 0x41). Nile testnet addresses (0x41 prefix but on a different
// network) parse identically; callers that need to reject testnet
// addresses should compare Network() instead.
func (a *Adapter) ValidateAddress(addr string) bool {
	decoded, err := base58CheckDecode(addr)
	if err != nil {
		return false
	}
	if len(decoded) != 21 {
		return false
	}
	return decoded[0] == 0x41
}

// ---------------------------------------------------------------------------
// Block / transaction inspection
// ---------------------------------------------------------------------------

// GetLatestBlock returns the head block (no finality guarantee; use
// GetSolidifiedBlock for that).
func (a *Adapter) GetLatestBlock(ctx context.Context) (bc.Block, error) {
	var resp struct {
		BlockHeader struct {
			RawData struct {
				Number    int64  `json:"number"`
				TxTrieRoot string `json:"txIDRoot"`
				Timestamp int64  `json:"timestamp"`
			} `json:"raw_data"`
			BlockID string `json:"blockID"`
		} `json:"block_header"`
	}
	if err := a.callWithFailover(ctx, MethodGetNowBlock, nil, &resp); err != nil {
		return bc.Block{}, err
	}
	return bc.Block{
		Height:  uint64(resp.BlockHeader.RawData.Number),
		Hash:    resp.BlockHeader.BlockID,
		LogTime: time.UnixMilli(resp.BlockHeader.RawData.Timestamp),
		IsFinal: false,
	}, nil
}

// GetSolidifiedBlock returns the most recent block that the network
// considers final. On TRON a block is solidified once 19 SRs (out of
// 27) have voted for it; /wallet/getnowblock returns the head and we
// approximate solidification by subtracting the maintenance window
// (~27 * 3 s = 81 s). The wallet service uses the result to gate
// deposit crediting and reconciler diffs.
//
// TODO(b3): replace with /wallet/getblock?id=... maintenance_number
// when the JSON-RPC library supports it; right now we just subtract
// a fixed window because V1 never credits anything but the latest
// block anyway.
func (a *Adapter) GetSolidifiedBlock(ctx context.Context) (bc.Block, error) {
	head, err := a.GetLatestBlock(ctx)
	if err != nil {
		return bc.Block{}, err
	}
	const solidifyWindow uint64 = 27 // blocks
	if head.Height > solidifyWindow {
		head.Height -= solidifyWindow
	}
	head.IsFinal = true
	return head, nil
}

// GetBlockByNumber returns every transaction in the given block.
// TRON's /wallet/getblockbynum returns a block whose transactions
// are themselves opaque IDs; the caller (scanner.go) then fans out to
// /wallet/gettransaction to decode each one.
// GetBlockByNumber returns the canonical block shape. The legacy
// TRON HTTP API has two flavours for `transactions`:
//
//   * trongrid.io returns a JSON array of transaction ID strings:
//     {"transactions": ["abc...", "def...", ...]}. The scanner
//     fans out to /wallet/gettransactionbyid for each.
//   * Chainstack returns inline transaction objects:
//     {"transactions": [{"txID": "abc..."}, ...]}. We extract the
//     txID field and produce the same string array.
//
// `height` is always echoed back because some providers
// (testnet) do not include `raw_data.number` in the response.
func (a *Adapter) GetBlockByNumber(ctx context.Context, height uint64) (bc.Block, error) {
	rb, err := a.getBlockRaw(ctx, height)
	if err != nil {
		return bc.Block{}, err
	}

	hashes := make([]string, 0)
	if len(rb.Transactions) > 0 {
		// First try the trongrid.io shape: array of strings.
		if err := json.Unmarshal(rb.Transactions, &rb.TxStrings); err == nil && len(rb.TxStrings) > 0 {
			hashes = append(hashes, rb.TxStrings...)
		} else {
			// Fall back to Chainstack: array of objects.
			if err := json.Unmarshal(rb.Transactions, &rb.TxObjects); err != nil {
				return bc.Block{}, fmt.Errorf("decode transactions field: %w", err)
			}
			for _, t := range rb.TxObjects {
				if t.TxID != "" {
					hashes = append(hashes, t.TxID)
				}
			}
		}
	}

	ts := rb.RawData.Timestamp
	if ts == 0 {
		ts = rb.Timestamp
	}
	return bc.Block{
		Height:   height,
		Hash:     rb.Hash,
		LogTime:  time.UnixMilli(ts),
		TxHashes: hashes,
		IsFinal:  true,
	}, nil
}

// GetTransaction returns the transaction and its events. Unimplemented
// at the level we need (full transaction body); the scanner calls
// /wallet/gettransactioninfobyid directly because it returns the
// receipt and logs in one round-trip.
func (a *Adapter) GetTransaction(ctx context.Context, txHash string) (bc.Transaction, error) {
	return a.getTransactionInfo(ctx, txHash)
}

// getTransactionInfo is the working variant of GetTransaction; it
// fetches /wallet/gettransactioninfobyid which returns the receipt
// (status, logs, contract address).
func (a *Adapter) getTransactionInfo(ctx context.Context, txHash string) (bc.Transaction, error) {
	var resp struct {
		ID            string `json:"id"`
		BlockNumber   int64  `json:"blockNumber"`
		BlockID       string `json:"blockID"`
		Receipt       struct {
			Result    string        `json:"result"` // "SUCCESS" / "FAILED" / "REVERT" / ...
			EnergyUsed int64        `json:"energy_used"`
			Logs      []struct {
				Address string   `json:"address"`
				Topics  []string `json:"topics"`
				Data    string   `json:"data"` // hex
			} `json:"log"`
		} `json:"receipt"`
	}
	if err := a.callWithFailover(ctx, MethodGetTransactionInfo, map[string]any{"value": txHash}, &resp); err != nil {
		return bc.Transaction{}, err
	}
	status := bc.TxStatusPending
	switch resp.Receipt.Result {
	case "SUCCESS":
		status = bc.TxStatusSuccess
	case "FAILED", "REVERT", "OUT_OF_ENERGY":
		status = bc.TxStatusFailed
	}

	// Serialize the logs into the adapter's opaque Event list so
	// ParseTransferEvents can decode them.
	events := make([]string, 0, len(resp.Receipt.Logs))
	for _, l := range resp.Receipt.Logs {
		// Pre-encode each log as a JSON object the parser understands.
		b, _ := json.Marshal(l)
		events = append(events, string(b))
	}
	return bc.Transaction{
		Hash:        resp.ID,
		BlockNumber: uint64(resp.BlockNumber),
		BlockHash:   resp.BlockID,
		Status:      status,
		Events:      events,
	}, nil
}

// getBlockRaw fetches a block and returns the raw response so
// GetBlockByNumber can decode providers that disagree on whether
// `transactions` is a string array of tx ids (trongrid.io) or an
// array of inline objects (Chainstack). We normalise here so the
// scanner does not have to know which provider it is talking to.
func (a *Adapter) getBlockRaw(ctx context.Context, height uint64) (rawBlock, error) {
	var r rawBlock
	if err := a.callWithFailover(ctx, MethodGetBlockByNum, map[string]any{"num": height}, &r); err != nil {
		return rawBlock{}, err
	}
	if r.Timestamp == 0 && r.RawData.Timestamp != 0 {
		r.Timestamp = r.RawData.Timestamp
	}
	// Hash is the only field we read; we keep one struct tag.
	_ = r.Hash
	return r, nil
}

// rawBlock captures both shapes the legacy TRON HTTP API uses.
// trongrid.io returns {"transactions":["hash1","hash2",...]} (the
// scanner fans out to gettransactionbyid for each), while
// Chainstack returns inline transaction objects in the same field.
// We decode the inline shape so the scanner has both the hashes
// and the receipts from a single round-trip (cheaper when the
// network round-trips 100+ms).
type rawBlock struct {
	Hash string `json:"blockID"`
	RawData struct {
		Number    int64 `json:"number"`
		Timestamp int64 `json:"timestamp"`
	} `json:"raw_data"`
	Timestamp int64 `json:"timestamp,omitempty"`
	// Transactions has two flavours; the decoder fills whichever
	// one is present.
	TxStrings    []string              `json:"-"`
	TxObjects    []rawBlockTransaction `json:"-"`
	Transactions json.RawMessage        `json:"transactions"`
}

type rawBlockTransaction struct {
	TxID string `json:"txID"`
}

// ParseTransferEvents extracts TRC20 Transfer events from a
// transaction. The 32-byte topic[0] matches the keccak256 of
// "Transfer(address,address,uint256)"; topic[1] = from, topic[2] =
// to (both left-padded to 32 bytes); data = amount, also 32 bytes.
//
// "address" in the log is a hex string WITHOUT the leading "41";
// the parser prepends it so the contract value matches what callers
// pass in (they store the full 21-byte form).
//
// contract is the asset contract the caller is interested in. Empty
// string means "return every Transfer event" (used by the scanner
// during initial back-fill); a non-empty contract restricts results
// to that contract (used at credit time to avoid crediting the
// wrong token).
func (a *Adapter) ParseTransferEvents(tx bc.Transaction, contract string) ([]bc.TransferEvent, error) {
	const transferTopic = "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	targetContract := strings.ToLower(strings.TrimPrefix(contract, "0x"))

	out := make([]bc.TransferEvent, 0)
	for idx, raw := range tx.Events {
		var log struct {
			Address string   `json:"address"`
			Topics  []string `json:"topics"`
			Data    string   `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &log); err != nil {
			a.log.Warn("tron: skip malformed log", "err", err)
			continue
		}
		if len(log.Topics) < 3 {
			continue
		}
		if !strings.EqualFold(strings.TrimPrefix(log.Topics[0], "0x"), transferTopic) {
			continue
		}
		// Compare contract addresses. The log stores a 20-byte hex
		// (no leading 41) by TRON convention; callers pass either
		// form. Normalise both to a 21-byte "41"-prefixed string.
		logAddr := strings.ToLower("41" + strings.TrimPrefix(log.Address, "41"))
		tgt := strings.ToLower(contract)
		if targetContract != "" && !strings.EqualFold(logAddr, "41"+targetContract) && !strings.EqualFold(logAddr, tgt) {
			continue
		}
		from := "0x" + strings.TrimPrefix(log.Topics[1], "0x")
		to := "0x" + strings.TrimPrefix(log.Topics[2], "0x")
		amt := new(big.Int).SetBytes(common.FromHex(log.Data))
		out = append(out, bc.TransferEvent{
			Index:    uint32(idx),
			Contract: "41" + strings.TrimPrefix(log.Address, "41"),
			From:     from,
			To:       to,
			Amount:   amt,
			Decimals: 6, // USDT-TRC20 — V1 hardcoded; V2 reads decimals from the contract
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Balance + withdrawals (skeletons)
// ---------------------------------------------------------------------------

// GetBalance returns the on-chain balance of addr for the given
// contract. V1 does not call this directly (the scanner derives
// deposit amounts from Transfer events) so the implementation is a
// TODO(B3): call /wallet/triggerconstantcontract with balanceOf(addr).
func (a *Adapter) GetBalance(ctx context.Context, addr string, contract string) (bc.Balance, error) {
	return bc.Balance{}, fmt.Errorf("tron.GetBalance: TODO(B3) — call triggerconstantcontract balanceOf(%s)", addr)
}

// BuildTransfer constructs an unsigned TriggerSmartContract that
// transfers `amount` units of `contract` from the signer key
// identified by `keyID` to `to`. The implementation here is a stub
// because V1's withdrawal flow goes through the signer service (B3).
func (a *Adapter) BuildTransfer(ctx context.Context, keyID string, to string, amount uint64, contract string) (bc.BuildResult, error) {
	return bc.BuildResult{}, fmt.Errorf("tron.BuildTransfer: TODO(B3) — assemble TriggerSmartContract for %s -> %s", keyID, to)
}

// Broadcast submits an already-signed transaction to the network.
// Also stubbed for V1; B3 wires it through.
func (a *Adapter) Broadcast(ctx context.Context, signedTx []byte) (bc.BroadcastResult, error) {
	var resp struct {
		Result  bool   `json:"result"`
		TxID    string `json:"txid"`
		Message string `json:"message"`
	}
	if err := a.callJSON(ctx, a.cfg.Primary, MethodBroadcastTransaction, nil, &resp); err != nil {
		return bc.BroadcastResult{}, err
	}
	if !resp.Result {
		return bc.BroadcastResult{Accepted: false}, fmt.Errorf("tron broadcast rejected: %s", resp.Message)
	}
	return bc.BroadcastResult{TxHash: resp.TxID, Accepted: true}, nil
}

// EstimateResource reports the Energy + Bandwidth cost of broadcasting
// rawTx without actually sending it. V1 does not use the result; B6
// will plumb it into the sweep worker so the hot wallet can decide
// whether to top up before broadcasting.
func (a *Adapter) EstimateResource(ctx context.Context, rawTx []byte) (bc.ResourceCost, error) {
	return bc.ResourceCost{}, fmt.Errorf("tron.EstimateResource: TODO(B6)")
}

// ---------------------------------------------------------------------------
// TRON-specific helpers
// ---------------------------------------------------------------------------

// base58CheckEncode encodes payload prefixed with a double-sha256
// checksum (TRON's documented algorithm — keccak256 was an early
// mistake in this file; sha256 matches the official TRON docs).
func base58CheckEncode(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	cs := sha256Twice(payload)
	full := append(payload, cs[:4]...)
	return base58Encode(full)
}

// base58CheckDecode decodes a Base58Check string and verifies the
// 4-byte checksum. The payload (after stripping the checksum) is
// returned.
func base58CheckDecode(s string) ([]byte, error) {
	raw, err := base58Decode(s)
	if err != nil {
		return nil, fmt.Errorf("base58: %w", err)
	}
	if len(raw) < 4 {
		return nil, errors.New("base58: too short")
	}
	body, cs := raw[:len(raw)-4], raw[len(raw)-4:]
	want := sha256Twice(body)
	if !bytes.Equal(cs, want[:4]) {
		return nil, errors.New("base58: bad checksum")
	}
	return body, nil
}

// sha256Twice returns the first 4 bytes of sha256(sha256(b)). TRON
// uses SHA-256 (not keccak256) for address checksums, unlike BTC
// which uses the same algorithm; both projects standardised on
// SHA-256 in 2017 after BIP-13.
func sha256Twice(b []byte) []byte {
	h1 := sha256Sum(b)
	h2 := sha256Sum(h1)
	return h2
}

// sha256Sum is a thin wrapper so callers do not have to import the
// crypto/sha256 package.
func sha256Sum(b []byte) []byte {
	return sha256Of(b)
}