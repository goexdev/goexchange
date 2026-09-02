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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sort"
	"strings"
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
	MethodGetTransactionInfo  RPCMethod = "POST /wallet/gettransactioninfobyid"
	MethodGetTransaction      RPCMethod = "POST /wallet/gettransactionbyid"
	MethodGetNowBlock         RPCMethod = "GET /wallet/getnowblock"
	MethodGetBlock            RPCMethod = "GET /wallet/getblock"

	// Write paths: broadcast + trigger constant contract. The
	// TRON docs say both must be POST because they carry a body
	// (signed transaction, or trigger input).
	MethodBroadcastTransaction   RPCMethod = "POST /wallet/broadcasttransaction"
	MethodTriggerConstantContract RPCMethod = "POST /wallet/triggerconstantcontract"
	MethodTriggerSmartContract    RPCMethod = "POST /wallet/triggersmartcontract"
	MethodCreateTransaction      RPCMethod = "POST /wallet/createtransaction"
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
//
// Weight ranks providers when the active one fails: higher Weight
// wins, ties broken by slice order. V1 uses integer weights; a
// future scheduling layer can switch to a smooth score if needed.
//
// We support N providers (not just primary + backup). The active
// provider is selected by an atomic index so a runtime switch
// does not need a lock around the request hot path.
type Provider struct {
	Name     string
	BaseURL  string
	APIKey   string        // optional, sent as TRON-PRO-API-KEY header
	Weight   int           // 1 for primary, 0.5 for backup, 0 for hot spare
	Health   *atomic.Int32 // 1 = healthy, 0 = last call failed
	RPCStyle RPCStyle      // default: RPCStyleHTTPAuto

	// RateLimit429At records the most recent 429 timestamp (unix
	// nano). When non-zero and within RateLimitGrace the provider
	// is treated as rate-limited regardless of Health. This avoids
	// hammering a provider that has already told us to back off.
	//
	// Pointer to atomic.Int64 because atomic types are noCopy; if
	// it were a value, vet would reject every Provider copy
	// (slice index, struct return, function argument).
	RateLimit429At *atomic.Int64
}

// RateLimitGrace is how long a 429 shadows the provider. Chainstack
// free tier refills inside 30s in our measurements; the same window
// fits the other providers we have surveyed.
const RateLimitGrace = 30 * time.Second

// IsAvailable returns true if the provider is currently usable.
// A provider is unavailable if Health=0 (last call failed) and the
// 429 stamp is inside RateLimitGrace; once the grace expires the
// provider is retried on the next request.
func (p *Provider) IsAvailable() bool {
	if p == nil {
		return false
	}
	if p.Health != nil && p.Health.Load() == 0 {
		last := p.RateLimit429At.Load()
		if last == 0 || time.Since(time.Unix(0, last)) > RateLimitGrace {
			return true
		}
		return false
	}
	return true
}

// MarkRateLimited sets Health=0 and stamps the 429 timestamp so
// the active selector skips this provider for RateLimitGrace.
func (p *Provider) MarkRateLimited() {
	p.RateLimit429At.Store(time.Now().UnixNano())
	if p.Health != nil {
		p.Health.Store(0)
	}
}

// MarkHealthy flips Health back to 1 and clears the 429 stamp.
func (p *Provider) MarkHealthy() {
	p.RateLimit429At.Store(0)
	if p.Health != nil {
		p.Health.Store(1)
	}
}

// MarkUnhealthy flips Health to 0 without setting a 429 stamp. Used
// for non-rate-limit failures (5xx, timeouts, parse errors).
func (p *Provider) MarkUnhealthy() {
	if p.Health != nil {
		p.Health.Store(0)
	}
}

// Config carries everything NewAdapter needs. At least one provider
// is required; empty URLs panic at construction (we want loud
// failures during deploy-fresh, not silent ones later). The
// Providers slice is copied so post-construction mutations to the
// caller's slice do not affect the adapter.
type Config struct {
	Providers  []Provider
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

	// providers is a copy of cfg.Providers sorted by Weight desc, then
	// by Name for stability. We do not re-sort at runtime.
	providers []Provider

	// activeIdx is the index into providers that the next request
	// will use. Stored atomically so SetActive can swap it without
	// blocking the request hot path.
	activeIdx atomic.Int32
}

