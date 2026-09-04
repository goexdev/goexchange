// Package mmbot exposes the Go-side types and the gRPC client used by
// the public API server to talk to the proprietary per-pair
// market-making bot engine.
//
// The bot engine itself lives in the private goexchange-core
// repository and runs as a separate gRPC service (port 50052 by
// convention). This package mirrors that contract in Go and
// exposes a small Client interface plus a default gRPC-backed
// implementation.
//
// See proto/mmbot.proto for the canonical service definition.
// The .pb.go + _grpc.pb.go stubs in ./mmbotv1 are pre-generated
// and committed, the same way matching/matchingv1 stubs are
// handled.
package mmbot

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	mmbotv1 "github.com/goexdev/goexchange/internal/mmbot/mmbotv1"
)

// Status mirrors proto mmbot.v1.BotStatus. The proto enum carries
// an UNSPECIFIED=0 sentinel; in Go we use the zero value of Status
// to mean "unset / unknown" rather than introducing a constant.
type Status string

const (
	StatusUnspecified Status = ""
	StatusStopped     Status = "STOPPED"
	StatusSeeding     Status = "SEEDING"
	StatusReady       Status = "READY"
	StatusRunning     Status = "RUNNING"
	StatusStopping    Status = "STOPPING"
	StatusFailed      Status = "FAILED"
)

// AllStatuses is a convenience for the admin List handler: passing
// this as the status filter means "any status".
func AllStatuses() []Status {
	return []Status{
		StatusStopped, StatusSeeding, StatusReady,
		StatusRunning, StatusStopping, StatusFailed,
	}
}

// BotState is the Go-side mirror of mmbotv1.BotState. We keep it
// as a separate type so handlers can pass plain Go values
// without dragging proto types into the api package.
//
// Decimal strings (mid_price, base_balance, quote_balance,
// pnl_quote, returned_quote, returned_base) are kept as strings
// rather than parsed into shopspring/decimal: the admin
// dashboard formats them, and the admin endpoint should not
// silently truncate or round. Handlers that need decimal math
// (none in MVP) parse at the call site.
type BotState struct {
	BotID        string
	Pair         string
	Status       Status
	MidPrice     string
	SpreadBps    int32
	BaseBalance  string
	QuoteBalance string
	OpenOrderIDs []string
	PnlQuote     string

	CreatedAt time.Time
	StartedAt *time.Time
	StoppedAt *time.Time

	LastError string
}

// StopResult is returned from Stop. Returned{Quote,Base} are
// decimal strings in quote / base units, or empty if
// return_inventory was false.
type StopResult struct {
	Bot           BotState
	ReturnedQuote string
	ReturnedBase  string
}

// StartParams captures the admin-supplied fields for Start. All
// numeric fields are decimal strings — we never parse them in
// the public API, the engine is the source of truth.
type StartParams struct {
	Pair             string
	MidPrice         string
	QuoteSeed        string
	BaseSeed         string
	SpreadBps        int32 // 0 = use engine default (10)
	TreasuryWallet   string // empty = use engine default
	MinQuotePerSide  string // empty = use engine default
}

// protoStatusToGo converts a proto BotStatus enum into our Go
// Status type. UNSPECIFIED maps to StatusUnspecified (empty).
func protoStatusToGo(s mmbotv1.BotStatus) Status {
	switch s {
	case mmbotv1.BotStatus_BOT_STATUS_STOPPED:
		return StatusStopped
	case mmbotv1.BotStatus_BOT_STATUS_SEEDING:
		return StatusSeeding
	case mmbotv1.BotStatus_BOT_STATUS_READY:
		return StatusReady
	case mmbotv1.BotStatus_BOT_STATUS_RUNNING:
		return StatusRunning
	case mmbotv1.BotStatus_BOT_STATUS_STOPPING:
		return StatusStopping
	case mmbotv1.BotStatus_BOT_STATUS_FAILED:
		return StatusFailed
	default:
		return StatusUnspecified
	}
}

// protoToState converts a proto BotState into our Go BotState.
func protoToState(s *mmbotv1.BotState) BotState {
	if s == nil {
		return BotState{}
	}
	out := BotState{
		BotID:        s.GetBotId(),
		Pair:         s.GetPair(),
		Status:       protoStatusToGo(s.GetStatus()),
		MidPrice:     s.GetMidPrice(),
		SpreadBps:    s.GetSpreadBps(),
		BaseBalance:  s.GetBaseBalance(),
		QuoteBalance: s.GetQuoteBalance(),
		OpenOrderIDs: append([]string(nil), s.GetOpenOrderIds()...),
		PnlQuote:     s.GetPnlQuote(),
		LastError:    s.GetLastError(),
	}
	// Timestamps: created_at is required (proto3 default returns
	// zero-value Timestamp), started_at / stopped_at are optional.
	if t := s.GetCreatedAt(); t != nil {
		out.CreatedAt = t.AsTime()
	}
	if t := s.GetStartedAt(); t != nil {
		ts := t.AsTime()
		out.StartedAt = &ts
	}
	if t := s.GetStoppedAt(); t != nil {
		ts := t.AsTime()
		out.StoppedAt = &ts
	}
	return out
}

// protoToStopResult converts a StopResponse into our Go type.
func protoToStopResult(s *mmbotv1.StopResponse) StopResult {
	if s == nil {
		return StopResult{}
	}
	return StopResult{
		Bot:           protoToState(s.GetBot()),
		ReturnedQuote: s.GetReturnedQuote(),
		ReturnedBase:  s.GetReturnedBase(),
	}
}

// stateToProto is the inverse of protoToState; used by tests
// and by any caller that needs to construct a proto from a
// Go-side struct.
func stateToProto(s BotState) *mmbotv1.BotState {
	out := &mmbotv1.BotState{
		BotId:        s.BotID,
		Pair:         s.Pair,
		Status:       statusToProto(s.Status),
		MidPrice:     s.MidPrice,
		SpreadBps:    s.SpreadBps,
		BaseBalance:  s.BaseBalance,
		QuoteBalance: s.QuoteBalance,
		OpenOrderIds: s.OpenOrderIDs,
		PnlQuote:     s.PnlQuote,
		LastError:    s.LastError,
	}
	if !s.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(s.CreatedAt)
	}
	if s.StartedAt != nil {
		out.StartedAt = timestamppb.New(*s.StartedAt)
	}
	if s.StoppedAt != nil {
		out.StoppedAt = timestamppb.New(*s.StoppedAt)
	}
	return out
}

func statusToProto(s Status) mmbotv1.BotStatus {
	switch s {
	case StatusStopped:
		return mmbotv1.BotStatus_BOT_STATUS_STOPPED
	case StatusSeeding:
		return mmbotv1.BotStatus_BOT_STATUS_SEEDING
	case StatusReady:
		return mmbotv1.BotStatus_BOT_STATUS_READY
	case StatusRunning:
		return mmbotv1.BotStatus_BOT_STATUS_RUNNING
	case StatusStopping:
		return mmbotv1.BotStatus_BOT_STATUS_STOPPING
	case StatusFailed:
		return mmbotv1.BotStatus_BOT_STATUS_FAILED
	default:
		return mmbotv1.BotStatus_BOT_STATUS_UNSPECIFIED
	}
}
