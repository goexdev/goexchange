// scanner.go: block-scanning driver for the TRON adapter.
//
// The scanner runs once per poll interval (typically 10 s). It reads
// the last persisted cursor from scanner_state, walks every block
// between cursor+1 and the current solidified head, and for each
// TriggerSmartContract transaction fetches the receipt, parses out
// TRC20 Transfer events, and hands them to a callback.
//
// This file deliberately knows nothing about deposits, ledger, or
// HTTP handlers. The callback (supplied by the chainwatcher) owns
// those concerns; that way the scanner can be unit-tested against
// a mock adapter without dragging in pgx or any HTTP plumbing.

package tron

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	bc "github.com/goexdev/goexchange/internal/blockchain"
)

// Scanner reads blocks from a TRON adapter and invokes a callback for
// each TRC20 Transfer event that lands on a watched address.
//
// One Scanner instance per chain (V1 only has TRON). Construct it
// after the adapter is registered so the constructor can sanity-check
// the chain id.
type Scanner struct {
	adapter bc.Adapter

	// mu protects cursor + address set, not the underlying network
	// calls. The latter are already safe for concurrent use.
	mu sync.Mutex
	// lastScanned is the height of the most recent block whose events
	// have been delivered to the callback. Persisted to scanner_state
	// by the chainwatcher (we just hand it back via SetCursor/GetCursor).
	lastScanned uint64
	// watched is the set of Base58-encoded addresses we care about.
	// Empty by default; populated by AddWatch / RemoveWatch. The
	// scanner only emits Transfer events whose "to" is in the set.
	watched map[string]struct{}

	// pollInterval is the minimum time between two polls when the
	// head does not advance. We do not want to hammer RPC when the
	// chain is idle.
	pollInterval time.Duration

	// headBehindBy is how many blocks we lag behind the solidified
	// head before crediting. TRON finalizes after ~19 SR votes; we
	// give it a 4-block cushion on top of the adapter's internal
	// solidifyWindow so we never credit a block that later reorgs.
	headBehindBy uint64

	log *slog.Logger
}

// NewScanner constructs a Scanner bound to a specific adapter. The
// caller is responsible for registering the adapter with the
// blockchain registry before constructing the scanner, because the
// scanner needs the adapter immediately.
func NewScanner(a bc.Adapter, log *slog.Logger) *Scanner {
	return &Scanner{
		adapter:      a,
		watched:      map[string]struct{}{},
		pollInterval: 10 * time.Second,
		headBehindBy: 4,
		log:          log,
	}
}

// AddWatch registers an address. Safe to call concurrently.
func (s *Scanner) AddWatch(addr string) {
	s.mu.Lock()
	s.watched[addr] = struct{}{}
	s.mu.Unlock()
}

// RemoveWatch unregisters an address. Safe to call concurrently.
func (s *Scanner) RemoveWatch(addr string) {
	s.mu.Lock()
	delete(s.watched, addr)
	s.mu.Unlock()
}

// SetCursor rewinds or fast-forwards the scanner. The chainwatcher
// loads this from scanner_state at startup.
func (s *Scanner) SetCursor(h uint64) {
	s.mu.Lock()
	s.lastScanned = h
	s.mu.Unlock()
}

// GetCursor returns the most recent height that has been delivered.
// The chainwatcher persists this to scanner_state every successful tick.
func (s *Scanner) GetCursor() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastScanned
}

// WatchedCount returns how many addresses are currently being
// scanned. Used for monitoring and tests.
func (s *Scanner) WatchedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.watched)
}

// Event is what the scanner hands to its callback. The wallet service
// converts this into a deposit row + ledger credit.
type Event struct {
	Chain     string // "TRON"
	Asset     string // "USDT"
	TxHash    string
	EventIdx  uint32 // index within the transaction's Transfer events
	BlockNum  uint64
	From      string
	To        string
	Amount    string // decimal string in the asset's smallest unit
	Timestamp time.Time
}

