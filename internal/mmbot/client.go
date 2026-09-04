// gRPC client that talks to the proprietary per-pair market-making
// bot engine running in goexchange-core. Generated stubs live
// in ./mmbotv1 (see proto/mmbot.proto).
//
// The bot engine listens on port 50052 by convention (matching
// uses 50051, signer uses 50061). When the bot binary is not
// running -- e.g. in dev without core, or during a partial
// deploy -- the dial fails and we surface the error to the
// admin handler. There is intentionally no noop fallback: an
// admin who hits /admin/mmbot/start must see a real error, not a
// silent "OK".
package mmbot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	mmbotv1 "github.com/goexdev/goexchange/internal/mmbot/mmbotv1"
)

// Client is the public interface used by admin handlers. It
// mirrors the four admin RPCs we expose. Tests can swap a fake
// implementation in; the gRPC-backed default lives in client.go.
type Client interface {
	Start(ctx context.Context, params StartParams) (BotState, error)
	Stop(ctx context.Context, botID string, returnInventory bool) (StopResult, error)
	Status(ctx context.Context, botID string) (BotState, error)
	List(ctx context.Context, pairFilter string, statusFilter Status) ([]BotState, error)
}

// NewGRPCClient dials addr (e.g. "mm-bot:50052") and returns a
// gRPC-backed Client. The connection uses insecure credentials
// because the bot lives on the internal Docker network. For
// external deployments, swap in TLS credentials.
//
// On dial failure we log and return an errorClient so callers
// can detect "bot engine down" without nil-deref panic. The
// admin handlers translate this into HTTP 503.
//
// Passing an empty addr is treated identically to a dial
// failure: the returned client always errors with a clear
// message. Callers should not nil-check the result.
//
// log may be nil; in that case log lines are silently dropped.
// This lets tests call NewGRPCClient without setting up a slog
// handler. Production callers should always pass a non-nil log.
func NewGRPCClient(addr string, log *slog.Logger) Client {
	log = safeLog(log)
	if addr == "" {
		log.Warn("mmbot: empty addr; bot engine unavailable")
		return &errorClient{err: errors.New("mmbot: bot engine not configured (empty addr)")}
	}
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		log.Error("mmbot: grpc dial failed at construct time",
			"addr", addr, "error", err)
		return &errorClient{err: fmt.Errorf("mmbot: dial %s: %w", addr, err)}
	}
	log.Info("mmbot: gRPC client connected", "addr", addr)
	return &grpcClientImpl{
		conn:   conn,
		log:    log,
		client: mmbotv1.NewMMBotServiceClient(conn),
	}
}

// grpcClientImpl is the real Client backed by gRPC.
type grpcClientImpl struct {
	conn   *grpc.ClientConn
	log    *slog.Logger
	client mmbotv1.MMBotServiceClient
}

func (g *grpcClientImpl) Start(ctx context.Context, params StartParams) (BotState, error) {
	req := &mmbotv1.StartRequest{
		Pair:            params.Pair,
		MidPrice:        params.MidPrice,
		QuoteSeed:       params.QuoteSeed,
		BaseSeed:        params.BaseSeed,
		SpreadBps:       params.SpreadBps,
		TreasuryWallet:  params.TreasuryWallet,
		MinQuotePerSide: params.MinQuotePerSide,
	}
	resp, err := g.client.Start(ctx, req)
	if err != nil {
		return BotState{}, fmt.Errorf("mmbot.Start: %w", err)
	}
	return protoToState(resp.GetBot()), nil
}

func (g *grpcClientImpl) Stop(ctx context.Context, botID string, returnInventory bool) (StopResult, error) {
	resp, err := g.client.Stop(ctx, &mmbotv1.StopRequest{
		BotId:           botID,
		ReturnInventory: returnInventory,
	})
	if err != nil {
		return StopResult{}, fmt.Errorf("mmbot.Stop: %w", err)
	}
	return protoToStopResult(resp), nil
}

func (g *grpcClientImpl) Status(ctx context.Context, botID string) (BotState, error) {
	resp, err := g.client.Status(ctx, &mmbotv1.StatusRequest{BotId: botID})
	if err != nil {
		return BotState{}, fmt.Errorf("mmbot.Status: %w", err)
	}
	return protoToState(resp.GetBot()), nil
}

func (g *grpcClientImpl) List(ctx context.Context, pairFilter string, statusFilter Status) ([]BotState, error) {
	req := &mmbotv1.ListRequest{PairFilter: pairFilter}
	if statusFilter != StatusUnspecified {
		req.StatusFilter = statusToProto(statusFilter)
	}
	resp, err := g.client.List(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("mmbot.List: %w", err)
	}
	out := make([]BotState, 0, len(resp.GetBots()))
	for _, b := range resp.GetBots() {
		out = append(out, protoToState(b))
	}
	return out, nil
}

// errorClient is the dial-failure fallback. Every method returns
// the same dial error so the admin handler can render an
// actionable 503 with the underlying cause. We do not silently
// succeed -- an admin who hits /admin/mmbot/start must see that
// the bot engine is not reachable.
type errorClient struct {
	err error
}

func (e *errorClient) Start(ctx context.Context, _ StartParams) (BotState, error) {
	return BotState{}, e.err
}
func (e *errorClient) Stop(ctx context.Context, _ string, _ bool) (StopResult, error) {
	return StopResult{}, e.err
}
func (e *errorClient) Status(ctx context.Context, _ string) (BotState, error) {
	return BotState{}, e.err
}
func (e *errorClient) List(ctx context.Context, _ string, _ Status) ([]BotState, error) {
	return nil, e.err
}

// safeLog returns the supplied logger or a discard logger if
// nil. Lets tests pass nil log without nil-deref.
func safeLog(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