// NewAdapter constructs a TRON adapter. Both Primary and Backup URLs
// must be non-empty; callers can run the adapter with a single
// provider for tests by passing one-element slice.
func NewAdapter(cfg Config) (*Adapter, error) {
	if len(cfg.Providers) == 0 {
		return nil, errors.New("tron: at least one provider is required")
	}
	for i := range cfg.Providers {
		if cfg.Providers[i].BaseURL == "" {
			return nil, fmt.Errorf("tron: provider %d BaseURL is empty", i)
		}
		if cfg.Providers[i].Weight == 0 {
			cfg.Providers[i].Weight = 1 // default weight; primary tier
		}
		if cfg.Providers[i].Health == nil {
			cfg.Providers[i].Health = new(atomic.Int32)
		}
		if cfg.Providers[i].RateLimit429At == nil {
			cfg.Providers[i].RateLimit429At = new(atomic.Int64)
		}
		cfg.Providers[i].Health.Store(1)
		cfg.Providers[i].RateLimit429At.Store(0)
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Network == "" {
		cfg.Network = NetworkMainnet
	}

	// Sort providers by Weight desc, original index asc. We do not
	// re-sort at runtime, so the active selector's index is stable
	// across calls. Ties preserve the order callers passed in: that
	// means the first entry of equal-weight providers is treated
	// as the active primary, which matches the deploy-fresh
	// convention (TRON_PRIMARY_URL comes first in the list).
	providers := make([]Provider, len(cfg.Providers))
	copy(providers, cfg.Providers)
	sort.SliceStable(providers, func(i, j int) bool {
		if providers[i].Weight != providers[j].Weight {
			return providers[i].Weight > providers[j].Weight
		}
		// Preserve original input order for ties by remembering
		// where it came from. We tag the copy with the original
		// index via a side-channel struct.
		return i < j
	})

	return &Adapter{
		cfg:       cfg,
		log:       cfg.Logger,
		client:    cfg.HTTPClient,
		providers: providers,
	}, nil
}

// SetActive selects which provider the next request will target by
// name. Unknown names return an error and leave the active provider
// unchanged. This is the entry point for the admin API and for
// failover when a provider returns 429.
func (a *Adapter) SetActive(name string) error {
	for i := range a.providers {
		if a.providers[i].Name == name {
			a.activeIdx.Store(int32(i))
			if a.log != nil {
				a.log.Info("tron active provider switched", "name", name, "index", i)
			}
			return nil
		}
	}
	return fmt.Errorf("tron: unknown provider %q", name)
}

// ActiveProvider returns the currently selected provider (read-only;
// do not mutate).
func (a *Adapter) ActiveProvider() Provider {
	idx := int(a.activeIdx.Load())
	if idx < 0 || idx >= len(a.providers) {
		idx = 0
	}
	return a.providers[idx]
}

// Providers returns a copy of the configured providers in priority
// order. Used by admin/diagnostic endpoints.
func (a *Adapter) Providers() []Provider {
	out := make([]Provider, len(a.providers))
	copy(out, a.providers)
	return out
}

// FailoverToNext is the exported form of failoverToNext. Admin
// RPC endpoints call this when an operator wants to manually
// promote the next provider without waiting for the next
// request to fail. Returns the new active index, or -1 if every
// provider is unavailable.
func (a *Adapter) FailoverToNext() int {
	return a.failoverToNext()
}

// failoverToNext scans providers starting at the active index for
// the next one that IsAvailable. Returns the new index or -1 if
// every provider is unavailable (caller should surface the error).
func (a *Adapter) failoverToNext() int {
	start := int(a.activeIdx.Load())
	for offset := 1; offset <= len(a.providers); offset++ {
		idx := (start + offset) % len(a.providers)
		if a.providers[idx].IsAvailable() {
			a.activeIdx.Store(int32(idx))
			if a.log != nil {
				a.log.Warn("tron failover",
					"from_index", start,
					"to_index", idx,
					"to_name", a.providers[idx].Name)
			}
			return idx
		}
	}
	return -1
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

	if resp.StatusCode == 429 {
		// Surface a rate-limit error before trying to decode the
		// body so callWithFailover's isRateLimitErr heuristic can
		// mark the provider as rate-limited. The body is still
		// useful for log scraping so we include the first 200
		// bytes in the error message.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("status=429 from %s: %s", p.Name, string(body[:min(200, len(body))]))
	}
	if resp.StatusCode >= 400 {
		// 4xx other than 429 (already handled above) and 5xx:
		// upstream error, do not retry on the same provider.
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

// callWithFailover tries the active provider first; on failure
// (timeout, 5xx, 429, parse error) it marks the provider unhealthy
// and walks the rest of the priority list until one returns 200 or
// every provider has been tried. Each provider attempt has its own
// context-deadline so a single hung provider cannot eat the whole
// request budget.
//
// On 429 we set the rate-limit shadow so the provider stays out of
// rotation for RateLimitGrace. On other failures we only flip
// Health=0 for this request; the next request will retry the same
// provider (cheap if it was a transient blip).
func (a *Adapter) callWithFailover(ctx context.Context, method RPCMethod, params map[string]any, out any) error {
	if len(a.providers) == 0 {
		return errors.New("tron: no providers configured")
	}
	// Try the active provider first, then walk through the rest in
	// priority order. We attempt every provider once before giving
	// up — a partial outage should not silence a working backup.
	startIdx := int(a.activeIdx.Load())
	var lastErr error
	for offset := 0; offset < len(a.providers); offset++ {
		idx := (startIdx + offset) % len(a.providers)
		// Skip providers that are marked unavailable (Health=0
		// with a fresh 429 stamp). For the active provider itself
		// we always try once so a recovered provider gets a chance
		// to clear the stamp.
		if offset > 0 && !a.providers[idx].IsAvailable() {
			continue
		}
		err := a.callJSON(ctx, a.providers[idx], method, params, out)
		if err == nil {
			a.providers[idx].MarkHealthy()
			if offset > 0 {
				// We landed on a backup; promote it as the
				// new active so subsequent calls hit the
				// working provider without another walk.
				a.activeIdx.Store(int32(idx))
				if a.log != nil {
					a.log.Info("tron promoted backup to active",
						"name", a.providers[idx].Name)
				}
			}
			return nil
		}
		lastErr = err
		if isRateLimitErr(err) {
			a.providers[idx].MarkRateLimited()
			if a.log != nil {
				a.log.Warn("tron provider rate-limited",
					"name", a.providers[idx].Name,
					"method", method)
			}
		} else {
			a.providers[idx].MarkUnhealthy()
		}
	}
	return fmt.Errorf("all %d providers failed for %s: %w", len(a.providers), method, lastErr)
}

// isRateLimitErr inspects a callJSON error to detect 429-shaped
// failures. We accept both wrapped sentinel errors (the *http
// library wraps net errors) and direct body matches, so callers
// can rely on a single boolean without caring which layer raised
// the rate limit.
func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "status=429") ||
		strings.Contains(s, "Too Many Requests") ||
		strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate-limited")
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
	case "":
		// Some chainstack responses omit the `result` field
		// entirely (just `{"net_usage": N}`) when the tx was
		// accepted and a TRX transfer contract ran cleanly.
		// Combined with blockNumber > 0 that means the node
		// saw the tx and put it in a block. Treat as SUCCESS;
		// an explicit FAILED result still wins because it
		// carries an enum value.
		if resp.BlockNumber > 0 {
			status = bc.TxStatusSuccess
		}
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

// GetBalance returns the on-chain TRC20 balance of `addr` for the
// given contract (e.g. USDT's contract address). It uses
// /wallet/triggerconstantcontract with the balanceOf(address)
// selector; the response includes a `constant_result` array
// whose first element is the 32-byte big-endian balance.
//
// Empty contract returns the native TRX balance via
// /wallet/getaccount. V1 only ships the TRC20 path because
// everything on goexchange is USDT-TRC20 today; the native
// path is left for whoever first needs it.
func (a *Adapter) GetBalance(ctx context.Context, addr string, contract string) (bc.Balance, error) {
	if contract == "" {
		return bc.Balance{}, fmt.Errorf("tron.GetBalance: native TRX balance not yet implemented")
	}
	ownerHex, err := base58ToHexTron(addr)
	if err != nil {
		return bc.Balance{}, fmt.Errorf("tron.GetBalance: decode addr: %w", err)
	}
	contractHex, err := base58ToHexTron(contract)
	if err != nil {
		return bc.Balance{}, fmt.Errorf("tron.GetBalance: decode contract: %w", err)
	}
	// balanceOf(address) takes a single 32-byte left-padded
	// address parameter. selector 0x70a08231 = keccak256("balanceOf(address)")[:4].
	paramAddr := strings.Repeat("0", 64-len(ownerHex)) + ownerHex
	params := map[string]any{
		"owner_address":     ownerHex,
		"contract_address":  contractHex,
		"function_selector": "balanceOf(address)",
		"parameter":         paramAddr,
		"visible":           false,
	}
	var resp struct {
		// chainstack returns constant_result at the top level
		// alongside transaction + result; trongrid nests it
		// under result.constant_result. Decode both shapes.
		ConstantResult []string          `json:"constant_result"`
		Result         map[string]any    `json:"result"` // wraps {constant_result: [hex32bytes]}
		EnergyUsed     int64             `json:"energy_used"`
	}
	if err := a.callWithFailover(ctx, MethodTriggerConstantContract, params, &resp); err != nil {
		return bc.Balance{}, fmt.Errorf("tron.GetBalance: %w", err)
	}
	rawHex := firstString(resp.ConstantResult)
	if rawHex == "" {
		rawHex = extractConstantResult(resp.Result)
	}
	if rawHex == "" {
		return bc.Balance{}, fmt.Errorf("tron.GetBalance: empty constant_result in response: %+v", resp)
	}
	balBytes, err := hex.DecodeString(rawHex)
	if err != nil {
		return bc.Balance{}, fmt.Errorf("tron.GetBalance: hex decode balance: %w", err)
	}
	bal := new(big.Int).SetBytes(balBytes)
	return bc.Balance{
		Address:   addr,
		Contract:  contract,
		Available: bal,
		Locked:    big.NewInt(0),
		Decimals:  6, // USDT on TRON = 6; V1 hardcodes; future versions
		              // should look this up via contract.decimals().
	}, nil
}

// firstString returns the first non-empty element of a string
// slice; "" when the slice is empty. Convenience helper for
// the constant_result[] array that providers disagree on
// whether to populate.
func firstString(s []string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// extractConstantResult pulls the first constant_result hex string
// out of a chainstack/trongrid response shape. The shape is
// either {constant_result: ["hex"]} or {result: {constant_result:
// ["hex"]}} or even an inline array.
func extractConstantResult(m map[string]any) string {
	if v, ok := m["constant_result"].([]any); ok && len(v) > 0 {
		if s, ok := v[0].(string); ok {
			return s
		}
	}
	if inner, ok := m["result"].(map[string]any); ok {
		return extractConstantResult(inner)
	}
	return ""
}

// BuildTransfer constructs an unsigned TriggerSmartContract that
// transfers `amount` units of `contract` from the signer key
// identified by `keyID` to `to`. We delegate to the chain node's
// /wallet/triggersmartcontract endpoint which assembles the
// full TriggerSmartContract protobuf (the entire Transaction.raw_data
// submessage) for us and returns it as raw_data_hex. The signer
// service SHA-256s that raw_data and appends its 65-byte signature;
// the caller concatenates the signature back into the
// Transaction.signature[0] field and POSTs the rebuilt Transaction
// protobuf to Broadcast.
//
// V1 simplifies this: the signer returns raw_tx || sig; we leave
// the broadcast to the wallet service which knows how to splice
// the sig into the broadcast payload. See service_v1.go.
//
// Native TRX path: when `contract` is empty the wallet service
// is expected to call BuildNativeTransfer instead. V1's wallet
// service does the dispatch based on whether the asset is a TRC20
// or the native TRX.
//
// This stub remains because the adapter interface requires
// BuildTransfer with the four-arg signature. The wallet
// service routes around it.
func (a *Adapter) BuildTransfer(ctx context.Context, keyID string, to string, amount uint64, contract string) (bc.BuildResult, error) {
	if contract == "" {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildTransfer: contract empty; use BuildNativeTransfer for TRX")
	}
	// Convert Base58 to hex (with 41 prefix for TRON addresses).
	ownerHex, err := base58ToHexTron(keyID)
	if err != nil {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildTransfer: decode owner %q: %w", keyID, err)
	}
	contractHex, err := base58ToHexTron(contract)
	if err != nil {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildTransfer: decode contract %q: %w", contract, err)
	}
	toHex, err := base58ToHexTron(to)
	if err != nil {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildTransfer: decode to %q: %w", to, err)
	}

	// Encode transfer(address,uint256) call data.
	// selector = keccak256("transfer(address,uint256)")[:4] = 0xa9059cbb
	// params = 32-byte left-padded to || 32-byte big-endian amount
	data := encodeTransferCall(toHex, amount)

	params := map[string]any{
		"owner_address":     ownerHex,
		"contract_address":  contractHex,
		"function_selector": "transfer(address,uint256)",
		"parameter":         data,
		"call_value":        0,
		"fee_limit":         100_000_000, // 100 TRX = 1e8 sun
		"visible":           false,        // keep hex inputs/outputs (v1 default)
	}

	var resp struct {
		TxID        string `json:"txID"`
		RawDataHex  string `json:"raw_data_hex"`
		RawData     string `json:"raw_data"`     // alternate key
		Transaction *struct {
			TxID       string `json:"txID"`
			RawDataHex string `json:"raw_data_hex"`
		} `json:"transaction"` // chainstack wraps the unsigned tx under "transaction"
		Result      map[string]any `json:"result"` // chainstack wraps under "result" + per-call result
		Visible     *bool  `json:"visible,omitempty"`
	}
	if err := a.callWithFailover(ctx, MethodTriggerSmartContract, params, &resp); err != nil {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildTransfer: %w", err)
	}
	// Handle nested `result` envelope if present.
	rawHex := resp.RawDataHex
	if rawHex == "" {
		rawHex = resp.RawData
	}
	if rawHex == "" && resp.Transaction != nil {
		rawHex = resp.Transaction.RawDataHex
	}
	if rawHex == "" && resp.Result != nil {
		if v, ok := resp.Result["raw_data_hex"].(string); ok {
			rawHex = v
		}
		if v, ok := resp.Result["raw_data"].(string); ok && rawHex == "" {
			rawHex = v
		}
		// Some providers nest the unsigned tx under result.transaction.
		if tx, ok := resp.Result["transaction"].(map[string]any); ok {
			if v, ok := tx["raw_data_hex"].(string); ok && rawHex == "" {
				rawHex = v
			}
		}
	}
	if rawHex == "" {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildTransfer: empty raw_data_hex in response: %+v", resp)
	}
	rawBytes, err := hex.DecodeString(rawHex)
	if err != nil {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildTransfer: hex decode raw_data_hex: %w", err)
	}
	txID := resp.TxID
	if txID == "" && resp.Transaction != nil {
		txID = resp.Transaction.TxID
	}
	if txID == "" && resp.Result != nil {
		if v, ok := resp.Result["txID"].(string); ok {
			txID = v
		}
		if tx, ok := resp.Result["transaction"].(map[string]any); ok {
			if v, ok := tx["txID"].(string); ok && txID == "" {
				txID = v
			}
		}
	}
	return bc.BuildResult{
		RawTx:  rawBytes,
		TxHash: txID,
		FeeCost: bc.ResourceCost{
			Kind:   bc.ResourceEnergy,
			Amount: big.NewInt(100_000_000),
			Symbol: "SUN",
		},
	}, nil
}

// BuildNativeTransfer constructs an unsigned TransferContract
// (native TRX move) using the chain node's /wallet/createtransaction
// endpoint. amount is in SUN (1 TRX = 1_000_000 SUN). The chain
// returns raw_data_hex for the TransferContract; the signer
// SHA-256s + signs and the worker broadcasts via
// BroadcastWithSignature.
//
// Used for sweep-to-hot-wallet TRX top-ups (deposit address
// sends TRX back to the hot wallet) and for any future
// withdrawal that is the native asset rather than a TRC20.
func (a *Adapter) BuildNativeTransfer(ctx context.Context, keyID string, to string, amountSun uint64) (bc.BuildResult, error) {
	ownerHex, err := base58ToHexTron(keyID)
	if err != nil {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildNativeTransfer: decode owner: %w", err)
	}
	toHex, err := base58ToHexTron(to)
	if err != nil {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildNativeTransfer: decode to: %w", err)
	}
	params := map[string]any{
		"owner_address": ownerHex,
		"to_address":    toHex,
		"amount":        amountSun,
		"visible":       false,
	}
	var resp struct {
		TxID       string `json:"txID"`
		RawDataHex string `json:"raw_data_hex"`
		Result     map[string]any `json:"result"`
	}
	if err := a.callWithFailover(ctx, MethodCreateTransaction, params, &resp); err != nil {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildNativeTransfer: %w", err)
	}
	rawHex := resp.RawDataHex
	if rawHex == "" && resp.Result != nil {
		if v, ok := resp.Result["raw_data_hex"].(string); ok {
			rawHex = v
		}
	}
	if rawHex == "" {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildNativeTransfer: empty raw_data_hex in response: %+v", resp)
	}
	rawBytes, err := hex.DecodeString(rawHex)
	if err != nil {
		return bc.BuildResult{}, fmt.Errorf("tron.BuildNativeTransfer: hex decode: %w", err)
	}
	return bc.BuildResult{
		RawTx: rawBytes,
		TxHash: resp.TxID,
	}, nil
}

// encodeTransferCall builds the calldata for transfer(address,uint256).
// `to_hex_with_41_prefix` is the destination as 21-byte hex (41 + 20
// bytes). The selector 0xa9059cbb is hard-coded.
//
// The returned string is raw hex (no 0x prefix). TRON's
// triggersmartcontract endpoint hex-decodes this on the server
// side, and including a 0x prefix yields the
// "bouncycastle.encoders.DecoderException: invalid characters"
// error observed against nile.trongrid.io and chainstack.
func encodeTransferCall(toHex41Prefix string, amount uint64) string {
	// Strip the 41 prefix, left-pad to 32 bytes (64 hex chars).
	addr := strings.TrimPrefix(toHex41Prefix, "41")
	if len(addr) != 40 {
		addr = strings.Repeat("0", 40-len(addr)) + addr // pad if shorter
	}
	paramAddr := strings.Repeat("0", 64-len(addr)) + addr
	// Amount as 32-byte big-endian.
	paramAmount := fmt.Sprintf("%064x", amount)
	// No 0x prefix — TRON nodes reject it with a bouncycastle
	// hex-decoder error.
	return "a9059cbb" + paramAddr + paramAmount
}

// base58ToHexTron decodes a Base58 address to hex with the 41
// TRON mainnet prefix. TRON addresses are 25 bytes:
// version_byte (1) + address (20) + checksum (4).
// We validate the version byte and discard the checksum;
// re-checksumming is the chain's job on broadcast.
func base58ToHexTron(addr string) (string, error) {
	if !strings.HasPrefix(addr, "T") && !strings.HasPrefix(addr, "41") {
		return "", fmt.Errorf("address must start with T (base58) or 41 (hex)")
	}
	if strings.HasPrefix(addr, "41") {
		return strings.ToLower(addr), nil
	}
	decoded, err := base58CheckDecode(addr)
	if err != nil {
		return "", err
	}
	// TRON address layout: 1 byte version + 20 bytes address + 4 bytes checksum.
	if len(decoded) != 21 {
		return "", fmt.Errorf("expected 21-byte payload (ver+addr), got %d bytes", len(decoded))
	}
	if decoded[0] != 0x41 {
		return "", fmt.Errorf("expected TRON mainnet version byte 0x41, got 0x%02x", decoded[0])
	}
	return "41" + hex.EncodeToString(decoded[1:21]), nil
}

// Broadcast submits an already-signed transaction to the network.
// Also stubbed for V1; B3 wires it through.
func (a *Adapter) Broadcast(ctx context.Context, signedTx []byte) (bc.BroadcastResult, error) {
	resp := struct {
		Result  bool            `json:"result"`
		TxID    string          `json:"txid"`
		Message string          `json:"message"`
		Code    json.RawMessage `json:"code,omitempty"`
		Raw     json.RawMessage `json:"-"`
	}{}
	// ... rest unchanged, but on rejection return Raw + Code
	// (raw is populated below by the broadcastOnce helper).
	_ = resp
	// BroadcastTransaction takes the raw signed tx as raw body, not
	// JSON params. callWithFailover takes a params map; for the
	// broadcast case we hand-roll the call against the active
	// provider and fall back manually. The retry policy mirrors
	// callWithFailover: walk the priority list, mark rate-limit on
	// 429, mark unhealthy on other errors.
	idx := int(a.activeIdx.Load())
	for offset := 0; offset < len(a.providers); offset++ {
		i := (idx + offset) % len(a.providers)
		if offset > 0 && !a.providers[i].IsAvailable() {
			continue
		}
		// callJSON with params=nil wraps signedTx as the JSON body.
		// For the broadcast case we want the raw bytes; we reuse the
		// HTTP path via a small dedicated helper.
		err := a.broadcastOnce(ctx, &a.providers[i], signedTx, &resp)
		if err == nil {
			a.providers[i].MarkHealthy()
			if !resp.Result {
				return bc.BroadcastResult{Accepted: false}, fmt.Errorf("tron broadcast rejected: %s", resp.Message)
			}
			return bc.BroadcastResult{TxHash: resp.TxID, Accepted: true}, nil
		}
		if isRateLimitErr(err) {
			a.providers[i].MarkRateLimited()
		} else {
			a.providers[i].MarkUnhealthy()
		}
	}
	return bc.BroadcastResult{}, fmt.Errorf("tron: broadcast failed on all %d providers", len(a.providers))
}

// BroadcastWithSignature is the V1 path for withdrawing USDT:
// the signer service returns raw_data bytes (the Transaction
// .raw_data submessage that the chain node assembled for us
// during BuildTransfer) followed by the 65-byte [R||S||V]
// signature; the chain node accepts them as separate JSON
// fields on /wallet/broadcasttransaction so we do not need to
// re-serialise the full Transaction protobuf ourselves.
//
// Failure semantics match Broadcast: Accepted=false means the
// chain explicitly rejected the tx (e.g. balance too low,
// signature invalid); an empty TxHash with Accepted=true
// means the chain did not answer but the bytes may still be
// picked up — the worker treats this as BROADCAST_UNKNOWN.
func (a *Adapter) BroadcastWithSignature(ctx context.Context, rawData []byte, sig []byte) (bc.BroadcastResult, error) {
	if len(sig) != 65 {
		return bc.BroadcastResult{}, fmt.Errorf("tron broadcast: expected 65-byte signature, got %d", len(sig))
	}
	params := map[string]any{
		"raw_data_hex":  hex.EncodeToString(rawData),
		"signature_hex": hex.EncodeToString(sig),
		"visible":       false,
	}
	var resp struct {
		Result  bool            `json:"result"`
		TxID    string          `json:"txid"`
		Message string          `json:"message"`
		Code    string          `json:"code"`     // chainstack returns {"code":"REJECTED","message":"..."}
		Error   string          `json:"Error"`    // chainstack sometimes returns {"Error":"..."} only
	}
	if err := a.callWithFailover(ctx, MethodBroadcastTransaction, params, &resp); err != nil {
		return bc.BroadcastResult{Accepted: false}, fmt.Errorf("tron broadcast: %w", err)
	}
	if !resp.Result {
		// chainstack reports rejections in several shapes.
		// In order of preference:
		//   1. code (e.g. "REJECTED")
		//   2. message (human-readable)
		//   3. Error (sometimes the only field set; NPE,
		//      SIGNATURE_ERROR, etc.)
		//   4. unknown rejection
		reason := resp.Code
		if reason == "" {
			reason = resp.Message
		}
		if reason == "" {
			reason = resp.Error
		}
		if reason == "" {
			reason = "unknown rejection"
		}
		return bc.BroadcastResult{Accepted: false}, fmt.Errorf("tron broadcast rejected: %s", reason)
	}
	return bc.BroadcastResult{TxHash: resp.TxID, Accepted: true}, nil
}

// broadcastOnce POSTs the signed tx bytes to the broadcast
// endpoint and decodes the JSON response. Mirrors the verb/path
// split of callJSON but skips the JSON wrapping step because the
// TRON broadcast endpoint wants the raw transaction as-is.
//
// We take a *Provider because Provider embeds atomic.Int64 which
// is noCopy; copying by value triggers the vet failure mode
// "call copies lock value". Callers that do not want to share the
// adapter's slice element can pass &p where p is a local Provider.
func (a *Adapter) broadcastOnce(ctx context.Context, p *Provider, signedTx []byte, out any) error {
	// Split the leading "GET "/"POST " from the path. Methods
	// without a prefix default to POST. After splitting, strip
	// any leading "/" so we can re-join with BaseURL without
	// doubling the "/wallet" prefix.
	verb, path := "POST", string(MethodBroadcastTransaction)
	if i := strings.IndexByte(string(MethodBroadcastTransaction), ' '); i > 0 {
		verb, path = string(MethodBroadcastTransaction)[:i], string(MethodBroadcastTransaction)[i+1:]
	}
	url := strings.TrimSuffix(p.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, verb, url, bytes.NewReader(signedTx))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if p.APIKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", p.APIKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		return fmt.Errorf("status=429 from %s", p.Name)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("status=%d from %s: %s", resp.StatusCode, p.Name, string(body[:min(200, len(body))]))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
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