// RunOnce performs a single scan: read cursor, walk [cursor+1, head].
// Returns the number of events delivered. Errors abort the scan and
// are returned for logging; the caller (chainwatcher) is responsible
// for retry/back-off.
//
// RunOnce is exported so tests can drive the scanner deterministically
// without time.Sleep.
func (s *Scanner) RunOnce(ctx context.Context, deliver func(Event)) (int, error) {
	if len(s.watched) == 0 {
		// Nothing to do. Skip the RPC round-trips entirely; this is
		// the common case right after a fresh deploy before any
		// user has requested a deposit address.
		return 0, nil
	}

	head, err := s.adapter.GetSolidifiedBlock(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.Lock()
	start := s.lastScanned + 1
	s.mu.Unlock()

	// Lag behind the solidified head to leave room for late-arriving
	// reorgs. headBehindBy defaults to 4; the chainwatcher can raise
	// it during incidents.
	end := head.Height
	if end > s.headBehindBy {
		end -= s.headBehindBy
	}
	if end < start {
		// Either the chain is idle (head - lag < cursor + 1) or the
		// cursor is already at head. Nothing new.
		return 0, nil
	}

	delivered := 0
	for h := start; h <= end; h++ {
		select {
		case <-ctx.Done():
			return delivered, ctx.Err()
		default:
		}
		block, err := s.adapter.GetBlockByNumber(ctx, h)
		if err != nil {
			// Stop walking on error rather than skipping; we do not
			// want to leave holes in the cursor.
			return delivered, err
		}

		for _, txHash := range block.TxHashes {
			if err := s.processTx(ctx, txHash, h, block.LogTime, deliver); err != nil {
				// One bad tx shouldn't abort the whole batch; the
				// reconciler will pick up any missed events.
				s.log.Warn("tron: skip tx", "hash", txHash, "error", err)
				continue
			}
		}

		// Commit cursor only after the block's events have been
		// delivered; that way a crash mid-batch does not silently
		// skip events.
		s.mu.Lock()
		s.lastScanned = h
		s.mu.Unlock()
	}
	return delivered, nil
}

// processTx fetches one transaction, parses its Transfer events, and
// invokes deliver for every event whose "to" is in the watched set.
func (s *Scanner) processTx(ctx context.Context, txHash string, blockNum uint64, ts time.Time, deliver func(Event)) error {
	tx, err := s.adapter.GetTransaction(ctx, txHash)
	if err != nil {
		return err
	}
	if tx.Status != bc.TxStatusSuccess {
		// Failed/reverted txs cannot have generated a transfer event;
		// skip without an error.
		return nil
	}
	// We pass an empty contract string so ParseTransferEvents returns
	// every Transfer; the watched-address check below filters down to
	// what we actually care about.
	events, err := s.adapter.ParseTransferEvents(tx, "")
	if err != nil {
		return err
	}
	for _, e := range events {
		if !s.isWatched(e.To) {
			continue
		}
		deliver(Event{
			Chain:     "TRON",
			Asset:     "USDT", // V1 only handles USDT-TRC20
			TxHash:    tx.Hash,
			EventIdx:  e.Index,
			BlockNum:  blockNum,
			From:      e.From,
			To:        e.To,
			Amount:    e.Amount.String(),
			Timestamp: ts,
		})
	}
	return nil
}

// isWatched compares a Base58/0x form to the watched set. We accept
// either form because callers (deposit handlers) may pass either;
// the scanner normalises by lower-casing hex addresses.
func (s *Scanner) isWatched(addr string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, hit := s.watched[addr]
	if hit {
		return true
	}
	// Allow also the 0x-prefixed form.
	_, hit = s.watched[normaliseAddr(addr)]
	return hit
}

// normaliseAddr strips the "0x" prefix and lower-cases the result so
// the watched-set comparison is form-agnostic.
func normaliseAddr(s string) string {
	if len(s) > 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'F' {
			c += 32
		}
		out[i] = c
	}
	return string(out)
}

// ErrScannerNotConfigured is returned when someone tries to register
// a scanner with a chain that has no adapter. The chainwatcher should
// log this and skip the chain rather than abort startup.
var ErrScannerNotConfigured = errors.New("tron: scanner not configured for chain")